package operational

import (
	"strconv"
	"strings"
	"time"
)

func assessGatewayControl(in Input) []Finding {
	out := []Finding{}
	for _, decision := range in.GatewayPreflights {
		f := Finding{Dimension: "preventive_control", Status: Inconclusive, ProvenanceStatus: "SERVER_OWNED_CUSTOMER_CONTROL", Code: "GATEWAY_PREFLIGHT_DENIED", Reason: "The connected gateway preflight rejected this proposed operation; no assertion is made about external executors.", Evidence: []EvidenceRef{{Reference: "gateway_preflight:" + decision.ID, Provenance: "server_owned_preflight"}}, AffectedResource: decision.Namespace + "/" + decision.ResourceName}
		for _, ref := range decision.ContractReferences {
			f.Evidence = append(f.Evidence, EvidenceRef{Reference: ref, Provenance: "customer_contract"})
		}
		for _, ref := range decision.ApprovalReferences {
			f.Evidence = append(f.Evidence, EvidenceRef{Reference: ref, Provenance: "authenticated_customer_user"})
		}
		if decision.Decision == "allowed" {
			f.Status = Pass
			f.Code = "GATEWAY_PREFLIGHT_ALLOWED"
			f.Reason = "The connected gateway received a scoped pre-action authorization decision; execution, attribution, and outcome remain separate."
		}
		out = append(out, f)
	}
	return out
}

func assessActionEffect(in Input) Finding {
	f := Finding{Dimension: "action_effect", Status: Inconclusive, ProvenanceStatus: "STATE_OR_REQUEST_MISSING", Code: "ACTION_EFFECT_UNRESOLVED", Reason: "A direct post-action state and a scoped gateway request are required to compare the requested and resulting image."}
	if in.Canonical.Final.SourceProvider != "direct-kubernetes-api:pinned-tls" || in.EvaluatedAt.Sub(in.Canonical.Final.CapturedAt) > in.Freshness {
		return f
	}
	for _, decision := range in.GatewayPreflights {
		if decision.Decision != "allowed" {
			continue
		}
		var audit *Action
		for i := range in.Actions {
			a := &in.Actions[i]
			if a.OriginAuthenticated && a.Kind == "Deployment" && a.Namespace == decision.Namespace && a.Name == decision.ResourceName && a.OccurredAt.After(decision.DecidedAt) && in.Canonical.Final.CapturedAt.After(a.OccurredAt) {
				audit = a
				break
			}
		}
		if audit == nil {
			continue
		}
		w := workload(in.Canonical.Final, decision.Namespace, decision.ResourceName)
		if w == nil || w.UID == "" || audit.UID != "" && w.UID != audit.UID || w.Image == "" {
			return f
		}
		f.Evidence = []EvidenceRef{{Reference: "gateway_preflight:" + decision.ID, Provenance: "server_owned_preflight"}, {Reference: "audit:" + audit.ID, Provenance: audit.Source}, {Reference: in.Canonical.Final.Reference, Provenance: in.Canonical.Final.SourceProvider}}
		f.ProvenanceStatus = "SOURCE_AUTHENTICATED"
		if w.Image != decision.Image {
			f.Status = Fail
			f.Code = "REQUESTED_IMAGE_NOT_PRESERVED"
			f.Reason = "A direct post-action Deployment observation differs from the gateway-requested image; the cause of the change is not inferred."
			return f
		}
		f.Status = Pass
		f.Code = "REQUESTED_IMAGE_OBSERVED"
		f.Reason = "A direct post-action Deployment observation matches the requested image; service outcome and agent causality remain separate."
		return f
	}
	return f
}

func metricMeetsRule(sample MetricSample, rule Rule) (bool, bool) {
	value, ok := sampleValue(&sample, rule.Type)
	if !ok {
		return false, false
	}
	if rule.Maximum != nil {
		return value <= *rule.Maximum, true
	}
	if rule.Minimum != nil {
		return value >= *rule.Minimum, true
	}
	return false, false
}
func directMetricReady(sample MetricSample, at time.Time, freshness time.Duration) bool {
	return sample.ProvenanceStatus == "SOURCE_AUTHENTICATED" && sample.RequestCount > 0 && !sample.WindowStartedAt.IsZero() && !sample.SampledAt.IsZero() && !sample.AcquiredAt.Before(sample.SampledAt.Add(-30*time.Second)) && sample.AcquiredAt.Sub(sample.SampledAt) <= freshness && at.Sub(sample.SampledAt) >= 0 && at.Sub(sample.SampledAt) <= freshness
}

// Recovery needs a before and after sample from a directly authenticated
// source, an observed operation, and the customer's actual thresholds.
func assessRecovery(in Input) Finding {
	f := Finding{Dimension: "service_recovery", Status: Inconclusive, ProvenanceStatus: "METRIC_OR_BASELINE_MISSING", Code: "RECOVERY_NOT_ESTABLISHED", Reason: "Direct pre-action and post-action service measurements are required to establish recovery."}
	var action *Action
	for i := range in.Actions {
		candidate := &in.Actions[i]
		if candidate.OriginAuthenticated && candidate.Kind == "Deployment" && (candidate.Operation == "patch" || candidate.Operation == "update") && (action == nil || candidate.OccurredAt.After(action.OccurredAt)) {
			action = candidate
		}
	}
	if action == nil {
		return f
	}
	serviceRules := 0
	verifiedRules := 0
	recovered := false
	for _, c := range in.Contracts {
		if !c.Active || c.Service == "" {
			continue
		}
		parts := strings.Split(c.Service, "/")
		if len(parts) != 2 {
			continue
		}
		for _, rule := range c.Rules {
			if rule.Type != "max_http_5xx_pct" && rule.Type != "max_p95_latency_ms" && rule.Type != "min_request_success_pct" {
				continue
			}
			serviceRules++
			var baseline, post *MetricSample
			for i := range in.MetricSamples {
				s := &in.MetricSamples[i]
				if s.Namespace != parts[0] || s.Service != parts[1] || s.ProvenanceStatus != "SOURCE_AUTHENTICATED" || s.RequestCount <= 0 || s.WindowStartedAt.IsZero() {
					continue
				}
				if s.SampledAt.Before(action.OccurredAt) && action.OccurredAt.Sub(s.SampledAt) <= in.Freshness && (baseline == nil || s.SampledAt.After(baseline.SampledAt)) {
					baseline = s
				}
				if s.SampledAt.After(action.OccurredAt) && !s.SampledAt.After(in.EvaluatedAt) && (post == nil || s.SampledAt.After(post.SampledAt)) {
					post = s
				}
			}
			if baseline == nil || post == nil || post.WindowStartedAt.Before(action.OccurredAt) || !directMetricReady(*post, in.EvaluatedAt, in.Freshness) || action.OccurredAt.Sub(baseline.SampledAt) > in.Freshness {
				return f
			}
			before, bok := metricMeetsRule(*baseline, rule)
			after, aok := metricMeetsRule(*post, rule)
			if !bok || !aok {
				return f
			}
			f.Evidence = append(f.Evidence, EvidenceRef{Reference: "metric_sample:" + baseline.ID, Provenance: "SOURCE_AUTHENTICATED"}, EvidenceRef{Reference: "metric_sample:" + post.ID, Provenance: "SOURCE_AUTHENTICATED"}, EvidenceRef{Reference: "audit:" + action.ID, Provenance: action.Source}, EvidenceRef{Reference: "contract:" + c.ID + ":v" + itoaVersion(c.Version), Provenance: "customer_contract"})
			if !after {
				f.Status = Fail
				f.ProvenanceStatus = "SOURCE_AUTHENTICATED"
				f.Code = "SERVICE_RECOVERY_FAILED"
				f.Reason = "A direct post-action service sample breaches the customer threshold; the Kubernetes mutation did not establish service recovery."
				return f
			}
			if !before {
				recovered = true
			}
			verifiedRules++
		}
	}
	if serviceRules == 0 || verifiedRules != serviceRules {
		return f
	}
	f.Status = Pass
	f.ProvenanceStatus = "SOURCE_AUTHENTICATED"
	f.Code = "SERVICE_HEALTH_MAINTAINED"
	f.Reason = "Direct pre-action and post-action samples meet customer thresholds; sustained stability remains separate."
	if recovered {
		f.Code = "SERVICE_RECOVERED"
		f.Reason = "A direct pre-action sample breached a customer threshold and a sufficient post-action sample met it; causality remains a separate assessment."
	}
	return f
}
func itoaVersion(v int) string { return strconv.Itoa(v) }
