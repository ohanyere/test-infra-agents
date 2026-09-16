package qualplan

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sort"

	"github.com/evalplane/evalplane/backend/internal/agent"
	"github.com/evalplane/evalplane/backend/internal/auth"
	"github.com/evalplane/evalplane/backend/internal/coverage"
	"github.com/evalplane/evalplane/backend/internal/environment"
	"github.com/evalplane/evalplane/backend/internal/evaluationsuite"
	"github.com/evalplane/evalplane/backend/internal/identity"
	"github.com/evalplane/evalplane/backend/internal/scenario"
	"github.com/evalplane/evalplane/backend/internal/scenariocertification"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

type roleRepository interface {
	GetRole(context.Context, identity.DBTX, string, string) (string, error)
}

// Service orchestrates qualification planning: claims -> coverage contract
// -> built-in bridge materialization -> authoritative certification truth ->
// scheduler truth -> deterministic selection -> persisted plan -> immutable
// suite creation through the existing EvaluationSuite model.
type Service struct {
	pool         *pgxpool.Pool
	repo         *Repository
	roles        roleRepository
	agents       *agent.Service
	scenarios    *scenario.Service
	scenarioRepo *scenario.Repository
	coverageRepo *coverage.Repository
	envRepo      *environment.Repository
	env          *environment.Service
	cert         *scenariocertification.Service
	suites       *evaluationsuite.Service
}

// NewService wires the planner. All collaborators are required except in
// focused unit tests, which exercise the pure planner directly.
func NewService(pool *pgxpool.Pool, repo *Repository, roles roleRepository, agents *agent.Service, scenarios *scenario.Service, scenarioRepo *scenario.Repository, coverageRepo *coverage.Repository, envRepo *environment.Repository, env *environment.Service, cert *scenariocertification.Service, suites *evaluationsuite.Service) *Service {
	return &Service{pool: pool, repo: repo, roles: roles, agents: agents, scenarios: scenarios, scenarioRepo: scenarioRepo, coverageRepo: coverageRepo, envRepo: envRepo, env: env, cert: cert, suites: suites}
}

// BuildPlan generates and persists a QualificationPlan for one exact
// AgentVersion. capabilityIDs optionally narrows to a claimed subset; empty
// means all declared competency claims. The subset must belong to the
// version's claims, and unknown capabilities are rejected.
func (s *Service) BuildPlan(ctx context.Context, principal auth.Principal, workspaceID, agentID, versionID string, capabilityIDs []string) (Plan, error) {
	if err := validateUUIDs(workspaceID, agentID, versionID); err != nil {
		return Plan{}, err
	}
	if err := s.requireWrite(ctx, principal, workspaceID); err != nil {
		return Plan{}, err
	}
	version, err := s.agents.GetVersion(ctx, principal, workspaceID, agentID, versionID)
	if err != nil {
		return Plan{}, err
	}
	claims, err := s.agents.ListVersionClaims(ctx, principal, workspaceID, agentID, versionID)
	if err != nil {
		return Plan{}, err
	}
	claimedSet := map[string]bool{}
	for _, claim := range claims {
		claimedSet[claim.CapabilityID] = true
	}
	// An empty subset means all declared competency claims. Capabilities
	// outside the version's claims are rejected: qualification never covers
	// unclaimed capabilities.
	selected, err := selectCapabilities(claimedSet, capabilityIDs)
	if err != nil {
		return Plan{}, err
	}

	requirements := map[string]RequirementView{}
	for _, requirement := range coverage.ListRequirementsV2() {
		requirements[requirement.CapabilityID] = RequirementFor(requirement)
	}

	// Phase 1: deterministic bridge materialization (write tx, idempotent).
	materialized, err := s.materializeCatalog(ctx, principal, workspaceID)
	if err != nil {
		return Plan{}, err
	}
	// Phase 2: candidate evaluation against authoritative truth (read-only).
	candidates, err := s.evaluateCandidates(ctx, principal, workspaceID, version, materialized)
	if err != nil {
		return Plan{}, err
	}
	policyAvail, err := s.repo.PolicyAvailable(ctx, s.pool, workspaceID)
	if err != nil {
		return Plan{}, err
	}
	outcome := BuildPlan(PlannerInput{
		AgentVersionID:  versionID,
		DeclaredRuntime: append([]string(nil), version.Capabilities...),
		Claimed:         selected,
		Requirements:    requirements,
		Candidates:      candidates,
		PolicyAvail:     policyAvail,
	})
	return s.repo.InsertPlan(ctx, s.pool, workspaceID, agentID, versionID, outcome.CapabilityIDs, outcome, principal.UserID)
}

// GetPlan reads one persisted plan. Any workspace member may read.
func (s *Service) GetPlan(ctx context.Context, principal auth.Principal, workspaceID, planID string) (Plan, error) {
	if err := validateUUIDs(workspaceID, planID); err != nil {
		return Plan{}, err
	}
	if _, err := s.role(ctx, principal, workspaceID); err != nil {
		return Plan{}, err
	}
	return s.repo.GetPlan(ctx, s.pool, workspaceID, planID)
}

// BuildSuite creates an immutable Qualification Suite (existing
// EvaluationSuite/SuiteVersion) from a READY plan. PARTIAL/BLOCKED plans
// cannot create an authoritative suite. Creation never executes the suite
// and never qualifies the agent.
func (s *Service) BuildSuite(ctx context.Context, principal auth.Principal, workspaceID, planID string) (SuiteBuild, evaluationsuite.Suite, evaluationsuite.Version, error) {
	if err := validateUUIDs(workspaceID, planID); err != nil {
		return SuiteBuild{}, evaluationsuite.Suite{}, evaluationsuite.Version{}, err
	}
	if err := s.requireWrite(ctx, principal, workspaceID); err != nil {
		return SuiteBuild{}, evaluationsuite.Suite{}, evaluationsuite.Version{}, err
	}
	plan, err := s.repo.GetPlan(ctx, s.pool, workspaceID, planID)
	if err != nil {
		return SuiteBuild{}, evaluationsuite.Suite{}, evaluationsuite.Version{}, err
	}
	if plan.State != StateReady {
		return SuiteBuild{}, evaluationsuite.Suite{}, evaluationsuite.Version{}, validation(fmt.Sprintf("qualification plan is %s: only READY plans can create an authoritative suite", plan.State))
	}
	if plan.GeneratedSuiteVersionID != nil {
		return SuiteBuild{}, evaluationsuite.Suite{}, evaluationsuite.Version{}, identity.ErrConflict
	}
	entries := suiteEntries(plan)
	if len(entries) == 0 {
		return SuiteBuild{}, evaluationsuite.Suite{}, evaluationsuite.Version{}, validation("qualification plan selects no tasks")
	}
	agentRecord, err := s.agents.GetWorkspace(ctx, principal, workspaceID, plan.AgentID)
	if err != nil {
		return SuiteBuild{}, evaluationsuite.Suite{}, evaluationsuite.Version{}, err
	}
	version, err := s.agents.GetVersion(ctx, principal, workspaceID, plan.AgentID, plan.AgentVersionID)
	if err != nil {
		return SuiteBuild{}, evaluationsuite.Suite{}, evaluationsuite.Version{}, err
	}
	suite, err := s.suites.CreateSuite(ctx, principal, workspaceID, evaluationsuite.CreateSuiteInput{
		Name: fmt.Sprintf("Qualification suite for %s %s", agentRecord.Slug, version.VersionLabel),
	})
	if err != nil {
		return SuiteBuild{}, evaluationsuite.Suite{}, evaluationsuite.Version{}, err
	}
	metadata, _ := json.Marshal(map[string]any{
		"purpose":                              SuitePurpose,
		"planner_contract_version":             plan.PlannerContractVersion,
		"coverage_contract_version":            plan.CoverageContractVersion,
		"task_family_contract_version":         plan.TaskFamilyContractVersion,
		"capability_catalog_version":           plan.CapabilityCatalogVersion,
		"certification_contract_version":       plan.CertificationContract,
		"evaluator_version":                    plan.EvaluatorVersion,
		"environment_profile_contract_version": plan.EnvironmentProfileContract,
		"capability_ids":                       plan.CapabilityIDs,
		"qualification_plan_id":                plan.ID,
		"agent_version_id":                     plan.AgentVersionID,
	})
	suiteVersion, err := s.suites.CreateSuiteVersion(ctx, principal, workspaceID, suite.ID, evaluationsuite.CreateVersionInput{
		Description: fmt.Sprintf("Authoritative qualification suite for agent version %s (%d tasks, %s). Generated by %s; execution and qualification verdicts are separate steps.", version.VersionLabel, len(entries), plan.CoverageContractVersion, PlannerContractVersion),
		Metadata:    metadata,
		Scenarios:   entries,
	})
	if err != nil {
		return SuiteBuild{}, evaluationsuite.Suite{}, evaluationsuite.Version{}, err
	}
	if err := s.repo.RecordSuiteBuild(ctx, s.pool, plan.ID, suite.ID, suiteVersion.ID); err != nil {
		return SuiteBuild{}, evaluationsuite.Suite{}, evaluationsuite.Version{}, err
	}
	total := 0
	for _, entry := range entries {
		total += entry.TrialCount
	}
	return SuiteBuild{PlanID: plan.ID, SuiteID: suite.ID, SuiteVersionID: suiteVersion.ID, EntryCount: len(entries), TotalTrials: total}, suite, suiteVersion, nil
}

// materializedTask is one bridge-materialized exact ScenarioVersion.
type materializedTask struct {
	task        BuiltinTask
	scenarioID  string
	versionID   string
	contentHash string
}

// materializeCatalog ensures every runnable built-in task has a persisted
// immutable ScenarioVersion in this workspace. Idempotent: unchanged
// definitions reuse the existing version; changed definitions create a new
// version; existing versions are never modified.
func (s *Service) materializeCatalog(ctx context.Context, principal auth.Principal, workspaceID string) ([]materializedTask, error) {
	tasks := ListBuiltinTasks()
	out := make([]materializedTask, 0, len(tasks))
	for _, task := range tasks {
		materialized, err := s.materializeOne(ctx, principal, workspaceID, task)
		if err != nil {
			return nil, err
		}
		out = append(out, materialized)
	}
	return out, nil
}

func (s *Service) materializeOne(ctx context.Context, principal auth.Principal, workspaceID string, task BuiltinTask) (materializedTask, error) {
	hash := ContentHash(task)
	// Fail fast when a catalog definition cannot be represented as a
	// persisted ScenarioVersion under current validation.
	if _, err := scenario.ValidateCreate(ToCreateInput(task, hash)); err != nil {
		return materializedTask{}, err
	}
	scenarioID, err := s.repo.FindScenarioIDBySlug(ctx, s.pool, workspaceID, BridgeSlug(task.ID))
	if err != nil && !errors.Is(err, identity.ErrNotFound) {
		return materializedTask{}, err
	}
	if err == nil {
		versions, listErr := s.scenarioRepo.ListVersions(ctx, s.pool, workspaceID, scenarioID, identity.Pagination{Limit: 1})
		if listErr != nil {
			return materializedTask{}, listErr
		}
		if len(versions) > 0 && ContentHashOfVersion(versions[0]) == hash {
			if ensureErr := s.ensureVersionWiring(ctx, principal, workspaceID, scenarioID, versions[0].ID, task); ensureErr != nil {
				return materializedTask{}, ensureErr
			}
			return materializedTask{task: task, scenarioID: scenarioID, versionID: versions[0].ID, contentHash: hash}, nil
		}
		created, createErr := s.createMaterializedVersion(ctx, principal, workspaceID, scenarioID, task, hash)
		if createErr != nil {
			return materializedTask{}, createErr
		}
		return materializedTask{task: task, scenarioID: scenarioID, versionID: created, contentHash: hash}, nil
	}
	// No scenario yet: create it. On slug conflict (concurrent builder),
	// fall back to the existing row (idempotent retry).
	tx, err := s.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return materializedTask{}, err
	}
	committed := false
	defer func() {
		if !committed {
			_ = tx.Rollback(ctx)
		}
	}()
	created, err := s.scenarioRepo.Create(ctx, tx, workspaceID, ToCreateInput(task, hash))
	if err != nil {
		if errors.Is(err, identity.ErrConflict) {
			_ = tx.Rollback(ctx)
			committed = true
			return s.materializeOne(ctx, principal, workspaceID, task)
		}
		return materializedTask{}, err
	}
	// scenarioRepo.Create inserts the scenario row only; the initial
	// immutable version + criteria + family assignment + environment
	// requirements are added explicitly for full control.
	versionID, err := s.insertMaterializedVersion(ctx, tx, principal, workspaceID, created.ID, 1, task, hash)
	if err != nil {
		return materializedTask{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return materializedTask{}, err
	}
	committed = true
	return materializedTask{task: task, scenarioID: created.ID, versionID: versionID, contentHash: hash}, nil
}

// createMaterializedVersion appends a new immutable version when the catalog
// definition changed.
func (s *Service) createMaterializedVersion(ctx context.Context, principal auth.Principal, workspaceID, scenarioID string, task BuiltinTask, hash string) (string, error) {
	tx, err := s.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return "", err
	}
	committed := false
	defer func() {
		if !committed {
			_ = tx.Rollback(ctx)
		}
	}()
	number, status, err := s.scenarioRepo.LockAndNextVersion(ctx, tx, workspaceID, scenarioID)
	if err != nil {
		return "", err
	}
	if status == "archived" {
		return "", identity.ErrConflict
	}
	versionID, err := s.insertMaterializedVersion(ctx, tx, principal, workspaceID, scenarioID, number, task, hash)
	if err != nil {
		return "", err
	}
	if err := tx.Commit(ctx); err != nil {
		return "", err
	}
	committed = true
	return versionID, nil
}

// insertMaterializedVersion writes one version row + criteria + family
// assignment + environment requirements. It never sets certification: newly
// materialized versions use the normal Stage-28 certification path.
func (s *Service) insertMaterializedVersion(ctx context.Context, tx pgx.Tx, principal auth.Principal, workspaceID, scenarioID string, number int, task BuiltinTask, hash string) (string, error) {
	if number == 0 {
		number = 1
	}
	created, err := s.scenarioRepo.CreateVersion(ctx, tx, scenarioID, number, ToVersionInput(task, hash))
	if err != nil {
		return "", err
	}
	criteria := make([]scenario.CriterionInput, 0, len(task.Criteria))
	for index, criterion := range task.Criteria {
		criteria = append(criteria, scenario.CriterionInput{
			Key: criterion.Key, Name: criterion.Name, MetricKey: criterion.MetricKey,
			Operator: criterion.Operator, TargetValue: criterion.Target,
			Unit: criterion.Unit, SortOrder: index,
		})
	}
	if _, err := s.scenarioRepo.CreateCriteria(ctx, tx, created.ID, criteria); err != nil {
		return "", err
	}
	if _, err := s.coverageRepo.UpsertAssignment(ctx, tx, created.ID, task.Family, task.CaseType, principal.UserID); err != nil {
		return "", err
	}
	if _, err := s.envRepo.UpsertRequirements(ctx, tx, workspaceID, created.ID, task.Requirements); err != nil {
		return "", err
	}
	return created.ID, nil
}

// ensureVersionWiring repairs missing family/requirements rows on a reused
// version without touching the immutable version content.
func (s *Service) ensureVersionWiring(ctx context.Context, principal auth.Principal, workspaceID, scenarioID, versionID string, task BuiltinTask) error {
	existing, err := s.coverageRepo.GetAssignment(ctx, s.pool, versionID)
	if err != nil && !errors.Is(err, identity.ErrNotFound) {
		return err
	}
	if errors.Is(err, identity.ErrNotFound) || existing.FamilyID != task.Family || existing.CaseType != task.CaseType {
		if _, err := s.coverageRepo.UpsertAssignment(ctx, s.pool, versionID, task.Family, task.CaseType, principal.UserID); err != nil {
			return err
		}
	}
	if _, err := s.envRepo.UpsertRequirements(ctx, s.pool, workspaceID, versionID, task.Requirements); err != nil {
		return err
	}
	return nil
}

// evaluateCandidates turns materialized tasks (plus workspace-authored
// versions carrying explicit family assignments) into planner candidates
// using Stage-28 certification truth and Stage-29 scheduler truth.
func (s *Service) evaluateCandidates(ctx context.Context, principal auth.Principal, workspaceID string, version agent.Version, materialized []materializedTask) ([]Candidate, error) {
	declared := map[string]bool{}
	for _, id := range version.Capabilities {
		declared[id] = true
	}
	compatible := func(requires []string) bool {
		for _, need := range requires {
			if !declared[need] {
				return false
			}
		}
		return true
	}
	out := make([]Candidate, 0, len(materialized)+16)
	for _, item := range materialized {
		certified, stale, certID, err := s.certificationTruth(ctx, principal, workspaceID, item.scenarioID, item.versionID)
		if err != nil {
			return nil, err
		}
		executable, profile, reason, err := s.environmentTruth(ctx, principal, workspaceID, item.scenarioID, item.versionID)
		if err != nil {
			return nil, err
		}
		out = append(out, Candidate{
			BuiltinTaskID:        item.task.ID,
			ScenarioID:           item.scenarioID,
			ScenarioVersionID:    item.versionID,
			FamilyID:             item.task.Family,
			CaseType:             item.task.CaseType,
			Capabilities:         append([]string(nil), item.task.Capabilities...),
			RuntimeContract:      item.task.Requirements.RuntimeContract,
			RuntimeAllowed:       environment.IsAllowedRuntimeContract(item.task.Requirements.RuntimeContract),
			Requires:             append([]string(nil), item.task.AllowedActions...),
			AgentCompatibleValue: compatible(item.task.AllowedActions),
			Certified:            certified,
			CertStale:            stale,
			CertificationID:      certID,
			EnvExecutable:        executable,
			EnvProfile:           profile,
			EnvReason:            reason,
			EnvComponents:        append([]string(nil), item.task.Requirements.RequiredComponents...),
			EnvIsolation:         item.task.Requirements.MinimumIsolation,
			ContentHash:          item.contentHash,
		})
	}
	authored, err := s.authoredCandidates(ctx, principal, workspaceID, version, materialized, compatible)
	if err != nil {
		return nil, err
	}
	return append(out, authored...), nil
}

// certificationTruth uses the actual Stage-28 eligibility records. Catalog
// "focused certification validation passed" notes never count. The latest
// certification ID is returned for reproducibility pins ("" when no attempt
// exists).
func (s *Service) certificationTruth(ctx context.Context, principal auth.Principal, workspaceID, scenarioID, versionID string) (bool, bool, string, error) {
	status, err := s.cert.GetStatus(ctx, principal, workspaceID, scenarioID, versionID)
	if err != nil {
		return false, false, "", err
	}
	var certID string
	if status.Latest != nil {
		certID = status.Latest.ID
	}
	switch status.EffectiveState {
	case scenariocertification.StateCertified:
		return true, false, certID, nil
	case scenariocertification.StateStale:
		return false, true, certID, nil
	default:
		return false, false, certID, nil
	}
}

// environmentTruth resolves placement through the Stage-29 scheduler.
// Planned profiles and missing providers stay unavailable; fidelity is
// never downgraded (Kind is not chosen for dedicated-only tasks).
func (s *Service) environmentTruth(ctx context.Context, principal auth.Principal, workspaceID, scenarioID, versionID string) (bool, string, string, error) {
	decision, _, err := s.env.Resolve(ctx, principal, workspaceID, environment.ResolveRequest{
		ScenarioID: scenarioID, ScenarioVersionID: versionID, Purpose: "trial",
	})
	if err != nil {
		return false, "", "", err
	}
	if decision.Profile != nil && decision.Provider != nil {
		return true, decision.Profile.ID, decision.ReasonCode, nil
	}
	return false, "", decision.ReasonCode, nil
}

// authoredCandidates adds workspace-authored ScenarioVersions carrying
// explicit family assignments. They go through identical certification,
// environment, and compatibility gates; structural validity alone never
// counts as authoritative eligibility.
func (s *Service) authoredCandidates(ctx context.Context, principal auth.Principal, workspaceID string, version agent.Version, materialized []materializedTask, compatible func([]string) bool) ([]Candidate, error) {
	materializedIDs := map[string]bool{}
	for _, item := range materialized {
		materializedIDs[item.versionID] = true
	}
	scenarios, err := s.scenarios.ListWorkspace(ctx, principal, workspaceID, scenario.Filters{Pagination: identity.Pagination{Limit: 100}})
	if err != nil {
		return nil, err
	}
	out := []Candidate{}
	for _, record := range scenarios {
		versions, err := s.scenarios.ListVersions(ctx, principal, workspaceID, record.ID, identity.Pagination{Limit: 25})
		if err != nil {
			return nil, err
		}
		versionIDs := make([]string, 0, len(versions))
		byID := map[string]scenario.Version{}
		for _, item := range versions {
			if materializedIDs[item.ID] {
				continue
			}
			versionIDs = append(versionIDs, item.ID)
			byID[item.ID] = item
		}
		assignments, err := s.coverageRepo.ListAssignments(ctx, s.pool, versionIDs)
		if err != nil {
			return nil, err
		}
		for _, versionID := range versionIDs {
			assignment, ok := assignments[versionID]
			if !ok || assignment.FamilyID == "" {
				continue
			}
			item := byID[versionID]
			certified, stale, certID, err := s.certificationTruth(ctx, principal, workspaceID, record.ID, versionID)
			if err != nil {
				return nil, err
			}
			executable, profile, reason, err := s.environmentTruth(ctx, principal, workspaceID, record.ID, versionID)
			if err != nil {
				return nil, err
			}
			runtimeContract := environment.DefaultRuntimeContract
			var envComponents []string
			var envIsolation string
			stored, storeErr := s.storedRequirements(ctx, versionID)
			if storeErr == nil {
				if stored.RuntimeContract != "" {
					runtimeContract = stored.RuntimeContract
				}
				envComponents = append([]string(nil), stored.RequiredComponents...)
				envIsolation = stored.MinimumIsolation
			}
			out = append(out, Candidate{
				BuiltinTaskID:        "authored:" + record.Slug + ":v" + itoa(item.Version),
				ScenarioID:           record.ID,
				ScenarioVersionID:    versionID,
				FamilyID:             assignment.FamilyID,
				CaseType:             assignment.CaseType,
				Capabilities:         capabilitiesForFamily(assignment.FamilyID),
				RuntimeContract:      runtimeContract,
				RuntimeAllowed:       environment.IsAllowedRuntimeContract(runtimeContract),
				Requires:             append([]string(nil), item.AllowedActions...),
				AgentCompatibleValue: compatible(item.AllowedActions),
				Certified:            certified,
				CertStale:            stale,
				CertificationID:      certID,
				EnvExecutable:        executable,
				EnvProfile:           profile,
				EnvReason:            reason,
				EnvComponents:        envComponents,
				EnvIsolation:         envIsolation,
				ContentHash:          "authored-" + versionID,
			})
		}
	}
	return out, nil
}

func (s *Service) storedRequirements(ctx context.Context, versionID string) (environment.Requirements, error) {
	row, err := s.envRepo.GetRequirements(ctx, s.pool, versionID)
	if err != nil {
		return environment.Requirements{}, err
	}
	return row.Requirements(), nil
}

// capabilitiesForFamily maps an authored version's explicit family to the
// competency capabilities exercised by that family, using the same
// task-capability links the built-in catalog declares. Families with no
// linked capability contribute no coverage.
func capabilitiesForFamily(familyID string) []string {
	linked := map[string]bool{}
	for _, task := range ListBuiltinTasks() {
		if task.Family != familyID {
			continue
		}
		for _, id := range task.Capabilities {
			linked[id] = true
		}
	}
	out := []string{}
	for id := range linked {
		out = append(out, id)
	}
	sort.Strings(out)
	return out
}

// suiteEntries dedupes selections across capabilities (one task may serve
// several) keeping the maximum trial count, in stable global order.
func suiteEntries(plan Plan) []evaluationsuite.ScenarioEntry {
	byVersion := map[string]evaluationsuite.ScenarioEntry{}
	for _, capability := range plan.Capabilities {
		for _, selected := range capability.SelectedTasks {
			entry, ok := byVersion[selected.ScenarioVersionID]
			if !ok || entry.TrialCount < selected.TrialCount {
				byVersion[selected.ScenarioVersionID] = evaluationsuite.ScenarioEntry{
					ScenarioVersionID: selected.ScenarioVersionID,
					TrialCount:        selected.TrialCount,
				}
			}
		}
	}
	entries := make([]evaluationsuite.ScenarioEntry, 0, len(byVersion))
	for _, entry := range byVersion {
		entries = append(entries, entry)
	}
	sort.Slice(entries, func(i, j int) bool {
		return entries[i].ScenarioVersionID < entries[j].ScenarioVersionID
	})
	for index := range entries {
		entries[index].SortOrder = index
	}
	return entries
}

// selectCapabilities resolves the caller's optional subset against the
// AgentVersion's declared claims. Pure function so subset rules stay
// unit-testable without a database.
func selectCapabilities(claimedSet map[string]bool, subset []string) ([]string, error) {
	selected := append([]string(nil), subset...)
	if len(selected) == 0 {
		for id := range claimedSet {
			selected = append(selected, id)
		}
	}
	selected = sortedUniqueCopy(selected)
	for _, id := range selected {
		if !claimedSet[id] {
			return nil, validation(fmt.Sprintf("capability %q is not claimed by this agent version: qualification plans only cover declared claims", id))
		}
	}
	return selected, nil
}

func (s *Service) role(ctx context.Context, principal auth.Principal, workspaceID string) (string, error) {
	role, err := s.roles.GetRole(ctx, s.pool, workspaceID, principal.UserID)
	if errors.Is(err, identity.ErrNotFound) {
		return "", auth.ErrForbidden
	}
	return role, err
}

func (s *Service) requireWrite(ctx context.Context, principal auth.Principal, workspaceID string) error {
	role, err := s.role(ctx, principal, workspaceID)
	if err != nil {
		return err
	}
	switch role {
	case "owner", "admin", "member":
		return nil
	default:
		return auth.ErrForbidden
	}
}

func validateUUIDs(values ...string) error {
	for _, value := range values {
		if identity.ValidateUUID(value) != nil {
			return validation("id must be a valid UUID")
		}
	}
	return nil
}

func validation(message string) error { return fmt.Errorf("%w: %s", identity.ErrValidation, message) }

func sortedUniqueCopy(values []string) []string {
	seen := map[string]bool{}
	out := []string{}
	for _, value := range values {
		if value == "" || seen[value] {
			continue
		}
		seen[value] = true
		out = append(out, value)
	}
	sort.Strings(out)
	return out
}

func itoa(value int) string { return fmt.Sprintf("%d", value) }
