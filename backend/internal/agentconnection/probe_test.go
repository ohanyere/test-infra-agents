package agentconnection

import (
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/evalplane/evalplane/backend/internal/agentgateway"
	"github.com/google/uuid"
)

func probeService(t *testing.T, timeout time.Duration) *Service {
	t.Helper()
	c, err := NewCipher(base64.StdEncoding.EncodeToString(make([]byte, 32)))
	if err != nil {
		t.Fatal(err)
	}
	return &Service{cipher: c, policy: agentgateway.EndpointPolicy{AllowPrivate: true}, timeout: timeout, now: time.Now}
}

func storedFor(t *testing.T, server *httptest.Server, token string) storedConnection {
	t.Helper()
	c := probeService(t, time.Second).cipher
	var envelope []byte
	if token != "" {
		envelope, _ = c.Encrypt(token)
	}
	return storedConnection{Info: Info{AgentVersionID: uuid.NewString(), Protocol: "http", EndpointURL: server.URL, AuthType: map[bool]string{true: AuthBearer, false: AuthNone}[token != ""], HasCredential: token != ""}, CredentialEnvelope: envelope}
}

func TestProbeHealthyAndDoesNotExecuteReturnedActions(t *testing.T) {
	called := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called++
		var request agentgateway.Request
		_ = json.NewDecoder(r.Body).Decode(&request)
		if string(request.VisibleContext) != `{"connection_check":true}` || len(request.AvailableActions) != 1 || request.AvailableActions[0] != "no_op" {
			t.Error("unsafe probe request")
		}
		_ = json.NewEncoder(w).Encode(agentgateway.Response{ProtocolVersion: agentgateway.ProtocolV1, Actions: []agentgateway.ProposedAction{{Type: "no_op", Parameters: map[string]any{}}}})
	}))
	defer server.Close()
	status, _ := probeService(t, time.Second).probe(t.Context(), storedFor(t, server, ""))
	if status != StatusHealthy || called != 1 {
		t.Fatalf("status=%s called=%d", status, called)
	}
}

func TestProbeClassifiesAuthenticationProtocolAndTimeout(t *testing.T) {
	tests := []struct {
		name, want string
		handler    http.HandlerFunc
		timeout    time.Duration
	}{
		{"auth", StatusAuthenticationFailed, func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusUnauthorized) }, time.Second},
		{"protocol", StatusProtocolError, func(w http.ResponseWriter, _ *http.Request) { _, _ = w.Write([]byte(`not-json`)) }, time.Second},
		{"timeout", StatusTimeout, func(w http.ResponseWriter, _ *http.Request) { time.Sleep(50 * time.Millisecond) }, 5 * time.Millisecond},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			server := httptest.NewServer(tc.handler)
			defer server.Close()
			status, _ := probeService(t, tc.timeout).probe(t.Context(), storedFor(t, server, ""))
			if status != tc.want {
				t.Fatalf("status=%s", status)
			}
		})
	}
}
