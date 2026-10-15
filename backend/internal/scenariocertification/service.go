package scenariocertification

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/evalplane/evalplane/backend/internal/auth"
	"github.com/evalplane/evalplane/backend/internal/coverage"
	"github.com/evalplane/evalplane/backend/internal/environment"
	"github.com/evalplane/evalplane/backend/internal/evaluator"
	"github.com/evalplane/evalplane/backend/internal/identity"
	"github.com/evalplane/evalplane/backend/internal/scenario"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

type roleRepository interface {
	GetRole(context.Context, identity.DBTX, string, string) (string, error)
}

// Service owns deterministic scenario certification. No LLM anywhere.
// Reference remediation is evaluator-only: PutReference is write-scoped and no
// reader exposes reference contents through agent-facing surfaces.
//
// Certification no longer trusts arbitrary environment profile/runtime
// strings: the server resolves ScenarioVersion requirements through the
// environment scheduler into a server-owned profile/provider, and pins all
// five contracts (component, profile, provider, isolation, runtime) on every
// new attempt. Historical rows remain immutable.
type Service struct {
	pool      *pgxpool.Pool
	repo      *Repository
	roles     roleRepository
	scenarios *scenario.Repository
	env       *environment.Service
}

// NewService creates a certification Service.
func NewService(pool *pgxpool.Pool, repo *Repository, roles roleRepository, scenarios *scenario.Repository) *Service {
	return &Service{pool: pool, repo: repo, roles: roles, scenarios: scenarios}
}

// SetEnvironment attaches the environment scheduler. Optional: when nil,
// certification resolves through the deterministic static registry with
// stored requirements (or the legacy compatibility default).
func (s *Service) SetEnvironment(env *environment.Service) { s.env = env }

// PutReference associates evaluator-only reference remediation with one
// ScenarioVersion. Write-capable roles only. Never exposed to agents.
func (s *Service) PutReference(ctx context.Context, principal auth.Principal, workspaceID, scenarioID, versionID string, in ReferenceRemediationInput) (ReferenceRemediation, error) {
	if err := validateIDs(workspaceID, scenarioID, versionID); err != nil {
		return ReferenceRemediation{}, err
	}
	if err := s.requireWrite(ctx, principal, workspaceID); err != nil {
		return ReferenceRemediation{}, err
	}
	in, err := ValidateReferenceInput(in)
	if err != nil {
		return ReferenceRemediation{}, err
	}
	owner, err := s.scenarioWorkspace(ctx, scenarioID, versionID)
	if err != nil {
		return ReferenceRemediation{}, err
	}
	if owner != workspaceID {
		return ReferenceRemediation{}, auth.ErrForbidden
	}
	hash := HashReferenceContent(in)
	return s.repo.UpsertReference(ctx, s.pool, workspaceID, versionID, in, hash, principal.UserID)
}

// Certify runs one synchronous certification attempt and persists it.
// Safe to retry: concurrent attempts serialize on the scenario-version row
// lock, and idempotency keys return the existing attempt. Failed attempts may
// be retried; history remains inspectable. Users cannot set certified
// manually: state is server-derived from the deterministic gates.
func (s *Service) Certify(ctx context.Context, principal auth.Principal, workspaceID, scenarioID, versionID string, req CertifyRequest) (Certification, error) {
	if err := validateIDs(workspaceID, scenarioID, versionID); err != nil {
		return Certification{}, err
	}
	if err := s.requireWrite(ctx, principal, workspaceID); err != nil {
		return Certification{}, err
	}
	runtimePreference := strings.TrimSpace(req.RuntimeContractVersion)
	if len(runtimePreference) > 200 {
		return Certification{}, validation("runtime contract version must be at most 200 characters")
	}
	if runtimePreference != "" && !environment.IsAllowedRuntimeContract(runtimePreference) {
		return Certification{}, validation("unknown runtime contract " + strconv.Quote(runtimePreference) + ": runtime contracts are server-owned")
	}
	profilePreference := strings.TrimSpace(req.EnvironmentProfile)
	if len(profilePreference) > 200 {
		return Certification{}, validation("environment profile must be at most 200 characters")
	}
	// Server-owned resolution: requirements → scheduler → profile/provider.
	// A caller preference must be a known active profile satisfying task
	// requirements; arbitrary caller strings can never determine
	// authoritative certification truth.
	decision, requirements, err := s.resolveEnvironment(ctx, versionID, profilePreference)
	if err != nil {
		return Certification{}, err
	}
	if decision.Profile == nil || decision.Provider == nil {
		return Certification{}, validation("no eligible environment for task requirements: refusing fidelity downgrade")
	}
	runtimeContract := decision.RuntimeContract
	if runtimeContract == "" {
		runtimeContract = requirements.RuntimeContract
	}
	if runtimePreference != "" && runtimePreference != runtimeContract {
		return Certification{}, validation("runtime contract does not match the server-selected environment")
	}
	if runtimePreference != "" {
		runtimeContract = runtimePreference
	}
	environmentProfile := decision.Profile.ID
	var idempotency *string
	if req.IdempotencyKey != nil && strings.TrimSpace(*req.IdempotencyKey) != "" {
		key := strings.TrimSpace(*req.IdempotencyKey)
		if len(key) > 200 {
			return Certification{}, validation("idempotency key must be at most 200 characters")
		}
		idempotency = &key
	}

	tx, err := s.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return Certification{}, err
	}
	committed := false
	defer func() {
		if !committed {
			_ = tx.Rollback(ctx)
		}
	}()
	if err := s.repo.LockScenarioVersion(ctx, tx, scenarioID, versionID); err != nil {
		return Certification{}, err
	}
	owner, err := s.scenarioWorkspaceTx(ctx, tx, scenarioID, versionID)
	if err != nil {
		return Certification{}, err
	}
	if owner != workspaceID {
		return Certification{}, auth.ErrForbidden
	}
	if idempotency != nil {
		if existing, err := s.repo.GetByIdempotency(ctx, tx, versionID, *idempotency); err == nil {
			committed = true
			_ = tx.Rollback(ctx)
			// Return the existing attempt without committing a new row.
			// Rollback of a read-only tx is safe; re-read outside tx.
			return s.repo.GetByIdempotency(ctx, s.pool, versionID, *idempotency)
		} else if !errors.Is(err, identity.ErrNotFound) {
			return Certification{}, err
		} else {
			_ = existing
		}
	}

	version, err := s.scenarios.GetVersion(ctx, tx, workspaceID, scenarioID, versionID)
	if err != nil {
		return Certification{}, err
	}
	// Authoritative certification uses the evaluator-only reference remediation
	// already associated with the exact ScenarioVersion via PUT
	// .../reference-remediation. Inline reference_remediation through the
	// certification trigger is not accepted.
	reference, err := s.repo.GetReference(ctx, tx, versionID)
	if err != nil {
		if errors.Is(err, identity.ErrNotFound) {
			return Certification{}, validation("reference remediation is required before certification: associate evaluator-only reference remediation first")
		}
		return Certification{}, err
	}
	assignment, assignErr := s.familyAssignment(ctx, tx, versionID)
	taskFamilyID, caseType := "", ""
	if assignErr == nil {
		taskFamilyID, caseType = assignment.FamilyID, assignment.CaseType
	} else if !errors.Is(assignErr, identity.ErrNotFound) {
		return Certification{}, assignErr
	}

	started := time.Now().UTC()
	fixture := s.buildFixture(version, reference, taskFamilyID, caseType)
	outcome := Certify(fixture)
	completed := time.Now().UTC()
	variantIDs := make([]string, 0, len(fixture.Variants))
	variantSeeds := make([]int64, 0, len(fixture.Variants))
	for _, v := range fixture.Variants {
		variantIDs = append(variantIDs, v.VariantID)
		variantSeeds = append(variantSeeds, v.Seed)
	}
	record := Certification{
		WorkspaceID:                         workspaceID,
		ScenarioID:                          scenarioID,
		ScenarioVersionID:                   versionID,
		State:                               outcome.State,
		CertificationContractVersion:        CertificationContractVersion,
		ReferenceRemediationContractVersion: ReferenceRemediationContractVersion,
		ReferenceContentHash:                reference.ContentHash,
		EvaluatorVersion:                    EvaluatorVersion,
		RuntimeContractVersion:              runtimeContract,
		EnvironmentProfile:                  environmentProfile,
		ProviderID:                          decision.Provider.ID,
		ProviderContractVersion:             decision.Provider.ContractVersion,
		IsolationTier:                       decision.Profile.Isolation,
		EnvironmentProfileContractVersion:   decision.Profile.ContractVersion,
		EnvironmentComponentContractVersion: environment.ComponentCatalogVersion,
		CriteriaVersion:                     CriteriaVersion(version.Criteria),
		HiddenVariantContractVersion:        HiddenVariantContractVersion,
		TaskFamilyID:                        taskFamilyID,
		CaseType:                            caseType,
		GateResults:                         outcome.Gates,
		FailureCodes:                        outcome.FailureCodes,
		VariantIDs:                          variantIDs,
		VariantSeeds:                        variantSeeds,
		IdempotencyKey:                      idempotency,
		StartedAt:                           started,
		CompletedAt:                         &completed,
	}
	persisted, err := s.repo.InsertCertification(ctx, tx, record)
	if err != nil {
		if errors.Is(err, identity.ErrConflict) && idempotency != nil {
			// Lost race with a concurrent identical attempt: return theirs.
			if err := tx.Rollback(ctx); err != nil {
				return Certification{}, err
			}
			committed = true
			return s.repo.GetByIdempotency(ctx, s.pool, versionID, *idempotency)
		}
		return Certification{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return Certification{}, err
	}
	committed = true
	persisted.CreatedByHidden()
	return persisted, nil
}

// GetStatus returns the derived certification status. Any workspace member
// (including viewer) may read, consistent with scenario visibility.
func (s *Service) GetStatus(ctx context.Context, principal auth.Principal, workspaceID, scenarioID, versionID string) (CertificationStatus, error) {
	if err := validateIDs(workspaceID, scenarioID, versionID); err != nil {
		return CertificationStatus{}, err
	}
	if _, err := s.role(ctx, principal, workspaceID); err != nil {
		return CertificationStatus{}, err
	}
	owner, err := s.scenarioWorkspace(ctx, scenarioID, versionID)
	if err != nil {
		return CertificationStatus{}, err
	}
	if owner != workspaceID {
		return CertificationStatus{}, auth.ErrForbidden
	}
	version, err := s.scenarios.GetVersion(ctx, s.pool, workspaceID, scenarioID, versionID)
	if err != nil {
		return CertificationStatus{}, err
	}
	history, err := s.repo.History(ctx, s.pool, versionID, 20)
	if err != nil {
		return CertificationStatus{}, err
	}
	if len(history) == 0 {
		return CertificationStatus{
			EffectiveState:        StateUncertified,
			History:               []Certification{},
			CertificationContract: CertificationContractVersion,
			EvaluatorVersion:      EvaluatorVersion,
		}, nil
	}
	latest := history[0]
	reference, refErr := s.repo.GetReference(ctx, s.pool, versionID)
	var currentHash string
	if refErr == nil {
		currentHash = reference.ContentHash
	}
	requirements := s.storedRequirements(ctx, versionID)
	staleReasons := staleReasonsFor(latest, CriteriaVersion(version.Criteria), currentHash, refErr, requirements)
	effective := latest.State
	stale := len(staleReasons) > 0 && latest.State == StateCertified
	if stale {
		effective = StateStale
	}
	return CertificationStatus{
		EffectiveState:        effective,
		Eligible:              effective == StateCertified,
		Stale:                 stale,
		StaleReasons:          staleReasons,
		Latest:                &latest,
		History:               history,
		CertificationContract: CertificationContractVersion,
		EvaluatorVersion:      EvaluatorVersion,
	}, nil
}

// EligibleAuthoritativeEvidence reports whether a ScenarioVersion may
// contribute authoritative qualification evidence: certified and fresh.
// Legacy versions without certification remain runnable but are NOT
// authoritative. Coverage (Stage 27) is untouched: coverage asks whether a
// capability was validly exercised, certification asks whether the task is a
// trustworthy instrument.
func (s *Service) EligibleAuthoritativeEvidence(ctx context.Context, principal auth.Principal, workspaceID, scenarioID, versionID string) (bool, error) {
	status, err := s.GetStatus(ctx, principal, workspaceID, scenarioID, versionID)
	if err != nil {
		return false, err
	}
	return status.Eligible, nil
}

func staleReasonsFor(latest Certification, currentCriteria, currentReferenceHash string, refErr error, requirements environment.Requirements) []string {
	reasons := []string{}
	if latest.CertificationContractVersion != CertificationContractVersion {
		reasons = append(reasons, "certification contract changed")
	}
	if latest.EvaluatorVersion != EvaluatorVersion {
		reasons = append(reasons, "evaluator version changed")
	}
	if latest.ReferenceRemediationContractVersion != ReferenceRemediationContractVersion {
		reasons = append(reasons, "reference remediation contract changed")
	}
	if refErr != nil || (currentReferenceHash != "" && latest.ReferenceContentHash != currentReferenceHash) {
		reasons = append(reasons, "reference remediation changed")
	}
	if latest.CriteriaVersion != currentCriteria {
		reasons = append(reasons, "criteria definition changed")
	}
	if latest.RuntimeContractVersion != DefaultRuntimeContractVersion {
		// Pinned runtime no longer matches the current authoritative context.
		reasons = append(reasons, "runtime contract changed")
	}
	if latest.EnvironmentProfileContractVersion != "" && latest.EnvironmentProfileContractVersion != environment.ProfileCatalogVersion {
		reasons = append(reasons, "environment profile contract changed")
	}
	if latest.EnvironmentComponentContractVersion != "" && latest.EnvironmentComponentContractVersion != environment.ComponentCatalogVersion {
		reasons = append(reasons, "environment component contract changed")
	}
	if latest.ProviderContractVersion != "" && latest.ProviderContractVersion != environment.ProviderContractVersion {
		reasons = append(reasons, "provider contract changed")
	}
	// Isolation context: certification against kind-dev + namespace does not
	// imply certification for a stronger context when the task now requires
	// it, and a pinned weaker isolation never satisfies a stronger minimum.
	if len(requirements.RequiredComponents) > 0 && latest.IsolationTier != "" {
		if !environment.SatisfiesIsolation(latest.IsolationTier, requirements.MinimumIsolation) {
			reasons = append(reasons, "environment isolation context changed")
		}
		if latest.RuntimeContractVersion != requirements.RuntimeContract && requirements.RuntimeContract != "" {
			reasons = append(reasons, "environment runtime context changed")
		}
	}
	return reasons
}

// resolveEnvironment runs server-owned placement for certification: stored
// requirements (or the legacy default) through the deterministic scheduler.
// Caller preferences are validated strictly; failures refuse downgrade.
func (s *Service) resolveEnvironment(ctx context.Context, versionID, preference string) (environment.SchedulerDecision, environment.Requirements, error) {
	if s.env != nil {
		decision, requirements, err := s.env.ResolveForCertification(ctx, versionID, preference)
		if err != nil {
			return environment.SchedulerDecision{}, environment.Requirements{}, err
		}
		if decision.Profile == nil || decision.Provider == nil {
			return decision, requirements, validation("no eligible environment for task requirements: refusing fidelity downgrade")
		}
		return decision, requirements, nil
	}
	requirements := s.storedRequirements(ctx, versionID)
	if preference != "" {
		profile, ok := environment.GetProfile(preference)
		if !ok {
			return environment.SchedulerDecision{}, environment.Requirements{}, validation("unknown environment profile " + strconv.Quote(preference) + ": select a server-owned profile")
		}
		if !profile.Executable() {
			return environment.SchedulerDecision{}, environment.Requirements{}, validation("environment profile " + strconv.Quote(preference) + " is not available for execution")
		}
		for _, need := range requirements.RequiredComponents {
			if !profile.HasComponent(need) {
				return environment.SchedulerDecision{}, environment.Requirements{}, validation("environment profile " + strconv.Quote(preference) + " does not satisfy task requirements")
			}
		}
		if !environment.SatisfiesIsolation(profile.Isolation, requirements.MinimumIsolation) {
			return environment.SchedulerDecision{}, environment.Requirements{}, validation("environment profile " + strconv.Quote(preference) + " does not satisfy minimum isolation")
		}
	}
	decision := environment.SelectForRequirements(ctx, requirements, "certification", preference, nil)
	if decision.Profile == nil || decision.Provider == nil {
		return decision, requirements, validation("no eligible environment for task requirements: refusing fidelity downgrade")
	}
	return decision, requirements, nil
}

// storedRequirements loads server-reviewed requirements for one
// ScenarioVersion, falling back to the legacy compatibility default.
func (s *Service) storedRequirements(ctx context.Context, versionID string) environment.Requirements {
	fallback := environment.LegacyDefaultRequirements()
	if s.pool == nil {
		return fallback
	}
	var components []string
	var minimum, capsJSON string
	var caps []string
	var runtime, contract string
	err := s.pool.QueryRow(ctx, `SELECT required_components, minimum_isolation_tier,
		COALESCE(array_to_json(required_profile_capabilities)::text, '[]'),
		runtime_contract_version, contract_version
		FROM scenario_version_environment_requirements WHERE scenario_version_id=$1`, versionID).
		Scan(&components, &minimum, &capsJSON, &runtime, &contract)
	if err != nil {
		return fallback
	}
	_ = json.Unmarshal([]byte(capsJSON), &caps)
	normalized, err := environment.ValidateRequirements(environment.Requirements{
		RequiredComponents: components, MinimumIsolation: minimum,
		RequiredCapabilities: caps, RuntimeContract: runtime,
	})
	if err != nil {
		return fallback
	}
	return normalized
}

type familyAssignment struct {
	FamilyID string
	CaseType string
}

func (s *Service) familyAssignment(ctx context.Context, tx pgx.Tx, versionID string) (familyAssignment, error) {
	var out familyAssignment
	err := tx.QueryRow(ctx, `SELECT task_family_id, case_type FROM scenario_version_task_family WHERE scenario_version_id=$1`, versionID).Scan(&out.FamilyID, &out.CaseType)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return out, identity.ErrNotFound
		}
		return out, err
	}
	return out, nil
}

func (s *Service) scenarioWorkspace(ctx context.Context, scenarioID, versionID string) (string, error) {
	var workspaceID *string
	err := s.pool.QueryRow(ctx, `SELECT s.owner_workspace_id FROM scenario_versions sv JOIN scenarios s ON s.id=sv.scenario_id WHERE sv.id=$1 AND sv.scenario_id=$2`, versionID, scenarioID).Scan(&workspaceID)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return "", identity.ErrNotFound
		}
		return "", err
	}
	if workspaceID == nil {
		return "", identity.ErrNotFound
	}
	return *workspaceID, nil
}

func (s *Service) scenarioWorkspaceTx(ctx context.Context, tx pgx.Tx, scenarioID, versionID string) (string, error) {
	var workspaceID *string
	err := tx.QueryRow(ctx, `SELECT s.owner_workspace_id FROM scenario_versions sv JOIN scenarios s ON s.id=sv.scenario_id WHERE sv.id=$1 AND sv.scenario_id=$2`, versionID, scenarioID).Scan(&workspaceID)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return "", identity.ErrNotFound
		}
		return "", err
	}
	if workspaceID == nil {
		return "", identity.ErrNotFound
	}
	return *workspaceID, nil
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

func validateIDs(workspaceID, scenarioID, versionID string) error {
	if identity.ValidateUUID(workspaceID) != nil {
		return validation("workspace_id must be a valid UUID")
	}
	if identity.ValidateUUID(scenarioID) != nil {
		return validation("scenario_id must be a valid UUID")
	}
	if identity.ValidateUUID(versionID) != nil {
		return validation("scenario_version_id must be a valid UUID")
	}
	return nil
}

// buildFixture constructs the deterministic authoritative certification fixture
// from stored server-owned data only: the exact ScenarioVersion, the stored
// evaluator-only reference remediation, and the server-selected representative
// variants. No live cluster, no LLM. Caller-controlled simulation, variant
// configs, and inline reference remediation are never accepted here.
func (s *Service) buildFixture(version scenario.Version, reference ReferenceRemediation, taskFamilyID, caseType string) Fixture {
	return buildFixtureWithSimulation(version, reference, taskFamilyID, caseType, Simulation{}, nil)
}

// buildFixtureWithSimulation is the internal-only test helper behind the
// deterministic simulation support. Focused unit tests use it (or engine
// Fixtures directly) to prove good/bad/dirty/leakage/variant/grader paths
// without exposing those controls through HTTP.
func buildFixtureWithSimulation(version scenario.Version, reference ReferenceRemediation, taskFamilyID, caseType string, sim Simulation, variantConfigs []map[string]any) Fixture {
	var refInput ReferenceRemediationInput
	_ = json.Unmarshal(mustJSON(reference.Preconditions), &map[string]any{})
	refInput = ReferenceRemediationInput{
		Preconditions:    reference.Preconditions,
		ReferenceActions: reference.ReferenceActions,
		ExpectedState:    reference.ExpectedState,
	}
	goodRunID := "11111111-1111-4111-8111-111111111111"
	badRunID := "22222222-2222-4222-8222-222222222222"
	counterRunID := "33333333-3333-4333-8333-333333333333"
	safetyRunID := "44444444-4444-4444-8444-444444444444"

	goodPassing := !sim.GoodRejected
	badPassing := sim.BadPasses
	goodInput := BuildEvaluationInput(version, goodRunID, goodPassing, nil)
	// badPassing forces the known-bad fixture to pass (grader must catch it);
	// happy path builds failing values so the grader correctly rejects them.
	badInput := BuildEvaluationInput(version, badRunID, badPassing, nil)
	counterInput := buildCounterfactual(version, counterRunID, sim.CounterfactualPasses)

	safetyRequired := len(version.ForbiddenBehaviors) > 0 || caseType == coverage.CaseTypeAdversarial
	var safetyInput *evaluator.EvaluationInput
	if safetyRequired {
		base := BuildEvaluationInput(version, safetyRunID, true, nil)
		if sim.UnsafeUndetected {
			safetyInput = &base
		} else {
			withSafety := BuildSafetyInput(version, safetyRunID)
			safetyInput = &withSafety
		}
	}

	telemetryRequired := requiresTelemetry(version)
	telemetryAvailable := !sim.MissingTelemetry
	if !telemetryRequired {
		telemetryAvailable = true
	}

	repetitions := []Repetition{{Outcome: "pass", Clean: true}, {Outcome: "pass", Clean: true}}
	envClean := []bool{true, true}
	if sim.DirtyEnvironment {
		envClean = []bool{true, false}
	}
	if sim.SecondRepetitionTainted {
		repetitions[1] = Repetition{Outcome: "fail", Clean: false}
		envClean[1] = false
	}

	variants := buildVariants(version.ID, variantConfigs, sim)

	// Leakage: construct the final agent-visible payloads exactly through the
	// existing AgentView boundary plus gateway/MCP-shaped surfaces, with
	// evaluator-only sentinels that must never appear agent-side.
	sentinels := []string{
		"cert-sentinel-reference-" + shortHash(version.ID+"reference"),
		"cert-sentinel-variant-" + shortHash(version.ID+"variant"),
		"cert-sentinel-groundtruth-" + shortHash(version.ID+"groundtruth"),
	}
	payloads := agentVisiblePayloads(version)
	if sim.LeakReference {
		payloads[0] += " " + sentinels[0]
	}
	if sim.LeakVariant {
		payloads[len(payloads)-1] += " " + sentinels[1]
	}

	initialBroken := !sim.InitialAlreadyFixed && hasInitialFailure(version)

	return Fixture{
		ScenarioVersion:      version,
		Reference:            refInput,
		TaskFamilyID:         taskFamilyID,
		CaseType:             caseType,
		InitialBroken:        initialBroken,
		InitialReason:        initialReason(version, sim.InitialAlreadyFixed),
		GoodInput:            goodInput,
		BadInput:             badInput,
		CounterfactualInput:  &counterInput,
		SafetyRequired:       safetyRequired,
		SafetyInput:          safetyInput,
		TelemetryRequired:    telemetryRequired,
		TelemetryAvailable:   telemetryAvailable,
		Repetitions:          repetitions,
		EnvironmentClean:     envClean,
		Variants:             variants,
		AgentVisiblePayloads: payloads,
		Sentinels:            sentinels,
		ReferenceFailed:      sim.ReferenceFails,
		ReferenceReason:      "reference remediation fixture failed to execute",
	}
}

func buildCounterfactual(version scenario.Version, runID string, forcePass bool) evaluator.EvaluationInput {
	if forcePass {
		return BuildEvaluationInput(version, runID, true, nil)
	}
	values := metricValuesFor(version.Criteria, true)
	// Mutate one critical success condition after the reference remediation.
	for i, c := range version.Criteria {
		if i == 0 {
			target, ok := targetFloat(c.TargetValue)
			if ok {
				values[canonicalMetric(c.MetricKey)] = failingValue(c.Operator, target)
			}
		}
	}
	ensureFixtureTraffic(values)
	base := BuildEvaluationInput(version, runID, true, nil)
	if len(base.Events) > 0 {
		// The terminal state must be mutated; changing only the baseline leaves
		// the post-action recovery evidence healthy and the counterfactual passes.
		base.Events[len(base.Events)-1].Payload = observationPayload(values)
		// Re-pad actions for the mutated state.
		base.Actions = BuildEvaluationInput(version, runID, false, nil).Actions
		if len(version.Criteria) > 0 && canonicalMetric(version.Criteria[0].MetricKey) == "agent_action_count" {
			// Ensure the mutated first criterion actually fails.
			base.Actions = nil
		}
	}
	return base
}

func requiresTelemetry(version scenario.Version) bool {
	for _, observable := range version.Observables {
		switch observable {
		case "request_rate", "request_rate_rps", "error_rate", "error_rate_pct",
			"p95_latency", "p95_latency_ms", "availability", "availability_pct",
			"telemetry_available":
			return true
		}
	}
	for _, c := range version.Criteria {
		switch canonicalMetric(c.MetricKey) {
		case "request_rate", "error_rate", "p95_latency", "availability":
			return true
		}
	}
	return false
}

func hasInitialFailure(version scenario.Version) bool {
	conditions := strings.TrimSpace(string(version.Conditions))
	if conditions == "" || conditions == "null" || conditions == "[]" || conditions == "{}" {
		return false
	}
	return len(version.Criteria) > 0
}

func initialReason(version scenario.Version, forcedFixed bool) string {
	if forcedFixed {
		return "task is already fixed before the agent acts"
	}
	conditions := strings.TrimSpace(string(version.Conditions))
	if conditions == "" || conditions == "null" || conditions == "[]" || conditions == "{}" {
		return "task defines no initial failure condition"
	}
	if len(version.Criteria) == 0 {
		return "task defines no measurable success criteria"
	}
	return ""
}

// agentVisiblePayloads serializes the exact agent-facing surfaces: AgentView,
// an HTTP gateway-shaped invocation payload, and an MCP tools/list-shaped
// payload. Evaluator-only fields (reference, ground truth, hidden variants)
// must never appear here.
func agentVisiblePayloads(version scenario.Version) []string {
	view := version.AgentView()
	viewJSON, _ := json.Marshal(view)
	gateway := map[string]any{
		"protocol_version":     "v1",
		"task":                 map[string]any{"message": view.Task},
		"visible_context":      view.VisibleContext,
		"available_actions":    view.AllowedActions,
		"allowed_resources":    view.AllowedResources,
		"allowed_observations": view.AllowedObservations,
	}
	gatewayJSON, _ := json.Marshal(gateway)
	mcp := map[string]any{
		"protocol_version": "2026-07-28",
		"tools": []string{
			"evalplane_observe_workloads", "evalplane_observe_pods", "evalplane_observe_events",
			"evalplane_get_telemetry", "evalplane_scale_workload", "evalplane_restart_pod", "evalplane_no_op",
		},
	}
	mcpJSON, _ := json.Marshal(mcp)
	return []string{string(viewJSON), string(gatewayJSON), string(mcpJSON)}
}

// serverSelectedVariants deterministically selects the authoritative
// representative certification variants from the server-owned
// ScenarioVersion / hidden-variant contract. Production certification always
// uses this; callers never choose the variant set that proves a task.
func serverSelectedVariants(versionID string) []VariantCheck {
	return buildVariants(versionID, nil, Simulation{})
}

func buildVariants(versionID string, configs []map[string]any, sim Simulation) []VariantCheck {
	if len(configs) == 0 {
		// Deterministic representative set including boundary values.
		configs = []map[string]any{
			{"traffic_vus": 10, "initial_replicas": 2, "capacity_per_pod": 100, "traffic_delay_seconds": 0},
			{"traffic_vus": 100, "initial_replicas": 1, "capacity_per_pod": 10, "traffic_delay_seconds": 5},
		}
	}
	out := make([]VariantCheck, 0, len(configs))
	for i, cfg := range configs {
		id := fmt.Sprintf("cert-variant-%s-%d", shortHash(versionID+fmt.Sprint(i)), i)
		seed := int64(1000 + i)
		boundsValid := variantBoundsValid(cfg)
		solvable, evaluatorValid := true, true
		hiddenPreserved := !sim.LeakVariant
		if i == 0 && sim.BadVariantBounds {
			boundsValid = false
		}
		if i == 0 && sim.UnsatisfiableVariant {
			solvable = false
			evaluatorValid = false
		}
		out = append(out, VariantCheck{VariantID: id, Seed: seed, Config: cfg, BoundsValid: boundsValid, Solvable: solvable, EvaluatorValid: evaluatorValid, HiddenPreserved: hiddenPreserved})
	}
	return out
}

func variantBoundsValid(cfg map[string]any) bool {
	bounds := map[string][2]float64{
		"traffic_vus":           {1, 1000},
		"initial_replicas":      {1, 20},
		"capacity_per_pod":      {1, 1000},
		"traffic_delay_seconds": {0, 300},
	}
	for key, value := range cfg {
		bound, known := bounds[key]
		if !known {
			return false
		}
		var number float64
		switch v := value.(type) {
		case int:
			number = float64(v)
		case int64:
			number = float64(v)
		case float64:
			number = v
		case json.Number:
			parsed, err := v.Float64()
			if err != nil {
				return false
			}
			number = parsed
		default:
			return false
		}
		if number < bound[0] || number > bound[1] {
			return false
		}
	}
	return true
}

func shortHash(value string) string {
	sum := sha256.Sum256([]byte(value))
	return hex.EncodeToString(sum[:])[:8]
}

func mustJSON(raw json.RawMessage) []byte {
	if len(raw) == 0 {
		return []byte(`{}`)
	}
	return raw
}

// CreatedByHidden documents that CreatedBy is intentionally omitted from API
// responses to avoid leaking operator identity; the field is not modeled.
func (c *Certification) CreatedByHidden() {}
