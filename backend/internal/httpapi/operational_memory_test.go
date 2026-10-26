package httpapi

import (
	"context"
	"github.com/evalplane/evalplane/backend/internal/auth"
	"github.com/evalplane/evalplane/backend/internal/operational"
	"github.com/go-chi/chi/v5"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

type memoryFake struct {
	OperationalMemoryService
	token     string
	seenScope bool
	denied    bool
}

func (f *memoryFake) GuidanceForAgent(_ context.Context, token string, op operational.ProposedOperation) (operational.Guidance, error) {
	f.token = token
	if f.denied {
		return operational.Guidance{}, auth.ErrForbidden
	}
	return operational.Guidance{ID: "guidance-1", ProposedOperation: op, TrustworthyHistoryAvailable: false, Limitations: []string{"No eligible history"}}, nil
}
func (f *memoryFake) ListEpisodes(_ context.Context, p auth.Principal, w, e string, provisional bool) ([]operational.Episode, error) {
	f.seenScope = p.UserID != "" && w == "11111111-1111-4111-8111-111111111111" && e == "22222222-2222-4222-8222-222222222222" && provisional
	if f.denied {
		return nil, auth.ErrForbidden
	}
	return []operational.Episode{{ID: "episode-1", Eligibility: "provisional"}}, nil
}
func TestAgentGuidanceEndpointRequiresBearerAndKeepsUnknownHistory(t *testing.T) {
	h := memoryHandlers{service: &memoryFake{}, logger: slog.New(slog.NewTextHandler(io.Discard, nil))}
	body := `{"namespace":"checkout","resource_kind":"Deployment","resource_name":"checkout","operation":"set_image"}`
	req := httptest.NewRequest(http.MethodPost, "/", strings.NewReader(body))
	response := httptest.NewRecorder()
	h.agentGuidance(response, req)
	if response.Code != http.StatusUnauthorized {
		t.Fatalf("missing bearer: %d", response.Code)
	}
	req = httptest.NewRequest(http.MethodPost, "/", strings.NewReader(body))
	req.Header.Set("Authorization", "Bearer rvg_credential")
	response = httptest.NewRecorder()
	h.agentGuidance(response, req)
	if response.Code != http.StatusOK || !strings.Contains(response.Body.String(), `"trustworthy_history_available":false`) {
		t.Fatalf("conservative guidance: %d %s", response.Code, response.Body.String())
	}
	fake := h.service.(*memoryFake)
	if fake.token != "rvg_credential" {
		t.Fatal("bearer was not delegated to credential validation")
	}
	fake.denied = true
	req = httptest.NewRequest(http.MethodPost, "/", strings.NewReader(body))
	req.Header.Set("Authorization", "Bearer rvg_credential")
	response = httptest.NewRecorder()
	h.agentGuidance(response, req)
	if response.Code != http.StatusForbidden {
		t.Fatalf("wrong scope: %d", response.Code)
	}
}
func TestEpisodeEndpointDelegatesTenantScope(t *testing.T) {
	fake := &memoryFake{}
	h := memoryHandlers{service: fake, logger: slog.New(slog.NewTextHandler(io.Discard, nil))}
	route := chi.NewRouteContext()
	route.URLParams.Add("workspaceID", "11111111-1111-4111-8111-111111111111")
	route.URLParams.Add("environmentID", "22222222-2222-4222-8222-222222222222")
	req := httptest.NewRequest(http.MethodGet, "/?include_provisional=true", nil)
	req = req.WithContext(auth.ContextWithPrincipal(context.WithValue(req.Context(), chi.RouteCtxKey, route), auth.Principal{UserID: "33333333-3333-4333-8333-333333333333"}))
	response := httptest.NewRecorder()
	h.episodes(response, req)
	if response.Code != http.StatusOK || !fake.seenScope {
		t.Fatalf("scope not delegated: %d", response.Code)
	}
	fake.denied = true
	response = httptest.NewRecorder()
	h.episodes(response, req)
	if response.Code != http.StatusForbidden {
		t.Fatalf("cross tenant denial: %d", response.Code)
	}
}
