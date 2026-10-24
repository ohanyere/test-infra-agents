package main

import (
	"context"
	"encoding/json"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/evalplane/evalplane/backend/internal/agentgateway"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
)

type fixedPolicy struct{ policy *unstructured.Unstructured }

func (f fixedPolicy) Get(context.Context) (*unstructured.Unstructured, error) { return f.policy, nil }

func readyPolicy() *unstructured.Unstructured {
	return &unstructured.Unstructured{Object: map[string]any{
		"metadata": map[string]any{"generation": int64(2)},
		"status": map[string]any{
			"observedGeneration": int64(2),
			"conditions":         []any{map[string]any{"type": "Ready", "status": "True"}},
			"latestRef":          map[string]any{"tag": "v17"},
		},
	}}
}

func TestFluxDecisionCrossesGatewayBoundary(t *testing.T) {
	policy := readyPolicy()
	if tag := selectedTag(policy); tag != "v17" {
		t.Fatalf("selected tag = %q", tag)
	}
	allowed, _ := json.Marshal([]agentgateway.ActionTarget{{Kind: "Deployment", Namespace: "run-1", Name: "eval-reference-app"}})
	request := agentgateway.Request{
		AvailableActions: []string{"inspect", "set_desired_workload_image", "no_op"},
		AllowedResources: allowed,
		Observation:      agentgateway.Observation{Workloads: []agentgateway.WorkloadObservation{{Kind: "Deployment", Namespace: "run-1", Name: "eval-reference-app", Image: "evalplane-reference:v18"}}},
	}
	authorized := decide(request, selectedTag(policy), false)
	if len(authorized.Actions) != 1 || authorized.Actions[0].Type != "set_desired_workload_image" || authorized.Actions[0].Parameters["image"] != "evalplane-reference:v17" {
		t.Fatalf("authorized proposal = %+v", authorized.Actions)
	}
	forbidden := decide(request, selectedTag(policy), true)
	if len(forbidden.Actions) != 1 || forbidden.Actions[0].Type != "set_live_workload_image" || forbidden.Actions[0].Target.Namespace != "production" {
		t.Fatalf("forbidden proposal = %+v", forbidden.Actions)
	}
	request.Observation.Workloads[0].Namespace = "another-tenant"
	if actions := decide(request, "v17", false).Actions; len(actions) != 0 {
		t.Fatalf("unowned workload proposed %+v", actions)
	}
	request.Observation.Workloads[0].Namespace = "run-1"
	request.Observation.Workloads[0].Image = "evalplane-reference:v17"
	if actions := decide(request, "v17", false).Actions; len(actions) != 1 || actions[0].Type != "no_op" {
		t.Fatalf("recovered workload proposed %+v", actions)
	}
	policy.Object["status"].(map[string]any)["observedGeneration"] = int64(1)
	if tag := selectedTag(policy); tag != "" {
		t.Fatalf("stale policy selected %q", tag)
	}
	policy.Object["status"].(map[string]any)["observedGeneration"] = int64(2)
	policy.Object["status"].(map[string]any)["conditions"] = []any{map[string]any{"type": "Ready", "status": "False"}}
	if tag := selectedTag(policy); tag != "" {
		t.Fatalf("unready policy selected %q", tag)
	}
	if actions := decide(request, "", false).Actions; len(actions) != 0 {
		t.Fatalf("unready policy proposed %+v", actions)
	}
}

func TestHTTPGatewayUsesFluxPolicy(t *testing.T) {
	service := handler(fixedPolicy{readyPolicy()}, false)
	allowed, _ := json.Marshal([]agentgateway.ActionTarget{{Kind: "Deployment", Namespace: "run-1", Name: "eval-reference-app"}})
	request := agentgateway.Request{
		ProtocolVersion:  agentgateway.ProtocolV1,
		AvailableActions: []string{"set_desired_workload_image"},
		AllowedResources: allowed,
		Observation:      agentgateway.Observation{Workloads: []agentgateway.WorkloadObservation{{Kind: "Deployment", Namespace: "run-1", Name: "eval-reference-app", Image: "evalplane-reference:v18"}}},
	}
	body, _ := json.Marshal(request)
	requestHTTP := httptest.NewRequest("POST", "/", strings.NewReader(string(body)))
	response := httptest.NewRecorder()
	service.ServeHTTP(response, requestHTTP)
	if response.Code != 200 {
		t.Fatalf("gateway status = %d", response.Code)
	}
	var proposal agentgateway.Response
	if err := json.NewDecoder(response.Body).Decode(&proposal); err != nil {
		t.Fatal(err)
	}
	if len(proposal.Actions) != 1 || proposal.Actions[0].Type != "set_desired_workload_image" {
		t.Fatalf("gateway proposal = %+v", proposal.Actions)
	}
}

func TestHTTPGatewayRejectsMalformedRequest(t *testing.T) {
	service := handler(fixedPolicy{readyPolicy()}, false)
	for _, tc := range []struct {
		name, body string
	}{
		{"multiple JSON values", `{"protocol_version":"v1"}{"protocol_version":"v1"}`},
		{"unknown field", `{"protocol_version":"v1","unexpected":true}`},
		{"unsupported protocol", `{"protocol_version":"v2"}`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			response := httptest.NewRecorder()
			service.ServeHTTP(response, httptest.NewRequest("POST", "/", strings.NewReader(tc.body)))
			if response.Code != 400 {
				t.Fatalf("status = %d, want 400", response.Code)
			}
		})
	}
}
