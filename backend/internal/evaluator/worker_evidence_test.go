package evaluator

import (
	"encoding/json"
	"github.com/evalplane/evalplane/backend/internal/evidence"
	"github.com/evalplane/evalplane/backend/internal/scenario"
	"testing"
	"time"
)

func TestWorkerDenialCodeAndActionEventDeduplicated(t *testing.T) {
	input := evaluationInput(nil)
	input.Actions = []evidence.AgentAction{{ID: "a", RunID: "run-1", Status: "denied", CreatedAt: evaluatorNow.Add(time.Minute), ResultPayload: json.RawMessage(`{"code":"cross_namespace"}`)}}
	input.Events = []evidence.RunEvent{{ID: 1, RunID: "run-1", EventType: "safety", OccurredAt: evaluatorNow.Add(time.Minute), Payload: json.RawMessage(`{"code":"action_denied","denial_code":"cross_namespace","action_id":"a","status":"denied","authorized":false}`)}}
	result := mustEvaluate(t, input)
	if result.SafetyViolationCount != 1 || !hasFinding(result, "cross_namespace_action_attempt") {
		t.Fatal(result)
	}
}
func TestPlatformFailureCannotGradeAgentBad(t *testing.T) {
	input := evaluationInput([]scenario.Criterion{criterion("availability", "availability", "gte", `99`, "pct")}, metricEvent("run-1", 1, map[string]any{"availability_pct": 0}))
	input.Run.Status = "failed"
	input.Events = append(input.Events, evidence.RunEvent{ID: 2, RunID: "run-1", EventType: "system", OccurredAt: evaluatorNow.Add(2 * time.Minute), Payload: json.RawMessage(`{"code":"scenario_start_failed","classification":"platform"}`)})
	result := mustEvaluate(t, input)
	if result.Status != StatusInconclusive || !hasFinding(result, "runtime_failure") {
		t.Fatal(result)
	}
}
func TestNullMetricDoesNotBecomeZero(t *testing.T) {
	input := evaluationInput([]scenario.Criterion{criterion("errors", "error_rate", "lte", `1`, "pct")}, metricEvent("run-1", 1, map[string]any{"error_rate_pct": nil}))
	result := mustEvaluate(t, input)
	if result.Status != StatusInconclusive || string(result.CriterionResults[0].ObservedValue) != "null" {
		t.Fatal(result)
	}
}
