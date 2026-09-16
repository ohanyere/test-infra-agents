package coverage

import (
	"context"
	"errors"
	"fmt"
	"sort"

	"github.com/evalplane/evalplane/backend/internal/auth"
	"github.com/evalplane/evalplane/backend/internal/identity"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

type roleRepository interface {
	GetRole(context.Context, identity.DBTX, string, string) (string, error)
}

// ClaimReader supplies immutable capability claims for one AgentVersion.
type ClaimReader interface {
	ListVersionClaims(context.Context, auth.Principal, string, string, string) ([]ClaimSummary, error)
}

// ClaimSummary is the minimal claim view the coverage service needs.
// It avoids importing the agent package (which would cycle through services).
type ClaimSummary struct {
	CapabilityID string
}

// ExecutionMembership proves an agent version belongs to an execution in this
// workspace. Implemented by the caller (evaluationsuite-backed) to keep this
// package free of cross-domain SQL.
type ExecutionMembership interface {
	AgentInExecution(ctx context.Context, workspaceID, executionID, agentVersionID string) (agentID string, ok bool, err error)
}

// Service owns deterministic coverage workflows: family assignment and
// persisted, reproducible assessments. No LLM anywhere.
type Service struct {
	pool   *pgxpool.Pool
	repo   *Repository
	roles  roleRepository
	claims ClaimReader
	execs  ExecutionMembership
}

// NewService creates a coverage Service. claims and execs may be nil in tests
// that only exercise assignment validation and the pure Assess engine.
func NewService(pool *pgxpool.Pool, repo *Repository, roles roleRepository, claims ClaimReader, execs ExecutionMembership) *Service {
	return &Service{pool: pool, repo: repo, roles: roles, claims: claims, execs: execs}
}

// AssignFamily sets the explicit task-family + case-type assignment for one
// ScenarioVersion. Idempotent for identical values. A differing assignment
// after evaluation evidence exists is rejected (historical immutability);
// before evidence it may be corrected explicitly, never silently.
func (s *Service) AssignFamily(ctx context.Context, principal auth.Principal, workspaceID, scenarioID, scenarioVersionID, familyID, caseType string) (Assignment, error) {
	if err := validateUUID(workspaceID, "workspace_id"); err != nil {
		return Assignment{}, err
	}
	if err := validateUUID(scenarioID, "scenario_id"); err != nil {
		return Assignment{}, err
	}
	if err := validateUUID(scenarioVersionID, "scenario_version_id"); err != nil {
		return Assignment{}, err
	}
	if err := s.requireWrite(ctx, principal, workspaceID); err != nil {
		return Assignment{}, err
	}
	familyID, err := ValidateTaskFamilyID(familyID)
	if err != nil {
		return Assignment{}, validation(err.Error())
	}
	caseType, err = ValidateCaseType(caseType)
	if err != nil {
		return Assignment{}, validation(err.Error())
	}
	ownerWorkspace, err := s.repo.ScenarioWorkspace(ctx, s.pool, scenarioID, scenarioVersionID)
	if err != nil {
		return Assignment{}, err
	}
	if ownerWorkspace != workspaceID {
		return Assignment{}, auth.ErrForbidden
	}
	existing, err := s.repo.GetAssignment(ctx, s.pool, scenarioVersionID)
	if err != nil && !errors.Is(err, identity.ErrNotFound) {
		return Assignment{}, err
	}
	if err == nil && existing.FamilyID == familyID && existing.CaseType == caseType {
		return existing, nil
	}
	if err == nil {
		hasEvidence, evidenceErr := s.repo.ScenarioEvidenceExists(ctx, s.pool, scenarioVersionID)
		if evidenceErr != nil {
			return Assignment{}, evidenceErr
		}
		if hasEvidence {
			return Assignment{}, identity.ErrConflict
		}
	}
	return s.repo.UpsertAssignment(ctx, s.pool, scenarioVersionID, familyID, caseType, principal.UserID)
}

// GetAssignment reads one ScenarioVersion assignment. Any workspace member
// (including viewer) may read.
func (s *Service) GetAssignment(ctx context.Context, principal auth.Principal, workspaceID, scenarioID, scenarioVersionID string) (Assignment, error) {
	if err := validateUUID(workspaceID, "workspace_id"); err != nil {
		return Assignment{}, err
	}
	if err := validateUUID(scenarioID, "scenario_id"); err != nil {
		return Assignment{}, err
	}
	if err := validateUUID(scenarioVersionID, "scenario_version_id"); err != nil {
		return Assignment{}, err
	}
	if _, err := s.role(ctx, principal, workspaceID); err != nil {
		return Assignment{}, err
	}
	ownerWorkspace, err := s.repo.ScenarioWorkspace(ctx, s.pool, scenarioID, scenarioVersionID)
	if err != nil {
		return Assignment{}, err
	}
	if ownerWorkspace != workspaceID {
		return Assignment{}, auth.ErrForbidden
	}
	return s.repo.GetAssignment(ctx, s.pool, scenarioVersionID)
}

// ComputeExecutionCoverage deterministically assesses every claimed capability
// for one agent version in one exact suite execution. Read-only: it never
// persists. Incomplete evidence yields UNTESTED/PARTIAL, never premature TESTED.
func (s *Service) ComputeExecutionCoverage(ctx context.Context, principal auth.Principal, workspaceID, agentID, agentVersionID, executionID string) ([]Assessment, error) {
	if err := validateUUIDs(workspaceID, agentID, agentVersionID, executionID); err != nil {
		return nil, err
	}
	if _, err := s.role(ctx, principal, workspaceID); err != nil {
		return nil, err
	}
	if s.claims == nil || s.execs == nil {
		return nil, fmt.Errorf("coverage service is not fully wired")
	}
	ownerAgentID, ok, err := s.execs.AgentInExecution(ctx, workspaceID, executionID, agentVersionID)
	if err != nil {
		return nil, err
	}
	if !ok || (agentID != "" && ownerAgentID != "" && ownerAgentID != agentID) {
		return nil, identity.ErrNotFound
	}
	claimRows, err := s.claims.ListVersionClaims(ctx, principal, workspaceID, agentID, agentVersionID)
	if err != nil {
		return nil, err
	}
	trials, err := s.repo.ListExecutionTrials(ctx, s.pool, workspaceID, executionID, agentVersionID)
	if err != nil {
		return nil, err
	}
	evidence := make([]TrialEvidence, 0, len(trials))
	for _, trial := range trials {
		evidence = append(evidence, TrialEvidence{
			RunID: trial.RunID, ScenarioVersionID: trial.ScenarioVersionID,
			FamilyID: trial.FamilyID, CaseType: trial.CaseType,
			Validity: trial.Validity, EvaluationResult: trial.EvaluationResult, VariantID: trial.VariantID,
		})
	}
	ids := make([]string, 0, len(claimRows))
	for _, claim := range claimRows {
		ids = append(ids, claim.CapabilityID)
	}
	sort.Strings(ids)
	return AssessMany(ids, evidence), nil
}

// PersistExecutionCoverage computes and persists assessments for every claimed
// capability in one exact execution scope. Safe to retry: the unique scope
// constraint makes concurrent creation idempotent.
func (s *Service) PersistExecutionCoverage(ctx context.Context, principal auth.Principal, workspaceID, agentID, agentVersionID, executionID string) ([]PersistedAssessment, error) {
	if err := validateUUIDs(workspaceID, agentID, agentVersionID, executionID); err != nil {
		return nil, err
	}
	if err := s.requireWrite(ctx, principal, workspaceID); err != nil {
		return nil, err
	}
	assessments, err := s.computeForWrite(ctx, principal, workspaceID, agentID, agentVersionID, executionID)
	if err != nil {
		return nil, err
	}
	trials, err := s.repo.ListExecutionTrials(ctx, s.pool, workspaceID, executionID, agentVersionID)
	if err != nil {
		return nil, err
	}
	scenarioSet := map[string]bool{}
	runSet := map[string]bool{}
	for _, trial := range trials {
		scenarioSet[trial.ScenarioVersionID] = true
		runSet[trial.RunID] = true
	}
	scenarioIDs := sortedKeys(scenarioSet)
	runIDs := sortedKeys(runSet)
	tx, err := s.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return nil, err
	}
	committed := false
	defer func() {
		if !committed {
			_ = tx.Rollback(ctx)
		}
	}()
	out := make([]PersistedAssessment, 0, len(assessments))
	for _, assessment := range assessments {
		persisted, insertErr := s.repo.InsertAssessment(ctx, tx, workspaceID, agentID, agentVersionID, executionID, assessment, scenarioIDs, runIDs)
		if insertErr != nil {
			// Idempotent retry: another writer won the race; read theirs.
			if errors.Is(insertErr, identity.ErrConflict) {
				existing, getErr := s.repo.GetAssessment(ctx, tx, workspaceID, agentVersionID, assessment.CapabilityID, executionID)
				if getErr != nil {
					return nil, getErr
				}
				out = append(out, existing)
				continue
			}
			return nil, insertErr
		}
		if persisted.ID == "" {
			existing, getErr := s.repo.GetAssessment(ctx, tx, workspaceID, agentVersionID, assessment.CapabilityID, executionID)
			if getErr != nil {
				return nil, getErr
			}
			out = append(out, existing)
			continue
		}
		out = append(out, persisted)
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, err
	}
	committed = true
	sort.Slice(out, func(i, j int) bool { return out[i].CapabilityID < out[j].CapabilityID })
	return out, nil
}

// ListVersionCoverage returns the latest persisted assessment per claimed
// capability for one agent version. Each row carries its own exact evidence
// scope (suite_execution_id); unassessed claims return UNTESTED placeholders
// so the API never returns em-dash domain state and never silently merges
// unrelated runs.
func (s *Service) ListVersionCoverage(ctx context.Context, principal auth.Principal, workspaceID, agentID, agentVersionID string) ([]PersistedAssessment, error) {
	if err := validateUUIDs(workspaceID, agentID, agentVersionID); err != nil {
		return nil, err
	}
	if _, err := s.role(ctx, principal, workspaceID); err != nil {
		return nil, err
	}
	if s.claims == nil {
		return nil, fmt.Errorf("coverage service is not fully wired")
	}
	claimRows, err := s.claims.ListVersionClaims(ctx, principal, workspaceID, agentID, agentVersionID)
	if err != nil {
		return nil, err
	}
	persisted, err := s.repo.ListLatestAssessments(ctx, s.pool, workspaceID, agentVersionID)
	if err != nil {
		return nil, err
	}
	byCapability := make(map[string]PersistedAssessment, len(persisted))
	for _, item := range persisted {
		byCapability[item.CapabilityID] = item
	}
	out := make([]PersistedAssessment, 0, len(claimRows))
	for _, claim := range claimRows {
		if item, ok := byCapability[claim.CapabilityID]; ok {
			out = append(out, item)
			continue
		}
		assessment := Assess(claim.CapabilityID, nil)
		out = append(out, PersistedAssessment{
			Assessment: assessment, WorkspaceID: workspaceID,
			AgentID: agentID, AgentVersionID: agentVersionID,
			ScenarioVersions: []string{}, RunIDs: []string{},
		})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].CapabilityID < out[j].CapabilityID })
	return out, nil
}

// ListExecutionCoverage reads persisted assessments for one exact execution.
// Empty (not 404) when nothing was persisted yet; compute endpoints stay
// separate so reads never fabricate TESTED state.
func (s *Service) ListExecutionCoverage(ctx context.Context, principal auth.Principal, workspaceID, executionID, agentVersionID string) ([]PersistedAssessment, error) {
	if err := validateUUIDs(workspaceID, executionID, agentVersionID); err != nil {
		return nil, err
	}
	if _, err := s.role(ctx, principal, workspaceID); err != nil {
		return nil, err
	}
	return s.repo.ListExecutionAssessments(ctx, s.pool, workspaceID, executionID, agentVersionID)
}

func (s *Service) computeForWrite(ctx context.Context, principal auth.Principal, workspaceID, agentID, agentVersionID, executionID string) ([]Assessment, error) {
	if s.claims == nil || s.execs == nil {
		return nil, fmt.Errorf("coverage service is not fully wired")
	}
	ownerAgentID, ok, err := s.execs.AgentInExecution(ctx, workspaceID, executionID, agentVersionID)
	if err != nil {
		return nil, err
	}
	if !ok || (agentID != "" && ownerAgentID != "" && ownerAgentID != agentID) {
		return nil, identity.ErrNotFound
	}
	claimRows, err := s.claims.ListVersionClaims(ctx, principal, workspaceID, agentID, agentVersionID)
	if err != nil {
		return nil, err
	}
	trials, err := s.repo.ListExecutionTrials(ctx, s.pool, workspaceID, executionID, agentVersionID)
	if err != nil {
		return nil, err
	}
	evidence := make([]TrialEvidence, 0, len(trials))
	for _, trial := range trials {
		evidence = append(evidence, TrialEvidence{
			RunID: trial.RunID, ScenarioVersionID: trial.ScenarioVersionID,
			FamilyID: trial.FamilyID, CaseType: trial.CaseType,
			Validity: trial.Validity, EvaluationResult: trial.EvaluationResult, VariantID: trial.VariantID,
		})
	}
	ids := make([]string, 0, len(claimRows))
	for _, claim := range claimRows {
		ids = append(ids, claim.CapabilityID)
	}
	sort.Strings(ids)
	return AssessMany(ids, evidence), nil
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

func validateUUID(value, label string) error {
	if identity.ValidateUUID(value) != nil {
		return validation(label + " must be a valid UUID")
	}
	return nil
}

func validateUUIDs(values ...string) error {
	labels := []string{"workspace_id", "agent_id", "agent_version_id", "execution_id"}
	for i, value := range values {
		label := "id"
		if i < len(labels) {
			label = labels[i]
		}
		if err := validateUUID(value, label); err != nil {
			return err
		}
	}
	return nil
}

func validation(message string) error { return fmt.Errorf("%w: %s", identity.ErrValidation, message) }

func sortedKeys(set map[string]bool) []string {
	out := make([]string, 0, len(set))
	for value := range set {
		out = append(out, value)
	}
	sort.Strings(out)
	return out
}
