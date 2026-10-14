package httpapi

import (
	"context"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/evalplane/evalplane/backend/internal/auth"
	"github.com/evalplane/evalplane/backend/internal/customercollector"
	"github.com/go-chi/chi/v5"
)

type collectorHandlerFake struct{ called bool }

func (f *collectorHandlerFake) Register(context.Context, auth.Principal, string, customercollector.CreateEnvironmentInput) (customercollector.RegisteredEnvironment, error) {
	return customercollector.RegisteredEnvironment{}, nil
}
func (f *collectorHandlerFake) List(context.Context, auth.Principal, string) ([]customercollector.Environment, error) {
	return nil, nil
}
func (f *collectorHandlerFake) Rotate(context.Context, auth.Principal, string, string) (customercollector.RegisteredEnvironment, error) {
	return customercollector.RegisteredEnvironment{}, nil
}
func (f *collectorHandlerFake) Revoke(context.Context, auth.Principal, string, string) error {
	return nil
}
func (f *collectorHandlerFake) StartSession(context.Context, auth.Principal, string, string, customercollector.CreateSessionInput) (customercollector.Session, error) {
	return customercollector.Session{}, nil
}
func (f *collectorHandlerFake) Ingest(_ context.Context, _ string, token string, _ customercollector.EvidenceBatch) (customercollector.IngestResult, error) {
	f.called = token != ""
	return customercollector.IngestResult{Accepted: true}, nil
}

func TestCollectorIngestionRequiresBearerCredential(t *testing.T) {
	fake := &collectorHandlerFake{}
	handler := customerCollectorHandlers{service: fake, logger: slog.New(slog.NewTextHandler(io.Discard, nil))}
	for _, test := range []struct {
		name, authorization string
		status              int
		call                bool
	}{{"missing", "", http.StatusUnauthorized, false}, {"malformed", "Bearer short", http.StatusUnauthorized, false}, {"valid", "Bearer rvxc_" + strings.Repeat("a", 43), http.StatusAccepted, true}} {
		t.Run(test.name, func(t *testing.T) {
			route := chi.NewRouteContext()
			route.URLParams.Add("sessionID", "11111111-1111-4111-8111-111111111111")
			request := httptest.NewRequest(http.MethodPost, "/", strings.NewReader(`{"batch_id":"22222222-2222-4222-8222-222222222222","sequence":1,"schema_version":"customer-kubernetes-observation:v1","phase":"baseline","captured_at":"2026-10-13T12:00:00Z","observation":{"timestamp":"2026-10-13T12:00:00Z","workloads":[],"pods":[],"metrics":{},"recent_events":[]}}`))
			request.Header.Set("Content-Type", "application/json")
			request.Header.Set("Authorization", test.authorization)
			request = request.WithContext(context.WithValue(request.Context(), chi.RouteCtxKey, route))
			response := httptest.NewRecorder()
			handler.ingest(response, request)
			if response.Code != test.status || fake.called != test.call {
				t.Fatalf("status=%d called=%t body=%s", response.Code, fake.called, response.Body.String())
			}
			fake.called = false
		})
	}
}
