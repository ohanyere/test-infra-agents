package httpapi

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/evalplane/evalplane/backend/internal/agent"
	"github.com/evalplane/evalplane/backend/internal/auth"
	"github.com/evalplane/evalplane/backend/internal/identity"
)

type fakeAgentService struct {
	writeErr error
	getErr   error
}

func (f fakeAgentService) ListPublic(context.Context, agent.Filters) ([]agent.Agent, error) {
	return []agent.Agent{{ID: testUUID, Slug: "public-agent", Visibility: "public", Status: "active"}}, nil
}
func (f fakeAgentService) GetPublic(context.Context, string) (agent.Agent, error) {
	if f.getErr != nil {
		return agent.Agent{}, f.getErr
	}
	return agent.Agent{ID: testUUID, Slug: "public-agent", Visibility: "public", Status: "active"}, nil
}
func (f fakeAgentService) ListWorkspace(context.Context, auth.Principal, string, agent.Filters) ([]agent.Agent, error) {
	if f.getErr != nil {
		return nil, f.getErr
	}
	return []agent.Agent{}, nil
}
func (f fakeAgentService) GetWorkspace(context.Context, auth.Principal, string, string) (agent.Agent, error) {
	if f.getErr != nil {
		return agent.Agent{}, f.getErr
	}
	return agent.Agent{ID: testUUID}, nil
}
func (f fakeAgentService) Create(_ context.Context, _ auth.Principal, _ string, in agent.CreateInput) (agent.Agent, error) {
	if f.writeErr != nil {
		return agent.Agent{}, f.writeErr
	}
	return agent.Agent{ID: testUUID, Name: in.Name}, nil
}
func (f fakeAgentService) Update(context.Context, auth.Principal, string, string, agent.UpdateInput) (agent.Agent, error) {
	return agent.Agent{}, f.writeErr
}
func (f fakeAgentService) CreateVersion(context.Context, auth.Principal, string, string, agent.VersionInput) (agent.Version, error) {
	return agent.Version{}, f.writeErr
}
func (fakeAgentService) ListVersions(context.Context, auth.Principal, string, string, identity.Pagination) ([]agent.Version, error) {
	return []agent.Version{}, nil
}
func (f fakeAgentService) GetVersion(context.Context, auth.Principal, string, string, string) (agent.Version, error) {
	return agent.Version{}, f.getErr
}
func (fakeAgentService) ListVersionClaims(context.Context, auth.Principal, string, string, string) ([]agent.CapabilityClaim, error) {
	return []agent.CapabilityClaim{}, nil
}

func agentRouter(authentication fakeAuth, service fakeAgentService) http.Handler {
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	return NewRouter(healthyDatabase{}, logger, WithIdentityService(fakeIdentity{}), WithAuthentication(authentication, authentication, false, 168*time.Hour, "http://localhost:3000"), WithAgentService(service))
}

func TestPublicAgentListIsUnauthenticated(t *testing.T) {
	w := httptest.NewRecorder()
	agentRouter(fakeAuth{authenticateErr: auth.ErrUnauthenticated}, fakeAgentService{}).ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/v1/agents", nil))
	if w.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", w.Code, w.Body.String())
	}
	var body struct {
		Items []agent.Agent `json:"items"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil || len(body.Items) != 1 || body.Items[0].Visibility != "public" || body.Items[0].Status != "active" {
		t.Fatalf("body=%s err=%v", w.Body.String(), err)
	}
}

func TestAgentWorkspaceRequiresAuthentication(t *testing.T) {
	w := httptest.NewRecorder()
	agentRouter(fakeAuth{authenticateErr: auth.ErrUnauthenticated}, fakeAgentService{}).ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/v1/workspaces/"+testUUID+"/agents", nil))
	if w.Code != http.StatusUnauthorized {
		t.Fatalf("status=%d body=%s", w.Code, w.Body.String())
	}
}

func TestAgentViewerCannotWrite(t *testing.T) {
	cases := []struct{ method, path, body string }{
		{http.MethodPost, "/v1/workspaces/" + testUUID + "/agents", `{"name":"Agent","slug":"agent","initial_version":{"version_label":"v1","metadata":{}}}`},
		{http.MethodPatch, "/v1/workspaces/" + testUUID + "/agents/" + testUUID, `{"name":"Renamed"}`},
		{http.MethodPost, "/v1/workspaces/" + testUUID + "/agents/" + testUUID + "/versions", `{"version_label":"v2","metadata":{}}`},
	}
	for _, tc := range cases {
		r := httptest.NewRequest(tc.method, tc.path, strings.NewReader(tc.body))
		r.Header.Set("Content-Type", "application/json")
		r.Header.Set("Origin", "http://localhost:3000")
		r.AddCookie(&http.Cookie{Name: auth.CookieName, Value: "token"})
		w := httptest.NewRecorder()
		agentRouter(fakeAuth{allow: true}, fakeAgentService{writeErr: auth.ErrForbidden}).ServeHTTP(w, r)
		if w.Code != http.StatusForbidden {
			t.Fatalf("%s %s status=%d body=%s", tc.method, tc.path, w.Code, w.Body.String())
		}
	}
}

func TestAgentMemberCanCreate(t *testing.T) {
	body := `{"name":"Agent","slug":"agent","initial_version":{"version_label":"v1","metadata":{}}}`
	r := httptest.NewRequest(http.MethodPost, "/v1/workspaces/"+testUUID+"/agents", strings.NewReader(body))
	r.Header.Set("Content-Type", "application/json")
	r.Header.Set("Origin", "http://localhost:3000")
	r.AddCookie(&http.Cookie{Name: auth.CookieName, Value: "token"})
	w := httptest.NewRecorder()
	agentRouter(fakeAuth{allow: true}, fakeAgentService{}).ServeHTTP(w, r)
	if w.Code != http.StatusCreated {
		t.Fatalf("status=%d body=%s", w.Code, w.Body.String())
	}
}

func TestAgentViewerCanReadAndMemberCanUpdateAndVersion(t *testing.T) {
	requests := []*http.Request{
		httptest.NewRequest(http.MethodGet, "/v1/workspaces/"+testUUID+"/agents", nil),
		httptest.NewRequest(http.MethodPatch, "/v1/workspaces/"+testUUID+"/agents/"+testUUID, strings.NewReader(`{"name":"Renamed"}`)),
		httptest.NewRequest(http.MethodPost, "/v1/workspaces/"+testUUID+"/agents/"+testUUID+"/versions", strings.NewReader(`{"version_label":"v2","metadata":{}}`)),
	}
	for _, r := range requests {
		if r.Method != http.MethodGet {
			r.Header.Set("Content-Type", "application/json")
			r.Header.Set("Origin", "http://localhost:3000")
		}
		r.AddCookie(&http.Cookie{Name: auth.CookieName, Value: "token"})
		w := httptest.NewRecorder()
		agentRouter(fakeAuth{allow: true}, fakeAgentService{}).ServeHTTP(w, r)
		if w.Code != http.StatusOK && w.Code != http.StatusCreated {
			t.Fatalf("%s %s status=%d body=%s", r.Method, r.URL.Path, w.Code, w.Body.String())
		}
	}
}

func TestPublicAgentDetailHidesNonPublic(t *testing.T) {
	w := httptest.NewRecorder()
	agentRouter(fakeAuth{}, fakeAgentService{getErr: identity.ErrNotFound}).ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/v1/agents/private-agent", nil))
	if w.Code != http.StatusNotFound {
		t.Fatalf("status=%d body=%s", w.Code, w.Body.String())
	}
}

func TestPublicAgentInvalidFilter(t *testing.T) {
	w := httptest.NewRecorder()
	agentRouter(fakeAuth{}, fakeAgentService{}).ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/v1/agents?interface_kind=grpc", nil))
	if w.Code != http.StatusBadRequest {
		t.Fatalf("status=%d body=%s", w.Code, w.Body.String())
	}
}
