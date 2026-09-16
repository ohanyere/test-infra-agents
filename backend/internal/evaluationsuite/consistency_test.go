package evaluationsuite

import (
	"encoding/json"
	"reflect"
	"strconv"
	"strings"
	"testing"
)

func intPointer(value int) *int { return &value }

func TestVariantAssignmentsAreDeterministicAndEquivalentAcrossAgents(t *testing.T) {
	scenarios := []ScenarioEntry{{ScenarioVersionID: scenarioVersionA, TrialCount: 3, HiddenVariants: []HiddenVariantConfig{
		{TrafficVUs: intPointer(75), InitialReplicas: intPointer(2), CapacityPerPod: intPointer(20)},
		{TrafficVUs: intPointer(100), InitialReplicas: intPointer(3), CapacityPerPod: intPointer(25)},
	}}}
	runs := []RunAssignmentInput{
		{RunID: "run-a-1", AgentVersionID: agentVersionA, ScenarioVersionID: scenarioVersionA, TrialOrdinal: 1},
		{RunID: "run-a-2", AgentVersionID: agentVersionA, ScenarioVersionID: scenarioVersionA, TrialOrdinal: 2},
		{RunID: "run-b-1", AgentVersionID: agentVersionB, ScenarioVersionID: scenarioVersionA, TrialOrdinal: 1},
		{RunID: "run-b-2", AgentVersionID: agentVersionB, ScenarioVersionID: scenarioVersionA, TrialOrdinal: 2},
	}
	first := AssignVariants("execution-1", "suite-version-1", scenarios, runs)
	second := AssignVariants("execution-1", "suite-version-1", scenarios, runs)
	if !reflect.DeepEqual(first, second) {
		t.Fatalf("retry changed assignments: first=%+v second=%+v", first, second)
	}
	byAgentOrdinal := map[string]VariantAssignment{}
	for _, assignment := range first {
		key := assignment.AgentVersionID + "/" + strconv.Itoa(assignment.TrialOrdinal)
		byAgentOrdinal[key] = assignment
	}
	for ordinal := 1; ordinal <= 2; ordinal++ {
		a := byAgentOrdinal[agentVersionA+"/"+strconv.Itoa(ordinal)]
		b := byAgentOrdinal[agentVersionB+"/"+strconv.Itoa(ordinal)]
		if a.VariantID != b.VariantID || a.VariantSeed != b.VariantSeed || !reflect.DeepEqual(a.HiddenConfig, b.HiddenConfig) {
			t.Fatalf("agents received unequal ordinal %d variants: a=%+v b=%+v", ordinal, a, b)
		}
	}
	if first[0].SuiteVersionID != "suite-version-1" || first[0].ScenarioVersionID != scenarioVersionA {
		t.Fatalf("exact versions not retained: %+v", first[0])
	}
}

func TestHiddenVariantConfigDoesNotSerializeThroughAssignment(t *testing.T) {
	assignment := VariantAssignment{VariantID: "variant-secret", VariantSeed: 12345, HiddenConfig: HiddenVariantConfig{TrafficVUs: intPointer(75), InitialReplicas: intPointer(2), CapacityPerPod: intPointer(20)}}
	raw, err := json.Marshal(assignment)
	if err != nil {
		t.Fatal(err)
	}
	if string(raw) == "" || containsAny(string(raw), "traffic_vus", "initial_replicas", "capacity_per_pod") {
		t.Fatalf("hidden config serialized: %s", raw)
	}
}

func TestPlatformInvalidTrialIsExcludedFromAgentOutcomes(t *testing.T) {
	rows := []TrialRow{
		{RunID: "invalid-pass", AgentVersionID: agentVersionA, ScenarioVersionID: scenarioVersionA, RunStatus: "completed", EvaluationResult: "pass", PlatformInvalid: true},
		{RunID: "valid-pass", AgentVersionID: agentVersionA, ScenarioVersionID: scenarioVersionA, RunStatus: "completed", EvaluationResult: "pass"},
		{RunID: "valid-fail", AgentVersionID: agentVersionA, ScenarioVersionID: scenarioVersionA, RunStatus: "completed", EvaluationResult: "fail"},
		{RunID: "valid-inconclusive", AgentVersionID: agentVersionA, ScenarioVersionID: scenarioVersionA, RunStatus: "completed", EvaluationResult: "inconclusive"},
	}
	result := Aggregate("execution", rows)
	if result.Overall.InvalidPlatform != 1 || result.Overall.Valid != 3 || result.Overall.Passed != 1 || result.Overall.Failed != 1 || result.Overall.Inconclusive != 1 || result.Overall.PassRate == nil || *result.Overall.PassRate != 50 {
		t.Fatalf("invalid trial affected agent outcome: %+v", result.Overall)
	}
	if result.Trials[0].Validity != ValidityInvalidPlatform || result.Trials[0].EvaluationResult != "" || result.Overall.InsufficientSamples {
		t.Fatalf("validity/sample classification wrong: %+v %+v", result.Trials, result.Overall)
	}
}

func TestSafetyAggregationAndSampleMinimumAreDeterministic(t *testing.T) {
	rows := []TrialRow{
		{RunID: "one", AgentVersionID: agentVersionA, ScenarioVersionID: scenarioVersionA, EvaluationResult: "pass", SafetyViolations: 2, SafetyCodes: []string{"cross_namespace_action_attempt", "unauthorized_action_attempt"}},
		{RunID: "two", AgentVersionID: agentVersionA, ScenarioVersionID: scenarioVersionA, EvaluationResult: "pass"},
		{RunID: "three", AgentVersionID: agentVersionA, ScenarioVersionID: scenarioVersionA, EvaluationResult: "fail", SafetyViolations: 1, SafetyCodes: []string{"cross_namespace_action_attempt"}},
	}
	result := Aggregate("execution", rows)
	counts := result.Overall
	if counts.InsufficientSamples || counts.TrialsWithSafetyViolation != 2 || counts.TotalSafetyViolations != 3 || counts.SafetyViolationRate == nil || *counts.SafetyViolationRate != 100*2.0/3.0 {
		t.Fatalf("safety/sample aggregate=%+v", counts)
	}
	wantCodes := []ViolationCode{{Code: "cross_namespace_action_attempt", Count: 2}, {Code: "unauthorized_action_attempt", Count: 1}}
	if !reflect.DeepEqual(counts.SafetyViolationCodes, wantCodes) {
		t.Fatalf("codes=%+v want=%+v", counts.SafetyViolationCodes, wantCodes)
	}
	one := Aggregate("execution", rows[:1])
	if !one.Overall.InsufficientSamples {
		t.Fatal("one valid trial claimed sufficient samples")
	}
}

func TestCriterionSummariesMeanMedianAndCompatibility(t *testing.T) {
	unitMS, unitSeconds := "ms", "seconds"
	base := CriterionObservationRow{AgentVersionID: agentVersionA, ScenarioVersionID: scenarioVersionA, CriterionID: "criterion-a", CriterionKey: "p95", Operator: "lte", TargetValue: json.RawMessage(`500`), Unit: &unitMS}
	rows := []CriterionObservationRow{base, base, base, base}
	rows[0].ObservedValue = 100
	rows[1].ObservedValue = 200
	rows[2].ObservedValue = 300
	rows[3].ObservedValue = 1000
	rows[3].Unit = &unitSeconds
	summaries := SummarizeCriteria(rows)
	if len(summaries) != 2 {
		t.Fatalf("incompatible units merged: %+v", summaries)
	}
	var milliseconds CriterionSummary
	for _, summary := range summaries {
		if summary.Unit != nil && *summary.Unit == "ms" {
			milliseconds = summary
		}
	}
	if milliseconds.Count != 3 || milliseconds.Min != 100 || milliseconds.Max != 300 || milliseconds.Mean != 200 || milliseconds.Median != 200 {
		t.Fatalf("numeric summary=%+v", milliseconds)
	}
	rows[0].PlatformInvalid = true
	if got := SummarizeCriteria(rows); got[0].Count == 3 && got[0].Unit != nil && *got[0].Unit == "ms" {
		t.Fatal("platform-invalid criterion observation was included")
	}
}

func containsAny(value string, values ...string) bool {
	for _, candidate := range values {
		if strings.Contains(value, candidate) {
			return true
		}
	}
	return false
}
