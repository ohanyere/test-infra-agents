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

type collectorHandlerFake struct {
	called      bool
	claimCalled bool
	claimInput  customercollector.SubmitActionClaimInput
	claimActor  auth.Principal
}

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
func (f *collectorHandlerFake) IngestForwardedAudit(_ context.Context, _ string, token string, _ customercollector.ForwardedAuditInput) (customercollector.ForwardedAuditResult, error) {
	f.called = token != ""
	return customercollector.ForwardedAuditResult{TrustClassification: customercollector.AuditCandidateTrust}, nil
}
func (f *collectorHandlerFake) IngestMetricCandidate(_ context.Context, _ string, token string, _ customercollector.MetricCandidateInput) (customercollector.MetricCandidateResult, error) {
	f.called = token != ""
	return customercollector.MetricCandidateResult{Trust: customercollector.MetricCandidateTrust}, nil
}
func (f *collectorHandlerFake) ListForwardedAudits(context.Context, auth.Principal, string, string, string) ([]customercollector.ForwardedAuditResult, error) {
	return nil, nil
}
func (f *collectorHandlerFake) ListAuditReconciliation(context.Context, auth.Principal, string, string, string) ([]customercollector.ReconciliationRevision, error) {
	return nil, nil
}
func (f *collectorHandlerFake) SubmitActionClaim(_ context.Context, p auth.Principal, workspaceID, environmentID, sessionID string, in customercollector.SubmitActionClaimInput) (customercollector.ActionClaim, error) {
	f.claimCalled = true
	f.claimInput = in
	f.claimActor = p
	return customercollector.ActionClaim{WorkspaceID: workspaceID, EnvironmentID: environmentID, SessionID: sessionID, ClaimID: in.ClaimID, Trust: customercollector.ActionClaimTrust}, nil
}

func TestActionClaimEndpointDerivesTenantAndLabelsUnverified(t *testing.T) {
	fake := &collectorHandlerFake{}
	handler := customerCollectorHandlers{service: fake, logger: slog.New(slog.NewTextHandler(io.Discard, nil))}
	workspaceID := "11111111-1111-4111-8111-111111111111"
	environmentID := "22222222-2222-4222-8222-222222222222"
	sessionID := "33333333-3333-4333-8333-333333333333"
	claimID := "44444444-4444-4444-8444-444444444444"
	route := chi.NewRouteContext()
	route = chi.NewRouteContext()
	for key, value := range map[string]string{"workspaceID": workspaceID, "environmentID": environmentID, "sessionID": sessionID} {
		route.URLParams.Add(key, value)
	}
	body := `{"contract_version":"customer-agent-action-claim:v1","agent_version_id":"55555555-5555-4555-8555-555555555555","claim_id":"` + claimID + `","idempotency_key":"66666666-6666-4666-8666-666666666666","action_type":"set_workload_image","resource":{"kind":"Deployment","namespace":"apps","name":"checkout","uid":"e47ac8b1-5509-4e5c-a321-9dd3bc8ef012"},"started_at":"2026-10-13T12:00:00Z","completed_at":"2026-10-13T12:00:01Z","outcome":"executed"}`
	request := httptest.NewRequest(http.MethodPost, "/", strings.NewReader(body))
	request.Header.Set("Content-Type", "application/json")
	request = request.WithContext(auth.ContextWithPrincipal(context.WithValue(request.Context(), chi.RouteCtxKey, route), auth.Principal{UserID: "77777777-7777-4777-8777-777777777777"}))
	response := httptest.NewRecorder()
	handler.submitActionClaim(response, request)
	if response.Code != http.StatusCreated || !fake.claimCalled || fake.claimActor.UserID == "" || fake.claimInput.AgentVersionID == "" {
		t.Fatalf("status=%d called=%t body=%s", response.Code, fake.claimCalled, response.Body.String())
	}
	if !strings.Contains(response.Body.String(), `"trust_classification":"UNVERIFIED_AGENT_CLAIM"`) || !strings.Contains(response.Body.String(), `"workspace_id":"`+workspaceID+`"`) {
		t.Fatalf("server-derived claim provenance missing: %s", response.Body.String())
	}

	for key, value := range map[string]string{"workspaceID": workspaceID, "environmentID": environmentID, "sessionID": sessionID} {
		route.URLParams.Add(key, value)
	}
	spoofed := strings.TrimSuffix(body, "}") + `,"workspace_id":"88888888-8888-4888-8888-888888888888"}`
	request = httptest.NewRequest(http.MethodPost, "/", strings.NewReader(spoofed))
	request.Header.Set("Content-Type", "application/json")
	request = request.WithContext(auth.ContextWithPrincipal(context.WithValue(request.Context(), chi.RouteCtxKey, route), auth.Principal{UserID: "77777777-7777-4777-8777-777777777777"}))
	response = httptest.NewRecorder()
	handler.submitActionClaim(response, request)
	if response.Code != http.StatusBadRequest {
		t.Fatalf("caller-supplied workspace identity was not rejected: status=%d body=%s", response.Code, response.Body.String())
	}
}
func (f *collectorHandlerFake) ListActionClaims(context.Context, auth.Principal, string, string, string) ([]customercollector.ActionClaim, error) {
	return nil, nil
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

func TestForwardedAuditEndpointKeepsInsufficientProvenance(t *testing.T) {
	fake := &collectorHandlerFake{}
	handler := customerCollectorHandlers{service: fake, logger: slog.New(slog.NewTextHandler(io.Discard, nil))}
	route := chi.NewRouteContext()
	route.URLParams.Add("sessionID", "11111111-1111-4111-8111-111111111111")
	body := `{"claimed_cluster_identity":"registered-cluster","event":{"apiVersion":"audit.k8s.io/v1"}}`
	request := httptest.NewRequest(http.MethodPost, "/", strings.NewReader(body))
	request.Header.Set("Authorization", "Bearer rvxc_"+strings.Repeat("a", 43))
	request.Header.Set("Content-Type", "application/json")
	request = request.WithContext(context.WithValue(request.Context(), chi.RouteCtxKey, route))
	response := httptest.NewRecorder()
	handler.ingestForwardedAudit(response, request)
	if response.Code != http.StatusAccepted || !fake.called || !strings.Contains(response.Body.String(), `"trust_classification":"INSUFFICIENT_PROVENANCE"`) {
		t.Fatalf("status=%d body=%s", response.Code, response.Body.String())
	}
}
