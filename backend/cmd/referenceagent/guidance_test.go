package main

import (
	"context"
	"encoding/json"
	"github.com/evalplane/evalplane/backend/internal/agentgateway"
	"github.com/evalplane/evalplane/backend/internal/operational"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestReferenceAgentConsultsGuidanceBeforeMutation(t *testing.T) {
	seen := false
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer secret" {
			t.Errorf("missing credential")
		}
		var op operational.ProposedOperation
		if err := json.NewDecoder(r.Body).Decode(&op); err != nil {
			t.Error(err)
		}
		if op.RunID == "" || op.Operation != "set_image" {
			t.Errorf("proposed operation: %+v", op)
		}
		seen = true
		_ = json.NewEncoder(w).Encode(operational.Guidance{ID: "guidance-1", AgentVersionID: "version-1", ProposedOperation: op, Requirements: []operational.Contract{{Rules: []operational.Rule{{Type: "forbid_operation", Operation: "patch", Namespace: "checkout", ResourceKind: "Deployment", ResourceName: "checkout"}}}}})
	}))
	defer server.Close()
	t.Setenv("REALVERAX_GUIDANCE_ENDPOINT", server.URL)
	t.Setenv("REALVERAX_GUIDANCE_TOKEN", "secret")
	target := &agentgateway.ActionTarget{Kind: "Deployment", Namespace: "checkout", Name: "checkout"}
	req := agentgateway.Request{RunID: "11111111-1111-4111-8111-111111111111", SessionID: "22222222-2222-4222-8222-222222222222", AgentVersionID: "version-1"}
	response := agentgateway.Response{Actions: []agentgateway.ProposedAction{{Type: "set_live_workload_image", Target: target, Parameters: map[string]any{"image": "checkout:v2"}}}}
	got, err := consultGuidance(context.Background(), req, response)
	if err != nil {
		t.Fatal(err)
	}
	if !seen || len(got.Actions) != 1 || got.Actions[0].Type != "no_op" {
		t.Fatalf("guidance ignored: %+v", got)
	}
}
func TestReferenceAgentRejectsUnboundGuidance(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(operational.Guidance{ID: "guidance-1", AgentVersionID: "different-agent"})
	}))
	defer server.Close()
	t.Setenv("REALVERAX_GUIDANCE_ENDPOINT", server.URL)
	t.Setenv("REALVERAX_GUIDANCE_TOKEN", "secret")
	target := &agentgateway.ActionTarget{Kind: "Deployment", Namespace: "checkout", Name: "checkout"}
	_, err := consultGuidance(context.Background(), agentgateway.Request{AgentVersionID: "version-1"}, agentgateway.Response{Actions: []agentgateway.ProposedAction{{Type: "scale_workload", Target: target}}})
	if err == nil {
		t.Fatal("unbound guidance accepted")
	}
}
