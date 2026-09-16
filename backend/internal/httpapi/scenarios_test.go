package httpapi

import (
	"context"
	"github.com/evalplane/evalplane/backend/internal/auth"
	"github.com/evalplane/evalplane/backend/internal/identity"
	"github.com/evalplane/evalplane/backend/internal/scenario"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

type fakeScenarioService struct{ writeErr, getErr error }

func (f fakeScenarioService) ListPublic(context.Context, scenario.Filters) ([]scenario.Scenario, error) {
	return []scenario.Scenario{{ID: testUUID, Slug: "public", Kind: "built_in", Visibility: "public", Status: "active"}}, nil
}
func (f fakeScenarioService) GetPublic(context.Context, string) (scenario.Scenario, error) {
	if f.getErr != nil {
		return scenario.Scenario{}, f.getErr
	}
	return scenario.Scenario{ID: testUUID}, nil
}
func (f fakeScenarioService) ListWorkspace(context.Context, auth.Principal, string, scenario.Filters) ([]scenario.Scenario, error) {
	if f.getErr != nil {
		return nil, f.getErr
	}
	return []scenario.Scenario{}, nil
}
func (f fakeScenarioService) GetWorkspace(context.Context, auth.Principal, string, string) (scenario.Scenario, error) {
	return scenario.Scenario{}, f.getErr
}
func (f fakeScenarioService) Create(context.Context, auth.Principal, string, scenario.CreateInput) (scenario.Scenario, error) {
	return scenario.Scenario{ID: testUUID}, f.writeErr
}
func (f fakeScenarioService) Update(context.Context, auth.Principal, string, string, scenario.UpdateInput) (scenario.Scenario, error) {
	return scenario.Scenario{}, f.writeErr
}
func (f fakeScenarioService) CreateVersion(context.Context, auth.Principal, string, string, scenario.VersionInput) (scenario.Version, error) {
	return scenario.Version{}, f.writeErr
}
func (fakeScenarioService) ListVersions(context.Context, auth.Principal, string, string, identity.Pagination) ([]scenario.Version, error) {
	return []scenario.Version{}, nil
}
func (f fakeScenarioService) GetVersion(context.Context, auth.Principal, string, string, string) (scenario.Version, error) {
	return scenario.Version{}, f.getErr
}
func scenarioRouter(a fakeAuth, s fakeScenarioService) http.Handler {
	l := slog.New(slog.NewTextHandler(io.Discard, nil))
	return NewRouter(healthyDatabase{}, l, WithIdentityService(fakeIdentity{}), WithAuthentication(a, a, false, 168*time.Hour, "http://localhost:3000"), WithScenarioService(s))
}
func TestPublicScenarioList(t *testing.T) {
	w := httptest.NewRecorder()
	scenarioRouter(fakeAuth{authenticateErr: auth.ErrUnauthenticated}, fakeScenarioService{}).ServeHTTP(w, httptest.NewRequest("GET", "/v1/scenarios", nil))
	if w.Code != 200 || !strings.Contains(w.Body.String(), `"kind":"built_in"`) {
		t.Fatalf("status=%d body=%s", w.Code, w.Body.String())
	}
}
func TestScenarioWorkspaceRequiresAuthentication(t *testing.T) {
	w := httptest.NewRecorder()
	scenarioRouter(fakeAuth{authenticateErr: auth.ErrUnauthenticated}, fakeScenarioService{}).ServeHTTP(w, httptest.NewRequest("GET", "/v1/workspaces/"+testUUID+"/scenarios", nil))
	if w.Code != 401 {
		t.Fatalf("status=%d", w.Code)
	}
}
func TestScenarioViewerCannotWrite(t *testing.T) {
	cases := []struct{ method, path, body string }{{"POST", "/v1/workspaces/" + testUUID + "/scenarios", `{"name":"S","slug":"s","initial_version":{}}`}, {"PATCH", "/v1/workspaces/" + testUUID + "/scenarios/" + testUUID, `{"name":"S"}`}, {"POST", "/v1/workspaces/" + testUUID + "/scenarios/" + testUUID + "/versions", `{}`}}
	for _, c := range cases {
		r := httptest.NewRequest(c.method, c.path, strings.NewReader(c.body))
		r.Header.Set("Content-Type", "application/json")
		r.Header.Set("Origin", "http://localhost:3000")
		r.AddCookie(&http.Cookie{Name: auth.CookieName, Value: "token"})
		w := httptest.NewRecorder()
		scenarioRouter(fakeAuth{allow: true}, fakeScenarioService{writeErr: auth.ErrForbidden}).ServeHTTP(w, r)
		if w.Code != 403 {
			t.Fatalf("%s status=%d body=%s", c.path, w.Code, w.Body.String())
		}
	}
}
func TestScenarioMemberCanWriteAndViewerRead(t *testing.T) {
	cases := []struct{ method, path, body string }{{"GET", "/v1/workspaces/" + testUUID + "/scenarios", ""}, {"POST", "/v1/workspaces/" + testUUID + "/scenarios", `{"name":"S","slug":"s","initial_version":{}}`}, {"PATCH", "/v1/workspaces/" + testUUID + "/scenarios/" + testUUID, `{"name":"S"}`}, {"POST", "/v1/workspaces/" + testUUID + "/scenarios/" + testUUID + "/versions", `{}`}}
	for _, c := range cases {
		r := httptest.NewRequest(c.method, c.path, strings.NewReader(c.body))
		if c.method != "GET" {
			r.Header.Set("Content-Type", "application/json")
			r.Header.Set("Origin", "http://localhost:3000")
		}
		r.AddCookie(&http.Cookie{Name: auth.CookieName, Value: "token"})
		w := httptest.NewRecorder()
		scenarioRouter(fakeAuth{allow: true}, fakeScenarioService{}).ServeHTTP(w, r)
		if w.Code != 200 && w.Code != 201 {
			t.Fatalf("%s status=%d body=%s", c.path, w.Code, w.Body.String())
		}
	}
}
func TestPublicScenarioHidesNonPublic(t *testing.T) {
	w := httptest.NewRecorder()
	scenarioRouter(fakeAuth{}, fakeScenarioService{getErr: identity.ErrNotFound}).ServeHTTP(w, httptest.NewRequest("GET", "/v1/scenarios/private", nil))
	if w.Code != 404 {
		t.Fatalf("status=%d", w.Code)
	}
}
