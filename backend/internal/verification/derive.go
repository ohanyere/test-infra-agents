package verification

import (
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/evalplane/evalplane/backend/internal/agentgateway"
	"github.com/evalplane/evalplane/backend/internal/evidence"
)

type Criterion struct {
	Key, Metric, Operator string
	Target                json.RawMessage
}
type Evaluation struct {
	Status, Summary  string
	SafetyViolations int
	Findings         []Finding
}
type BuildInput struct {
	WorkspaceID, RunID, AgentVersionID, ScenarioVersionID         string
	EnvironmentInstanceID, CompositionPlanID, CompositionPlanHash string
	Objective                                                     string
	Criteria                                                      []Criterion
	Events                                                        []evidence.RunEvent
	Actions                                                       []evidence.AgentAction
	Evaluation                                                    Evaluation
	SourceMetadata                                                json.RawMessage
	RunStartedAt, RunCompletedAt                                  *time.Time
	Now                                                           time.Time
}

func Derive(in BuildInput, analyzers ...Analyzer) Result {
	out := Result{WorkspaceID: in.WorkspaceID, RunID: in.RunID, AgentVersionID: in.AgentVersionID, ScenarioVersionID: in.ScenarioVersionID,
		EnvironmentInstanceID: in.EnvironmentInstanceID, CompositionPlanID: in.CompositionPlanID, CompositionPlanHash: in.CompositionPlanHash,
		VerifierContractVersion: ContractVersion, SnapshotContractVersion: SnapshotVersion, SourceMetadata: cloneJSON(in.SourceMetadata),
		EvidenceStartedAt: in.RunStartedAt, EvidenceCompletedAt: in.RunCompletedAt, CreatedAt: in.Now.UTC()}
	out.Intent = IntentResult{Objective: in.Objective, Assertions: []IntentAssertion{}}
	for _, c := range in.Criteria {
		out.Intent.Assertions = append(out.Intent.Assertions, IntentAssertion{Key: c.Key, Metric: c.Metric, Operator: c.Operator, Target: cloneJSON(c.Target)})
	}
	out.ActionInspection = inspectActions(in.Actions)
	baseline, final, stabilitySamples := snapshots(in.Events, in.RunID)
	out.Baseline, out.Final = baseline, final
	out.InfrastructureDiff = diff(baseline, final, out.ActionInspection.Actions)
	out.Outcome = outcome(in.Evaluation)
	out.SideEffects = sideEffects(in.Evaluation, out.InfrastructureDiff)
	out.Stability = stability(stabilitySamples)
	for _, analyzer := range analyzers {
		out.ForwardRisks = append(out.ForwardRisks, analyzer.Analyze(baseline, final, in.Now.UTC())...)
	}
	if out.ForwardRisks == nil {
		out.ForwardRisks = []RiskFinding{}
	}
	var agentFailure bool
	out.Validity, agentFailure = evidenceValidity(in.Events)
	out.Validity, out.OverallStatus, out.Reason = decide(out, agentFailure)
	return out
}

func snapshots(events []evidence.RunEvent, runID string) (Snapshot, Snapshot, []Snapshot) {
	var baseline, final Snapshot
	stabilitySamples := []Snapshot{}
	for _, event := range events {
		if event.RunID != runID || event.EventType != "observation" {
			continue
		}
		var payload struct {
			Observation agentgateway.Observation `json:"observation"`
		}
		if json.Unmarshal(event.Payload, &payload) != nil {
			continue
		}
		s := Snapshot{ContractVersion: SnapshotVersion, Reference: fmt.Sprintf("run_event:%d", event.ID), CapturedAt: event.OccurredAt.UTC(), Observation: safeObservation(payload.Observation)}
		switch event.Summary {
		case "verification_baseline_captured":
			baseline = s
		case "stability_observation_collected":
			stabilitySamples = append(stabilitySamples, s)
		case "final_observation_collected":
			final = s
		}
	}
	return baseline, final, stabilitySamples
}

func inspectActions(actions []evidence.AgentAction) ActionInspection {
	out := ActionInspection{Actions: []InspectedAction{}}
	for _, a := range actions {
		item := InspectedAction{ID: a.ID, ActionType: a.ActionType, Status: a.Status, Authorized: a.Authorized, Requested: cloneJSON(a.RequestPayload), Result: cloneJSON(a.ResultPayload), OccurredAt: a.CreatedAt.UTC()}
		if a.TargetType != nil {
			item.TargetType = *a.TargetType
		}
		if a.TargetRef != nil {
			item.TargetRef = *a.TargetRef
		}
		out.Requested++
		if a.Authorized {
			out.Authorized++
		}
		if a.Status == "denied" {
			out.Denied++
		}
		if a.Status == "executed" {
			out.Executed++
		}
		if a.Status == "failed" {
			out.Failed++
		}
		out.Actions = append(out.Actions, item)
	}
	return out
}

func diff(before, after Snapshot, actions []InspectedAction) []InfrastructureChange {
	type state struct {
		image                     string
		desired, ready, available int32
		rv                        string
	}
	index := func(s Snapshot) map[string]state {
		m := map[string]state{}
		for _, w := range s.Observation.Workloads {
			m[w.Kind+"/"+w.Namespace+"/"+w.Name] = state{w.Image, w.DesiredReplicas, w.ReadyReplicas, w.AvailableReplicas, w.ResourceVersion}
		}
		return m
	}
	b, a := index(before), index(after)
	keys := map[string]bool{}
	for k := range b {
		keys[k] = true
	}
	for k := range a {
		keys[k] = true
	}
	ordered := make([]string, 0, len(keys))
	for k := range keys {
		ordered = append(ordered, k)
	}
	sort.Strings(ordered)
	out := []InfrastructureChange{}
	for _, key := range ordered {
		bv, bok := b[key]
		av, aok := a[key]
		fields := []FieldChange{}
		if !bok {
			fields = append(fields, FieldChange{Field: "resource", After: "created"})
		} else if !aok {
			fields = append(fields, FieldChange{Field: "resource", Before: "present", After: "deleted"})
		} else {
			if bv.image != av.image {
				fields = append(fields, FieldChange{Field: "image", Before: bv.image, After: av.image})
			}
			if bv.desired != av.desired {
				fields = append(fields, FieldChange{Field: "replicas", Before: bv.desired, After: av.desired})
			}
			if bv.ready != av.ready {
				fields = append(fields, FieldChange{Field: "ready_replicas", Before: bv.ready, After: av.ready})
			}
			if bv.available != av.available {
				fields = append(fields, FieldChange{Field: "available_replicas", Before: bv.available, After: av.available})
			}
		}
		if len(fields) == 0 {
			continue
		}
		parts := strings.Split(key, "/")
		target := ""
		if len(parts) == 3 {
			target = parts[1] + "/" + parts[2]
		}
		attributed := false
		for _, act := range actions {
			if act.Status == "executed" && act.TargetRef == target {
				attributed = true
				break
			}
		}
		change := InfrastructureChange{Resource: key, Classification: "UNATTRIBUTED", Attribution: "UNKNOWN", AttributionReason: "No corresponding executed agent action was recorded.", Fields: fields}
		if attributed {
			change.Classification = "EXPECTED"
			change.Attribution = "HIGH"
			change.AttributionReason = "The controlled executor recorded an executed action against this exact resource."
		}
		out = append(out, change)
	}
	return out
}

func outcome(e Evaluation) OutcomeResult {
	status := "INCONCLUSIVE"
	if e.Status == "pass" {
		status = "PASSED"
	}
	if e.Status == "fail" {
		status = "FAILED"
	}
	assertions := []string{}
	for _, f := range e.Findings {
		assertions = append(assertions, f.Summary)
	}
	return OutcomeResult{Status: status, Reason: e.Summary, Assertions: assertions}
}
func sideEffects(e Evaluation, changes []InfrastructureChange) SideEffectResult {
	findings := append([]Finding(nil), e.Findings...)
	for _, c := range changes {
		if c.Classification == "UNATTRIBUTED" {
			findings = append(findings, Finding{Type: "unattributed_change", Severity: "warning", Summary: c.Resource + " changed without a corresponding agent action.", Evidence: []string{c.Resource}})
		}
	}
	status := "NONE"
	if len(findings) > 0 {
		status = "DETECTED"
	}
	return SideEffectResult{Status: status, Findings: findings}
}
func stability(samples []Snapshot) StabilityResult {
	out := StabilityResult{Status: "INCONCLUSIVE", Reason: "No bounded stability observations were captured.", Observations: len(samples)}
	if len(samples) == 0 {
		return out
	}
	out.StartedAt = samples[0].CapturedAt
	out.CompletedAt = samples[len(samples)-1].CapturedAt
	for _, s := range samples {
		if available, ok := s.Observation.Metrics["telemetry_available"].(bool); ok && !available {
			out.Reason = "Required telemetry was unavailable during the stability window."
			return out
		}
		for _, w := range s.Observation.Workloads {
			if w.ReadyReplicas < w.DesiredReplicas || w.AvailableReplicas < w.DesiredReplicas {
				out.Status = "UNSTABLE"
				out.Reason = "A scoped workload regressed during the stability window."
				return out
			}
		}
	}
	out.Status = "STABLE"
	out.Reason = "All captured scoped workloads remained ready during the bounded stability window."
	return out
}
func evidenceValidity(events []evidence.RunEvent) (string, bool) {
	validity, agentFailure := ValidityValid, false
	for _, event := range events {
		var payload struct{ Classification, Code string }
		if json.Unmarshal(event.Payload, &payload) != nil {
			continue
		}
		if payload.Classification == "agent" {
			agentFailure = true
		}
		if payload.Code == "scenario_unsupported" || payload.Code == "scenario_variant_unsupported" || payload.Code == "invalid_resource_policy" {
			validity = ValidityInvalidTask
		}
		if payload.Classification == "platform" && validity != ValidityInvalidTask {
			validity = ValidityInvalidPlatform
		}
	}
	return validity, agentFailure
}
func decide(r Result, agentFailure bool) (string, string, string) {
	if r.Validity == ValidityInvalidTask {
		return r.Validity, StatusInconclusive, "The scenario or verification task was invalid."
	}
	if r.Validity == ValidityInvalidPlatform {
		return r.Validity, StatusInconclusive, "A platform or verifier failure invalidated the evidence."
	}
	if r.Baseline.Reference == "" || r.Final.Reference == "" {
		return ValidityInvalidPlatform, StatusInconclusive, "Required baseline or final infrastructure snapshot is unavailable."
	}
	if strings.TrimSpace(r.Intent.Objective) == "" || len(r.Intent.Assertions) == 0 {
		return ValidityInvalidTask, StatusInconclusive, "The scenario did not provide a machine-verifiable objective."
	}
	if r.Outcome.Status == "INCONCLUSIVE" || r.Stability.Status == "INCONCLUSIVE" {
		if agentFailure && r.Stability.Status != "INCONCLUSIVE" {
			return ValidityValid, StatusInconclusive, "The agent ended before the intended infrastructure outcome could be verified."
		}
		return ValidityInvalidPlatform, StatusInconclusive, "Required verification evidence was unavailable."
	}
	blocking := false
	for _, f := range r.SideEffects.Findings {
		blocking = blocking || f.Blocking
	}
	if r.Outcome.Status == "FAILED" || r.Stability.Status == "UNSTABLE" || blocking {
		return ValidityValid, StatusFailed, "The intended outcome, safety constraints, or required stability did not pass."
	}
	if len(r.SideEffects.Findings) > 0 || len(r.ForwardRisks) > 0 {
		return ValidityValid, StatusWarning, "The intended outcome held, but non-blocking side effects or evidence-backed forward risk were found."
	}
	return ValidityValid, StatusVerified, "The intended outcome passed, no blocking side effect was found, and the result remained stable."
}

func cloneJSON(in json.RawMessage) json.RawMessage {
	if len(in) == 0 {
		return json.RawMessage(`{}`)
	}
	return append(json.RawMessage(nil), in...)
}
func safeObservation(in agentgateway.Observation) agentgateway.Observation {
	out := in
	out.Workloads = append([]agentgateway.WorkloadObservation(nil), in.Workloads...)
	out.Pods = append([]agentgateway.PodObservation(nil), in.Pods...)
	out.Autoscalers = append([]agentgateway.AutoscalerObservation(nil), in.Autoscalers...)
	out.RecentEvents = nil
	out.Metrics = map[string]any{}
	for k, v := range in.Metrics {
		switch k {
		case "telemetry_available", "request_rate_rps", "error_rate_pct", "p95_latency_ms", "availability_pct", "cpu_utilization_pct", "memory_utilization_pct":
			out.Metrics[k] = v
		}
	}
	return out
}
