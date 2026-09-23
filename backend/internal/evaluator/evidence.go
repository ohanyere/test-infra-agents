package evaluator

import (
	"encoding/json"
	"fmt"
	"math"
	"sort"
	"strconv"
	"time"

	"github.com/evalplane/evalplane/backend/internal/agentgateway"
	"github.com/evalplane/evalplane/backend/internal/evaluationmetric"
	"github.com/evalplane/evalplane/backend/internal/evidence"
	"github.com/evalplane/evalplane/backend/internal/telemetry"
)

type metricSample struct {
	value      float64
	occurredAt time.Time
	sequence   int64
	evidenceID string
}

func normalizeEvidence(input EvaluationInput) evaluationEvidence {
	out := evaluationEvidence{Metrics: map[string]MetricValue{}, Events: []evidence.RunEvent{}, Actions: []evidence.AgentAction{}}
	samples := map[string][]metricSample{}
	invalid := map[string][]string{}
	telemetrySeen := false
	telemetryUnavailable := false
	currentErrorBoundary := outcomeEvidenceBoundary(input)
	currentErrorTrafficSeen := false

	for _, event := range input.Events {
		if event.RunID != input.Run.ID || !withinRunWindow(input, event.OccurredAt) {
			continue
		}
		out.Events = append(out.Events, event)
		ref := eventReference(event)
		if event.EventType == "observation" {
			var payload struct {
				Observation struct {
					Workloads []agentgateway.WorkloadObservation `json:"workloads"`
					Pods      []agentgateway.PodObservation      `json:"pods"`
					Metrics   map[string]json.RawMessage         `json:"metrics"`
				} `json:"observation"`
			}
			if json.Unmarshal(event.Payload, &payload) != nil {
				continue
			}
			metrics := payload.Observation.Metrics
			if raw, ok := metrics["telemetry_available"]; ok {
				var available bool
				if json.Unmarshal(raw, &available) == nil {
					telemetrySeen = telemetrySeen || available
					telemetryUnavailable = telemetryUnavailable || !available
				}
			}
			addJSONMetric(samples, invalid, "request_rate", metrics["request_rate_rps"], event, ref)
			addJSONMetric(samples, invalid, "error_rate", metrics["error_rate_pct"], event, ref)
			if !event.OccurredAt.Before(currentErrorBoundary) {
				if requestRate, ok := nonnegativeMetric(metrics["request_rate_rps"]); ok && requestRate > 0 {
					currentErrorTrafficSeen = true
					addJSONMetric(samples, invalid, "current_error_rate", metrics["error_rate_pct"], event, ref)
				}
			}
			addJSONMetric(samples, invalid, "p95_latency", metrics["p95_latency_ms"], event, ref)
			addJSONMetric(samples, invalid, "availability", metrics["availability_pct"], event, ref)
			var desired, available, ready, notReady, restarts float64
			for _, workload := range payload.Observation.Workloads {
				desired += float64(workload.DesiredReplicas)
				available += float64(workload.AvailableReplicas)
			}
			for _, pod := range payload.Observation.Pods {
				if pod.Ready {
					ready++
				} else {
					notReady++
				}
				restarts += float64(pod.RestartCount)
			}
			if payload.Observation.Workloads != nil {
				addSample(samples, "deployment_desired_replicas", desired, event, ref)
				addSample(samples, "deployment_available_replicas", available, event, ref)
			}
			if payload.Observation.Pods != nil {
				addSample(samples, "pod_ready_count", ready, event, ref)
				addSample(samples, "pod_not_ready_count", notReady, event, ref)
				addSample(samples, "pod_restart_count", restarts, event, ref)
			}
		}
		if event.EventType == "system" || event.EventType == "recovery" {
			var payload struct {
				Code           string `json:"code"`
				Classification string `json:"classification"`
			}
			if json.Unmarshal(event.Payload, &payload) == nil && (isRuntimeFailureCode(payload.Code) || payload.Classification == "platform") {
				out.RuntimeFailure = true
				out.RuntimeEvidence = append(out.RuntimeEvidence, ref)
			}
		}
	}
	for _, action := range input.Actions {
		if action.RunID == input.Run.ID && actionWithinRunWindow(input, action) {
			out.Actions = append(out.Actions, action)
		}
	}
	sort.Slice(out.Events, func(i, j int) bool { return out.Events[i].Sequence < out.Events[j].Sequence })
	sort.Slice(out.Actions, func(i, j int) bool {
		if out.Actions[i].CreatedAt.Equal(out.Actions[j].CreatedAt) {
			return out.Actions[i].ID < out.Actions[j].ID
		}
		return out.Actions[i].CreatedAt.Before(out.Actions[j].CreatedAt)
	})
	samples["agent_action_count"] = []metricSample{{value: float64(len(out.Actions)), evidenceID: "run:" + input.Run.ID}}
	for _, definition := range uniqueDefinitions() {
		metricSamples := samples[definition.Canonical]
		if len(invalid[definition.Canonical]) > 0 {
			out.Metrics[definition.Canonical] = MetricValue{State: ValueInvalid, Unit: definition.Unit, EvidenceIDs: invalid[definition.Canonical], Reason: "observed metric was not a finite number"}
		} else if len(metricSamples) > 0 {
			value, refs := reduce(metricSamples, definition.Reducer)
			out.Metrics[definition.Canonical] = MetricValue{State: ValuePresent, Value: &value, Unit: definition.Unit, EvidenceIDs: refs}
		} else {
			reason := "no observation contained this metric"
			if definition.Canonical == "current_error_rate" && !currentErrorTrafficSeen {
				reason = "no post-action observation contained positive request traffic"
			}
			if definition.Telemetry && telemetryUnavailable && !telemetrySeen {
				reason = "telemetry was unavailable for the evaluation window"
			}
			out.Metrics[definition.Canonical] = MetricValue{State: ValueUnavailable, Unit: definition.Unit, Reason: reason}
		}
	}
	return out
}

// outcomeEvidenceBoundary keeps current-state criteria tied to evidence gathered
// after the latest executed mutation. Read-only inspect/no_op actions do not move
// the boundary. Runs without a mutation use their normal start boundary.
func outcomeEvidenceBoundary(input EvaluationInput) time.Time {
	boundary := input.Run.CreatedAt
	if input.Run.StartedAt != nil {
		boundary = *input.Run.StartedAt
	}
	for _, action := range input.Actions {
		if action.RunID != input.Run.ID || !actionWithinRunWindow(input, action) || action.Status != "executed" || !action.Authorized || action.ActionType == "inspect" || action.ActionType == "no_op" {
			continue
		}
		occurred := action.CreatedAt
		if action.StartedAt != nil {
			occurred = *action.StartedAt
		}
		if action.CompletedAt != nil {
			occurred = *action.CompletedAt
		}
		if occurred.After(boundary) {
			boundary = occurred
		}
	}
	// A trailing Prometheus rate is fully post-action only after its complete
	// lookback has elapsed. Samples before this point remain baseline/history.
	return boundary.Add(telemetry.RateWindow)
}

func nonnegativeMetric(raw json.RawMessage) (float64, bool) {
	if len(raw) == 0 || string(raw) == "null" {
		return 0, false
	}
	var value float64
	if json.Unmarshal(raw, &value) != nil || math.IsNaN(value) || math.IsInf(value, 0) || value < 0 {
		return 0, false
	}
	return value, true
}

func addJSONMetric(samples map[string][]metricSample, invalid map[string][]string, key string, raw json.RawMessage, event evidence.RunEvent, ref string) {
	if len(raw) == 0 || string(raw) == "null" {
		return
	}
	var value float64
	if json.Unmarshal(raw, &value) != nil || math.IsNaN(value) || math.IsInf(value, 0) || value < 0 || ((key == "availability" || key == "error_rate" || key == "current_error_rate") && value > 100) {
		invalid[key] = append(invalid[key], ref)
		return
	}
	addSample(samples, key, value, event, ref)
}

func addSample(samples map[string][]metricSample, key string, value float64, event evidence.RunEvent, ref string) {
	samples[key] = append(samples[key], metricSample{value: value, occurredAt: event.OccurredAt, sequence: event.Sequence, evidenceID: ref})
}

func reduce(samples []metricSample, reducer string) (float64, []string) {
	selected := samples[0]
	refs := make([]string, 0, len(samples))
	for _, sample := range samples {
		refs = append(refs, sample.evidenceID)
		switch reducer {
		case "minimum":
			if sample.value < selected.value {
				selected = sample
			}
		case "maximum":
			if sample.value > selected.value {
				selected = sample
			}
		case "latest":
			if sample.occurredAt.After(selected.occurredAt) || (sample.occurredAt.Equal(selected.occurredAt) && sample.sequence > selected.sequence) {
				selected = sample
			}
		}
	}
	if reducer == "count" {
		return selected.value, refs
	}
	return selected.value, refs
}

func uniqueDefinitions() []evaluationmetric.Definition {
	seen := map[string]bool{}
	result := make([]evaluationmetric.Definition, 0)
	for _, definition := range evaluationmetric.Definitions() {
		if !seen[definition.Canonical] {
			seen[definition.Canonical] = true
			result = append(result, definition)
		}
	}
	sort.Slice(result, func(i, j int) bool { return result[i].Canonical < result[j].Canonical })
	return result
}

func eventReference(event evidence.RunEvent) string {
	return "run_event:" + strconv.FormatInt(event.ID, 10)
}

func withinRunWindow(input EvaluationInput, occurred time.Time) bool {
	start := input.Run.CreatedAt
	if input.Run.StartedAt != nil {
		start = *input.Run.StartedAt
	}
	if !start.IsZero() && occurred.Before(start) {
		return false
	}
	return input.Run.CompletedAt == nil || !occurred.After(*input.Run.CompletedAt)
}

func actionWithinRunWindow(input EvaluationInput, action evidence.AgentAction) bool {
	timestamp := action.CreatedAt
	if action.StartedAt != nil {
		timestamp = *action.StartedAt
	}
	return withinRunWindow(input, timestamp)
}

func isRuntimeFailureCode(code string) bool {
	switch code {
	case "runtime_failure", "platform_failure", "environment_failure", "worker_lease_expired", "run_abandoned", "worker_recovery_cleanup_failed":
		return true
	default:
		return false
	}
}

func metricFor(key string, normalized evaluationEvidence) (MetricValue, bool) {
	definition, ok := evaluationmetric.Lookup(key)
	if !ok {
		return MetricValue{State: ValueUnavailable, Reason: fmt.Sprintf("unsupported evaluator metric %q", key)}, false
	}
	return normalized.Metrics[definition.Canonical], true
}
