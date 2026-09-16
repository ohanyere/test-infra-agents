package agentconnection

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/evalplane/evalplane/backend/internal/agentgateway"
)

func TestConnectionProbeRejectsMutatingProposalWithoutExecution(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode(agentgateway.Response{ProtocolVersion: agentgateway.ProtocolV1, Actions: []agentgateway.ProposedAction{{
			Type: "scale_workload", Target: &agentgateway.ActionTarget{Kind: "Deployment", Namespace: "customer", Name: "app"}, Parameters: map[string]any{"replicas": 10},
		}}})
	}))
	defer server.Close()
	status, _ := probeService(t, time.Second).probe(t.Context(), storedFor(t, server, ""))
	if status != StatusProtocolError {
		t.Fatalf("status=%s", status)
	}
}
