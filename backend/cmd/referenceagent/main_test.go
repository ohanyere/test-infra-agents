package main

import (
	"encoding/json"
	"testing"

	"github.com/evalplane/evalplane/backend/internal/agentgateway"
)

func TestReferencePolicyUsesOnlyObservedPressureAndPermission(t *testing.T) {
	request := agentgateway.Request{AvailableActions: []string{"inspect", "scale_workload", "no_op"}, AllowedResources: json.RawMessage(`[{"kind":"Deployment","namespace":"owned","name":"app"}]`), Observation: agentgateway.Observation{Workloads: []agentgateway.WorkloadObservation{{Kind: "Deployment", Namespace: "owned", Name: "app", DesiredReplicas: 2, AvailableReplicas: 2}}, Metrics: map[string]any{}}}
	if got := decide(request).Actions[0].Type; got != "inspect" {
		t.Fatalf("unavailable telemetry: %s", got)
	}
	request.Observation.Metrics["error_rate_pct"] = float64(20)
	if got := decide(request).Actions[0]; got.Type != "scale_workload" || got.Parameters["replicas"] != 4 {
		t.Fatalf("pressure decision: %+v", got)
	}
	request.AvailableActions = []string{"inspect", "no_op"}
	if got := decide(request).Actions[0].Type; got != "inspect" {
		t.Fatalf("unauthorized scale: %s", got)
	}
	request.Observation.Workloads[0].DesiredReplicas = 4
	request.Observation.Workloads[0].AvailableReplicas = 4
	request.Observation.Metrics["error_rate_pct"] = float64(0)
	if got := decide(request).Actions[0].Type; got != "no_op" {
		t.Fatalf("healthy decision: %s", got)
	}
}

func TestReferencePolicyRollsBackFailedImage(t *testing.T) {
	request := agentgateway.Request{AvailableActions: []string{"inspect", "set_workload_image", "no_op"}, AllowedResources: json.RawMessage(`[{"kind":"Deployment","namespace":"owned","name":"app"}]`), Observation: agentgateway.Observation{Workloads: []agentgateway.WorkloadObservation{{Kind: "Deployment", Namespace: "owned", Name: "app", Image: "checkout:v18", DesiredReplicas: 2, AvailableReplicas: 2}}, Metrics: map[string]any{"error_rate_pct": float64(100)}}}
	got := decide(request).Actions[0]
	if got.Type != "set_workload_image" || got.Parameters["image"] != "checkout:v17" {
		t.Fatalf("action=%+v", got)
	}
	request.Observation.Workloads[0].Image = "checkout:v17"
	request.Observation.Metrics["error_rate_pct"] = float64(0)
	if got = decide(request).Actions[0]; got.Type != "no_op" {
		t.Fatalf("post-recovery action=%+v", got)
	}
}
