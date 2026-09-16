package httpapi

import (
	"context"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/evalplane/evalplane/backend/internal/agentconnection"
	"github.com/evalplane/evalplane/backend/internal/auth"
)

type fakeConnectionService struct {
	writeErr error
	input    agentconnection.CredentialInput
}

func (f *fakeConnectionService) Get(context.Context, auth.Principal, string, string) (agentconnection.Info, error) {
	return safeConnectionInfo(), nil
}
func (f *fakeConnectionService) UpdateCredential(_ context.Context, _ auth.Principal, _, _ string, input agentconnection.CredentialInput) (agentconnection.Info, error) {
	f.input = input
	return safeConnectionInfo(), f.writeErr
}
func (f *fakeConnectionService) Test(context.Context, auth.Principal, string, string) (agentconnection.Info, error) {
	return safeConnectionInfo(), f.writeErr
}
func (f *fakeConnectionService) Compatibility(context.Context, auth.Principal, string, string, string) (agentconnection.Compatibility, error) {
	return agentconnection.Compatibility{Compatible: true}, nil
}
func (f *fakeConnectionService) TestConfiguration(context.Context, auth.Principal, string, agentconnection.TestConfigurationInput) (agentconnection.TestResult, error) {
	return agentconnection.TestResult{Status: "healthy", DiagnosticCode: "connection_healthy"}, f.writeErr
}
func safeConnectionInfo() agentconnection.Info {
	return agentconnection.Info{AgentVersionID: testUUID, Protocol: "http", EndpointURL: "https://agent.example/invoke", AuthType: "bearer", HasCredential: true, LastCheckStatus: "healthy"}
}

func connectionRouter(service AgentConnectionService) http.Handler {
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	return NewRouter(healthyDatabase{}, logger, WithIdentityService(fakeIdentity{}), WithAuthentication(fakeAuth{allow: true}, fakeAuth{allow: true}, false, 168*time.Hour, "http://localhost:3000"), WithAgentConnectionService(service))
}

func TestConnectionResponsesNeverExposeCredentialMaterial(t *testing.T) {
	NF := testUUID
	req := httptest.NewRequest(http.MethodGet, "/v1/workspaces/"+NF+"/agent-versions/"+NF+"/connection", nil)
	req.AddCookie(&http.Cookie{Name: auth.CookieName, Value: "token"})
	w := httptest.NewRecorder()
	connectionRouter(&fakeConnectionService{}).ServeHTTP(w, req)
	body := w.Body.String()
	for _, prohibited := range []string{"bearer_token", "ciphertext", "nonce", "credential_envelope"} {
		if strings.Contains(body, prohibited) {
			t.Fatalf("response leaked %s: %s", prohibited, body)
		}
	}
	if w.Code != http.StatusOK || !strings.Contains(body, `"has_credential":true`) {
		t.Fatalf("status=%d body=%s", w.Code, body)
	}
}

func TestCredentialEndpointPassesSecretOnlyToServiceAndReturnsSafeShape(t *testing.T) {
	service := &fakeConnectionService{}
	req := httptest.NewRequest(http.MethodPut, "/v1/workspaces/"+testUUID+"/agent-versions/"+testUUID+"/credential", strings.NewReader(`{"auth_type":"bearer","bearer_token":"do-not-return"}`))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Origin", "http://localhost:3000")
	req.AddCookie(&http.Cookie{Name: auth.CookieName, Value: "token"})
	w := httptest.NewRecorder()
	connectionRouter(service).ServeHTTP(w, req)
	if service.input.BearerToken != "do-not-return" {
		t.Fatal("credential did not reach service")
	}
	if strings.Contains(w.Body.String(), "do-not-return") {
		t.Fatalf("credential returned: %s", w.Body.String())
	}
}

func TestViewerCannotRotateOrTestConnection(t *testing.T) {
	service := &fakeConnectionService{writeErr: auth.ErrForbidden}
	for _, tc := range []struct{ method, path, body string }{{http.MethodPut, "/credential", `{"auth_type":"none","bearer_token":""}`}, {http.MethodPost, "/connection/test", `{}`}} {
		req := httptest.NewRequest(tc.method, "/v1/workspaces/"+testUUID+"/agent-versions/"+testUUID+tc.path, strings.NewReader(tc.body))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Origin", "http://localhost:3000")
		req.AddCookie(&http.Cookie{Name: auth.CookieName, Value: "token"})
		w := httptest.NewRecorder()
		connectionRouter(service).ServeHTTP(w, req)
		if w.Code != http.StatusForbidden {
			t.Fatalf("%s status=%d", tc.path, w.Code)
		}
	}
}
