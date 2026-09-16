package httpapi

import (
	"context"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/evalplane/evalplane/backend/internal/agentregression"
	"github.com/evalplane/evalplane/backend/internal/auth"
)

type fakeRegressionService struct {
	in       agentregression.CreateInput
	writeErr error
}

func (f *fakeRegressionService) Create(_ context.Context, _ auth.Principal, _ string, in agentregression.CreateInput) (agentregression.Comparison, error) {
	f.in = in
	return agentregression.Comparison{ID: testUUID}, f.writeErr
}
func (f *fakeRegressionService) Get(context.Context, auth.Principal, string, string) (agentregression.Comparison, error) {
	return agentregression.Comparison{ID: testUUID}, nil
}
func (f *fakeRegressionService) Results(context.Context, auth.Principal, string, string) (agentregression.Results, error) {
	return agentregression.Results{Classification: agentregression.ClassificationNoDetectedRegression}, nil
}
func regressionRouter(service AgentRegressionService) http.Handler {
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	authentication := fakeAuth{allow: true}
	return NewRouter(healthyDatabase{}, logger, WithIdentityService(fakeIdentity{}), WithAuthentication(authentication, authentication, false, 168*time.Hour, "http://localhost:3000"), WithAgentRegressionService(service))
}
func TestAgentRegressionRoutesAndExactInput(t *testing.T) {
	service := &fakeRegressionService{}
	body := `{"suite_version_id":"33333333-3333-4333-8333-333333333333","baseline_agent_version_id":"11111111-1111-4111-8111-111111111111","candidate_agent_version_id":"22222222-2222-4222-8222-222222222222"}`
	request := authenticatedSuiteRequest(http.MethodPost, "/v1/workspaces/"+testUUID+"/agent-regressions", body)
	response := httptest.NewRecorder()
	regressionRouter(service).ServeHTTP(response, request)
	if response.Code != http.StatusCreated || service.in.BaselineAgentVersionID != "11111111-1111-4111-8111-111111111111" || service.in.CandidateAgentVersionID != "22222222-2222-4222-8222-222222222222" {
		t.Fatalf("status=%d body=%s input=%#v", response.Code, response.Body.String(), service.in)
	}
	for _, suffix := range []string{"", "/results"} {
		request = authenticatedSuiteRequest(http.MethodGet, "/v1/workspaces/"+testUUID+"/agent-regressions/"+testUUID+suffix, "")
		response = httptest.NewRecorder()
		regressionRouter(service).ServeHTTP(response, request)
		if response.Code != http.StatusOK {
			t.Fatalf("GET %s status=%d", suffix, response.Code)
		}
	}
}
func TestAgentRegressionViewerCannotCreate(t *testing.T) {
	service := &fakeRegressionService{writeErr: auth.ErrForbidden}
	request := authenticatedSuiteRequest(http.MethodPost, "/v1/workspaces/"+testUUID+"/agent-regressions", `{"suite_version_id":"33333333-3333-4333-8333-333333333333","baseline_agent_version_id":"11111111-1111-4111-8111-111111111111","candidate_agent_version_id":"22222222-2222-4222-8222-222222222222"}`)
	response := httptest.NewRecorder()
	regressionRouter(service).ServeHTTP(response, request)
	if response.Code != http.StatusForbidden {
		t.Fatalf("status=%d", response.Code)
	}
}
