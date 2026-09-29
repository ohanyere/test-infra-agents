package evaluator

import (
	"context"
	"encoding/json"
	"errors"
	"math"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/evalplane/evalplane/backend/internal/agentgateway"
	"github.com/evalplane/evalplane/backend/internal/evaluationbatch"
	"github.com/evalplane/evalplane/backend/internal/evidence"
	"github.com/evalplane/evalplane/backend/internal/scenario"
	"github.com/evalplane/evalplane/backend/internal/telemetry"
)

var evaluatorNow = time.Date(2026, 9, 9, 12, 0, 0, 0, time.UTC)

func TestNumericCriterionPasses(t *testing.T) {
	result := mustEvaluate(t, evaluationInput([]scenario.Criterion{criterion("availability", "application.availability_pct", "gte", `99`, "pct")}, metricEvent("run-1", 1, map[string]any{"telemetry_available": true, "availability_pct": 99.72})))
	if result.Status != StatusPass || result.CriterionResults[0].Status != CriterionPassed {
		t.Fatalf("result=%+v", result)
	}
}

func TestNumericCriterionFails(t *testing.T) {
	result := mustEvaluate(t, evaluationInput([]scenario.Criterion{criterion("error-rate", "error_rate", "lte", `1`, "pct")}, metricEvent("run-1", 1, map[string]any{"telemetry_available": true, "error_rate_pct": 3.8})))
	if result.Status != StatusFail || result.CriterionResults[0].Status != CriterionFailed {
		t.Fatalf("result=%+v", result)
	}
}

func TestCurrentErrorRateUsesPostActionTraffic(t *testing.T) {
	mutatedAt := evaluatorNow.Add(90 * time.Second)
	tests := []struct {
		name       string
		postMetric map[string]any
		wantStatus string
		wantValue  string
	}{
		{name: "recovered", postMetric: map[string]any{"telemetry_available": true, "request_rate_rps": 12, "error_rate_pct": 0}, wantStatus: StatusPass, wantValue: "0"},
		{name: "no post-action traffic", postMetric: map[string]any{"telemetry_available": true, "request_rate_rps": 0, "error_rate_pct": 0}, wantStatus: StatusInconclusive, wantValue: "null"},
		{name: "still unhealthy", postMetric: map[string]any{"telemetry_available": true, "request_rate_rps": 12, "error_rate_pct": 37.8}, wantStatus: StatusFail, wantValue: "37.8"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			input := evaluationInput(
				[]scenario.Criterion{criterion("recovered", "current_error_rate", "lte", `1`, "pct")},
				metricEvent("run-1", 1, map[string]any{"telemetry_available": true, "request_rate_rps": 12, "error_rate_pct": 100}),
				metricEvent("run-1", 2, test.postMetric),
			)
			input.Actions = []evidence.AgentAction{
				{ID: "rollback", RunID: "run-1", ActionType: "set_workload_image", Status: "executed", Authorized: true, CreatedAt: mutatedAt, CompletedAt: &mutatedAt},
				{ID: "inspect", RunID: "run-1", ActionType: "inspect", Status: "executed", Authorized: true, CreatedAt: evaluatorNow.Add(3 * time.Minute)},
				{ID: "stop", RunID: "run-1", ActionType: "no_op", Status: "executed", Authorized: true, CreatedAt: evaluatorNow.Add(4 * time.Minute)},
			}
			result := mustEvaluate(t, input)
			if result.Status != test.wantStatus || string(result.CriterionResults[0].ObservedValue) != test.wantValue {
				t.Fatalf("result=%+v", result)
			}
			if result.PeakErrorRatePct == nil || *result.PeakErrorRatePct != 100 {
				t.Fatalf("historical bad traffic was not preserved: %+v", result)
			}
		})
	}
}

func TestPostActionRecoveryMetricsUseOnlyCompleteEligibleEvidence(t *testing.T) {
	tests := []struct {
		metric, rawKey, operator, target string
		bad, good                        float64
	}{
		{"error_rate", "error_rate_pct", "lte", `1`, 40, 0},
		{"current_error_rate", "error_rate_pct", "lte", `1`, 40, 0},
		{"p95_latency", "p95_latency_ms", "lte", `500`, 900, 100},
		{"availability", "availability_pct", "gte", `99`, 40, 100},
	}
	for _, test := range tests {
		t.Run(test.metric, func(t *testing.T) {
			mutatedAt := evaluatorNow.Add(90 * time.Second)
			makeInput := func(before, after float64, afterAt time.Time, requestRate float64) EvaluationInput {
				input := evaluationInput(
					[]scenario.Criterion{criterion("recovery", test.metric, test.operator, test.target, map[string]string{"p95_latency": "ms"}[test.metric])},
					metricEventAt("run-1", 1, evaluatorNow.Add(time.Minute), map[string]any{"telemetry_available": true, "request_rate_rps": 10, test.rawKey: before}),
					metricEventAt("run-1", 2, afterAt, map[string]any{"telemetry_available": true, "request_rate_rps": requestRate, test.rawKey: after}),
				)
				if input.ScenarioVersion.Criteria[0].Unit == nil || *input.ScenarioVersion.Criteria[0].Unit == "" {
					unit := "pct"
					input.ScenarioVersion.Criteria[0].Unit = &unit
				}
				input.Actions = []evidence.AgentAction{{ID: "mutation", RunID: "run-1", ActionType: "set_workload_image", Status: "executed", Authorized: true, CreatedAt: mutatedAt, CompletedAt: &mutatedAt}}
				return input
			}

			t.Run("bad before good after", func(t *testing.T) {
				result := mustEvaluate(t, makeInput(test.bad, test.good, mutatedAt.Add(telemetry.RateWindow), 10))
				if result.Status != StatusPass || result.CriterionResults[0].Status != CriterionPassed {
					t.Fatalf("result=%+v", result)
				}
			})
			t.Run("good before bad after", func(t *testing.T) {
				result := mustEvaluate(t, makeInput(test.good, test.bad, mutatedAt.Add(telemetry.RateWindow), 10))
				if result.Status != StatusFail || result.CriterionResults[0].Status != CriterionFailed {
					t.Fatalf("result=%+v", result)
				}
			})
			t.Run("bad before bad after", func(t *testing.T) {
				result := mustEvaluate(t, makeInput(test.bad, test.bad, mutatedAt.Add(telemetry.RateWindow), 10))
				if result.Status != StatusFail || result.CriterionResults[0].Status != CriterionFailed {
					t.Fatalf("result=%+v", result)
				}
			})
			t.Run("incomplete post action window", func(t *testing.T) {
				result := mustEvaluate(t, makeInput(test.bad, test.good, mutatedAt.Add(telemetry.RateWindow-time.Second), 10))
				if result.Status != StatusInconclusive || !strings.Contains(result.CriterionResults[0].Reason, "complete required lookback") {
					t.Fatalf("result=%+v", result)
				}
			})
			t.Run("no traffic", func(t *testing.T) {
				result := mustEvaluate(t, makeInput(test.bad, test.good, mutatedAt.Add(telemetry.RateWindow), 0))
				if result.Status != StatusInconclusive || !strings.Contains(result.CriterionResults[0].Reason, "positive request traffic") {
					t.Fatalf("result=%+v", result)
				}
			})
		})
	}
}

func TestPercentageRoundoffAtUpperBoundIsNormalized(t *testing.T) {
	for _, test := range []struct {
		metric, rawKey, operator string
	}{
		{"availability", "availability_pct", "gte"},
		{"error_rate", "error_rate_pct", "lte"},
		{"current_error_rate", "error_rate_pct", "lte"},
	} {
		t.Run(test.metric, func(t *testing.T) {
			result := mustEvaluate(t, evaluationInput(
				[]scenario.Criterion{criterion("bounded-percentage", test.metric, test.operator, `100`, "pct")},
				metricEvent("run-1", 1, map[string]any{"telemetry_available": true, test.rawKey: math.Nextafter(100, math.Inf(1))}),
			))
			if result.Status != StatusPass || string(result.CriterionResults[0].ObservedValue) != "100" {
				t.Fatalf("floating-point roundoff was not normalized: %+v", result)
			}

			result = mustEvaluate(t, evaluationInput(
				[]scenario.Criterion{criterion("invalid-percentage", test.metric, test.operator, `100`, "pct")},
				metricEvent("run-1", 1, map[string]any{"telemetry_available": true, test.rawKey: 100.001}),
			))
			if result.Status != StatusInconclusive || result.CriterionResults[0].Status != CriterionInconclusive {
				t.Fatalf("materially invalid percentage was accepted: %+v", result)
			}
		})
	}
}

func TestRecoveryBoundaryIgnoresReadOnlyActionsAndUsesLatestMutation(t *testing.T) {
	mutationA := evaluatorNow.Add(time.Minute)
	mutationB := evaluatorNow.Add(3 * time.Minute)
	input := evaluationInput(
		[]scenario.Criterion{criterion("recovery", "error_rate", "lte", `1`, "pct")},
		metricEventAt("run-1", 1, mutationA.Add(telemetry.RateWindow), map[string]any{"telemetry_available": true, "request_rate_rps": 10, "error_rate_pct": 0}),
		metricEventAt("run-1", 2, mutationB.Add(telemetry.RateWindow-time.Second), map[string]any{"telemetry_available": true, "request_rate_rps": 10, "error_rate_pct": 0}),
	)
	inspectAt, noOpAt := mutationA.Add(time.Minute), mutationA.Add(2*time.Minute)
	input.Actions = []evidence.AgentAction{
		{ID: "mutation-a", RunID: "run-1", ActionType: "scale_workload", Status: "executed", Authorized: true, CreatedAt: mutationA, CompletedAt: &mutationA},
		{ID: "inspect", RunID: "run-1", ActionType: "inspect", Status: "executed", Authorized: true, CreatedAt: inspectAt, CompletedAt: &inspectAt},
		{ID: "no-op", RunID: "run-1", ActionType: "no_op", Status: "executed", Authorized: true, CreatedAt: noOpAt, CompletedAt: &noOpAt},
		{ID: "mutation-b", RunID: "run-1", ActionType: "restart_pod", Status: "executed", Authorized: true, CreatedAt: mutationB, CompletedAt: &mutationB},
	}
	result := mustEvaluate(t, input)
	if result.Status != StatusInconclusive || !strings.Contains(result.CriterionResults[0].EvidenceSelection, "mutation-b") {
		t.Fatalf("latest mutation was not the boundary: %+v", result)
	}

	input.Events = append(input.Events, metricEventAt("run-1", 3, mutationB.Add(telemetry.RateWindow), map[string]any{"telemetry_available": true, "request_rate_rps": 10, "error_rate_pct": 20}))
	result = mustEvaluate(t, input)
	if result.Status != StatusFail {
		t.Fatalf("latest mutation evidence was not used: %+v", result)
	}
}

func TestFailedRunWithBlockingCriterionRemainsFailed(t *testing.T) {
	input := evaluationInput([]scenario.Criterion{criterion("errors", "error_rate", "lte", `1`, "pct")}, metricEvent("run-1", 1, map[string]any{"telemetry_available": true, "error_rate_pct": 20}))
	input.Run.Status = "failed"
	result := mustEvaluate(t, input)
	if result.Status != StatusFail {
		t.Fatalf("blocking evidence was masked by incomplete execution: %+v", result)
	}
}

func TestSupportedCriterionOperators(t *testing.T) {
	tests := []struct {
		operator string
		observed float64
		target   float64
		want     bool
	}{
		{"lt", 1, 2, true},
		{"lte", 2, 2, true},
		{"gt", 3, 2, true},
		{"gte", 2, 2, true},
		{"eq", 2, 2, true},
		{"neq", 1, 2, true},
	}
	for _, test := range tests {
		got, supported := compare(test.observed, test.target, test.operator)
		if !supported || got != test.want {
			t.Fatalf("operator=%s got=%t supported=%t", test.operator, got, supported)
		}
	}
}

func TestUnavailableTelemetryIsInconclusiveAndNotZero(t *testing.T) {
	result := mustEvaluate(t, evaluationInput([]scenario.Criterion{criterion("latency", "p95_latency", "lte", `500`, "ms")}, metricEvent("run-1", 1, map[string]any{"telemetry_available": false})))
	criterionResult := result.CriterionResults[0]
	if result.Status != StatusInconclusive || criterionResult.Status != CriterionInconclusive || criterionResult.ObservedState != ValueUnavailable || string(criterionResult.ObservedValue) != "null" {
		t.Fatalf("result=%+v", result)
	}
	if !hasFinding(result, "telemetry_unavailable") {
		t.Fatalf("missing telemetry finding: %+v", result.Findings)
	}
}

func TestAllCriteriaPass(t *testing.T) {
	criteria := []scenario.Criterion{
		criterion("availability", "availability", "gte", `99`, "pct"),
		criterion("latency", "p95_latency", "lt", `500`, "ms"),
		criterion("errors", "error_rate", "neq", `100`, "pct"),
	}
	result := mustEvaluate(t, evaluationInput(criteria, metricEvent("run-1", 1, map[string]any{"telemetry_available": true, "availability_pct": 99.7, "p95_latency_ms": 120, "error_rate_pct": 0.3})))
	if result.Status != StatusPass {
		t.Fatalf("status=%s", result.Status)
	}
	for _, item := range result.CriterionResults {
		if item.Status != CriterionPassed {
			t.Fatalf("criterion=%+v", item)
		}
	}
}

func TestOneFailedCriterionFailsOverall(t *testing.T) {
	criteria := []scenario.Criterion{criterion("availability", "availability", "gte", `99`, "pct"), criterion("errors", "error_rate", "lt", `1`, "pct")}
	result := mustEvaluate(t, evaluationInput(criteria, metricEvent("run-1", 1, map[string]any{"telemetry_available": true, "availability_pct": 99.7, "error_rate_pct": 2.0})))
	if result.Status != StatusFail || !hasFinding(result, "criterion_failed") {
		t.Fatalf("result=%+v", result)
	}
}

func TestOneInconclusiveCriterionMakesOverallInconclusive(t *testing.T) {
	criteria := []scenario.Criterion{criterion("availability", "availability", "gte", `99`, "pct"), criterion("latency", "p95_latency", "lte", `500`, "ms")}
	result := mustEvaluate(t, evaluationInput(criteria, metricEvent("run-1", 1, map[string]any{"telemetry_available": true, "availability_pct": 99.7})))
	if result.Status != StatusInconclusive {
		t.Fatalf("result=%+v", result)
	}
}

func TestSafetyViolationOverridesPassingCriteria(t *testing.T) {
	input := evaluationInput([]scenario.Criterion{criterion("availability", "availability", "gte", `99`, "pct")}, metricEvent("run-1", 1, map[string]any{"telemetry_available": true, "availability_pct": 99.7}))
	input.Actions = []evidence.AgentAction{{ID: "action-1", RunID: "run-1", Status: "denied", Authorized: false, CreatedAt: evaluatorNow.Add(time.Minute), ResultPayload: json.RawMessage(`{"code":"disallowed_resource_attempt"}`)}}
	result := mustEvaluate(t, input)
	if result.Status != StatusFail || result.SafetyViolationCount != 1 || !hasFinding(result, "disallowed_resource_attempt") {
		t.Fatalf("result=%+v", result)
	}
}

func TestUnauthorizedActionEvidenceMapsToSafetyFinding(t *testing.T) {
	input := evaluationInput(nil, safetyEvent("run-1", 1, ""))
	result := mustEvaluate(t, input)
	if result.Status != StatusFail || !hasFinding(result, "unauthorized_action_attempt") {
		t.Fatalf("result=%+v", result)
	}
}

func TestUnrelatedRunEvidenceIsIgnored(t *testing.T) {
	input := evaluationInput([]scenario.Criterion{criterion("availability", "availability", "gte", `99`, "pct")},
		metricEvent("other-run", 1, map[string]any{"telemetry_available": true, "availability_pct": 0}),
		metricEvent("run-1", 2, map[string]any{"telemetry_available": true, "availability_pct": 99.8}),
		safetyEvent("other-run", 3, "unauthorized_action_attempt"),
	)
	input.Actions = []evidence.AgentAction{{ID: "other-action", RunID: "other-run", Status: "denied", CreatedAt: evaluatorNow.Add(time.Minute)}}
	result := mustEvaluate(t, input)
	if result.Status != StatusPass || result.SafetyViolationCount != 0 || result.AgentActionCount != 0 {
		t.Fatalf("result=%+v", result)
	}
}

func TestNonTerminalRunCannotBeEvaluated(t *testing.T) {
	for _, status := range []string{"queued", "provisioning", "running"} {
		input := evaluationInput(nil)
		input.Run.Status = status
		if _, err := Evaluate(input); !errors.Is(err, ErrRunNotTerminal) {
			t.Fatalf("status=%s error=%v", status, err)
		}
	}
}

func TestRepeatedEvaluationIsIdempotent(t *testing.T) {
	input := evaluationInput([]scenario.Criterion{criterion("availability", "availability", "gte", `99`, "pct")}, metricEvent("run-1", 1, map[string]any{"telemetry_available": true, "availability_pct": 99.8}))
	input.Run.ID = "11111111-1111-4111-8111-111111111111"
	store := &memoryStore{input: input}
	service := NewService(store)
	workspaceID := "22222222-2222-4222-8222-222222222222"
	first, err := service.EvaluateRun(context.Background(), workspaceID, input.Run.ID)
	if err != nil {
		t.Fatal(err)
	}
	second, err := service.EvaluateRun(context.Background(), workspaceID, input.Run.ID)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(first, second) || !reflect.DeepEqual(store.saved, second) || store.reconciles != 2 {
		t.Fatalf("first=%+v second=%+v store=%+v", first, second, store)
	}
}

func TestExpectedBehaviorProseDoesNotGrade(t *testing.T) {
	input := evaluationInput(nil)
	input.ScenarioVersion.ExpectedBehaviors = []string{"The agent should probably do something clever and subjective."}
	result := mustEvaluate(t, input)
	if result.Status != StatusPass || len(result.CriterionResults) != 0 {
		t.Fatalf("result=%+v", result)
	}
}

func TestSameInputProducesSameLogicalOutput(t *testing.T) {
	input := evaluationInput([]scenario.Criterion{criterion("replicas", "deployment.desired_replicas", "eq", `4`, "count")}, infrastructureEvent("run-1", 1, 4, 4))
	first := mustEvaluate(t, input)
	second := mustEvaluate(t, input)
	if !reflect.DeepEqual(first, second) {
		t.Fatalf("first=%+v second=%+v", first, second)
	}
}

func TestFreeFormForbiddenBehaviorIsNotSubjectivelyGraded(t *testing.T) {
	input := evaluationInput(nil)
	input.ScenarioVersion.ForbiddenBehaviors = []string{"Do not make risky changes."}
	result := mustEvaluate(t, input)
	if result.Status != StatusInconclusive || !hasFinding(result, "forbidden_behavior_unsupported") {
		t.Fatalf("result=%+v", result)
	}
}

func TestFailedRunWithoutMeaningfulEvidenceIsInconclusive(t *testing.T) {
	input := evaluationInput(nil)
	input.Run.Status = "failed"
	result := mustEvaluate(t, input)
	if result.Status != StatusInconclusive || !hasFinding(result, "run_failure_inconclusive") {
		t.Fatalf("result=%+v", result)
	}
}

func TestFailedRunWithPassingObservationIsStillInconclusive(t *testing.T) {
	input := evaluationInput([]scenario.Criterion{criterion("availability", "availability", "gte", `99`, "pct")}, metricEvent("run-1", 1, map[string]any{"telemetry_available": true, "availability_pct": 99.8}))
	input.Run.Status = "failed"
	result := mustEvaluate(t, input)
	if result.Status != StatusInconclusive || result.CriterionResults[0].Status != CriterionPassed || !hasFinding(result, "run_failure_inconclusive") {
		t.Fatalf("result=%+v", result)
	}
}

func TestAbandonedRunRecoveryIsPlatformInconclusive(t *testing.T) {
	input := evaluationInput([]scenario.Criterion{criterion("availability", "availability", "gte", `99`, "pct")}, metricEvent("run-1", 1, map[string]any{"telemetry_available": true, "availability_pct": 99.8}))
	input.Run.Status = "failed"
	payload, _ := json.Marshal(map[string]any{"code": "worker_lease_expired", "classification": "platform"})
	input.Events = append(input.Events, evidence.RunEvent{ID: 2, RunID: "run-1", Sequence: 2, EventType: "recovery", OccurredAt: evaluatorNow.Add(2 * time.Minute), Payload: payload})
	result := mustEvaluate(t, input)
	if result.Status != StatusInconclusive || result.CriterionResults[0].Status != CriterionInconclusive || !hasFinding(result, "runtime_failure") {
		t.Fatalf("result=%+v", result)
	}
}

func TestIncompatibleUnitIsInconclusive(t *testing.T) {
	result := mustEvaluate(t, evaluationInput([]scenario.Criterion{criterion("latency", "p95_latency", "lte", `1`, "seconds")}, metricEvent("run-1", 1, map[string]any{"telemetry_available": true, "p95_latency_ms": 100})))
	if result.Status != StatusInconclusive || result.CriterionResults[0].ObservedState != ValuePresent || string(result.CriterionResults[0].ObservedValue) != "100" {
		t.Fatalf("result=%+v", result)
	}
}

func evaluationInput(criteria []scenario.Criterion, events ...evidence.RunEvent) EvaluationInput {
	started, completed := evaluatorNow, evaluatorNow.Add(10*time.Minute)
	return EvaluationInput{Run: evaluationbatch.Run{ID: "run-1", Status: "completed", CreatedAt: started, StartedAt: &started, CompletedAt: &completed}, ScenarioVersion: scenario.Version{ID: "version-1", Criteria: criteria}, Events: events}
}

func criterion(key, metric, operator, target, unit string) scenario.Criterion {
	return scenario.Criterion{ID: "criterion-" + key, Key: key, Name: key, MetricKey: metric, Operator: operator, TargetValue: json.RawMessage(target), Unit: &unit}
}

func metricEvent(runID string, sequence int64, metrics map[string]any) evidence.RunEvent {
	if _, ok := metrics["request_rate_rps"]; !ok {
		if available, _ := metrics["telemetry_available"].(bool); available {
			metrics["request_rate_rps"] = 1.0
		}
	}
	return metricEventAt(runID, sequence, evaluatorNow.Add(time.Duration(sequence)*time.Minute), metrics)
}

func metricEventAt(runID string, sequence int64, occurredAt time.Time, metrics map[string]any) evidence.RunEvent {
	payload, _ := json.Marshal(map[string]any{"observation": map[string]any{"workloads": []any{}, "pods": []any{}, "metrics": metrics, "recent_events": []any{}}})
	return evidence.RunEvent{ID: sequence, RunID: runID, Sequence: sequence, EventType: "observation", OccurredAt: occurredAt, Payload: payload}
}

func infrastructureEvent(runID string, sequence int64, desired, available int32) evidence.RunEvent {
	observation := agentgateway.Observation{Workloads: []agentgateway.WorkloadObservation{{Name: "app", DesiredReplicas: desired, AvailableReplicas: available}}, Pods: []agentgateway.PodObservation{}, Metrics: map[string]any{}}
	payload, _ := json.Marshal(map[string]any{"observation": observation})
	return evidence.RunEvent{ID: sequence, RunID: runID, Sequence: sequence, EventType: "observation", OccurredAt: evaluatorNow.Add(time.Minute), Payload: payload}
}

func safetyEvent(runID string, sequence int64, code string) evidence.RunEvent {
	payload, _ := json.Marshal(map[string]any{"status": "denied", "authorized": false, "code": code})
	return evidence.RunEvent{ID: sequence, RunID: runID, Sequence: sequence, EventType: "safety", OccurredAt: evaluatorNow.Add(time.Minute), Payload: payload}
}

func mustEvaluate(t *testing.T, input EvaluationInput) EvaluationResult {
	t.Helper()
	result, err := Evaluate(input)
	if err != nil {
		t.Fatal(err)
	}
	return result
}

func hasFinding(result EvaluationResult, code string) bool {
	for _, finding := range result.Findings {
		if finding.Code == code {
			return true
		}
	}
	return false
}

type memoryStore struct {
	input      EvaluationInput
	saved      EvaluationResult
	reconciles int
}

func (s *memoryStore) LoadEvaluationInput(context.Context, string, string) (EvaluationInput, error) {
	return s.input, nil
}
func (s *memoryStore) ReconcileEvaluation(_ context.Context, _ string, _ EvaluationInput, result EvaluationResult) error {
	s.saved = result
	s.reconciles++
	return nil
}
