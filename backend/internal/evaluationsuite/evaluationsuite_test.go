package evaluationsuite

import (
	"encoding/json"
	"errors"
	"reflect"
	"testing"

	"github.com/evalplane/evalplane/backend/internal/identity"
)

const (
	scenarioVersionA = "11111111-1111-4111-8111-111111111111"
	scenarioVersionB = "22222222-2222-4222-8222-222222222222"
	agentVersionA    = "33333333-3333-4333-8333-333333333333"
	agentVersionB    = "44444444-4444-4444-8444-444444444444"
)

func TestValidateVersionPinsExactScenarioVersionsInRequestOrder(t *testing.T) {
	in := CreateVersionInput{Scenarios: []ScenarioEntry{
		{ScenarioVersionID: scenarioVersionB, TrialCount: 3, SortOrder: 99},
		{ScenarioVersionID: scenarioVersionA, TrialCount: 5, SortOrder: 99},
	}}
	got, err := ValidateVersion(in)
	if err != nil {
		t.Fatal(err)
	}
	if got.Scenarios[0].ScenarioVersionID != scenarioVersionB || got.Scenarios[0].SortOrder != 0 || got.Scenarios[1].ScenarioVersionID != scenarioVersionA || got.Scenarios[1].SortOrder != 1 {
		t.Fatalf("membership was not pinned in request order: %+v", got.Scenarios)
	}
}

func TestValidateVersionRejectsDuplicateScenarioAndInvalidTrialCount(t *testing.T) {
	tests := []CreateVersionInput{
		{Scenarios: []ScenarioEntry{{ScenarioVersionID: scenarioVersionA, TrialCount: 1}, {ScenarioVersionID: scenarioVersionA, TrialCount: 2}}},
		{Scenarios: []ScenarioEntry{{ScenarioVersionID: scenarioVersionA, TrialCount: 0}}},
		{Scenarios: []ScenarioEntry{{ScenarioVersionID: scenarioVersionA, TrialCount: 21}}},
	}
	for _, in := range tests {
		if _, err := ValidateVersion(in); !errors.Is(err, identity.ErrValidation) {
			t.Fatalf("expected validation error for %+v, got %v", in, err)
		}
	}
}

func TestValidateStartPinsExactAgentsAndRejectsDuplicates(t *testing.T) {
	in := StartExecutionInput{SuiteVersionID: scenarioVersionA, AgentVersionIDs: []string{agentVersionB, agentVersionA}}
	got, err := ValidateStart(in)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(got.AgentVersionIDs, in.AgentVersionIDs) {
		t.Fatalf("agent ordering changed: %v", got.AgentVersionIDs)
	}
	in.AgentVersionIDs = []string{agentVersionA, agentVersionA}
	if _, err := ValidateStart(in); !errors.Is(err, identity.ErrValidation) {
		t.Fatalf("expected duplicate-agent rejection, got %v", err)
	}
}

func TestBuildBatchPlanReusesBatchesAndProducesExpectedRunCounts(t *testing.T) {
	version := Version{ID: scenarioVersionA, Scenarios: []ScenarioEntry{
		{ScenarioVersionID: scenarioVersionA, TrialCount: 5},
		{ScenarioVersionID: scenarioVersionB, TrialCount: 3},
	}}
	agents := []string{agentVersionA, agentVersionB}
	plan := BuildBatchPlan(version, agents)
	if len(plan) != 2 {
		t.Fatalf("batches=%d want=2", len(plan))
	}
	runs := 0
	for _, batch := range plan {
		runs += batch.RunsPerAgent * len(batch.AgentVersionIDs)
		if batch.EnvironmentMode != "isolated" {
			t.Fatalf("unexpected environment mode %q", batch.EnvironmentMode)
		}
	}
	if runs != 16 {
		t.Fatalf("runs=%d want=16", runs)
	}
	if plan[0].ScenarioVersionID != scenarioVersionA || plan[1].ScenarioVersionID != scenarioVersionB || plan[0].RunsPerAgent != 5 || plan[1].RunsPerAgent != 3 {
		t.Fatalf("plan does not retain exact suite definition: %+v", plan)
	}
	agents[0] = "changed"
	if plan[0].AgentVersionIDs[0] != agentVersionA {
		t.Fatal("plan did not copy exact pinned AgentVersion IDs")
	}
}

func TestSuiteVersionDefinitionIsCopiedAndHasNoMutationAPI(t *testing.T) {
	original := Version{ID: scenarioVersionA, Version: 1, Scenarios: []ScenarioEntry{{ScenarioVersionID: scenarioVersionA, TrialCount: 2}}}
	definition := append([]ScenarioEntry(nil), original.Scenarios...)
	definition[0].TrialCount = 4
	newVersion := Version{ID: scenarioVersionB, Version: 2, Scenarios: definition}
	if original.Version != 1 || original.Scenarios[0].TrialCount != 2 || newVersion.Version != 2 || newVersion.Scenarios[0].TrialCount != 4 {
		t.Fatalf("new definition mutated historical version: old=%+v new=%+v", original, newVersion)
	}
}

func TestAggregateIsDeterministicAndDistinguishesPendingRunning(t *testing.T) {
	rows := []TrialRow{
		{AgentVersionID: agentVersionB, ScenarioVersionID: scenarioVersionB, RunStatus: "completed", EvaluationResult: "inconclusive"},
		{AgentVersionID: agentVersionA, ScenarioVersionID: scenarioVersionA, RunStatus: "completed", EvaluationResult: "pass"},
		{AgentVersionID: agentVersionA, ScenarioVersionID: scenarioVersionA, RunStatus: "completed", EvaluationResult: "fail"},
		{AgentVersionID: agentVersionA, ScenarioVersionID: scenarioVersionB, RunStatus: "running"},
		{AgentVersionID: agentVersionB, ScenarioVersionID: scenarioVersionA, RunStatus: "queued"},
	}
	first := Aggregate(scenarioVersionA, rows)
	second := Aggregate(scenarioVersionA, append([]TrialRow(nil), rows...))
	if !reflect.DeepEqual(first, second) {
		t.Fatalf("same input produced different output: %#v %#v", first, second)
	}
	passRate, safetyRate := 50.0, 0.0
	want := Counts{Total: 5, Scheduled: 5, Completed: 3, Valid: 3, Passed: 1, Failed: 1, Inconclusive: 1, Pending: 1, Running: 1, PassRate: &passRate, SafetyViolationRate: &safetyRate}
	if !reflect.DeepEqual(first.Overall, want) {
		t.Fatalf("overall=%+v want=%+v", first.Overall, want)
	}
	if len(first.ByAgent) != 2 || len(first.ByScenario) != 4 {
		t.Fatalf("unexpected groups: agents=%d pairs=%d", len(first.ByAgent), len(first.ByScenario))
	}
}

func TestResultsDoNotGenerateSyntheticScore(t *testing.T) {
	payload, err := json.Marshal(Aggregate(scenarioVersionA, []TrialRow{{AgentVersionID: agentVersionA, ScenarioVersionID: scenarioVersionA, EvaluationResult: "pass"}}))
	if err != nil {
		t.Fatal(err)
	}
	var object map[string]any
	if err := json.Unmarshal(payload, &object); err != nil {
		t.Fatal(err)
	}
	for _, prohibited := range []string{"score", "ranking", "recommendation", "best_agent"} {
		if _, exists := object[prohibited]; exists {
			t.Fatalf("suite results unexpectedly contain %s: %s", prohibited, payload)
		}
	}
}
