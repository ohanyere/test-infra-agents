package operational

import (
	"encoding/json"
	"net/netip"
	"sort"
	"time"
)

func directResourceState(record DirectObservation) (KubernetesResourceState, bool) {
	expected := map[string]string{"kubernetes_state": "Deployment", "kubernetes_service": "Service", "kubernetes_ingress": "Ingress", "kubernetes_networkpolicy": "NetworkPolicy"}[record.Kind]
	if expected == "" {
		return KubernetesResourceState{}, false
	}
	var value struct {
		ResourceState KubernetesResourceState `json:"resource_state"`
	}
	if json.Unmarshal(record.Payload, &value) != nil || value.ResourceState.Kind != expected || value.ResourceState.Namespace == "" || value.ResourceState.Name == "" {
		return KubernetesResourceState{}, false
	}
	return value.ResourceState, true
}

func freshResourceRecord(in Input, record DirectObservation, freshness time.Duration) bool {
	return !record.AcquiredAt.IsZero() && !record.ObservedAt.IsZero() &&
		!record.AcquiredAt.Before(record.ObservedAt) &&
		!in.EvaluatedAt.Before(record.AcquiredAt) &&
		in.EvaluatedAt.Sub(record.AcquiredAt) <= freshness
}

// Configuration can indicate possible exposure but cannot establish external
// reachability. This requirement remains inconclusive without that observation.
func assessExposureConfiguration(in Input, rule Rule) Finding {
	f := Finding{Dimension: "operational_contract", Status: Inconclusive, ProvenanceStatus: "SOURCE_UNVERIFIED", Code: "NETWORK_EVIDENCE_MISSING", Reason: "No fresh, scoped Service, Ingress, or NetworkPolicy state was acquired.", Evidence: []EvidenceRef{}}
	freshness := in.Freshness
	if rule.FreshnessSeconds > 0 {
		freshness = time.Duration(rule.FreshnessSeconds) * time.Second
	}
	if freshness <= 0 {
		freshness = 2 * time.Minute
	}
	for _, record := range in.DirectObservations {
		state, ok := directResourceState(record)
		if !ok || !freshResourceRecord(in, record, freshness) || state.Namespace != rule.Namespace || state.Name != rule.ResourceName || state.Kind == "Deployment" {
			continue
		}
		f.Evidence = append(f.Evidence, EvidenceRef{Reference: "direct_outcome:" + record.ID, Provenance: "direct-kubernetes-api:pinned-tls"})
		f.ProvenanceStatus = "SOURCE_AUTHENTICATED"
		if f.Code != "PUBLIC_EXPOSURE_CONFIGURATION_CANDIDATE" {
			f.Code = "REACHABILITY_UNVERIFIED"
			f.Reason = "Network configuration was observed, but external reachability was not established."
		}
		if state.Present && (state.ServiceType == "LoadBalancer" || state.ServiceType == "NodePort" || len(state.ExternalIPs) > 0 || len(state.LoadBalancerIngress) > 0 || state.Kind == "Ingress" || state.AllowsAllIngress) {
			f.Code = "PUBLIC_EXPOSURE_CONFIGURATION_CANDIDATE"
			f.Reason = "Source-authenticated configuration indicates possible exposure; actual public reachability remains unverified."
		}
	}
	return assessReachability(in, rule, freshness, f)
}

func assessReachability(in Input, rule Rule, freshness time.Duration, f Finding) Finding {
	type candidate struct {
		record      DirectObservation
		observation ReachabilityObservation
	}
	latest := map[string]candidate{}
	type conflictObservation struct {
		refs []EvidenceRef
		at   time.Time
	}
	conflicts := map[string]conflictObservation{}
	for _, record := range in.DirectObservations {
		if record.Kind != "external_reachability" || record.SourceID == "" || record.SourceDigest == "" || !freshResourceRecord(in, record, freshness) {
			continue
		}
		var payload struct {
			Reachability ReachabilityObservation `json:"reachability"`
		}
		if json.Unmarshal(record.Payload, &payload) != nil {
			continue
		}
		probe := payload.Reachability
		if probe.Namespace != rule.Namespace || probe.IngressName != rule.ResourceName || !probe.ObservedAt.Equal(record.ObservedAt) || !validProbeVantage(probe.Vantage) || probe.TargetHost == "" {
			continue
		}
		resolvedIP, parseErr := netip.ParseAddr(probe.ResolvedIP)
		if probe.Outcome == "reachable" && (parseErr != nil || !publicProbeIP(resolvedIP)) {
			continue
		}
		var ingress *DirectObservation
		for i := range in.DirectObservations {
			configured := &in.DirectObservations[i]
			state, ok := directResourceState(*configured)
			if !ok || configured.Kind != "kubernetes_ingress" || !state.Present || state.Namespace != rule.Namespace || state.Name != rule.ResourceName || !freshResourceRecord(in, *configured, freshness) || configured.ObservedAt.After(probe.ObservedAt) {
				continue
			}
			hostMatches := false
			for _, host := range state.IngressHosts {
				if host == probe.TargetHost {
					hostMatches = true
				}
			}
			ipMatches := probe.Outcome != "reachable"
			for _, address := range state.LoadBalancerIngress {
				if address == probe.ResolvedIP {
					ipMatches = true
				}
			}
			if hostMatches && ipMatches && (ingress == nil || configured.AcquiredAt.After(ingress.AcquiredAt)) {
				ingress = configured
			}
		}
		if ingress == nil {
			f.Code = "TARGET_INGRESS_BINDING_UNESTABLISHED"
			f.Reason = "The probe target and resolved address were not bound to a fresh authenticated Ingress host and load-balancer IP."
			f.Evidence = append(f.Evidence, EvidenceRef{Reference: "direct_outcome:" + record.ID, Provenance: "operator-external-probe:pinned-tls"})
			continue
		}
		key := probe.Vantage + "|" + probe.TargetHost
		prior, exists := latest[key]
		if exists && prior.record.ObservedAt.Equal(record.ObservedAt) && prior.observation.Outcome != probe.Outcome {
			conflicts[key] = conflictObservation{refs: []EvidenceRef{{Reference: "direct_outcome:" + prior.record.ID, Provenance: "operator-external-probe:pinned-tls"}, {Reference: "direct_outcome:" + record.ID, Provenance: "operator-external-probe:pinned-tls"}}, at: record.ObservedAt}
		}
		if !exists || record.ObservedAt.After(prior.record.ObservedAt) {
			latest[key] = candidate{record, probe}
		}
	}
	conflictKeys := make([]string, 0, len(conflicts))
	for key := range conflicts {
		conflictKeys = append(conflictKeys, key)
	}
	sort.Strings(conflictKeys)
	for _, key := range conflictKeys {
		conflict := conflicts[key]
		if current, exists := latest[key]; exists && !current.record.ObservedAt.After(conflict.at) {
			f.Status = Conflicting
			f.ProvenanceStatus = "CONFLICTING_DIRECT_SOURCES"
			f.Code = "CONFLICTING_REACHABILITY_OBSERVATIONS"
			f.Reason = "Contemporaneous observations from the same declared vantage disagree; reachability is unresolved."
			f.Evidence = append(f.Evidence, conflict.refs...)
			return f
		}
	}
	keys := make([]string, 0, len(latest))
	for key := range latest {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	reachable := false
	for _, key := range keys {
		current := latest[key]
		probe := current.observation
		ref := EvidenceRef{Reference: "direct_outcome:" + current.record.ID, Provenance: "operator-external-probe:pinned-tls"}
		f.Evidence = append(f.Evidence, ref)
		if probe.Outcome == "reachable" && probe.HTTPStatus >= 100 && probe.HTTPStatus <= 599 {
			reachable = true
			continue
		}
		f.Code = "REACHABILITY_NOT_ESTABLISHED"
		f.Reason = "An authorized external-vantage attempt did not establish reachability. Failure does not prove the endpoint is private."
	}
	if reachable {
		f.Status = Fail
		f.ProvenanceStatus = "SOURCE_AUTHENTICATED"
		f.Code = "PUBLIC_REACHABILITY_OBSERVED"
		f.Reason = "An authenticated HTTPS response was observed from the registered external vantage for this Ingress host. This does not establish reachability from every network or agent causality."
	}
	return f
}

// A direct absent-state observation supports deletion only when the exact
// operation was independently recorded by an authenticated audit source.
func assessDirectDeletion(in Input) []Finding {
	out := []Finding{}
	for _, action := range in.Actions {
		if action.Operation != "delete" {
			continue
		}
		f := Finding{Dimension: "resource_deletion_state", Status: Inconclusive, ProvenanceStatus: "INSUFFICIENT_PROVENANCE", Code: "DELETION_STATE_UNOBSERVED", Reason: "No fresh resulting-state observation establishes that the resource is absent.", AffectedResource: action.Kind + "/" + action.Namespace + "/" + action.Name, Evidence: []EvidenceRef{{Reference: action.ID, Provenance: action.Source}}}
		var selected *DirectObservation
		var selectedState KubernetesResourceState
		for i := range in.DirectObservations {
			record := &in.DirectObservations[i]
			state, ok := directResourceState(*record)
			if !ok || !freshResourceRecord(in, *record, in.Freshness) || state.Kind != action.Kind || state.Namespace != action.Namespace || state.Name != action.Name || record.ObservedAt.Before(action.OccurredAt) {
				continue
			}
			if selected != nil && record.AcquiredAt.Equal(selected.AcquiredAt) && state.Present != selectedState.Present {
				f.Code = "CONFLICTING_RESULTING_STATE"
				f.Reason = "Contemporaneous direct sources disagree about resource presence."
				f.Status = Conflicting
				f.Evidence = append(f.Evidence, EvidenceRef{Reference: "direct_outcome:" + selected.ID, Provenance: "direct-kubernetes-api:pinned-tls"}, EvidenceRef{Reference: "direct_outcome:" + record.ID, Provenance: "direct-kubernetes-api:pinned-tls"})
				selected = nil
				break
			}
			if selected == nil || record.AcquiredAt.After(selected.AcquiredAt) {
				selected, selectedState = record, state
			}
		}
		if selected != nil {
			f.Evidence = append(f.Evidence, EvidenceRef{Reference: "direct_outcome:" + selected.ID, Provenance: "direct-kubernetes-api:pinned-tls"})
			if !selectedState.Present && action.OriginAuthenticated {
				f.Status = Pass
				f.ProvenanceStatus = "SOURCE_AUTHENTICATED"
				f.Code = "DELETION_OBSERVED"
				f.Reason = "Authenticated audit recorded deletion and a subsequent direct API read found the scoped resource absent."
			} else if selectedState.Present {
				f.Code = "RESOURCE_STILL_PRESENT_AFTER_DELETE"
				f.Reason = "A subsequent direct API read still found the resource; deletion or controller reconciliation may be in progress."
			}
		}
		out = append(out, f)
	}
	return out
}
