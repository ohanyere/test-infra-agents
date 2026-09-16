package failuretaxonomy

import "testing"

func TestKnownCodesMapDeterministically(t *testing.T) {
	tests := map[string]string{
		"unauthorized_action_attempt": CategorySafety, "cross_namespace_action_attempt": CategorySafety,
		"criterion_failed:error_rate": CategoryCriterion, "criterion_inconclusive:p95": CategoryCriterion,
		"telemetry_unavailable": CategoryTelemetry, "worker_lease_expired": CategoryPlatform,
		"run_abandoned": CategoryPlatform, "runtime_failure": CategoryPlatform,
		"agent_gateway_timeout": CategoryAgentBehavior, "action_failed": CategoryExecution,
		"new_structured_code": CategoryUnknown,
	}
	for code, want := range tests {
		if got := Category(code); got != want {
			t.Fatalf("Category(%q)=%q want %q", code, got, want)
		}
	}
}

func TestAggregateDeduplicatesSourcesAndSeparatesPlatform(t *testing.T) {
	rows := []Occurrence{
		{SourceID: "f1", RunID: "r1", AgentVersionID: "a", ScenarioVersionID: "s1", Code: "criterion_failed:error_rate"},
		{SourceID: "f1", RunID: "r1", AgentVersionID: "a", ScenarioVersionID: "s1", Code: "criterion_failed:error_rate"},
		{SourceID: "f2", RunID: "r1", AgentVersionID: "a", ScenarioVersionID: "s1", Code: "criterion_failed:error_rate"},
		{SourceID: "f3", RunID: "r2", AgentVersionID: "a", ScenarioVersionID: "s1", Code: "criterion_failed:error_rate"},
		{SourceID: "e1", RunID: "r3", AgentVersionID: "a", ScenarioVersionID: "s2", Code: "run_abandoned", PlatformInvalid: true},
	}
	got := Aggregate("execution", rows)
	if len(got.Failures) != 2 {
		t.Fatalf("failures=%#v", got.Failures)
	}
	var criterion, platform *FailureGroup
	for i := range got.Failures {
		switch got.Failures[i].Category {
		case CategoryCriterion:
			criterion = &got.Failures[i]
		case CategoryPlatform:
			platform = &got.Failures[i]
		}
	}
	if criterion == nil || criterion.AffectedTrials != 2 || criterion.TotalOccurrences != 3 || !criterion.AgentRelated {
		t.Fatalf("criterion=%#v", criterion)
	}
	if platform == nil || !platform.PlatformRelated || platform.AgentRelated || platform.AffectedTrials != 1 {
		t.Fatalf("platform=%#v", platform)
	}
}
