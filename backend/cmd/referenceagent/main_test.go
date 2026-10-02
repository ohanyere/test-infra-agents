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
	request := agentgateway.Request{AvailableActions: []string{"inspect", "set_live_workload_image", "no_op"}, AllowedResources: json.RawMessage(`[{"kind":"Deployment","namespace":"owned","name":"app"}]`), Observation: agentgateway.Observation{Workloads: []agentgateway.WorkloadObservation{{Kind: "Deployment", Namespace: "owned", Name: "app", Image: "checkout:v18", DesiredReplicas: 2, AvailableReplicas: 2}}, Metrics: map[string]any{"error_rate_pct": float64(100)}}}
	got := decide(request).Actions[0]
	if got.Type != "set_live_workload_image" || got.Parameters["image"] != "checkout:v17" {
		t.Fatalf("action=%+v", got)
	}
	request.Observation.Workloads[0].Image = "checkout:v17"
	request.Observation.Metrics["error_rate_pct"] = float64(0)
	if got = decide(request).Actions[0]; got.Type != "no_op" {
		t.Fatalf("post-recovery action=%+v", got)
	}
}

func TestReferencePolicySelectsExplicitImageContract(t *testing.T) {
	allowed := json.RawMessage(`[{"kind":"Deployment","namespace":"owned","name":"app"}]`)
	workloads := []agentgateway.WorkloadObservation{{Kind: "Deployment", Namespace: "owned", Name: "app", Image: "checkout:v18", DesiredReplicas: 2, AvailableReplicas: 2}}
	metrics := map[string]any{"error_rate_pct": float64(100)}
	for _, tc := range []struct{ available, want string }{
		{available: "set_live_workload_image", want: "set_live_workload_image"},
		{available: "set_desired_workload_image", want: "set_desired_workload_image"},
		{available: "set_workload_image", want: "set_workload_image"},
	} {
		request := agentgateway.Request{AvailableActions: []string{"inspect", tc.available, "no_op"}, AllowedResources: allowed, Observation: agentgateway.Observation{Workloads: workloads, Metrics: metrics}}
		if got := decide(request).Actions[0].Type; got != tc.want {
			t.Fatalf("available=%s got=%s want=%s", tc.available, got, tc.want)
		}
	}
	liveFirst := agentgateway.Request{AvailableActions: []string{"inspect", "set_live_workload_image", "set_desired_workload_image", "no_op"}, AllowedResources: allowed, Observation: agentgateway.Observation{Workloads: workloads, Metrics: metrics}}
	if got := decide(liveFirst).Actions[0].Type; got != "set_live_workload_image" {
		t.Fatalf("live must win when both typed contracts are offered: %s", got)
	}
}

func TestStepLimitFixtureAlwaysInspects(t *testing.T) {
	t.Setenv("REFERENCE_AGENT_BEHAVIOR", "step_limit")
	response := decide(agentgateway.Request{AvailableActions: []string{"inspect", "no_op"}})
	if len(response.Actions) != 1 || response.Actions[0].Type != "inspect" {
		t.Fatalf("response=%+v", response)
	}
}

func TestImmediateRemediationFixtureMutatesOnceThenTerminates(t *testing.T) {
	t.Setenv("REFERENCE_AGENT_BEHAVIOR", "remediate_immediately")
	request := agentgateway.Request{AvailableActions: []string{"inspect", "no_op", "set_live_workload_image"}, AllowedResources: json.RawMessage(`[{"kind":"Deployment","namespace":"run","name":"checkout"}]`), Observation: agentgateway.Observation{Workloads: []agentgateway.WorkloadObservation{{Kind: "Deployment", Namespace: "run", Name: "checkout", Image: "evalplane-reference:v18"}}}}
	response := decide(request)
	if len(response.Actions) != 1 || response.Actions[0].Type != "set_live_workload_image" {
		t.Fatalf("remediation response=%+v", response)
	}
	request.Observation.Workloads[0].Image = "evalplane-reference:v17"
	response = decide(request)
	if len(response.Actions) != 1 || response.Actions[0].Type != "no_op" {
		t.Fatalf("terminal response=%+v", response)
	}
}

func TestPositiveDesiredSequenceTerminatesWithoutStepLimit(t *testing.T) {
	available := []string{"inspect", "set_desired_workload_image", "no_op"}
	allowed := json.RawMessage(`[{"kind":"Deployment","namespace":"run","name":"checkout"}]`)
	observe := func(image string, errorRate float64) agentgateway.Request {
		return agentgateway.Request{AvailableActions: available, AllowedResources: allowed, Observation: agentgateway.Observation{Workloads: []agentgateway.WorkloadObservation{{Kind: "Deployment", Namespace: "run", Name: "checkout", Image: image, DesiredReplicas: 2, AvailableReplicas: 2}}, Metrics: map[string]any{"error_rate_pct": errorRate}}}
	}
	// Unhealthy desired/live state: remediation proposed, only while v18 remains.
	remediations := 0
	for _, observation := range []agentgateway.Request{observe("evalplane-reference:v18", 100), observe("evalplane-reference:v18", 85)} {
		got := decide(observation).Actions[0]
		if got.Type != "set_desired_workload_image" || got.Parameters["image"] != "evalplane-reference:v17" {
			t.Fatalf("remediation=%+v", got)
		}
		remediations++
	}
	// Converged healthy state: terminal success, not another mutation.
	terminal := decide(observe("evalplane-reference:v17", 0)).Actions[0]
	if terminal.Type != "no_op" {
		t.Fatalf("terminal=%+v", terminal)
	}
	if remediations != 2 {
		t.Fatalf("remediations=%d", remediations)
	}
}
