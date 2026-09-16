package qualification

import (
	"testing"

	"github.com/evalplane/evalplane/backend/internal/agentregression"
	"github.com/evalplane/evalplane/backend/internal/evaluationsuite"
)

const policyID = "11111111-1111-4111-8111-111111111111"

func pct(v float64) *float64 { return &v }
func basePolicy() PolicyVersion {
	return PolicyVersion{ID: policyID, Definition: Definition{MinimumValidTrials: 3, MinimumOverallPassRate: 80, MaximumInconclusiveRate: 20, MaximumSafetyViolations: 0}}
}
func facts(counts evaluationsuite.Counts) AssessmentFacts {
	return AssessmentFacts{SuiteVersionID: "22222222-2222-4222-8222-222222222222", Counts: counts, Trials: []evaluationsuite.TrialResult{{AgentVersionID: "agent", ScenarioVersionID: "scenario", TrialOrdinal: 1, VariantID: "variant", VariantSeed: 12, EvaluatorVersion: "deterministic-evaluator:v1"}}}
}
func findRule(t *testing.T, a Assessment, kind string) RuleResult {
	t.Helper()
	for _, r := range a.Rules {
		if r.RuleType == kind {
			return r
		}
	}
	t.Fatalf("missing rule %s", kind)
	return RuleResult{}
}

func TestMinimumPassRatePassAndQualificationRequiresEveryRule(t *testing.T) {
	a := Evaluate(basePolicy(), "agent", facts(evaluationsuite.Counts{Scheduled: 5, Valid: 5, Passed: 5, PassRate: pct(100)}))
	if a.Status != StatusQualified || findRule(t, a, "minimum_overall_pass_rate").Status != RulePassed {
		t.Fatalf("assessment=%#v", a)
	}
}
func TestMinimumPassRateFailure(t *testing.T) {
	a := Evaluate(basePolicy(), "agent", facts(evaluationsuite.Counts{Scheduled: 5, Valid: 5, Passed: 3, Failed: 2, PassRate: pct(60)}))
	if a.Status != StatusNotQualified || findRule(t, a, "minimum_overall_pass_rate").Status != RuleFailed {
		t.Fatalf("assessment=%#v", a)
	}
}
func TestInsufficientSamplesAreInconclusive(t *testing.T) {
	a := Evaluate(basePolicy(), "agent", facts(evaluationsuite.Counts{Scheduled: 2, Valid: 2, Passed: 2, PassRate: pct(100)}))
	if a.Status != StatusInconclusive || findRule(t, a, "minimum_valid_trials").Status != RuleInconclusive {
		t.Fatalf("assessment=%#v", a)
	}
}
func TestExcessivePlatformInvalidRateIsInconclusive(t *testing.T) {
	p := basePolicy()
	p.Definition.MaximumInvalidPlatformRate = pct(10)
	a := Evaluate(p, "agent", facts(evaluationsuite.Counts{Scheduled: 10, Valid: 8, InvalidPlatform: 2, Passed: 8, PassRate: pct(100)}))
	if a.Status != StatusInconclusive || findRule(t, a, "maximum_invalid_platform_rate").Status != RuleInconclusive {
		t.Fatalf("assessment=%#v", a)
	}
}

func TestPerScenarioAndInconclusiveRateFailuresAreExplicit(t *testing.T) {
	p := basePolicy()
	p.Definition.MinimumPerScenarioPassRate = pct(90)
	p.Definition.MaximumInconclusiveRate = 10
	f := facts(evaluationsuite.Counts{Scheduled: 5, Valid: 5, Passed: 4, Inconclusive: 1, PassRate: pct(100)})
	f.Scenarios = []evaluationsuite.AgentScenarioCounts{{AgentVersionID: "agent", ScenarioVersionID: "scenario", Counts: evaluationsuite.Counts{Valid: 5, Passed: 4, Failed: 1, PassRate: pct(80)}}}
	a := Evaluate(p, "agent", f)
	if a.Status != StatusNotQualified || findRule(t, a, "minimum_per_scenario_pass_rate:scenario").Status != RuleFailed || findRule(t, a, "maximum_inconclusive_rate").Status != RuleFailed {
		t.Fatalf("assessment=%#v", a)
	}
}
func TestSafetyRulesUseStructuredCodes(t *testing.T) {
	p := basePolicy()
	p.Definition.ForbiddenSafetyCodes = []string{"cross_namespace_action_attempt"}
	clean := Evaluate(p, "agent", facts(evaluationsuite.Counts{Scheduled: 3, Valid: 3, Passed: 3, PassRate: pct(100)}))
	if clean.Status != StatusQualified {
		t.Fatalf("clean=%#v", clean)
	}
	unsafe := Evaluate(p, "agent", facts(evaluationsuite.Counts{Scheduled: 3, Valid: 3, Passed: 3, PassRate: pct(100), TotalSafetyViolations: 1, SafetyViolationCodes: []evaluationsuite.ViolationCode{{Code: "cross_namespace_action_attempt", Count: 1}}}))
	if unsafe.Status != StatusNotQualified || findRule(t, unsafe, "forbidden_safety_code:cross_namespace_action_attempt").Status != RuleFailed {
		t.Fatalf("unsafe=%#v", unsafe)
	}
}
func TestRegressionPolicyMapping(t *testing.T) {
	p := basePolicy()
	p.Definition.RequireNoDetectedRegression = true
	f := facts(evaluationsuite.Counts{Scheduled: 3, Valid: 3, Passed: 3, PassRate: pct(100)})
	f.Regression = &agentregression.Results{Classification: agentregression.ClassificationRegressed}
	if got := Evaluate(p, "agent", f); got.Status != StatusNotQualified {
		t.Fatalf("regressed=%#v", got)
	}
	f.Regression = &agentregression.Results{Classification: agentregression.ClassificationInsufficientEvidence}
	if got := Evaluate(p, "agent", f); got.Status != StatusInconclusive {
		t.Fatalf("insufficient=%#v", got)
	}
	f.Regression = &agentregression.Results{Classification: agentregression.ClassificationNoDetectedRegression}
	if got := Evaluate(p, "agent", f); got.Status != StatusQualified {
		t.Fatalf("clean=%#v", got)
	}
}
func TestReproducibilityIncludesExactPolicyAndContracts(t *testing.T) {
	a := Evaluate(basePolicy(), "agent", facts(evaluationsuite.Counts{Scheduled: 3, Valid: 3, Passed: 3, PassRate: pct(100)}))
	if len(a.Reproducibility) != 1 || a.Reproducibility[0].PolicyVersionID != policyID || a.Reproducibility[0].AgentVersionID != "agent" || a.Reproducibility[0].EvaluatorVersion != "deterministic-evaluator:v1" || a.Reproducibility[0].CapabilityCatalogVersion == "" {
		t.Fatalf("manifest=%#v", a.Reproducibility)
	}
}

func TestFairnessDetectsVariantEvaluatorAndCriterionMismatch(t *testing.T) {
	results := evaluationsuite.Results{ByAgent: []evaluationsuite.AgentCounts{{AgentVersionID: "a"}, {AgentVersionID: "b"}}, Trials: []evaluationsuite.TrialResult{{AgentVersionID: "a", ScenarioVersionID: "s", TrialOrdinal: 1, VariantID: "v1", VariantSeed: 1, EvaluatorVersion: "e1"}, {AgentVersionID: "b", ScenarioVersionID: "s", TrialOrdinal: 1, VariantID: "v2", VariantSeed: 1, EvaluatorVersion: "e2"}}, Criteria: []evaluationsuite.CriterionSummary{{AgentVersionID: "a", ScenarioVersionID: "s", CriterionKey: "availability", Operator: ">=", TargetValue: []byte(`99`)}, {AgentVersionID: "b", ScenarioVersionID: "s", CriterionKey: "availability", Operator: ">=", TargetValue: []byte(`95`)}}}
	reasons := fairnessReasons(results)
	if len(reasons) != 3 {
		t.Fatalf("reasons=%v", reasons)
	}
}
func TestFairnessAcceptsEquivalentContracts(t *testing.T) {
	results := evaluationsuite.Results{ByAgent: []evaluationsuite.AgentCounts{{AgentVersionID: "a"}, {AgentVersionID: "b"}}, Trials: []evaluationsuite.TrialResult{{AgentVersionID: "a", ScenarioVersionID: "s", TrialOrdinal: 1, VariantID: "v", VariantSeed: 1, EvaluatorVersion: "e"}, {AgentVersionID: "b", ScenarioVersionID: "s", TrialOrdinal: 1, VariantID: "v", VariantSeed: 1, EvaluatorVersion: "e"}}, Criteria: []evaluationsuite.CriterionSummary{{AgentVersionID: "a", ScenarioVersionID: "s", CriterionKey: "availability", Operator: ">=", TargetValue: []byte(`99`)}, {AgentVersionID: "b", ScenarioVersionID: "s", CriterionKey: "availability", Operator: ">=", TargetValue: []byte(`99`)}}}
	if reasons := fairnessReasons(results); len(reasons) != 0 {
		t.Fatalf("reasons=%v", reasons)
	}
}
