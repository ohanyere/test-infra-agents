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

	"github.com/evalplane/evalplane/backend/internal/auth"
	"github.com/evalplane/evalplane/backend/internal/identity"
	"github.com/evalplane/evalplane/backend/internal/reliability"
)

type fakeReliabilityService struct {
	readErr error
}

func (f *fakeReliabilityService) ScenarioReliability(context.Context, auth.Principal, string, string, string, string) (reliability.ScenarioReport, error) {
	return reliability.ScenarioReport{ReliabilityContract: reliability.ReliabilityContractVersion}, f.readErr
}
func (f *fakeReliabilityService) ExecutionReliability(context.Context, auth.Principal, string, string, string, string) (reliability.ExecutionReport, reliability.ComparisonReport, error) {
	return reliability.ExecutionReport{ReliabilityContract: reliability.ReliabilityContractVersion}, reliability.ComparisonReport{}, f.readErr
}
func (f *fakeReliabilityService) AgentVersionReliability(context.Context, auth.Principal, string, string, string) (reliability.AgentReport, error) {
	return reliability.AgentReport{ReliabilityContract: reliability.ReliabilityContractVersion}, f.readErr
}

func reliabilityRouter(authentication fakeAuth, service *fakeReliabilityService) http.Handler {
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	return NewRouter(healthyDatabase{}, logger, WithIdentityService(fakeIdentity{}), WithAuthentication(authentication, authentication, false, 168*time.Hour, "http://localhost:3000"), WithReliabilityService(service))
}

func authenticatedReliabilityRequest(method, path string) *http.Request {
	request := httptest.NewRequest(method, path, strings.NewReader(""))
	request.AddCookie(&http.Cookie{Name: auth.CookieName, Value: "token"})
	return request
}

func TestReliabilityRoutesRequireAuthentication(t *testing.T) {
	paths := []string{
		"/v1/workspaces/" + testUUID + "/agents/" + testUUID + "/versions/" + testUUID + "/reliability",
		"/v1/workspaces/" + testUUID + "/suite-executions/" + testUUID + "/reliability",
		"/v1/workspaces/" + testUUID + "/scenarios/" + testUUID + "/versions/" + testUUID + "/reliability",
	}
	for _, path := range paths {
		request := httptest.NewRequest(http.MethodGet, path, nil)
		response := httptest.NewRecorder()
		reliabilityRouter(fakeAuth{authenticateErr: auth.ErrUnauthenticated}, &fakeReliabilityService{}).ServeHTTP(response, request)
		if response.Code != http.StatusUnauthorized {
			t.Fatalf("%s status=%d", path, response.Code)
		}
	}
}

func TestReliabilityViewerCanRead(t *testing.T) {
	paths := []string{
		"/v1/workspaces/" + testUUID + "/agents/" + testUUID + "/versions/" + testUUID + "/reliability",
		"/v1/workspaces/" + testUUID + "/suite-executions/" + testUUID + "/reliability",
		"/v1/workspaces/" + testUUID + "/scenarios/" + testUUID + "/versions/" + testUUID + "/reliability",
		"/v1/workspaces/" + testUUID + "/scenarios/" + testUUID + "/versions/" + testUUID + "/reliability?agent_version_id=" + testUUID,
	}
	for _, path := range paths {
		request := authenticatedReliabilityRequest(http.MethodGet, path)
		response := httptest.NewRecorder()
		reliabilityRouter(fakeAuth{allow: true}, &fakeReliabilityService{}).ServeHTTP(response, request)
		if response.Code != http.StatusOK {
			t.Fatalf("%s status=%d body=%s", path, response.Code, response.Body.String())
		}
		var payload map[string]any
		if err := json.Unmarshal(response.Body.Bytes(), &payload); err != nil {
			t.Fatalf("%s invalid JSON: %v", path, err)
		}
		if payload["reliability_contract_version"] != reliability.ReliabilityContractVersion {
			t.Fatalf("%s must pin the reliability contract: %v", path, payload)
		}
	}
}

func TestReliabilityComparisonRequiresBothSides(t *testing.T) {
	path := "/v1/workspaces/" + testUUID + "/suite-executions/" + testUUID + "/reliability?baseline_agent_version_id=" + testUUID
	request := authenticatedReliabilityRequest(http.MethodGet, path)
	response := httptest.NewRecorder()
	reliabilityRouter(fakeAuth{allow: true}, &fakeReliabilityService{}).ServeHTTP(response, request)
	if response.Code != http.StatusBadRequest {
		t.Fatalf("status=%d body=%s", response.Code, response.Body.String())
	}
}

func TestReliabilityRejectsInvalidUUIDs(t *testing.T) {
	paths := []string{
		"/v1/workspaces/not-a-uuid/agents/" + testUUID + "/versions/" + testUUID + "/reliability",
		"/v1/workspaces/" + testUUID + "/suite-executions/not-a-uuid/reliability",
		"/v1/workspaces/" + testUUID + "/scenarios/" + testUUID + "/versions/" + testUUID + "/reliability?agent_version_id=nope",
		"/v1/workspaces/" + testUUID + "/suite-executions/" + testUUID + "/reliability?baseline_agent_version_id=" + testUUID + "&candidate_agent_version_id=nope",
	}
	for _, path := range paths {
		request := authenticatedReliabilityRequest(http.MethodGet, path)
		response := httptest.NewRecorder()
		reliabilityRouter(fakeAuth{allow: true}, &fakeReliabilityService{}).ServeHTTP(response, request)
		if response.Code != http.StatusBadRequest {
			t.Fatalf("%s status=%d body=%s", path, response.Code, response.Body.String())
		}
	}
}

func TestReliabilityWorkspaceIsolation(t *testing.T) {
	request := authenticatedReliabilityRequest(http.MethodGet, "/v1/workspaces/"+testUUID+"/suite-executions/"+testUUID+"/reliability")
	response := httptest.NewRecorder()
	reliabilityRouter(fakeAuth{allow: true}, &fakeReliabilityService{readErr: identity.ErrNotFound}).ServeHTTP(response, request)
	if response.Code != http.StatusNotFound {
		t.Fatalf("status=%d body=%s", response.Code, response.Body.String())
	}
}

func TestReliabilityComparisonShape(t *testing.T) {
	path := "/v1/workspaces/" + testUUID + "/suite-executions/" + testUUID + "/reliability?baseline_agent_version_id=" + testUUID + "&candidate_agent_version_id=" + testUUID
	request := authenticatedReliabilityRequest(http.MethodGet, path)
	response := httptest.NewRecorder()
	reliabilityRouter(fakeAuth{allow: true}, &fakeReliabilityService{}).ServeHTTP(response, request)
	if response.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", response.Code, response.Body.String())
	}
	var payload map[string]any
	if err := json.Unmarshal(response.Body.Bytes(), &payload); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}
	if _, ok := payload["report"]; !ok {
		t.Fatalf("comparison response must carry report: %v", payload)
	}
	if _, ok := payload["comparison"]; !ok {
		t.Fatalf("comparison response must carry comparison: %v", payload)
	}
}
