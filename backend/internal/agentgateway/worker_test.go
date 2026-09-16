package agentgateway

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestWorkerGatewayPreservesProposalsForAuthorization(t *testing.T) {
	request := validRequest()
	request.VisibleContext = json.RawMessage(`{"service":"checkout"}`)
	request.AllowedResources = json.RawMessage(`[{"kind":"Deployment","name":"checkout"}]`)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var received Request
		if err := json.NewDecoder(r.Body).Decode(&received); err != nil {
			t.Error(err)
		}
		if string(received.VisibleContext) != string(request.VisibleContext) || string(received.AllowedResources) != string(request.AllowedResources) {
			t.Error("agent-visible policy was not preserved")
		}
		_, _ = w.Write([]byte(`{"protocol_version":"v1","actions":[{"type":"unsupported","parameters":{}}]}`))
	}))
	defer server.Close()
	response, err := NewHTTPGateway(nil, nil).WithoutEvidence().Invoke(context.Background(), Connection{EndpointURL: server.URL}, request)
	if err != nil || len(response.Actions) != 1 || response.Actions[0].Type != "unsupported" {
		t.Fatalf("worker must receive bounded proposals for structured denial: response=%+v error=%v", response, err)
	}
}
