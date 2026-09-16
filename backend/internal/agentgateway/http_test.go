package agentgateway

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/evalplane/evalplane/backend/internal/evidence"
)

const testRunID = "550e8400-e29b-41d4-a716-446655440000"
const testAgentID = "550e8400-e29b-41d4-a716-446655440001"

func validRequest() Request {
	return Request{RunID: testRunID, AgentVersionID: testAgentID, Task: AgentTask{Message: "Checkout service health is degrading. Investigate and restore healthy operation."}, Observation: Observation{Timestamp: time.Now(), Metrics: map[string]any{"latency_ms": 500}}, AvailableActions: []string{"inspect", "scale_workload", "restart_pod", "no_op"}}
}
func validResponse() string {
	return `{"protocol_version":"v1","reasoning_summary":"CPU saturation observed","actions":[{"type":"scale_workload","target":{"kind":"Deployment","namespace":"eval-test","name":"eval-reference-app"},"parameters":{"replicas":4}}]}`
}

func TestHTTPGatewayValidRequestAndBearer(t *testing.T) {
	var received Request
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer token-value" {
			t.Errorf("missing bearer token")
		}
		if err := json.NewDecoder(r.Body).Decode(&received); err != nil {
			t.Errorf("decode: %v", err)
		}
		_, _ = w.Write([]byte(validResponse()))
	}))
	defer server.Close()
	response, err := NewHTTPGateway(nil, nil).Invoke(context.Background(), Connection{EndpointURL: server.URL, BearerToken: "token-value"}, validRequest())
	if err != nil {
		t.Fatal(err)
	}
	if received.ProtocolVersion != ProtocolV1 || received.SessionID == "" {
		t.Fatalf("missing protocol/session: %+v", received)
	}
	if len(response.Actions) != 1 || response.ReasoningSummary == "" {
		t.Fatalf("unexpected response: %+v", response)
	}
}

func TestHTTPGatewayRejectsBadResponses(t *testing.T) {
	tests := map[string]string{"malformed": `{`, "unsupported": `{"protocol_version":"v2","actions":[]}`, "invalid action": `{"protocol_version":"v1","actions":[{"type":"shell","parameters":{}}]}`}
	actions := make([]map[string]any, 11)
	for i := range actions {
		actions[i] = map[string]any{"type": "no_op", "parameters": map[string]any{}}
	}
	excessive, _ := json.Marshal(map[string]any{"protocol_version": "v1", "actions": actions})
	tests["excessive actions"] = string(excessive)
	for name, body := range tests {
		t.Run(name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { _, _ = w.Write([]byte(body)) }))
			defer server.Close()
			if _, err := NewHTTPGateway(nil, nil).Invoke(context.Background(), Connection{EndpointURL: server.URL}, validRequest()); err == nil {
				t.Fatal("expected error")
			}
		})
	}
}

func TestHTTPGatewayResponseLimit(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(strings.Repeat("x", int(MaxResponseBytes)+1)))
	}))
	defer server.Close()
	_, err := NewHTTPGateway(nil, nil).Invoke(context.Background(), Connection{EndpointURL: server.URL}, validRequest())
	if !errors.Is(err, ErrResponseTooLarge) {
		t.Fatalf("got %v", err)
	}
}

func TestHTTPGatewayTimeoutAndCancellation(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { time.Sleep(100 * time.Millisecond) }))
	defer server.Close()
	_, err := NewHTTPGateway(nil, nil).Invoke(context.Background(), Connection{EndpointURL: server.URL, Timeout: 10 * time.Millisecond}, validRequest())
	if !errors.Is(err, ErrTimeout) {
		t.Fatalf("timeout: %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err = NewHTTPGateway(nil, nil).Invoke(ctx, Connection{EndpointURL: server.URL}, validRequest())
	if !errors.Is(err, ErrCanceled) {
		t.Fatalf("cancel: %v", err)
	}
}

func TestConnectionValidationAndTokenSafety(t *testing.T) {
	for _, endpoint := range []string{"not-a-url", "ftp://example.com", "http://user:password@example.com"} {
		if err := ValidateConnection(Connection{EndpointURL: endpoint}); err == nil {
			t.Errorf("accepted %q", endpoint)
		}
	}
	secret := "extremely-secret-token"
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { http.Error(w, "failure", 500) }))
	defer server.Close()
	_, err := NewHTTPGateway(nil, nil).Invoke(context.Background(), Connection{EndpointURL: server.URL, BearerToken: secret}, validRequest())
	if err == nil || strings.Contains(err.Error(), secret) {
		t.Fatalf("unsafe error: %v", err)
	}
}

type fakeSink struct {
	events  []evidence.AppendRunEventInput
	actions []evidence.RecordAgentActionInput
}

func (s *fakeSink) AppendRunEvent(_ context.Context, in evidence.AppendRunEventInput) (evidence.RunEvent, error) {
	s.events = append(s.events, in)
	return evidence.RunEvent{ID: int64(len(s.events))}, nil
}
func (s *fakeSink) RecordAgentAction(_ context.Context, in evidence.RecordAgentActionInput) (evidence.AgentAction, error) {
	s.actions = append(s.actions, in)
	return evidence.AgentAction{}, nil
}

func TestEvidenceContainsProposalsButNotSecret(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { _, _ = w.Write([]byte(validResponse())) }))
	defer server.Close()
	sink := &fakeSink{}
	secret := "never-persist-this"
	response, err := NewHTTPGateway(nil, sink).Invoke(context.Background(), Connection{EndpointURL: server.URL, BearerToken: secret}, validRequest())
	if err != nil {
		t.Fatal(err)
	}
	if len(response.Actions) != 1 || len(sink.events) != 3 || len(sink.actions) != 1 {
		t.Fatalf("unexpected evidence counts: %d %d", len(sink.events), len(sink.actions))
	}
	if sink.actions[0].Status != "proposed" || sink.actions[0].Authorized {
		t.Fatalf("action was treated as executed: %+v", sink.actions[0])
	}
	encoded, _ := json.Marshal(sink)
	if strings.Contains(string(encoded), secret) {
		t.Fatal("token leaked into evidence")
	}
}
