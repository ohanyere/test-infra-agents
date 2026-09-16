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

	"github.com/evalplane/evalplane/backend/internal/auth"
	"github.com/evalplane/evalplane/backend/internal/evaluationsuite"
	"github.com/evalplane/evalplane/backend/internal/identity"
)

type fakeSuiteService struct {
	writeErr error
	readErr  error
	startIn  evaluationsuite.StartExecutionInput
}

func (f *fakeSuiteService) CreateSuite(context.Context, auth.Principal, string, evaluationsuite.CreateSuiteInput) (evaluationsuite.Suite, error) {
	return evaluationsuite.Suite{ID: testUUID}, f.writeErr
}
func (f *fakeSuiteService) CreateSuiteVersion(context.Context, auth.Principal, string, string, evaluationsuite.CreateVersionInput) (evaluationsuite.Version, error) {
	return evaluationsuite.Version{ID: testUUID}, f.writeErr
}
func (f *fakeSuiteService) GetSuite(context.Context, auth.Principal, string, string) (evaluationsuite.Suite, error) {
	return evaluationsuite.Suite{ID: testUUID}, f.readErr
}
func (f *fakeSuiteService) ListSuites(context.Context, auth.Principal, string, evaluationsuite.Filters) ([]evaluationsuite.Suite, error) {
	return []evaluationsuite.Suite{}, f.readErr
}
func (f *fakeSuiteService) GetSuiteVersion(context.Context, auth.Principal, string, string) (evaluationsuite.Version, error) {
	return evaluationsuite.Version{ID: testUUID}, f.readErr
}
func (f *fakeSuiteService) StartSuiteExecution(_ context.Context, _ auth.Principal, _ string, in evaluationsuite.StartExecutionInput) (evaluationsuite.Execution, error) {
	f.startIn = in
	return evaluationsuite.Execution{ID: testUUID}, f.writeErr
}
func (f *fakeSuiteService) GetSuiteExecution(context.Context, auth.Principal, string, string) (evaluationsuite.Execution, error) {
	return evaluationsuite.Execution{ID: testUUID}, f.readErr
}
func (f *fakeSuiteService) GetSuiteResults(context.Context, auth.Principal, string, string) (evaluationsuite.Results, error) {
	return evaluationsuite.Results{ExecutionID: testUUID}, f.readErr
}

func suiteRouter(authentication fakeAuth, service *fakeSuiteService) http.Handler {
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	return NewRouter(healthyDatabase{}, logger, WithIdentityService(fakeIdentity{}), WithAuthentication(authentication, authentication, false, 168*time.Hour, "http://localhost:3000"), WithEvaluationSuiteService(service))
}

func TestEvaluationSuiteRoutesRequireAuthentication(t *testing.T) {
	request := httptest.NewRequest(http.MethodGet, "/v1/workspaces/"+testUUID+"/evaluation-suites", nil)
	response := httptest.NewRecorder()
	suiteRouter(fakeAuth{authenticateErr: auth.ErrUnauthenticated}, &fakeSuiteService{}).ServeHTTP(response, request)
	if response.Code != http.StatusUnauthorized {
		t.Fatalf("status=%d", response.Code)
	}
}

func TestEvaluationSuiteViewerCanRead(t *testing.T) {
	paths := []string{
		"/v1/workspaces/" + testUUID + "/evaluation-suites",
		"/v1/workspaces/" + testUUID + "/evaluation-suites/" + testUUID,
		"/v1/workspaces/" + testUUID + "/evaluation-suite-versions/" + testUUID,
		"/v1/workspaces/" + testUUID + "/suite-executions/" + testUUID,
		"/v1/workspaces/" + testUUID + "/suite-executions/" + testUUID + "/results",
	}
	for _, path := range paths {
		request := authenticatedSuiteRequest(http.MethodGet, path, "")
		response := httptest.NewRecorder()
		suiteRouter(fakeAuth{allow: true}, &fakeSuiteService{}).ServeHTTP(response, request)
		if response.Code != http.StatusOK {
			t.Fatalf("%s status=%d body=%s", path, response.Code, response.Body.String())
		}
	}
}

func TestEvaluationSuiteViewerCannotCreateOrStart(t *testing.T) {
	tests := []struct {
		path string
		body string
	}{
		{"/v1/workspaces/" + testUUID + "/evaluation-suites", `{"name":"Core qualification"}`},
		{"/v1/workspaces/" + testUUID + "/evaluation-suites/" + testUUID + "/versions", `{"scenarios":[{"scenario_version_id":"11111111-1111-4111-8111-111111111111","trial_count":3}]}`},
		{"/v1/workspaces/" + testUUID + "/suite-executions", `{"suite_version_id":"11111111-1111-4111-8111-111111111111","agent_version_ids":["22222222-2222-4222-8222-222222222222"]}`},
	}
	for _, test := range tests {
		request := authenticatedSuiteRequest(http.MethodPost, test.path, test.body)
		response := httptest.NewRecorder()
		suiteRouter(fakeAuth{allow: true}, &fakeSuiteService{writeErr: auth.ErrForbidden}).ServeHTTP(response, request)
		if response.Code != http.StatusForbidden {
			t.Fatalf("%s status=%d body=%s", test.path, response.Code, response.Body.String())
		}
	}
}

func TestStartSuiteExecutionPassesExactVersionIDs(t *testing.T) {
	service := &fakeSuiteService{}
	body := `{"suite_version_id":"11111111-1111-4111-8111-111111111111","agent_version_ids":["22222222-2222-4222-8222-222222222222","33333333-3333-4333-8333-333333333333"]}`
	request := authenticatedSuiteRequest(http.MethodPost, "/v1/workspaces/"+testUUID+"/suite-executions", body)
	response := httptest.NewRecorder()
	suiteRouter(fakeAuth{allow: true}, service).ServeHTTP(response, request)
	if response.Code != http.StatusCreated {
		t.Fatalf("status=%d body=%s", response.Code, response.Body.String())
	}
	if service.startIn.SuiteVersionID != testUUID || len(service.startIn.AgentVersionIDs) != 2 || service.startIn.AgentVersionIDs[0] != "22222222-2222-4222-8222-222222222222" {
		t.Fatalf("exact version input was not preserved: %+v", service.startIn)
	}
}

func TestEvaluationSuiteWorkspaceIsolation(t *testing.T) {
	request := authenticatedSuiteRequest(http.MethodGet, "/v1/workspaces/"+testUUID+"/evaluation-suites/"+testUUID, "")
	response := httptest.NewRecorder()
	suiteRouter(fakeAuth{allow: true}, &fakeSuiteService{readErr: identity.ErrNotFound}).ServeHTTP(response, request)
	if response.Code != http.StatusNotFound {
		t.Fatalf("status=%d body=%s", response.Code, response.Body.String())
	}
}

func authenticatedSuiteRequest(method, path, body string) *http.Request {
	request := httptest.NewRequest(method, path, strings.NewReader(body))
	request.AddCookie(&http.Cookie{Name: auth.CookieName, Value: "token"})
	if method != http.MethodGet {
		request.Header.Set("Content-Type", "application/json")
		request.Header.Set("Origin", "http://localhost:3000")
	}
	return request
}
