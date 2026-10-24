package operational

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/evalplane/evalplane/backend/internal/agentgateway"
	"github.com/evalplane/evalplane/backend/internal/gitops"
	"github.com/evalplane/evalplane/backend/internal/verification"
)

func TestDirectOutcomesRemainSeparate(t *testing.T) {
	now := time.Now().UTC().Truncate(time.Second)
	actionAt := now.Add(-90 * time.Second)
	stateAt := now.Add(-45 * time.Second)
	metricAt := now.Add(-30 * time.Second)
	floor, errorCeiling := float64(3), float64(1)
	contract := Contract{ID: "customer-contract", Version: 4, Active: true, EffectiveAt: now.Add(-time.Hour), Service: "checkout/checkout", Rules: []Rule{
		{Type: "min_available_replicas", Namespace: "checkout", ResourceName: "checkout", Minimum: &floor},
		{Type: "post_image", Namespace: "checkout", ResourceName: "checkout", Value: "checkout:v2"},
		{Type: "max_http_5xx_pct", Maximum: &errorCeiling},
		{Type: "no_public_exposure", Namespace: "checkout", ResourceName: "checkout"},
	}}
	snapshot := agentgateway.Observation{Timestamp: stateAt, Workloads: []agentgateway.WorkloadObservation{{Kind: "Deployment", Namespace: "checkout", Name: "checkout", UID: "uid-checkout", Generation: 2, ObservedGeneration: 2, DesiredReplicas: 3, ReadyReplicas: 3, AvailableReplicas: 3, UpdatedReplicas: 3, Image: "checkout:v2"}}}
	statePayload, _ := json.Marshal(map[string]any{"observation": snapshot})
	argoPayload, _ := json.Marshal(map[string]any{"gitops": gitops.Evidence{Desired: &gitops.DesiredState{Reference: "contract:customer-contract:v4", Revision: "abcdef", Resource: "Deployment/checkout/checkout", Image: "checkout:v2", CapturedAt: stateAt}, Argo: &gitops.ArgoState{Reference: "direct_outcome:argo", SyncStatus: "Synced", HealthStatus: "Healthy", ObservedRevision: "abcdef", CapturedAt: stateAt}}})
	makeMetric := func(id string, at time.Time, rate float64) DirectObservation {
		sample := MetricSample{ID: id, Namespace: "checkout", Service: "checkout", SampledAt: at, AcquiredAt: at, WindowStartedAt: at.Add(-time.Minute), RequestCount: 100, ErrorRatePct: &rate, ProvenanceStatus: "SOURCE_AUTHENTICATED"}
		payload, _ := json.Marshal(map[string]any{"metric_sample": sample})
		return DirectObservation{ID: id, Kind: "prometheus", Payload: payload, ObservedAt: at, AcquiredAt: at}
	}
	preflight := GatewayPreflight{ID: "preflight", Decision: "allowed", Namespace: "checkout", ResourceName: "checkout", Image: "checkout:v2", DecidedAt: actionAt.Add(-time.Second)}
	input := Input{Canonical: verification.Result{ID: "canonical", OverallStatus: verification.StatusInconclusive, Final: verification.Snapshot{Reference: "collector-final", SourceProvider: "customer-collector"}}, Contracts: []Contract{contract}, DirectObservations: []DirectObservation{{ID: "state", Kind: "kubernetes_state", Payload: statePayload, AcquiredAt: stateAt}, {ID: "argo", Kind: "argocd", Payload: argoPayload, AcquiredAt: stateAt}, makeMetric("before", actionAt.Add(-30*time.Second), 4), makeMetric("after", metricAt, 0.2)}, Actions: []Action{{ID: "audit", Kind: "Deployment", Namespace: "checkout", Name: "checkout", UID: "uid-checkout", Operation: "patch", OccurredAt: actionAt, Source: "customer-api-server-audit", OriginAuthenticated: true}}, GatewayPreflights: []GatewayPreflight{preflight}, EvaluatedAt: now, Freshness: 2 * time.Minute}
	r := Evaluate(input)
	for dimension, expected := range map[string]string{"infrastructure_state": Pass, "gitops": Pass, "service_outcome": Pass, "service_recovery": Pass, "action_effect": Pass} {
		if f := finding(r, dimension); f.Status != expected || f.ProvenanceStatus != "SOURCE_AUTHENTICATED" {
			t.Fatalf("%s: %+v", dimension, f)
		}
	}
	if r.CanonicalOverallStatus != verification.StatusInconclusive || r.Final.SourceProvider != "customer-collector" || r.IndependentFinal == nil {
		t.Fatalf("canonical outcome was overwritten or direct state lost: %+v", r)
	}
	for _, f := range r.Findings {
		if f.RuleType == "no_public_exposure" && (f.Status != Inconclusive || f.Code != "NETWORK_EVIDENCE_MISSING") {
			t.Fatalf("Deployment image was treated as exposure evidence: %+v", f)
		}
	}
	input.DirectObservations[3] = makeMetric("bad-after", metricAt, 5)
	r = Evaluate(input)
	if f := finding(r, "service_outcome"); f.Status != Fail || f.Code != "SERVICE_THRESHOLD_BREACHED" {
		t.Fatalf("successful mutation concealed failed service: %+v", f)
	}
	if f := finding(r, "service_recovery"); f.Status != Fail || f.Code != "SERVICE_RECOVERY_FAILED" {
		t.Fatalf("failed recovery concealed: %+v", f)
	}
	input.DirectObservations = input.DirectObservations[:2]
	r = Evaluate(input)
	if f := finding(r, "service_outcome"); f.Status != Inconclusive {
		t.Fatalf("missing metrics passed: %+v", f)
	}
	input.EvaluatedAt = now.Add(5 * time.Minute)
	r = Evaluate(input)
	if f := finding(r, "infrastructure_state"); f.Status != Inconclusive || f.Code != "MISSING_OR_STALE_STATE" {
		t.Fatalf("stale state passed: %+v", f)
	}
}

func TestContradictoryDirectStateCannotEstablishFinalOutcome(t *testing.T) {
	now := time.Now().UTC()
	floor := float64(1)
	mk := func(id, source, image string) DirectObservation {
		payload, _ := json.Marshal(map[string]any{"observation": agentgateway.Observation{Timestamp: now, Workloads: []agentgateway.WorkloadObservation{{Kind: "Deployment", Namespace: "checkout", Name: "checkout", UID: "same-uid", Image: image, Generation: 2, ObservedGeneration: 2, DesiredReplicas: 1, UpdatedReplicas: 1, AvailableReplicas: 1}}}})
		return DirectObservation{ID: id, SourceID: source, Kind: "kubernetes_state", Payload: payload, ObservedAt: now, AcquiredAt: now}
	}
	contract := Contract{ID: "c", Version: 1, Active: true, EffectiveAt: now.Add(-time.Hour), Rules: []Rule{{Type: "min_available_replicas", Namespace: "checkout", ResourceName: "checkout", Minimum: &floor}}}
	report := Evaluate(Input{Canonical: verification.Result{Final: verification.Snapshot{}}, Contracts: []Contract{contract}, DirectObservations: []DirectObservation{mk("one", "source-one", "checkout:v1"), mk("two", "source-two", "checkout:v2")}, EvaluatedAt: now})
	if report.IndependentFinal != nil || finding(report, "infrastructure_state").Status != Inconclusive {
		t.Fatalf("conflicting sources promoted a final state: %+v", report)
	}
	if f := finding(report, "source_consistency"); f.Status != Conflicting || len(f.Evidence) != 2 {
		t.Fatalf("contradiction was not retained: %+v", f)
	}
}

func TestDirectMetricWindowRequiresCoverageAndFreshness(t *testing.T) {
	now := time.Now().UTC()
	max := float64(1)
	c := Contract{ID: "service", Version: 1, Active: true, EffectiveAt: now.Add(-time.Hour), Service: "checkout/checkout", Rules: []Rule{{Type: "max_http_5xx_pct", Maximum: &max, WindowSeconds: 60}}}
	rate := float64(0.3)
	first := MetricSample{ID: "first", Namespace: "checkout", Service: "checkout", SampledAt: now.Add(-35 * time.Second), AcquiredAt: now.Add(-35 * time.Second), WindowStartedAt: now.Add(-95 * time.Second), RequestCount: 100, ErrorRatePct: &rate, ProvenanceStatus: "SOURCE_AUTHENTICATED"}
	last := first
	last.ID = "last"
	last.SampledAt = now.Add(-5 * time.Second)
	last.AcquiredAt = last.SampledAt
	last.WindowStartedAt = last.SampledAt.Add(-time.Minute)
	in := Input{Contracts: []Contract{c}, MetricSamples: []MetricSample{first, last}, EvaluatedAt: now, Freshness: 2 * time.Minute}
	if f := assessMetricWindow(in); f.Status != Pass || f.Code != "SERVICE_WINDOW_STABLE" {
		t.Fatalf("covered direct window did not pass: %+v", f)
	}
	first.WindowStartedAt = first.SampledAt
	in.MetricSamples[0] = first
	in.MetricSamples[1].WindowStartedAt = last.SampledAt
	if f := assessMetricWindow(in); f.Status != Inconclusive {
		t.Fatalf("partial coverage passed: %+v", f)
	}
	in.MetricSamples[0].WindowStartedAt = now.Add(-95 * time.Second)
	in.EvaluatedAt = now.Add(3 * time.Minute)
	if f := assessMetricWindow(in); f.Status != Inconclusive {
		t.Fatalf("stale window passed: %+v", f)
	}
}
