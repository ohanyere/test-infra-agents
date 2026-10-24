package verification

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/evalplane/evalplane/backend/internal/agentgateway"
	"github.com/evalplane/evalplane/backend/internal/evidence"
	"github.com/evalplane/evalplane/backend/internal/kubeaudit"
)

func TestIndependentScaleRequiresAuditAndObservedOutcome(t *testing.T) {
	base := time.Date(2026, 10, 24, 12, 0, 0, 0, time.UTC)
	claim := IndependentScaleClaim{Principal: "system:serviceaccount:realverax-audit-proof:direct-agent", Namespace: "realverax-audit-proof", Name: "direct-app", Replicas: 5, Success: true}
	input := independentInput(base, 5, 5)
	event := kubeaudit.Event{APIVersion: kubeaudit.AuditAPIVersion, Kind: "Event", AuditID: "audit-direct-scale", Stage: "ResponseComplete", Verb: "patch", RequestAt: base.Add(time.Second), StageAt: base.Add(2 * time.Second), User: kubeaudit.User{Username: claim.Principal}, ObjectRef: kubeaudit.ObjectRef{APIGroup: "apps", APIVersion: "v1", Resource: "deployments", Namespace: claim.Namespace, Name: claim.Name}, Response: kubeaudit.Response{Code: 200}}
	source := kubeaudit.SourceIdentity{SourceType: "kubernetes-apiserver-audit", ClusterIdentity: "kind:evalplane-e2e", ConfigurationIdentity: "kind-audit-policy:sha256:fixture", AcquisitionMethod: "runner-docker-copy-from-kind-control-plane", VerificationState: "runner_bound_test_source", AcquiredAt: base.Add(5 * time.Second)}
	positive := deriveIndependentScale(input, claim, event, source, true)
	if positive.OverallStatus != StatusVerified || positive.ActionInspection.Attribution.Status != AttributionAttributed || positive.Outcome.Status != "PASSED" || positive.Final.GitOps != nil {
		t.Fatalf("direct scale was not independently verified: status=%s attribution=%s outcome=%s", positive.OverallStatus, positive.ActionInspection.Attribution.Status, positive.Outcome.Status)
	}
	if len(positive.Provenance.Providers) == 0 || positive.Provenance.Providers[len(positive.Provenance.Providers)-1].Role != "action_evidence" {
		t.Fatal("audit provider provenance is missing")
	}
	missing := deriveIndependentScale(input, claim, kubeaudit.Event{}, kubeaudit.SourceIdentity{}, false)
	if missing.OverallStatus != StatusInconclusive || missing.ActionInspection.Attribution.Status != AttributionInconclusive {
		t.Fatalf("missing audit verified a claim: %s", missing.OverallStatus)
	}
	denied := event
	denied.Response.Code = 403
	deniedResult := deriveIndependentScale(independentInput(base, 3, 3), claim, denied, source, true)
	if deniedResult.OverallStatus != StatusFailed || deniedResult.ActionInspection.Denied != 1 || deniedResult.ActionInspection.Executed != 0 {
		t.Fatalf("RBAC denial was treated as success: %+v", deniedResult.ActionInspection)
	}
	wrongPrincipal := event
	wrongPrincipal.User.Username = "system:serviceaccount:realverax-audit-proof:independent-observer"
	wrong := deriveIndependentScale(input, claim, wrongPrincipal, source, true)
	if wrong.OverallStatus == StatusVerified {
		t.Fatal("another Kubernetes principal was attributed to the agent")
	}
	falseClaim := deriveIndependentScale(independentInput(base, 3, 3), claim, kubeaudit.Event{}, kubeaudit.SourceIdentity{}, false)
	if falseClaim.OverallStatus != StatusInconclusive || falseClaim.Outcome.Status == "PASSED" {
		t.Fatal("a false success claim passed without an API response")
	}
	contradicted := deriveIndependentScale(independentInput(base, 3, 3), claim, event, source, true)
	if contradicted.OverallStatus == StatusVerified || contradicted.ActionInspection.Attribution.Status == AttributionAttributed {
		t.Fatal("a successful API response without the claimed state transition was verified")
	}
	missingBaseline := input
	missingBaseline.Events = missingBaseline.Events[1:]
	withoutBaseline := deriveIndependentScale(missingBaseline, claim, event, source, true)
	if withoutBaseline.OverallStatus == StatusVerified {
		t.Fatal("missing baseline evidence was verified")
	}
}

func independentInput(base time.Time, desired, ready int32) BuildInput {
	workload := func(replicas, available int32, generation int64) agentgateway.Observation {
		return agentgateway.Observation{Workloads: []agentgateway.WorkloadObservation{{Kind: "Deployment", Namespace: "realverax-audit-proof", Name: "direct-app", UID: "workload-uid", ResourceVersion: "rv", Generation: generation, ObservedGeneration: generation, DesiredReplicas: replicas, ReadyReplicas: available, AvailableReplicas: available, UpdatedReplicas: available}}, Metrics: map[string]any{}}
	}
	makeEvent := func(id int64, summary string, at time.Time, obs agentgateway.Observation) evidence.RunEvent {
		obs.Timestamp = at
		payload, _ := json.Marshal(map[string]any{"observation": obs, "source_provider": "kubernetes-api:read-only-serviceaccount"})
		return evidence.RunEvent{ID: id, RunID: "run", EventType: "observation", Summary: summary, OccurredAt: at, Payload: payload}
	}
	return BuildInput{RunID: "run", Objective: "Scale direct-app to five ready replicas", Criteria: []Criterion{{Key: "five-ready", Metric: "ready_replicas", Operator: "eq", Target: json.RawMessage(`5`)}}, Now: base.Add(5 * time.Second), Events: []evidence.RunEvent{makeEvent(1, "verification_baseline_captured", base, workload(3, 3, 1)), makeEvent(2, "stability_observation_collected", base.Add(3*time.Second), workload(desired, ready, 2)), makeEvent(3, "final_observation_collected", base.Add(4*time.Second), workload(desired, ready, 2))}}
}
