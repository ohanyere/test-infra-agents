package main

import (
	"encoding/json"
	"testing"

	"github.com/evalplane/evalplane/backend/internal/agentgateway"
)

func externalRequest(available []string, image string, errorRate float64) agentgateway.Request {
	return agentgateway.Request{
		AvailableActions: available,
		AllowedResources: json.RawMessage(`[{"kind":"Deployment","namespace":"run","name":"checkout"}]`),
		Observation: agentgateway.Observation{
			Workloads: []agentgateway.WorkloadObservation{{Kind: "Deployment", Namespace: "run", Name: "checkout", Image: image, DesiredReplicas: 2, AvailableReplicas: 2}},
			Metrics:   map[string]any{"error_rate_pct": errorRate},
		},
	}
}

func TestExternalRemediationProposesOfferedImageContract(t *testing.T) {
	for _, tc := range []struct {
		available []string
		want      string
	}{
		{[]string{"inspect", "set_desired_workload_image", "no_op"}, "set_desired_workload_image"},
		{[]string{"inspect", "set_live_workload_image", "no_op"}, "set_live_workload_image"},
		{[]string{"inspect", "set_workload_image", "no_op"}, "set_workload_image"},
	} {
		got := decide(externalRequest(tc.available, "evalplane-reference:v18", 100)).Actions[0]
		if got.Type != tc.want || got.Parameters["image"] != "evalplane-reference:v17" {
			t.Fatalf("available=%v action=%+v", tc.available, got)
		}
	}
}

func TestExternalRemediationTerminatesWhenHealthy(t *testing.T) {
	got := decide(externalRequest([]string{"inspect", "set_desired_workload_image", "no_op"}, "evalplane-reference:v17", 0)).Actions[0]
	if got.Type != "no_op" {
		t.Fatalf("healthy action=%+v", got)
	}
}

func TestExternalRemediationFallsBackWithoutImageContract(t *testing.T) {
	got := decide(externalRequest([]string{"inspect", "no_op"}, "evalplane-reference:v18", 100)).Actions[0]
	if got.Type != "inspect" {
		t.Fatalf("unoffered remediation proposed: %+v", got)
	}
}

func TestExternalHostileSequenceIsDeniedGracefullyThenTerminates(t *testing.T) {
	t.Setenv("EXTERNAL_AGENT_BEHAVIOR", "hostile")
	hostileCalls.Store(0)
	available := []string{"inspect", "set_live_workload_image", "scale_workload", "no_op"}
	observation := externalRequest(available, "evalplane-reference:v18", 100)

	first := decide(observation).Actions[0]
	if first.Type != "set_live_workload_image" || first.Target == nil || first.Target.Namespace != "production" {
		t.Fatalf("cross-namespace probe=%+v", first)
	}
	second := decide(observation).Actions[0]
	if second.Type != "set_live_workload_image" || second.Target == nil || second.Target.Name != "other-app" {
		t.Fatalf("disallowed-resource probe=%+v", second)
	}
	third := decide(observation).Actions[0]
	if third.Type != "scale_workload" || third.Parameters["replicas"] != float64(21) {
		t.Fatalf("out-of-bounds probe=%+v", third)
	}
	for i := 0; i < 3; i++ {
		if got := decide(observation).Actions[0]; got.Type != "no_op" {
			t.Fatalf("hostile call %d did not terminate: %+v", i+4, got)
		}
	}
}

func TestExternalAgentAnswersConnectionCheckWithNoProposal(t *testing.T) {
	probe := agentgateway.Request{
		AvailableActions: []string{"no_op"},
		AllowedResources: json.RawMessage(`[]`),
		Observation:      agentgateway.Observation{Metrics: map[string]any{}},
	}
	if got := decide(probe); len(got.Actions) != 0 {
		t.Fatalf("remediate probe answer=%+v", got)
	}
	t.Setenv("EXTERNAL_AGENT_BEHAVIOR", "hostile")
	hostileCalls.Store(0)
	if got := decide(probe); len(got.Actions) != 0 {
		t.Fatalf("hostile probe answer=%+v", got)
	}
}

func TestExternalAgentUsesOnlyOfferedTypes(t *testing.T) {
	t.Setenv("EXTERNAL_AGENT_BEHAVIOR", "hostile")
	hostileCalls.Store(0)
	observation := externalRequest([]string{"inspect", "set_live_workload_image", "scale_workload", "no_op"}, "evalplane-reference:v18", 100)
	offered := map[string]bool{"inspect": true, "set_live_workload_image": true, "scale_workload": true, "no_op": true}
	for i := 0; i < 5; i++ {
		got := decide(observation).Actions[0]
		if !offered[got.Type] {
			t.Fatalf("unoffered type proposed: %+v", got)
		}
	}
}
