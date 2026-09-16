package reliability

import (
	"context"
	"errors"
	"fmt"
	"sort"

	"github.com/evalplane/evalplane/backend/internal/auth"
	"github.com/evalplane/evalplane/backend/internal/identity"
	"github.com/jackc/pgx/v5/pgxpool"
)

type roleRepository interface {
	GetRole(context.Context, identity.DBTX, string, string) (string, error)
}

// ClaimReader supplies immutable capability claims for one AgentVersion
// without creating an import cycle (mirrors the coverage ClaimReader).
type ClaimReader interface {
	ListVersionClaims(context.Context, auth.Principal, string, string, string) ([]ClaimSummary, error)
}

// ClaimSummary is the minimal claim view the reliability service needs.
type ClaimSummary struct {
	CapabilityID string
}

// Service owns deterministic reliability reporting. All readings are
// recomputed from exact persisted trial evidence on every call: there are no
// mutable "current score" rows, no score submission endpoints, and no LLM.
// Reports pin reliability-metrics-contract:v1 and reliability-policy:v1 so
// historical readings stay reproducible.
type Service struct {
	pool   *pgxpool.Pool
	repo   *Repository
	roles  roleRepository
	claims ClaimReader
}

// NewService creates a reliability Service. claims may be nil in tests that
// only exercise the pure builders; capability sections then stay empty.
func NewService(pool *pgxpool.Pool, repo *Repository, roles roleRepository, claims ClaimReader) *Service {
	return &Service{pool: pool, repo: repo, roles: roles, claims: claims}
}

// ScenarioReliability reports task-level reliability for one exact
// ScenarioVersion. agentVersionID is optional: when empty, one task entry
// per agent version with evidence is returned; otherwise only that agent's
// entry. Saturation is always computed across all agents.
func (s *Service) ScenarioReliability(ctx context.Context, principal auth.Principal, workspaceID, scenarioID, scenarioVersionID, agentVersionID string) (ScenarioReport, error) {
	if err := validateUUID(workspaceID, "workspace_id"); err != nil {
		return ScenarioReport{}, err
	}
	if err := validateUUID(scenarioID, "scenario_id"); err != nil {
		return ScenarioReport{}, err
	}
	if err := validateUUID(scenarioVersionID, "scenario_version_id"); err != nil {
		return ScenarioReport{}, err
	}
	if agentVersionID != "" {
		if err := validateUUID(agentVersionID, "agent_version_id"); err != nil {
			return ScenarioReport{}, err
		}
	}
	if _, err := s.role(ctx, principal, workspaceID); err != nil {
		return ScenarioReport{}, err
	}
	policy := DefaultPolicy()
	familyID, err := s.repo.ScenarioIdentity(ctx, s.pool, workspaceID, scenarioID, scenarioVersionID)
	if err != nil {
		return ScenarioReport{}, err
	}
	trials, err := s.repo.ListScenarioTrials(ctx, s.pool, workspaceID, scenarioVersionID, agentVersionID)
	if err != nil {
		return ScenarioReport{}, err
	}
	if agentVersionID != "" && len(trials) == 0 {
		return ScenarioReport{}, identity.ErrNotFound
	}
	alternatives, err := s.repo.FamilyAlternatives(ctx, s.pool, workspaceID, familyID, scenarioVersionID)
	if err != nil {
		return ScenarioReport{}, err
	}
	report := ScenarioReport{
		ScenarioID:          scenarioID,
		ScenarioVersionID:   scenarioVersionID,
		ReliabilityContract: ReliabilityContractVersion,
		ReliabilityPolicy:   policy,
		CoverageContract:    CurrentCoverageContractVersion,
		AgentVersionID:      agentVersionID,
		Saturation:          BuildSaturation(scenarioVersionID, trials, alternatives),
	}
	byAgent := groupByAgent(trials)
	for _, agent := range byAgent {
		agentTrials := agent.trials
		report.Tasks = append(report.Tasks, BuildTaskReliability(policy, scenarioVersionID, agent.id, agentTrials, nil))
	}
	if len(byAgent) == 0 {
		report.Tasks = []TaskReliability{BuildTaskReliability(policy, scenarioVersionID, agentVersionID, nil, nil)}
	}
	return report, nil
}

// ExecutionReliability reports SuiteExecution-scope reliability: the overall
// aggregate plus per-agent task, family, and capability readings.
// Baseline/candidate agent version IDs are optional: when both are set, a
// reliability-delta comparison over the same compatible evidence is
// included; incompatible evidence yields structured not_comparable reasons.
func (s *Service) ExecutionReliability(ctx context.Context, principal auth.Principal, workspaceID, executionID, baselineAgentVersionID, candidateAgentVersionID string) (ExecutionReport, ComparisonReport, error) {
	if err := validateUUID(workspaceID, "workspace_id"); err != nil {
		return ExecutionReport{}, ComparisonReport{}, err
	}
	if err := validateUUID(executionID, "suite_execution_id"); err != nil {
		return ExecutionReport{}, ComparisonReport{}, err
	}
	if _, err := s.role(ctx, principal, workspaceID); err != nil {
		return ExecutionReport{}, ComparisonReport{}, err
	}
	policy := DefaultPolicy()
	suiteVersionID, _, err := s.repo.ExecutionSuiteVersion(ctx, s.pool, workspaceID, executionID)
	if err != nil {
		return ExecutionReport{}, ComparisonReport{}, err
	}
	trials, err := s.repo.ListExecutionTrials(ctx, s.pool, workspaceID, executionID, "")
	if err != nil {
		return ExecutionReport{}, ComparisonReport{}, err
	}
	if len(trials) == 0 {
		agents, agentsErr := s.repo.ExecutionAgents(ctx, s.pool, workspaceID, executionID)
		if agentsErr != nil {
			return ExecutionReport{}, ComparisonReport{}, agentsErr
		}
		if len(agents) == 0 {
			return ExecutionReport{}, ComparisonReport{}, identity.ErrNotFound
		}
	}
	report := ExecutionReport{
		SuiteExecutionID:    executionID,
		SuiteVersionID:      suiteVersionID,
		ReliabilityContract: ReliabilityContractVersion,
		ReliabilityPolicy:   policy,
		CoverageContract:    CurrentCoverageContractVersion,
		Overall:             SummarizeScope(policy, trials, policy.MinValidTrialsFamily, policy.TargetValidTrialsFamily),
		ByAgent:             []ExecutionAgentEntry{},
		Saturation:          []Saturation{},
	}
	byAgent := groupByAgent(trials)
	for _, agent := range byAgent {
		entry, entryErr := s.executionAgentEntry(ctx, principal, workspaceID, policy, executionID, suiteVersionID, agent.id, agent.trials)
		if entryErr != nil {
			return ExecutionReport{}, ComparisonReport{}, entryErr
		}
		report.ByAgent = append(report.ByAgent, entry)
	}
	byScenario := map[string][]Trial{}
	for _, trial := range trials {
		byScenario[trial.ScenarioVersionID] = append(byScenario[trial.ScenarioVersionID], trial)
	}
	scenarioIDs := make([]string, 0, len(byScenario))
	for id := range byScenario {
		scenarioIDs = append(scenarioIDs, id)
	}
	sort.Strings(scenarioIDs)
	for _, id := range scenarioIDs {
		familyID := ""
		for _, trial := range byScenario[id] {
			if trial.FamilyID != "" {
				familyID = trial.FamilyID
				break
			}
		}
		alternatives, altErr := s.repo.FamilyAlternatives(ctx, s.pool, workspaceID, familyID, id)
		if altErr != nil {
			return ExecutionReport{}, ComparisonReport{}, altErr
		}
		report.Saturation = append(report.Saturation, BuildSaturation(id, byScenario[id], alternatives))
	}
	var comparison ComparisonReport
	if baselineAgentVersionID != "" || candidateAgentVersionID != "" {
		if baselineAgentVersionID == "" || candidateAgentVersionID == "" {
			return ExecutionReport{}, ComparisonReport{}, validation("baseline_agent_version_id and candidate_agent_version_id must be provided together")
		}
		if err := validateUUID(baselineAgentVersionID, "baseline_agent_version_id"); err != nil {
			return ExecutionReport{}, ComparisonReport{}, err
		}
		if err := validateUUID(candidateAgentVersionID, "candidate_agent_version_id"); err != nil {
			return ExecutionReport{}, ComparisonReport{}, err
		}
		var baselineTrials, candidateTrials []Trial
		for _, trial := range trials {
			switch trial.AgentVersionID {
			case baselineAgentVersionID:
				baselineTrials = append(baselineTrials, trial)
			case candidateAgentVersionID:
				candidateTrials = append(candidateTrials, trial)
			}
		}
		if len(baselineTrials) == 0 || len(candidateTrials) == 0 {
			return ExecutionReport{}, ComparisonReport{}, identity.ErrNotFound
		}
		comparison = ComparisonReport{
			Scope:                   "execution:" + executionID,
			BaselineAgentVersionID:  baselineAgentVersionID,
			CandidateAgentVersionID: candidateAgentVersionID,
			Delta:                   Compare(policy, "execution:"+executionID, baselineTrials, candidateTrials),
			ReliabilityContract:     ReliabilityContractVersion,
		}
	}
	return report, comparison, nil
}

// AgentVersionReliability reports AgentVersion-scope reliability across all
// suite executions in the workspace: per-task readings, family readings,
// capability readings from the version's claimed capabilities, and a
// per-execution evidence summary.
func (s *Service) AgentVersionReliability(ctx context.Context, principal auth.Principal, workspaceID, agentID, agentVersionID string) (AgentReport, error) {
	if err := validateUUID(workspaceID, "workspace_id"); err != nil {
		return AgentReport{}, err
	}
	if err := validateUUID(agentID, "agent_id"); err != nil {
		return AgentReport{}, err
	}
	if err := validateUUID(agentVersionID, "agent_version_id"); err != nil {
		return AgentReport{}, err
	}
	if _, err := s.role(ctx, principal, workspaceID); err != nil {
		return AgentReport{}, err
	}
	if err := s.repo.AgentVersionOwner(ctx, s.pool, workspaceID, agentID, agentVersionID); err != nil {
		return AgentReport{}, err
	}
	policy := DefaultPolicy()
	trials, err := s.repo.ListAgentTrials(ctx, s.pool, workspaceID, agentVersionID)
	if err != nil {
		return AgentReport{}, err
	}
	report := AgentReport{
		AgentID:             agentID,
		AgentVersionID:      agentVersionID,
		ReliabilityContract: ReliabilityContractVersion,
		ReliabilityPolicy:   policy,
		CoverageContract:    CurrentCoverageContractVersion,
		Summary:             SummarizeScope(policy, trials, policy.MinValidTrialsFamily, policy.TargetValidTrialsFamily),
		Tasks:               []TaskReliability{},
		Executions:          []AgentExecutionEntry{},
	}
	byScenario := map[string][]Trial{}
	for _, trial := range trials {
		byScenario[trial.ScenarioVersionID] = append(byScenario[trial.ScenarioVersionID], trial)
	}
	scenarioIDs := make([]string, 0, len(byScenario))
	for id := range byScenario {
		scenarioIDs = append(scenarioIDs, id)
	}
	sort.Strings(scenarioIDs)
	for _, id := range scenarioIDs {
		report.Tasks = append(report.Tasks, BuildTaskReliability(policy, id, agentVersionID, byScenario[id], nil))
	}
	report.Families = BuildFamilyReliabilities(policy, trials)
	report.Capabilities = s.capabilitiesFor(ctx, principal, workspaceID, policy, agentID, agentVersionID, trials)
	byExecution := map[string][]Trial{}
	for _, trial := range trials {
		byExecution[trial.SuiteExecutionID] = append(byExecution[trial.SuiteExecutionID], trial)
	}
	executionIDs := make([]string, 0, len(byExecution))
	for id := range byExecution {
		executionIDs = append(executionIDs, id)
	}
	sort.Strings(executionIDs)
	for _, id := range executionIDs {
		executionTrials := byExecution[id]
		scenarios := map[string]bool{}
		families := map[string]bool{}
		variants := map[string]bool{}
		invalid := 0
		for _, trial := range executionTrials {
			if trial.ScenarioVersionID != "" {
				scenarios[trial.ScenarioVersionID] = true
			}
			if trial.FamilyID != "" {
				families[trial.FamilyID] = true
			}
			if trial.VariantID != "" {
				variants[trial.VariantID] = true
			}
			if trial.Validity == ValidityInvalidPlatform {
				invalid++
			}
		}
		report.Executions = append(report.Executions, AgentExecutionEntry{
			SuiteExecutionID:      id,
			Summary:               SummarizeScope(policy, executionTrials, policy.MinValidTrialsScenario, policy.TargetValidTrialsScenario),
			DistinctScenarios:     len(scenarios),
			DistinctFamilies:      len(families),
			DistinctVariants:      len(variants),
			PlatformInvalidTrials: invalid,
		})
	}
	return report, nil
}

func (s *Service) executionAgentEntry(ctx context.Context, principal auth.Principal, workspaceID string, policy Policy, executionID, suiteVersionID, agentVersionID string, trials []Trial) (ExecutionAgentEntry, error) {
	byScenario := map[string][]Trial{}
	for _, trial := range trials {
		byScenario[trial.ScenarioVersionID] = append(byScenario[trial.ScenarioVersionID], trial)
	}
	scenarioIDs := make([]string, 0, len(byScenario))
	for id := range byScenario {
		scenarioIDs = append(scenarioIDs, id)
	}
	sort.Strings(scenarioIDs)
	entry := ExecutionAgentEntry{
		AgentVersionID:   agentVersionID,
		Summary:          SummarizeScope(policy, trials, policy.MinValidTrialsFamily, policy.TargetValidTrialsFamily),
		Tasks:            []TaskReliability{},
		CoverageContract: CurrentCoverageContractVersion,
	}
	for _, id := range scenarioIDs {
		var eligible *int
		if count, ok, err := s.repo.ScenarioEligibleVariants(ctx, s.pool, suiteVersionID, id); err == nil && ok {
			eligible = &count
		}
		entry.Tasks = append(entry.Tasks, BuildTaskReliability(policy, id, agentVersionID, byScenario[id], eligible))
	}
	entry.Families = BuildFamilyReliabilities(policy, trials)
	entry.Capabilities = s.capabilitiesForExecution(ctx, principal, workspaceID, policy, agentVersionID, trials)
	return entry, nil
}

func (s *Service) capabilitiesFor(ctx context.Context, principal auth.Principal, workspaceID string, policy Policy, agentID, agentVersionID string, trials []Trial) []CapabilityReliability {
	if s.claims == nil {
		return []CapabilityReliability{}
	}
	claims, err := s.claims.ListVersionClaims(ctx, principal, workspaceID, agentID, agentVersionID)
	if err != nil || len(claims) == 0 {
		return []CapabilityReliability{}
	}
	ids := make([]string, 0, len(claims))
	for _, claim := range claims {
		ids = append(ids, claim.CapabilityID)
	}
	return BuildCapabilityReliabilities(policy, ids, trials)
}

func (s *Service) capabilitiesForExecution(ctx context.Context, principal auth.Principal, workspaceID string, policy Policy, agentVersionID string, trials []Trial) []CapabilityReliability {
	if s.claims == nil {
		return []CapabilityReliability{}
	}
	agentID, err := s.repo.AgentForVersion(ctx, s.pool, workspaceID, agentVersionID)
	if err != nil {
		return []CapabilityReliability{}
	}
	claims, err := s.claims.ListVersionClaims(ctx, principal, workspaceID, agentID, agentVersionID)
	if err != nil || len(claims) == 0 {
		return []CapabilityReliability{}
	}
	ids := make([]string, 0, len(claims))
	for _, claim := range claims {
		ids = append(ids, claim.CapabilityID)
	}
	return BuildCapabilityReliabilities(policy, ids, trials)
}

type agentGroup struct {
	id     string
	trials []Trial
}

func groupByAgent(trials []Trial) []agentGroup {
	byAgent := map[string][]Trial{}
	for _, trial := range trials {
		byAgent[trial.AgentVersionID] = append(byAgent[trial.AgentVersionID], trial)
	}
	ids := make([]string, 0, len(byAgent))
	for id := range byAgent {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	out := make([]agentGroup, 0, len(ids))
	for _, id := range ids {
		out = append(out, agentGroup{id: id, trials: byAgent[id]})
	}
	return out
}

func (s *Service) role(ctx context.Context, principal auth.Principal, workspaceID string) (string, error) {
	role, err := s.roles.GetRole(ctx, s.pool, workspaceID, principal.UserID)
	if errors.Is(err, identity.ErrNotFound) {
		return "", auth.ErrForbidden
	}
	return role, err
}

func validateUUID(value, label string) error {
	if identity.ValidateUUID(value) != nil {
		return validation(label + " must be a valid UUID")
	}
	return nil
}

func validation(message string) error { return fmt.Errorf("%w: %s", identity.ErrValidation, message) }
