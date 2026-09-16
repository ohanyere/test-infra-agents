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
	"github.com/evalplane/evalplane/backend/internal/failuretaxonomy"
	"github.com/evalplane/evalplane/backend/internal/qualification"
)

type fakeQualificationService struct {
	writeErr     error
	assessmentIn qualification.CreateAssessmentInput
}

func (f *fakeQualificationService) CreatePolicy(context.Context, auth.Principal, string, qualification.CreatePolicyInput) (qualification.Policy, error) {
	return qualification.Policy{ID: testUUID}, f.writeErr
}
func (f *fakeQualificationService) CreatePolicyVersion(context.Context, auth.Principal, string, string, qualification.CreateVersionInput) (qualification.PolicyVersion, error) {
	return qualification.PolicyVersion{ID: testUUID}, f.writeErr
}
func (f *fakeQualificationService) GetPolicy(context.Context, auth.Principal, string, string) (qualification.Policy, error) {
	return qualification.Policy{ID: testUUID}, nil
}
func (f *fakeQualificationService) GetPolicyVersion(context.Context, auth.Principal, string, string) (qualification.PolicyVersion, error) {
	return qualification.PolicyVersion{ID: testUUID}, nil
}
func (f *fakeQualificationService) ListPolicies(context.Context, auth.Principal, string) ([]qualification.Policy, error) {
	return []qualification.Policy{}, nil
}
func (f *fakeQualificationService) CreateAssessment(_ context.Context, _ auth.Principal, _ string, in qualification.CreateAssessmentInput) (qualification.Assessment, error) {
	f.assessmentIn = in
	return qualification.Assessment{ID: testUUID}, f.writeErr
}
func (f *fakeQualificationService) GetAssessment(context.Context, auth.Principal, string, string) (qualification.Assessment, error) {
	return qualification.Assessment{ID: testUUID}, nil
}
func (f *fakeQualificationService) ComparativeReport(context.Context, auth.Principal, string, string, string) (qualification.ComparativeReport, error) {
	return qualification.ComparativeReport{Comparability: "comparable"}, nil
}

type fakeFailureService struct{}

func (fakeFailureService) ForExecution(context.Context, auth.Principal, string, string) (failuretaxonomy.Results, error) {
	return failuretaxonomy.Results{TaxonomyVersion: failuretaxonomy.Version}, nil
}
func qualificationRouter(service QualificationService) http.Handler {
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	authentication := fakeAuth{allow: true}
	return NewRouter(healthyDatabase{}, logger, WithIdentityService(fakeIdentity{}), WithAuthentication(authentication, authentication, false, 168*time.Hour, "http://localhost:3000"), WithQualificationService(service), WithFailureTaxonomyService(fakeFailureService{}))
}

func TestQualificationReadRoutes(t *testing.T) {
	service := &fakeQualificationService{}
	paths := []string{"/v1/workspaces/" + testUUID + "/qualification-policies", "/v1/workspaces/" + testUUID + "/qualification-policies/" + testUUID, "/v1/workspaces/" + testUUID + "/qualification-policy-versions/" + testUUID, "/v1/workspaces/" + testUUID + "/qualification-assessments/" + testUUID, "/v1/workspaces/" + testUUID + "/suite-executions/" + testUUID + "/comparison", "/v1/workspaces/" + testUUID + "/suite-executions/" + testUUID + "/failures"}
	for _, path := range paths {
		request := authenticatedSuiteRequest(http.MethodGet, path, "")
		response := httptest.NewRecorder()
		qualificationRouter(service).ServeHTTP(response, request)
		if response.Code != http.StatusOK {
			t.Fatalf("%s status=%d body=%s", path, response.Code, response.Body.String())
		}
	}
}
func TestQualificationAssessmentPinsExactVersions(t *testing.T) {
	service := &fakeQualificationService{}
	body := `{"policy_version_id":"11111111-1111-4111-8111-111111111111","suite_execution_id":"22222222-2222-4222-8222-222222222222","agent_version_id":"33333333-3333-4333-8333-333333333333"}`
	request := authenticatedSuiteRequest(http.MethodPost, "/v1/workspaces/"+testUUID+"/qualification-assessments", body)
	response := httptest.NewRecorder()
	qualificationRouter(service).ServeHTTP(response, request)
	if response.Code != http.StatusCreated || service.assessmentIn.PolicyVersionID != "11111111-1111-4111-8111-111111111111" || service.assessmentIn.AgentVersionID != "33333333-3333-4333-8333-333333333333" {
		t.Fatalf("status=%d input=%#v", response.Code, service.assessmentIn)
	}
}
func TestQualificationViewerCannotWrite(t *testing.T) {
	service := &fakeQualificationService{writeErr: auth.ErrForbidden}
	tests := []struct{ path, body string }{{"/v1/workspaces/" + testUUID + "/qualification-policies", `{"name":"Production adoption"}`}, {"/v1/workspaces/" + testUUID + "/qualification-policies/" + testUUID + "/versions", `{"definition":{"minimum_valid_trials":3,"minimum_overall_pass_rate":90,"maximum_inconclusive_rate":10,"maximum_safety_violations":0}}`}, {"/v1/workspaces/" + testUUID + "/qualification-assessments", `{"policy_version_id":"11111111-1111-4111-8111-111111111111","suite_execution_id":"22222222-2222-4222-8222-222222222222","agent_version_id":"33333333-3333-4333-8333-333333333333"}`}}
	for _, tc := range tests {
		request := httptest.NewRequest(http.MethodPost, tc.path, strings.NewReader(tc.body))
		request.AddCookie(&http.Cookie{Name: auth.CookieName, Value: "token"})
		request.Header.Set("Content-Type", "application/json")
		request.Header.Set("Origin", "http://localhost:3000")
		response := httptest.NewRecorder()
		qualificationRouter(service).ServeHTTP(response, request)
		if response.Code != http.StatusForbidden {
			t.Fatalf("%s status=%d", tc.path, response.Code)
		}
	}
}
