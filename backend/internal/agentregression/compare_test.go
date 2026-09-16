package agentregression

import (
	"encoding/json"
	"testing"
)

const (
	baselineID  = "11111111-1111-4111-8111-111111111111"
	candidateID = "22222222-2222-4222-8222-222222222222"
	suiteID     = "33333333-3333-4333-8333-333333333333"
	scenarioID  = "44444444-4444-4444-8444-444444444444"
)

func comparison() Comparison {
	return Comparison{SuiteVersionID: suiteID, BaselineAgentVersionID: baselineID, CandidateAgentVersionID: candidateID}
}
func trial(agent string, ordinal int, result string) TrialRow {
	return TrialRow{ScenarioVersionID: scenarioID, AgentVersionID: agent, RunID: agent, EvaluationID: agent + "-evaluation", TrialOrdinal: ordinal, VariantID: "variant-" + string(rune('0'+ordinal)), VariantSeed: int64(100 + ordinal), EvaluationResult: result, EvaluatorVersion: "deterministic-evaluator:v1"}
}

func TestPairedOutcomeTransitionsAndPinnedManifest(t *testing.T) {
	rows := []TrialRow{trial(baselineID, 1, "pass"), trial(candidateID, 1, "fail"), trial(baselineID, 2, "fail"), trial(candidateID, 2, "pass"), trial(baselineID, 3, "pass"), trial(candidateID, 3, "pass")}
	got := Compare(comparison(), rows, nil)
	if got.Classification != ClassificationMixed || got.Transitions.Regressions != 1 || got.Transitions.Improvements != 1 || got.Transitions.Unchanged != 1 {
		t.Fatalf("unexpected result: %#v", got)
	}
	if got.EvidenceQuality.ComparablePairs != 3 || got.InsufficientEvidence {
		t.Fatalf("quality=%#v", got.EvidenceQuality)
	}
	manifest := got.Reproducibility[0]
	if manifest.SuiteVersionID != suiteID || manifest.BaselineAgentVersionID != baselineID || manifest.CandidateAgentVersionID != candidateID || manifest.HiddenVariantConfigIdentity == "" || manifest.EvaluatorVersion == "" {
		t.Fatalf("manifest=%#v", manifest)
	}
}

func TestInvalidPlatformVisibleAndNeverRegression(t *testing.T) {
	b := trial(baselineID, 1, "pass")
	c := trial(candidateID, 1, "fail")
	c.PlatformInvalid = true
	got := Compare(comparison(), []TrialRow{b, c}, nil)
	if got.EvidenceQuality.InvalidCandidatePairs != 1 || got.EvidenceQuality.ComparablePairs != 0 || got.Transitions.Regressions != 0 || got.Classification != ClassificationInsufficientEvidence {
		t.Fatalf("result=%#v", got)
	}
}

func TestVariantAndEvaluatorContractsMustMatch(t *testing.T) {
	b := trial(baselineID, 1, "pass")
	c := trial(candidateID, 1, "fail")
	c.VariantID = "different"
	b2 := trial(baselineID, 2, "pass")
	c2 := trial(candidateID, 2, "fail")
	c2.EvaluatorVersion = "deterministic-evaluator:v2"
	got := Compare(comparison(), []TrialRow{b, c, b2, c2}, nil)
	if got.EvidenceQuality.IncompatiblePairs != 2 || got.EvidenceQuality.ComparablePairs != 0 {
		t.Fatalf("quality=%#v", got.EvidenceQuality)
	}
}

func TestSafetyRegressionResolvedAndPersistentUseCodes(t *testing.T) {
	rows := []TrialRow{}
	for i := 1; i <= 3; i++ {
		b := trial(baselineID, i, "pass")
		c := trial(candidateID, i, "pass")
		b.SafetyCodes = []string{"resolved", "persistent"}
		c.SafetyCodes = []string{"new", "persistent"}
		rows = append(rows, b, c)
	}
	got := Compare(comparison(), rows, nil)
	if got.Classification != ClassificationMixed || got.Safety.New[0].Code != "new" || got.Safety.New[0].Count != 3 || got.Safety.Resolved[0].Code != "resolved" || got.Safety.Persistent[0].Code != "persistent" {
		t.Fatalf("safety=%#v classification=%s", got.Safety, got.Classification)
	}
}

func TestCriterionContractsAndOperatorAwareMetricDeltas(t *testing.T) {
	rows := []TrialRow{}
	criteria := []CriterionRow{}
	for i := 1; i <= 3; i++ {
		rows = append(rows, trial(baselineID, i, "pass"), trial(candidateID, i, "pass"))
		criteria = append(criteria,
			CriterionRow{ScenarioVersionID: scenarioID, AgentVersionID: baselineID, TrialOrdinal: i, VariantID: "x", CriterionKey: "p95_latency", Operator: "<=", TargetValue: json.RawMessage(`500`), Status: "passed", ObservedState: "present", ObservedValue: json.RawMessage(`400`)},
			CriterionRow{ScenarioVersionID: scenarioID, AgentVersionID: candidateID, TrialOrdinal: i, VariantID: "x", CriterionKey: "p95_latency", Operator: "<=", TargetValue: json.RawMessage(`500`), Status: "failed", ObservedState: "present", ObservedValue: json.RawMessage(`550`)})
	}
	// An incompatible target is deliberately not merged with the baseline contract.
	criteria = append(criteria, CriterionRow{ScenarioVersionID: scenarioID, AgentVersionID: candidateID, TrialOrdinal: 1, CriterionKey: "availability", Operator: ">=", TargetValue: json.RawMessage(`99`), Status: "passed"})
	got := Compare(comparison(), rows, criteria)
	if len(got.Criteria) != 1 || got.Criteria[0].Regressions != 3 || got.Criteria[0].MetricDeltas[0].Delta != 150 || got.Criteria[0].MetricDeltas[0].Movement != "away_from_requirement" {
		t.Fatalf("criteria=%#v", got.Criteria)
	}
}

func TestTooFewComparablePairsAreExplicit(t *testing.T) {
	got := Compare(comparison(), []TrialRow{trial(baselineID, 1, "pass"), trial(candidateID, 1, "pass")}, nil)
	if !got.InsufficientEvidence || got.Classification != ClassificationInsufficientEvidence {
		t.Fatalf("result=%#v", got)
	}
}

func TestInconclusiveIsNotTreatedAsFailure(t *testing.T) {
	rows := []TrialRow{trial(baselineID, 1, "inconclusive"), trial(candidateID, 1, "fail"), trial(baselineID, 2, "inconclusive"), trial(candidateID, 2, "pass"), trial(baselineID, 3, "pass"), trial(candidateID, 3, "inconclusive")}
	got := Compare(comparison(), rows, nil)
	if got.Transitions.Regressions != 0 || got.Transitions.Improvements != 0 || got.Transitions.InconclusiveTransitions != 1 || got.Transitions.ResolvedInconclusive != 1 || got.Transitions.CandidateInconclusive != 1 {
		t.Fatalf("transitions=%#v", got.Transitions)
	}
}

func TestNewSafetyViolationRemainsRegressionWithSmallSample(t *testing.T) {
	b := trial(baselineID, 1, "pass")
	c := trial(candidateID, 1, "pass")
	c.SafetyCodes = []string{"unauthorized_action_attempt"}
	got := Compare(comparison(), []TrialRow{b, c}, nil)
	if got.Classification != ClassificationRegressed || !got.InsufficientEvidence {
		t.Fatalf("result=%#v", got)
	}
}
