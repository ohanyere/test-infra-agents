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
	"github.com/evalplane/evalplane/backend/internal/operational"
	"github.com/go-chi/chi/v5"
)

type operationalHandlerFake struct {
	seen   bool
	denied bool
}

func (f *operationalHandlerFake) ConfigureMonitoring(_ context.Context, p auth.Principal, w, e, s string, in operational.MonitoringInput) (operational.MonitoringPlan, error) {
	f.seen = p.UserID != "" && w == "11111111-1111-4111-8111-111111111111" && e == "22222222-2222-4222-8222-222222222222" && s == "33333333-3333-4333-8333-333333333333" && len(in.SourceIDs) == 1
	if f.denied {
		return operational.MonitoringPlan{}, auth.ErrForbidden
	}
	return operational.MonitoringPlan{SessionID: s, State: "active"}, nil
}
func (f *operationalHandlerFake) Monitoring(_ context.Context, p auth.Principal, w, e, s string) (operational.MonitoringView, error) {
	if f.denied {
		return operational.MonitoringView{}, auth.ErrForbidden
	}
	return operational.MonitoringView{Plan: &operational.MonitoringPlan{SessionID: s, State: "active"}}, nil
}
func (f *operationalHandlerFake) ChangeMonitoring(_ context.Context, p auth.Principal, w, e, s, action string) (operational.MonitoringPlan, error) {
	if f.denied {
		return operational.MonitoringPlan{}, auth.ErrForbidden
	}
	return operational.MonitoringPlan{SessionID: s, State: action}, nil
}

func (*operationalHandlerFake) SaveContract(context.Context, auth.Principal, string, string, operational.ContractInput) (operational.Contract, error) {
	return operational.Contract{}, nil
}
func (*operationalHandlerFake) ListContracts(context.Context, auth.Principal, string, string) ([]operational.Contract, error) {
	return nil, nil
}
func (*operationalHandlerFake) AssessRun(context.Context, auth.Principal, string, string, string) (operational.Report, error) {
	return operational.Report{}, nil
}
func (*operationalHandlerFake) ListReports(context.Context, auth.Principal, string, string, string) ([]operational.Report, error) {
	return nil, nil
}
func (f *operationalHandlerFake) ResolveEvidence(_ context.Context, p auth.Principal, w, e, s, ref string) (operational.EvidenceDetail, error) {
	f.seen = p.UserID != "" && w == "11111111-1111-4111-8111-111111111111" && e == "22222222-2222-4222-8222-222222222222" && s == "33333333-3333-4333-8333-333333333333" && ref == "receipt:55555555-5555-4555-8555-555555555555"
	if f.denied {
		return operational.EvidenceDetail{}, auth.ErrForbidden
	}
	return operational.EvidenceDetail{Reference: ref, Kind: "receipt", Summary: "Scoped signed receipt"}, nil
}
func (*operationalHandlerFake) RecordApproval(context.Context, auth.Principal, string, string, string, operational.ApprovalInput) (operational.ApprovalRecord, error) {
	return operational.ApprovalRecord{}, nil
}
func (*operationalHandlerFake) ListApprovals(context.Context, auth.Principal, string, string, string) ([]operational.ApprovalRecord, error) {
	return nil, nil
}
func (f *operationalHandlerFake) ListFindings(_ context.Context, p auth.Principal, w, e, s string) ([]operational.FindingRecord, error) {
	f.seen = w == "11111111-1111-4111-8111-111111111111" && e == "22222222-2222-4222-8222-222222222222" && s == "33333333-3333-4333-8333-333333333333" && p.UserID != ""
	if f.denied {
		return nil, auth.ErrForbidden
	}
	return []operational.FindingRecord{{ID: "finding-1", Disposition: "candidate", Finding: operational.Finding{Code: "UNVERIFIED_METRIC_THRESHOLD_BREACH_CANDIDATE", ProvenanceStatus: "COLLECTOR_AUTHENTICATED_METRICS_UNVERIFIED"}}}, nil
}
func (*operationalHandlerFake) ListStabilityFindings(context.Context, auth.Principal, string, string, string) ([]operational.FindingRecord, error) {
	return nil, nil
}
func (*operationalHandlerFake) ListEvidenceReferences(context.Context, auth.Principal, string, string, string) ([]operational.EvidenceRef, error) {
	return nil, nil
}
func (*operationalHandlerFake) ListNotifications(context.Context, auth.Principal, string, string) ([]operational.NotificationEvent, error) {
	return nil, nil
}
func (*operationalHandlerFake) ListSessions(context.Context, auth.Principal, string, string) ([]operational.SessionSummary, error) {
	return nil, nil
}
func (*operationalHandlerFake) EnvironmentStatus(context.Context, auth.Principal, string, string) (operational.EnvironmentStatus, error) {
	return operational.EnvironmentStatus{}, nil
}

func TestOperationalFindingEndpointKeepsScopeAndProvenance(t *testing.T) {
	fake := &operationalHandlerFake{}
	handler := operationalHandlers{service: fake, logger: slog.New(slog.NewTextHandler(io.Discard, nil))}
	route := chi.NewRouteContext()
	route.URLParams.Add("workspaceID", "11111111-1111-4111-8111-111111111111")
	route.URLParams.Add("environmentID", "22222222-2222-4222-8222-222222222222")
	route.URLParams.Add("sessionID", "33333333-3333-4333-8333-333333333333")
	request := httptest.NewRequest(http.MethodGet, "/", nil)
	request = request.WithContext(auth.ContextWithPrincipal(context.WithValue(request.Context(), chi.RouteCtxKey, route), auth.Principal{UserID: "44444444-4444-4444-8444-444444444444"}))
	response := httptest.NewRecorder()
	handler.findings(response, request)
	if response.Code != http.StatusOK || !fake.seen || !strings.Contains(response.Body.String(), "COLLECTOR_AUTHENTICATED_METRICS_UNVERIFIED") {
		t.Fatalf("scoped finding response: %d %s", response.Code, response.Body.String())
	}
	fake.denied = true
	response = httptest.NewRecorder()
	handler.findings(response, request)
	if response.Code != http.StatusForbidden {
		t.Fatalf("cross-tenant denial: %d", response.Code)
	}
}
func TestEvidenceDetailEndpointUsesScopedService(t *testing.T) {
	fake := &operationalHandlerFake{}
	handler := operationalHandlers{service: fake, logger: slog.New(slog.NewTextHandler(io.Discard, nil))}
	route := chi.NewRouteContext()
	route.URLParams.Add("workspaceID", "11111111-1111-4111-8111-111111111111")
	route.URLParams.Add("environmentID", "22222222-2222-4222-8222-222222222222")
	route.URLParams.Add("sessionID", "33333333-3333-4333-8333-333333333333")
	req := httptest.NewRequest(http.MethodGet, "/?reference=receipt%3A55555555-5555-4555-8555-555555555555", nil)
	req = req.WithContext(auth.ContextWithPrincipal(context.WithValue(req.Context(), chi.RouteCtxKey, route), auth.Principal{UserID: "44444444-4444-4444-8444-444444444444"}))
	rsp := httptest.NewRecorder()
	handler.evidenceDetail(rsp, req)
	if rsp.Code != http.StatusOK || !fake.seen || !strings.Contains(rsp.Body.String(), "Scoped signed receipt") {
		t.Fatalf("scoped evidence detail: %d %s", rsp.Code, rsp.Body.String())
	}
	fake.denied = true
	rsp = httptest.NewRecorder()
	handler.evidenceDetail(rsp, req)
	if rsp.Code != http.StatusForbidden {
		t.Fatalf("cross-scope evidence read: %d", rsp.Code)
	}
}

func TestMonitoringHandlersScopeAndAuthorization(t *testing.T) {
	fake := &operationalHandlerFake{}
	h := monitoringHandlers{service: fake, operationalHandlers: operationalHandlers{logger: slog.New(slog.NewTextHandler(io.Discard, nil))}}
	route := chi.NewRouteContext()
	route.URLParams.Add("workspaceID", "11111111-1111-4111-8111-111111111111")
	route.URLParams.Add("environmentID", "22222222-2222-4222-8222-222222222222")
	route.URLParams.Add("sessionID", "33333333-3333-4333-8333-333333333333")
	ctx := auth.ContextWithPrincipal(context.WithValue(context.Background(), chi.RouteCtxKey, route), auth.Principal{UserID: "44444444-4444-4444-8444-444444444444"})
	req := httptest.NewRequest(http.MethodPost, "/", strings.NewReader(`{"source_ids":["55555555-5555-4555-8555-555555555555"],"cadence_seconds":60,"duration_seconds":180}`)).WithContext(ctx)
	rsp := httptest.NewRecorder()
	h.configure(rsp, req)
	if rsp.Code != http.StatusCreated || !fake.seen || !strings.Contains(rsp.Body.String(), `"state":"active"`) {
		t.Fatalf("configure monitor: %d %s", rsp.Code, rsp.Body.String())
	}
	fake.denied = true
	rsp = httptest.NewRecorder()
	h.get(rsp, httptest.NewRequest(http.MethodGet, "/", nil).WithContext(ctx))
	if rsp.Code != http.StatusForbidden {
		t.Fatalf("cross-scope monitor read: %d", rsp.Code)
	}
}
