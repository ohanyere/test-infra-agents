package operational

import (
	"bytes"
	"encoding/json"
	"sort"
	"time"

	"github.com/evalplane/evalplane/backend/internal/verification"
)

// A monitoring transport gap does not replace a factual episode when no new
// operation, contract, state, or outcome evidence was acquired. This applies
// to both automatic and manual episode materialization.
func monitoringOnlyGap(before, after Report) bool {
	if !latestMonitorUnavailable(after.MonitoringEvents) {
		return false
	}
	clean := func(r Report) ([]byte, error) {
		r.Revision = 0
		r.SourceFingerprint = ""
		r.EvaluatedAt = time.Time{}
		r.Findings = nil
		r.MonitoringEvents = nil
		return json.Marshal(r)
	}
	prior, priorErr := clean(before)
	current, currentErr := clean(after)
	return priorErr == nil && currentErr == nil && bytes.Equal(prior, current)
}

// assessDirectStability uses only Realverax-acquired Kubernetes snapshots.
// Collector snapshots and a single healthy response cannot satisfy a window.
func assessDirectStability(in Input, all []verification.Snapshot) Finding {
	f := Finding{Dimension: "direct_stability", Status: Inconclusive, ProvenanceStatus: "SOURCE_AUTHENTICATED", Code: "DIRECT_STABILITY_UNOBSERVED", Reason: "Multiple fresh direct Kubernetes observations spanning the customer stability window are required."}
	window := in.Window
	if window <= 0 {
		window = 5 * time.Minute
	}
	var rules []Rule
	for _, contract := range in.Contracts {
		if !contract.Active || contract.EffectiveAt.After(in.EvaluatedAt) {
			continue
		}
		for _, rule := range contract.Rules {
			if rule.Type == "min_available_replicas" && rule.Minimum != nil {
				rules = append(rules, rule)
				if rule.WindowSeconds > 0 && time.Duration(rule.WindowSeconds)*time.Second > window {
					window = time.Duration(rule.WindowSeconds) * time.Second
				}
			}
		}
	}
	if len(rules) == 0 {
		return f
	}
	seen := map[string]bool{}
	var snapshots []verification.Snapshot
	for _, sample := range all {
		if sample.SourceProvider == "direct-kubernetes-api:pinned-tls" && !seen[sample.Reference] {
			seen[sample.Reference] = true
			snapshots = append(snapshots, sample)
		}
	}
	if len(snapshots) < 2 {
		return f
	}
	sort.Slice(snapshots, func(i, j int) bool { return snapshots[i].CapturedAt.Before(snapshots[j].CapturedAt) })
	end := snapshots[len(snapshots)-1].CapturedAt
	if end.After(in.EvaluatedAt) || in.EvaluatedAt.Sub(end) > in.Freshness {
		return f
	}
	start := end.Add(-window)
	var windowSamples []verification.Snapshot
	for _, sample := range snapshots {
		if !sample.CapturedAt.Before(start) {
			windowSamples = append(windowSamples, sample)
		}
	}
	if len(windowSamples) < 2 || windowSamples[0].CapturedAt.After(start.Add(window/5)) {
		return f
	}
	for i, sample := range windowSamples {
		if i > 0 && sample.CapturedAt.Sub(windowSamples[i-1].CapturedAt) > window/2 {
			return f
		}
		f.Evidence = append(f.Evidence, ref(sample)...)
		for _, rule := range rules {
			state := workload(sample, rule.Namespace, rule.ResourceName)
			if state == nil {
				return f
			}
			if float64(state.AvailableReplicas) < *rule.Minimum {
				f.Status = Fail
				f.Code = "DIRECT_STABILITY_REGRESSION"
				f.Reason = "A direct Kubernetes observation fell below the customer replica floor during the stability window."
				return f
			}
		}
	}
	f.Status = Pass
	f.Code = "DIRECT_STABILITY_SUSTAINED"
	f.Reason = "Direct Kubernetes observations cover the customer stability window without a replica-floor breach. Service health remains separate."
	return f
}

// Monitoring availability is distinct from customer service health. A failed
// source request records an inconclusive monitoring finding, never a service
// failure or a successful recovery.
func latestMonitorEvents(events []MonitoringEvent) map[string]MonitoringEvent {
	latest := map[string]MonitoringEvent{}
	for _, event := range events {
		old, ok := latest[event.SourceID]
		if !ok || event.CycleNumber > old.CycleNumber || event.CycleNumber == old.CycleNumber && (event.Attempt > old.Attempt || event.Attempt == old.Attempt && (event.RecordedAt.After(old.RecordedAt) || event.RecordedAt.Equal(old.RecordedAt) && event.ID > old.ID)) {
			latest[event.SourceID] = event
		}
	}
	return latest
}
func assessSourceHealth(events []MonitoringEvent) []Finding {
	latest := latestMonitorEvents(events)
	out := []Finding{}
	sources := make([]string, 0, len(latest))
	for source := range latest {
		sources = append(sources, source)
	}
	sort.Strings(sources)
	for _, source := range sources {
		event := latest[source]
		if event.Status != "unavailable" {
			continue
		}
		out = append(out, Finding{Dimension: "source_health", Status: Inconclusive, ProvenanceStatus: "REALVERAX_ACQUISITION_FAILURE", Code: "SOURCE_ACQUISITION_UNAVAILABLE", Reason: "The registered source could not be acquired. Application health is unknown from this failure: " + event.Reason, AffectedResource: source, Evidence: []EvidenceRef{{Reference: "monitoring_event:" + event.ID, Provenance: "server_owned_acquisition"}}})
	}
	return out
}
func latestMonitorUnavailable(events []MonitoringEvent) bool {
	for _, f := range assessSourceHealth(events) {
		if f.Code == "SOURCE_ACQUISITION_UNAVAILABLE" {
			return true
		}
	}
	return false
}
func reportFinding(r Report, dimension string) Finding {
	for _, f := range r.Findings {
		if f.Dimension == dimension {
			return f
		}
	}
	return Finding{}
}
func assuranceTransitions(before, after Report) []Finding {
	out := []Finding{}
	for _, dimension := range []string{"service_outcome", "infrastructure_state", "gitops"} {
		prior, current := reportFinding(before, dimension), reportFinding(after, dimension)
		if prior.Dimension == "" || current.Dimension == "" || current.ProvenanceStatus != "SOURCE_AUTHENTICATED" || len(current.Evidence) == 0 {
			continue
		}
		if prior.Status == Pass && current.Status == Fail {
			out = append(out, Finding{Dimension: "assurance_transition", Status: Fail, ProvenanceStatus: "SOURCE_AUTHENTICATED", Code: "ASSURANCE_" + dimension + "_REGRESSION", Reason: "A fresh independently acquired observation contradicts the previous positive assessment; cause and agent attribution remain separate.", AffectedResource: current.AffectedResource, Evidence: append(append([]EvidenceRef{}, prior.Evidence...), current.Evidence...)})
		}
		if prior.Status == Fail && current.Status == Pass {
			// A single healthy service sample cannot establish recovery. Require
			// an independently supported service stability window.
			if dimension == "service_outcome" {
				stability := reportFinding(after, "service_stability")
				if stability.Status != Pass || stability.ProvenanceStatus != "SOURCE_AUTHENTICATED" {
					continue
				}
			}
			if dimension == "infrastructure_state" {
				stability := reportFinding(after, "direct_stability")
				if stability.Status != Pass || stability.ProvenanceStatus != "SOURCE_AUTHENTICATED" {
					continue
				}
			}
			out = append(out, Finding{Dimension: "assurance_transition", Status: Pass, ProvenanceStatus: "SOURCE_AUTHENTICATED", Code: "ASSURANCE_" + dimension + "_RECOVERED", Reason: "Fresh independently acquired evidence supports recovery of this dimension; earlier failure and unrelated customer controls remain in history.", AffectedResource: current.AffectedResource, Evidence: append(append([]EvidenceRef{}, prior.Evidence...), current.Evidence...)})
		}
	}
	priorSources, currentSources := latestMonitorEvents(before.MonitoringEvents), latestMonitorEvents(after.MonitoringEvents)
	sources := make([]string, 0, len(priorSources))
	for source := range priorSources {
		sources = append(sources, source)
	}
	sort.Strings(sources)
	for _, source := range sources {
		prior, current := priorSources[source], currentSources[source]
		if prior.Status == "unavailable" && current.Status == "acquired" && prior.ID != current.ID {
			out = append(out, Finding{Dimension: "source_health", Status: Pass, ProvenanceStatus: "REALVERAX_ACQUISITION_RESTORED", Code: "SOURCE_CONTACT_RESTORED", Reason: "A registered source responded again. This does not establish application recovery.", AffectedResource: source, Evidence: []EvidenceRef{{Reference: "monitoring_event:" + current.ID, Provenance: "server_owned_acquisition"}}})
		}
	}
	return out
}
