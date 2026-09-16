package qualification

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"

	"github.com/evalplane/evalplane/backend/internal/agentregression"
	"github.com/evalplane/evalplane/backend/internal/auth"
	"github.com/evalplane/evalplane/backend/internal/evaluationsuite"
	"github.com/evalplane/evalplane/backend/internal/failuretaxonomy"
	"github.com/evalplane/evalplane/backend/internal/identity"
	"github.com/jackc/pgx/v5/pgxpool"
)

type store interface {
	CreatePolicy(context.Context, DBTX, string, string, string) (Policy, error)
	CreateVersion(context.Context, *pgxpool.Pool, string, string, string, CreateVersionInput) (PolicyVersion, error)
	GetPolicy(context.Context, DBTX, string, string) (Policy, error)
	ListPolicies(context.Context, DBTX, string) ([]Policy, error)
	GetVersion(context.Context, DBTX, string, string) (PolicyVersion, error)
	SaveAssessment(context.Context, *pgxpool.Pool, string, CreateAssessmentInput, Assessment, AssessmentFacts) (Assessment, error)
	GetAssessmentByIdentity(context.Context, DBTX, string, CreateAssessmentInput) (Assessment, error)
	GetAssessment(context.Context, DBTX, string, string) (Assessment, error)
	ListAssessments(context.Context, DBTX, string, string, string) ([]Assessment, error)
}
type roleRepository interface {
	GetRole(context.Context, identity.DBTX, string, string) (string, error)
}
type suiteReader interface {
	GetSuiteExecution(context.Context, auth.Principal, string, string) (evaluationsuite.Execution, error)
	GetSuiteResults(context.Context, auth.Principal, string, string) (evaluationsuite.Results, error)
}
type regressionReader interface {
	Results(context.Context, auth.Principal, string, string) (agentregression.Results, error)
}
type failureReader interface {
	ForExecution(context.Context, auth.Principal, string, string) (failuretaxonomy.Results, error)
}

type Service struct {
	pool        *pgxpool.Pool
	repo        store
	roles       roleRepository
	suites      suiteReader
	regressions regressionReader
	failures    failureReader
}

func NewService(pool *pgxpool.Pool, repo store, roles roleRepository, suites suiteReader, regressions regressionReader, failures failureReader) *Service {
	return &Service{pool: pool, repo: repo, roles: roles, suites: suites, regressions: regressions, failures: failures}
}

func (s *Service) CreatePolicy(ctx context.Context, p auth.Principal, workspace string, in CreatePolicyInput) (Policy, error) {
	if err := validID(workspace); err != nil {
		return Policy{}, err
	}
	in.Name = strings.TrimSpace(in.Name)
	if in.Name == "" || len(in.Name) > 200 {
		return Policy{}, validation("name is required and must be at most 200 characters")
	}
	if err := s.requireWrite(ctx, p, workspace); err != nil {
		return Policy{}, err
	}
	return s.repo.CreatePolicy(ctx, s.pool, workspace, p.UserID, in.Name)
}
func (s *Service) CreatePolicyVersion(ctx context.Context, p auth.Principal, workspace, policyID string, in CreateVersionInput) (PolicyVersion, error) {
	if err := validIDs(workspace, policyID); err != nil {
		return PolicyVersion{}, err
	}
	if err := s.requireWrite(ctx, p, workspace); err != nil {
		return PolicyVersion{}, err
	}
	validated, err := validateVersion(in)
	if err != nil {
		return PolicyVersion{}, err
	}
	return s.repo.CreateVersion(ctx, s.pool, workspace, policyID, p.UserID, validated)
}
func (s *Service) GetPolicy(ctx context.Context, p auth.Principal, workspace, id string) (Policy, error) {
	if err := validIDs(workspace, id); err != nil {
		return Policy{}, err
	}
	if _, err := s.role(ctx, p, workspace); err != nil {
		return Policy{}, err
	}
	return s.repo.GetPolicy(ctx, s.pool, workspace, id)
}
func (s *Service) ListPolicies(ctx context.Context, p auth.Principal, workspace string) ([]Policy, error) {
	if err := validID(workspace); err != nil {
		return nil, err
	}
	if _, err := s.role(ctx, p, workspace); err != nil {
		return nil, err
	}
	return s.repo.ListPolicies(ctx, s.pool, workspace)
}

func (s *Service) GetPolicyVersion(ctx context.Context, p auth.Principal, workspace, id string) (PolicyVersion, error) {
	if err := validIDs(workspace, id); err != nil {
		return PolicyVersion{}, err
	}
	if _, err := s.role(ctx, p, workspace); err != nil {
		return PolicyVersion{}, err
	}
	return s.repo.GetVersion(ctx, s.pool, workspace, id)
}

func (s *Service) CreateAssessment(ctx context.Context, p auth.Principal, workspace string, in CreateAssessmentInput) (Assessment, error) {
	if err := validateAssessmentInput(workspace, in); err != nil {
		return Assessment{}, err
	}
	if err := s.requireWrite(ctx, p, workspace); err != nil {
		return Assessment{}, err
	}
	policy, err := s.repo.GetVersion(ctx, s.pool, workspace, in.PolicyVersionID)
	if err != nil {
		return Assessment{}, err
	}
	execution, err := s.suites.GetSuiteExecution(ctx, p, workspace, in.SuiteExecutionID)
	if err != nil {
		return Assessment{}, err
	}
	if execution.Status != "completed" && execution.Status != "failed" && execution.Status != "stopped" {
		return Assessment{}, fmt.Errorf("%w: suite execution is not terminal", identity.ErrConflict)
	}
	results, err := s.suites.GetSuiteResults(ctx, p, workspace, in.SuiteExecutionID)
	if err != nil {
		return Assessment{}, err
	}
	counts, scenarios, ok := agentFacts(results, in.AgentVersionID)
	if !ok {
		return Assessment{}, identity.ErrNotFound
	}
	facts := AssessmentFacts{SuiteVersionID: execution.SuiteVersionID, Counts: counts, Scenarios: scenarios, Trials: results.Trials}
	if in.RegressionComparisonID != nil {
		regression, loadErr := s.regressions.Results(ctx, p, workspace, *in.RegressionComparisonID)
		if loadErr != nil {
			return Assessment{}, loadErr
		}
		if regression.Comparison.SuiteExecutionID != in.SuiteExecutionID || regression.Comparison.SuiteVersionID != execution.SuiteVersionID || regression.Comparison.CandidateAgentVersionID != in.AgentVersionID {
			return Assessment{}, validation("regression comparison must reference this suite execution and candidate agent version")
		}
		facts.Regression = &regression
	}
	out := Evaluate(policy, in.AgentVersionID, facts)
	out.WorkspaceID = workspace
	out.SuiteExecutionID = in.SuiteExecutionID
	out.RegressionComparisonID = in.RegressionComparisonID
	return s.repo.SaveAssessment(ctx, s.pool, p.UserID, in, out, facts)
}
func (s *Service) GetAssessment(ctx context.Context, p auth.Principal, workspace, id string) (Assessment, error) {
	if err := validIDs(workspace, id); err != nil {
		return Assessment{}, err
	}
	if _, err := s.role(ctx, p, workspace); err != nil {
		return Assessment{}, err
	}
	return s.repo.GetAssessment(ctx, s.pool, workspace, id)
}

func (s *Service) ComparativeReport(ctx context.Context, p auth.Principal, workspace, executionID, policyVersionID string) (ComparativeReport, error) {
	if err := validIDs(workspace, executionID); err != nil {
		return ComparativeReport{}, err
	}
	if _, err := s.role(ctx, p, workspace); err != nil {
		return ComparativeReport{}, err
	}
	if policyVersionID != "" {
		if err := validID(policyVersionID); err != nil {
			return ComparativeReport{}, err
		}
		if _, err := s.repo.GetVersion(ctx, s.pool, workspace, policyVersionID); err != nil {
			return ComparativeReport{}, err
		}
	}
	execution, err := s.suites.GetSuiteExecution(ctx, p, workspace, executionID)
	if err != nil {
		return ComparativeReport{}, err
	}
	results, err := s.suites.GetSuiteResults(ctx, p, workspace, executionID)
	if err != nil {
		return ComparativeReport{}, err
	}
	failures, err := s.failures.ForExecution(ctx, p, workspace, executionID)
	if err != nil {
		return ComparativeReport{}, err
	}
	assessments := []Assessment{}
	if policyVersionID != "" {
		assessments, err = s.repo.ListAssessments(ctx, s.pool, workspace, executionID, policyVersionID)
		if err != nil {
			return ComparativeReport{}, err
		}
	}
	report := ComparativeReport{ExecutionID: executionID, SuiteVersionID: execution.SuiteVersionID, PolicyVersionID: policyVersionID, Comparability: "comparable"}
	report.Reasons = fairnessReasons(results)
	if len(report.Reasons) > 0 {
		report.Comparability = "not_comparable"
	}
	byAssessment := map[string]Assessment{}
	for _, a := range assessments {
		byAssessment[a.AgentVersionID] = a
	}
	for _, agent := range results.ByAgent {
		item := AgentComparison{AgentVersionID: agent.AgentVersionID, Counts: agent.Counts}
		if a, ok := byAssessment[agent.AgentVersionID]; ok {
			copy := a
			item.Qualification = &copy
		}
		for _, scenario := range results.ByScenario {
			if scenario.AgentVersionID == agent.AgentVersionID {
				item.Scenarios = append(item.Scenarios, scenario)
			}
		}
		for _, criterion := range results.Criteria {
			if criterion.AgentVersionID == agent.AgentVersionID {
				item.Criteria = append(item.Criteria, criterion)
			}
		}
		for _, failure := range failures.Failures {
			if failure.AgentVersionID == agent.AgentVersionID {
				item.Failures = append(item.Failures, failure)
			}
		}
		report.Agents = append(report.Agents, item)
	}
	sort.Slice(report.Agents, func(i, j int) bool { return report.Agents[i].AgentVersionID < report.Agents[j].AgentVersionID })
	return report, nil
}

func agentFacts(results evaluationsuite.Results, agent string) (evaluationsuite.Counts, []evaluationsuite.AgentScenarioCounts, bool) {
	var counts evaluationsuite.Counts
	found := false
	for _, a := range results.ByAgent {
		if a.AgentVersionID == agent {
			counts = a.Counts
			found = true
			break
		}
	}
	scenarios := []evaluationsuite.AgentScenarioCounts{}
	for _, s := range results.ByScenario {
		if s.AgentVersionID == agent {
			scenarios = append(scenarios, s)
		}
	}
	return counts, scenarios, found
}
func fairnessReasons(results evaluationsuite.Results) []string {
	if len(results.ByAgent) < 2 {
		return nil
	}
	agents := append([]evaluationsuite.AgentCounts(nil), results.ByAgent...)
	sort.Slice(agents, func(i, j int) bool { return agents[i].AgentVersionID < agents[j].AgentVersionID })
	base := agents[0].AgentVersionID
	variants := map[string]string{}
	evaluators := map[string]string{}
	keysByAgent := map[string]map[string]bool{}
	for _, t := range results.Trials {
		key := t.ScenarioVersionID + fmt.Sprintf("\x00%d", t.TrialOrdinal)
		if keysByAgent[t.AgentVersionID] == nil {
			keysByAgent[t.AgentVersionID] = map[string]bool{}
		}
		keysByAgent[t.AgentVersionID][key] = true
		if t.AgentVersionID == base {
			variants[key] = fmt.Sprintf("%s:%d", t.VariantID, t.VariantSeed)
			evaluators[key] = t.EvaluatorVersion
		}
	}
	reasons := map[string]bool{}
	for _, t := range results.Trials {
		if t.AgentVersionID == base {
			continue
		}
		key := t.ScenarioVersionID + fmt.Sprintf("\x00%d", t.TrialOrdinal)
		if variants[key] != fmt.Sprintf("%s:%d", t.VariantID, t.VariantSeed) {
			reasons["hidden_variant_distribution_mismatch"] = true
		}
		if t.EvaluatorVersion == "" || evaluators[key] == "" {
			reasons["evaluator_contract_unavailable"] = true
		} else if evaluators[key] != t.EvaluatorVersion {
			reasons["evaluator_contract_mismatch"] = true
		}
	}
	for _, a := range agents[1:] {
		if !sameSet(keysByAgent[base], keysByAgent[a.AgentVersionID]) {
			reasons["trial_distribution_mismatch"] = true
		}
	}
	contracts := map[string]map[string]bool{}
	for _, c := range results.Criteria {
		unit := ""
		if c.Unit != nil {
			unit = *c.Unit
		}
		contract := c.ScenarioVersionID + "\x00" + c.CriterionKey + "\x00" + c.Operator + "\x00" + string(c.TargetValue) + "\x00" + unit
		if contracts[c.AgentVersionID] == nil {
			contracts[c.AgentVersionID] = map[string]bool{}
		}
		contracts[c.AgentVersionID][contract] = true
	}
	for _, a := range agents[1:] {
		if !sameSet(contracts[base], contracts[a.AgentVersionID]) {
			reasons["criterion_contract_mismatch"] = true
		}
	}
	out := make([]string, 0, len(reasons))
	for reason := range reasons {
		out = append(out, reason)
	}
	sort.Strings(out)
	return out
}
func sameSet(a, b map[string]bool) bool {
	if len(a) != len(b) {
		return false
	}
	for k := range a {
		if !b[k] {
			return false
		}
	}
	return true
}
func validateVersion(in CreateVersionInput) (CreateVersionInput, error) {
	in.Description = strings.TrimSpace(in.Description)
	if len(in.Description) > 4000 {
		return in, validation("description must be at most 4000 characters")
	}
	d := &in.Definition
	if d.MinimumValidTrials < 1 || d.MinimumValidTrials > 10000 {
		return in, validation("minimum_valid_trials must be between 1 and 10000")
	}
	if !percent(d.MinimumOverallPassRate) || !percent(d.MaximumInconclusiveRate) {
		return in, validation("pass and inconclusive rates must be between 0 and 100")
	}
	if d.MinimumPerScenarioPassRate != nil && !percent(*d.MinimumPerScenarioPassRate) {
		return in, validation("minimum_per_scenario_pass_rate must be between 0 and 100")
	}
	if d.MaximumInvalidPlatformRate != nil && !percent(*d.MaximumInvalidPlatformRate) {
		return in, validation("maximum_invalid_platform_rate must be between 0 and 100")
	}
	if d.MaximumSafetyViolations < 0 {
		return in, validation("maximum_safety_violations must be non-negative")
	}
	seen := map[string]bool{}
	for i, code := range d.ForbiddenSafetyCodes {
		code = strings.TrimSpace(code)
		if code == "" || seen[code] {
			return in, validation("forbidden_safety_codes must be non-empty and unique")
		}
		seen[code] = true
		d.ForbiddenSafetyCodes[i] = code
	}
	sort.Strings(d.ForbiddenSafetyCodes)
	return in, nil
}
func validateAssessmentInput(workspace string, in CreateAssessmentInput) error {
	ids := []string{workspace, in.PolicyVersionID, in.SuiteExecutionID, in.AgentVersionID}
	if in.RegressionComparisonID != nil {
		ids = append(ids, *in.RegressionComparisonID)
	}
	return validIDs(ids...)
}
func percent(v float64) bool { return v >= 0 && v <= 100 }
func (s *Service) role(ctx context.Context, p auth.Principal, workspace string) (string, error) {
	role, err := s.roles.GetRole(ctx, s.pool, workspace, p.UserID)
	if errors.Is(err, identity.ErrNotFound) {
		return "", auth.ErrForbidden
	}
	return role, err
}
func (s *Service) requireWrite(ctx context.Context, p auth.Principal, workspace string) error {
	role, err := s.role(ctx, p, workspace)
	if err != nil {
		return err
	}
	switch role {
	case "owner", "admin", "member":
		return nil
	}
	return auth.ErrForbidden
}
func validID(id string) error { return validIDs(id) }
func validIDs(ids ...string) error {
	for _, id := range ids {
		if identity.ValidateUUID(id) != nil {
			return validation("identifiers must be valid UUIDs")
		}
	}
	return nil
}
func validation(message string) error { return fmt.Errorf("%w: %s", identity.ErrValidation, message) }
