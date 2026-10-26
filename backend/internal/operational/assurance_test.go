package operational

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/evalplane/evalplane/backend/internal/agentgateway"
	"github.com/evalplane/evalplane/backend/internal/gitops"
	"github.com/evalplane/evalplane/backend/internal/verification"
)

func TestDraftDoesNotDeactivateActiveContract(t *testing.T) {
	now := time.Now().UTC()
	history := []Contract{{ID: "one", Version: 1, Active: true, State: "active", EffectiveAt: now.Add(-time.Hour)}, {ID: "one", Version: 2, State: "draft", EffectiveAt: now.Add(-time.Minute)}}
	selected := activeContracts(history, now)
	if len(selected) != 1 || selected[0].Version != 1 {
		t.Fatalf("draft replaced active contract: %+v", selected)
	}
}
func TestApprovalTimeScopeAndConflict(t *testing.T) {
	now := time.Now().UTC()
	requested := verification.InspectedAction{ID: "action-1", Status: "executed", TargetRef: "checkout/checkout", OccurredAt: now}
	in := Input{Canonical: verification.Result{ActionInspection: verification.ActionInspection{Actions: []verification.InspectedAction{requested}}}, EvaluatedAt: now}
	rule := Rule{Type: "require_approval", Namespace: "checkout", ResourceName: "checkout", ApprovalRequired: true}
	if f := assessApproval(in, rule); f.Status != Inconclusive || f.Code != "APPROVAL_EVIDENCE_MISSING" {
		t.Fatalf("missing approval: %+v", f)
	}
	in.Approvals = []ApprovalRecord{{ID: "late", ActionID: "action-1", Resource: "checkout/checkout", Decision: "approved", RecordedAt: now.Add(time.Second), ValidUntil: now.Add(time.Hour)}}
	if f := assessApproval(in, rule); f.Code != "APPROVAL_EVIDENCE_MISSING" {
		t.Fatalf("late approval accepted: %+v", f)
	}
	in.Approvals[0].RecordedAt = now.Add(-time.Minute)
	if f := assessApproval(in, rule); f.Code != "APPROVAL_RECORDED" || f.Status != Inconclusive {
		t.Fatalf("approval promoted to operation proof: %+v", f)
	}
	in.Approvals = append(in.Approvals, ApprovalRecord{ID: "denial", ActionID: "action-1", Resource: "checkout/checkout", Decision: "denied", RecordedAt: now.Add(-time.Minute), ValidUntil: now.Add(time.Hour)})
	if f := assessApproval(in, rule); f.Status != Conflicting {
		t.Fatalf("conflicting approvals: %+v", f)
	}
}
func TestExternalClaimCanBeMatchedWithoutProvingExecution(t *testing.T) {
	now := time.Now().UTC()
	raw, _ := json.Marshal(map[string]any{"claim_id": "claim-1", "resource_namespace": "checkout", "resource_name": "checkout", "claimed_started_at": now, "claimed_outcome": "executed"})
	in := Input{ClaimPayloads: []json.RawMessage{raw}, Approvals: []ApprovalRecord{{ID: "approval-1", ActionID: "claim-1", Resource: "checkout/checkout", Decision: "approved", RecordedAt: now.Add(-time.Minute), ValidUntil: now.Add(time.Minute)}}}
	f := assessApproval(in, Rule{Type: "require_approval", Namespace: "checkout", ResourceName: "checkout", ApprovalRequired: true})
	if f.Code != "APPROVAL_RECORDED" || f.Status != Inconclusive {
		t.Fatalf("external claim/approval trust: %+v", f)
	}
}
func TestServiceReadinessPreconditionUsesOnlyPriorFreshSample(t *testing.T) {
	now := time.Now().UTC()
	ceiling := float64(1)
	rate := float64(4)
	in := Input{Canonical: verification.Result{ActionInspection: verification.ActionInspection{Actions: []verification.InspectedAction{{ID: "action", Status: "executed", TargetRef: "checkout/checkout", OccurredAt: now}}}}, Freshness: time.Minute, MetricSamples: []MetricSample{{ID: "late", Namespace: "checkout", Service: "checkout", SampledAt: now.Add(time.Second), AcquiredAt: now.Add(time.Second), ErrorRatePct: &rate}}}
	rule := Rule{Type: "service_ready_before_mutation", Namespace: "checkout", ResourceName: "checkout", Maximum: &ceiling}
	if f := assessRule(in, "checkout/checkout", rule); f.Code != "SERVICE_PRECONDITION_METRIC_MISSING" {
		t.Fatalf("post-action metric used as precondition: %+v", f)
	}
	in.MetricSamples[0].SampledAt = now.Add(-time.Minute)
	in.MetricSamples[0].AcquiredAt = now.Add(-time.Minute)
	if f := assessRule(in, "checkout/checkout", rule); f.Status != Inconclusive || f.Code != "SERVICE_READINESS_BREACH_CANDIDATE" {
		t.Fatalf("unverified service precondition promoted: %+v", f)
	}
}
func TestPreconditionAndPostconditionRemainProvisional(t *testing.T) {
	now := time.Now().UTC()
	floor := float64(3)
	requested := verification.InspectedAction{ID: "action", Status: "executed", TargetRef: "checkout/checkout", OccurredAt: now}
	baseline := verification.Snapshot{Reference: "baseline", CapturedAt: now.Add(-time.Minute), Observation: agentgateway.Observation{Timestamp: now.Add(-time.Minute), Workloads: []agentgateway.WorkloadObservation{{Kind: "Deployment", Namespace: "checkout", Name: "checkout", AvailableReplicas: 2}}}}
	final := verification.Snapshot{Reference: "final", CapturedAt: now.Add(time.Minute), Observation: agentgateway.Observation{Timestamp: now.Add(time.Minute), Workloads: []agentgateway.WorkloadObservation{{Kind: "Deployment", Namespace: "checkout", Name: "checkout", Image: "old", Generation: 2, ObservedGeneration: 2}}}}
	in := Input{Canonical: verification.Result{Baseline: baseline, Final: final, ActionInspection: verification.ActionInspection{Actions: []verification.InspectedAction{requested}}}, Freshness: 2 * time.Minute, EvaluatedAt: now.Add(time.Minute)}
	if f := assessRule(in, "", Rule{Type: "ready_before_mutation", Namespace: "checkout", ResourceName: "checkout", Minimum: &floor}); f.Status != Inconclusive || !strings.Contains(f.Code, "CANDIDATE") {
		t.Fatalf("unverified baseline promoted: %+v", f)
	}
	in.Canonical.Baseline.Observation.Timestamp = now.Add(-time.Hour)
	if f := assessRule(in, "", Rule{Type: "ready_before_mutation", Namespace: "checkout", ResourceName: "checkout", Minimum: &floor}); f.Code != "STALE_OR_MISSING_PRECONDITION" {
		t.Fatalf("stale baseline accepted: %+v", f)
	}
	if f := assessRule(in, "", Rule{Type: "post_image", Namespace: "checkout", ResourceName: "checkout", Value: "new"}); f.Status != Inconclusive || f.Code != "UNVERIFIED_IMAGE_POSTCONDITION_BREACH_CANDIDATE" {
		t.Fatalf("unverified final promoted: %+v", f)
	}
}
func TestOverlappingContractsAndFindingDisposition(t *testing.T) {
	a, b := float64(3), float64(5)
	c := []Contract{{ID: "a", Version: 1, Active: true, Rules: []Rule{{Type: "min_available_replicas", Namespace: "checkout", ResourceName: "checkout", Minimum: &a}}}, {ID: "b", Version: 1, Active: true, Rules: []Rule{{Type: "min_available_replicas", Namespace: "checkout", ResourceName: "checkout", Minimum: &b}}}}
	if f := assessContractConflicts(c); len(f) != 1 || f[0].Status != Conflicting {
		t.Fatalf("contract overlap: %+v", f)
	}
	if disposition := findingDisposition(Finding{Status: Fail, ProvenanceStatus: "COLLECTOR_AUTHENTICATED_SOURCE_UNVERIFIED"}); disposition != "candidate" {
		t.Fatalf("unverified breach promoted: %s", disposition)
	}
	if disposition := findingDisposition(Finding{Status: Fail, ContractID: "contract", ProvenanceStatus: "SOURCE_AUTHENTICATED", Evidence: []EvidenceRef{{Reference: "source"}}}); disposition != "violation" {
		t.Fatalf("authenticated breach not classified: %s", disposition)
	}
	if disposition := findingDisposition(Finding{Status: Fail, ProvenanceStatus: "SOURCE_AUTHENTICATED", Evidence: []EvidenceRef{{Reference: "source"}}}); disposition != "candidate" {
		t.Fatalf("unscoped outcome promoted to contract violation: %s", disposition)
	}
}
func TestUnavailableReconciliationCreatesProvisionalNotification(t *testing.T) {
	r := Evaluate(Input{Reconciliations: []ReconciliationState{{ID: "case-1", State: "unavailable", Result: "missing", Reason: "audit stream disconnected"}}, EvaluatedAt: time.Now().UTC()})
	var found bool
	for _, f := range r.Findings {
		if f.Code == "RECONCILIATION_FAILURE" {
			found = true
			if f.Status != Inconclusive || notificationKind(f, findingDisposition(f)) != "reconciliation_failure" {
				t.Fatalf("reconciliation promoted or not notified: %+v", f)
			}
		}
	}
	if !found {
		t.Fatal("missing reconciliation finding")
	}
}
func TestServiceWindowNeedsCoverageAndKeepsUnverifiedBreachProvisional(t *testing.T) {
	now := time.Now().UTC()
	ceiling := float64(1)
	rate := float64(4)
	contract := Contract{ID: "service", Active: true, Service: "checkout/checkout", Rules: []Rule{{Type: "max_http_5xx_pct", Maximum: &ceiling, WindowSeconds: 60}}}
	in := Input{Contracts: []Contract{contract}, Window: time.Minute, Freshness: time.Minute, EvaluatedAt: now}
	if f := assessMetricWindow(in); f.Code != "SERVICE_WINDOW_UNOBSERVED" {
		t.Fatalf("missing window passed: %+v", f)
	}
	in.MetricSamples = []MetricSample{{ID: "first", Namespace: "checkout", Service: "checkout", SampledAt: now.Add(-time.Minute), AcquiredAt: now.Add(-time.Minute), ErrorRatePct: &rate, ProvenanceStatus: "COLLECTOR_AUTHENTICATED_METRICS_UNVERIFIED"}, {ID: "middle", Namespace: "checkout", Service: "checkout", SampledAt: now.Add(-30 * time.Second), AcquiredAt: now.Add(-30 * time.Second), ErrorRatePct: &rate, ProvenanceStatus: "COLLECTOR_AUTHENTICATED_METRICS_UNVERIFIED"}, {ID: "last", Namespace: "checkout", Service: "checkout", SampledAt: now, AcquiredAt: now, ErrorRatePct: &rate, ProvenanceStatus: "COLLECTOR_AUTHENTICATED_METRICS_UNVERIFIED"}}
	if f := assessMetricWindow(in); f.Status != Inconclusive || f.Code != "UNVERIFIED_SERVICE_WINDOW_BREACH_CANDIDATE" {
		t.Fatalf("forwarded metric promoted: %+v", f)
	}
}
func TestDriftWindowSeparatesReconciliationAndRegression(t *testing.T) {
	now := time.Now().UTC()
	sample := func(at time.Time, image string) verification.Snapshot {
		return verification.Snapshot{Reference: at.String(), CapturedAt: at, Observation: agentgateway.Observation{Workloads: []agentgateway.WorkloadObservation{{Kind: "Deployment", Namespace: "checkout", Name: "checkout", Image: image}}}}
	}
	in := Input{Canonical: verification.Result{Final: verification.Snapshot{GitOps: &gitops.Evidence{Desired: &gitops.DesiredState{Resource: "Deployment/checkout/checkout", Image: "checkout:v2"}}}}, Window: time.Minute}
	samples := []verification.Snapshot{sample(now.Add(-time.Minute), "checkout:v1"), sample(now, "checkout:v2")}
	if f := assessDriftWindow(in, samples); f.Code != "GITOPS_RECONCILIATION_OBSERVED_PROVISIONAL" || f.Status != Inconclusive {
		t.Fatalf("reconciliation: %+v", f)
	}
	samples[0] = sample(now.Add(-time.Minute), "checkout:v2")
	samples[1] = sample(now, "checkout:v1")
	if f := assessDriftWindow(in, samples); f.Code != "POST_CHANGE_DRIFT_CANDIDATE" || f.Status != Inconclusive {
		t.Fatalf("regression causality promoted: %+v", f)
	}
}
func TestWebhookDeliveryRequires2xxAndSignature(t *testing.T) {
	secret := []byte("a-signing-secret-over-16-bytes")
	received := false
	client := &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		body, _ := io.ReadAll(r.Body)
		mac := hmac.New(sha256.New, secret)
		_, _ = mac.Write(body)
		if r.Method != http.MethodPost || r.Header.Get("Idempotency-Key") != "event-1" || r.Header.Get("X-Realverax-Signature") != "sha256="+hex.EncodeToString(mac.Sum(nil)) {
			t.Error("webhook request lacks authenticated delivery metadata")
		}
		received = true
		return &http.Response{StatusCode: http.StatusAccepted, Body: io.NopCloser(strings.NewReader("")), Header: make(http.Header)}, nil
	})}
	sender := WebhookSender{Endpoint: "http://localhost/webhook", Secret: secret, Client: client}
	if err := sender.Send(context.Background(), "event-1", []byte(`{"kind":"candidate_violation"}`)); err != nil || !received {
		t.Fatalf("webhook delivery: %v", err)
	}
	if err := (WebhookSender{Endpoint: "http://example.com", Secret: secret}).Send(context.Background(), "event-2", []byte(`{}`)); err == nil {
		t.Fatal("insecure remote webhook accepted")
	}
	reject := WebhookSender{Endpoint: "http://localhost/webhook", Secret: secret, Client: &http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: http.StatusServiceUnavailable, Body: io.NopCloser(strings.NewReader("")), Header: make(http.Header)}, nil
	})}}
	if err := reject.Send(context.Background(), "event-3", []byte(`{}`)); err == nil {
		t.Fatal("non-2xx webhook response treated as delivered")
	}
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }
