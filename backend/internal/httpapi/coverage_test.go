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
	"github.com/evalplane/evalplane/backend/internal/coverage"
)

type fakeCoverageService struct {
	writeErr    error
	readErr     error
	assigned    coverage.Assignment
	assessments []coverage.PersistedAssessment
	computed    []coverage.Assessment
}

func (f *fakeCoverageService) AssignFamily(context.Context, auth.Principal, string, string, string, string, string) (coverage.Assignment, error) {
	if f.writeErr != nil {
		return coverage.Assignment{}, f.writeErr
	}
	if f.assigned.ScenarioVersionID == "" {
		f.assigned = coverage.Assignment{ScenarioVersionID: testUUID, FamilyID: "kubernetes.capacity.traffic_spike", CaseType: "positive", ContractVersion: coverage.TaskFamilyContractVersion}
	}
	return f.assigned, nil
}
func (f *fakeCoverageService) GetAssignment(context.Context, auth.Principal, string, string, string) (coverage.Assignment, error) {
	if f.readErr != nil {
		return coverage.Assignment{}, f.readErr
	}
	return coverage.Assignment{ScenarioVersionID: testUUID, FamilyID: "kubernetes.capacity.traffic_spike", CaseType: "positive", ContractVersion: coverage.TaskFamilyContractVersion}, nil
}
func (f *fakeCoverageService) ComputeExecutionCoverage(context.Context, auth.Principal, string, string, string, string) ([]coverage.Assessment, error) {
	if f.readErr != nil {
		return nil, f.readErr
	}
	return f.computed, nil
}
func (f *fakeCoverageService) PersistExecutionCoverage(context.Context, auth.Principal, string, string, string, string) ([]coverage.PersistedAssessment, error) {
	if f.writeErr != nil {
		return nil, f.writeErr
	}
	return f.assessments, nil
}
func (f *fakeCoverageService) ListVersionCoverage(context.Context, auth.Principal, string, string, string) ([]coverage.PersistedAssessment, error) {
	if f.readErr != nil {
		return nil, f.readErr
	}
	return f.assessments, nil
}
func (f *fakeCoverageService) ListExecutionCoverage(context.Context, auth.Principal, string, string, string) ([]coverage.PersistedAssessment, error) {
	if f.readErr != nil {
		return nil, f.readErr
	}
	return f.assessments, nil
}

func coverageRouter(service CoverageService) http.Handler {
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	return NewRouter(healthyDatabase{}, logger,
		WithIdentityService(fakeIdentity{}),
		WithAuthentication(fakeAuth{allow: true}, fakeAuth{allow: true}, false, 168*time.Hour, "http://localhost:3000"),
		WithAgentService(fakeAgentService{}),
		WithScenarioService(fakeScenarioService{}),
		WithCoverageService(service))
}

func authenticatedCoverageRequest(method, path, body string) *httptest.ResponseRecorder {
	var reader *strings.Reader
	if body == "" {
		reader = strings.NewReader("")
	} else {
		reader = strings.NewReader(body)
	}
	r := httptest.NewRequest(method, path, reader)
	if method != http.MethodGet {
		r.Header.Set("Content-Type", "application/json")
		r.Header.Set("Origin", "http://localhost:3000")
	}
	r.AddCookie(&http.Cookie{Name: auth.CookieName, Value: "token"})
	w := httptest.NewRecorder()
	return w
}

func serveCoverage(service CoverageService, method, path, body string) *httptest.ResponseRecorder {
	w := authenticatedCoverageRequest(method, path, body)
	var reader *strings.Reader
	if body == "" {
		reader = strings.NewReader("")
	} else {
		reader = strings.NewReader(body)
	}
	r := httptest.NewRequest(method, path, reader)
	if method != http.MethodGet {
		r.Header.Set("Content-Type", "application/json")
		r.Header.Set("Origin", "http://localhost:3000")
	}
	r.AddCookie(&http.Cookie{Name: auth.CookieName, Value: "token"})
	recorder := httptest.NewRecorder()
	coverageRouter(service).ServeHTTP(recorder, r)
	_ = w
	return recorder
}

func TestTaskFamilyCatalogReadWorks(t *testing.T) {
	w := serveCoverage(&fakeCoverageService{}, http.MethodGet, "/v1/task-family-catalog", "")
	if w.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", w.Code, w.Body.String())
	}
	var body struct {
		Contract string                `json:"contract"`
		Items    []coverage.TaskFamily `json:"items"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if body.Contract != "task-family-contract:v1" {
		t.Fatalf("contract=%q", body.Contract)
	}
	if len(body.Items) == 0 {
		t.Fatal("expected built-in families")
	}
	seen := map[string]bool{}
	for _, item := range body.Items {
		if seen[item.ID] {
			t.Fatalf("duplicate %q", item.ID)
		}
		seen[item.ID] = true
	}
}

func TestCoverageRequirementsReadWorks(t *testing.T) {
	w := serveCoverage(&fakeCoverageService{}, http.MethodGet, "/v1/capability-coverage-requirements", "")
	if w.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", w.Code, w.Body.String())
	}
	var body struct {
		Contract string                         `json:"contract"`
		Items    []coverage.CoverageRequirement `json:"items"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if body.Contract != "capability-coverage-contract:v1" {
		t.Fatalf("contract=%q", body.Contract)
	}
	if len(body.Items) == 0 {
		t.Fatal("expected requirements")
	}
}

func TestVersionCoverageReadWorks(t *testing.T) {
	service := &fakeCoverageService{assessments: []coverage.PersistedAssessment{
		{Assessment: coverage.Assess("kubernetes.capacity.remediation", nil), WorkspaceID: testUUID, AgentID: testUUID, AgentVersionID: testUUID, SuiteExecutionID: testUUID},
	}}
	w := serveCoverage(service, http.MethodGet, "/v1/workspaces/"+testUUID+"/agents/"+testUUID+"/versions/"+testUUID+"/capability-coverage", "")
	if w.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", w.Code, w.Body.String())
	}
	var body struct {
		CoverageContractVersion   string                         `json:"coverage_contract_version"`
		TaskFamilyContractVersion string                         `json:"task_family_contract_version"`
		CapabilityCatalogVersion  string                         `json:"capability_catalog_version"`
		Items                     []coverage.PersistedAssessment `json:"items"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if body.CoverageContractVersion != "capability-coverage-contract:v1" ||
		body.TaskFamilyContractVersion != "task-family-contract:v1" ||
		body.CapabilityCatalogVersion != "infrastructure-capability-catalog:v1" {
		t.Fatalf("contracts not pinned: %+v", body)
	}
	if strings.Contains(w.Body.String(), "—") {
		t.Fatal("coverage API must not return em-dash domain state")
	}
	if strings.Contains(w.Body.String(), `"qualified"`) && strings.Contains(w.Body.String(), `"not_qualified"`) {
		t.Fatal("coverage must not infer qualification states")
	}
}

func TestExecutionCoverageReadWorks(t *testing.T) {
	service := &fakeCoverageService{computed: []coverage.Assessment{coverage.Assess("kubernetes.capacity.remediation", nil)}}
	w := serveCoverage(service, http.MethodGet, "/v1/workspaces/"+testUUID+"/suite-executions/"+testUUID+"/agents/"+testUUID+"/capability-coverage", "")
	if w.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", w.Code, w.Body.String())
	}
}

func TestCoverageViewerCanRead(t *testing.T) {
	// Reads flow through the same service; a viewer-forbidden read surfaces 403.
	service := &fakeCoverageService{readErr: auth.ErrForbidden}
	w := serveCoverage(service, http.MethodGet, "/v1/workspaces/"+testUUID+"/agents/"+testUUID+"/versions/"+testUUID+"/capability-coverage", "")
	if w.Code != http.StatusForbidden {
		t.Fatalf("status=%d body=%s", w.Code, w.Body.String())
	}
}

func TestCoverageViewerCannotWrite(t *testing.T) {
	service := &fakeCoverageService{writeErr: auth.ErrForbidden}
	w := serveCoverage(service, http.MethodPost, "/v1/workspaces/"+testUUID+"/suite-executions/"+testUUID+"/agents/"+testUUID+"/capability-coverage/assessments", `{"agent_id":"`+testUUID+`"}`)
	if w.Code != http.StatusForbidden {
		t.Fatalf("status=%d body=%s", w.Code, w.Body.String())
	}
	assign := serveCoverage(service, http.MethodPost, "/v1/workspaces/"+testUUID+"/scenarios/"+testUUID+"/versions/"+testUUID+"/task-family", `{"family_id":"kubernetes.capacity.traffic_spike","case_type":"positive"}`)
	if assign.Code != http.StatusForbidden {
		t.Fatalf("assign status=%d body=%s", assign.Code, assign.Body.String())
	}
}

func TestNoManualTestedEndpoint(t *testing.T) {
	// There must be no user endpoint that sets tested=true directly.
	for _, path := range []string{
		"/v1/workspaces/" + testUUID + "/agents/" + testUUID + "/versions/" + testUUID + "/capability-coverage",
		"/v1/workspaces/" + testUUID + "/suite-executions/" + testUUID + "/agents/" + testUUID + "/capability-coverage",
	} {
		r := httptest.NewRequest(http.MethodPut, path, strings.NewReader(`{"tested":true}`))
		r.Header.Set("Content-Type", "application/json")
		r.Header.Set("Origin", "http://localhost:3000")
		r.AddCookie(&http.Cookie{Name: auth.CookieName, Value: "token"})
		w := httptest.NewRecorder()
		coverageRouter(&fakeCoverageService{}).ServeHTTP(w, r)
		if w.Code == http.StatusOK {
			t.Fatalf("PUT %s must not exist as a manual tested setter", path)
		}
	}
}

func TestAssignFamilyValidation(t *testing.T) {
	w := serveCoverage(&fakeCoverageService{}, http.MethodPost, "/v1/workspaces/"+testUUID+"/scenarios/"+testUUID+"/versions/"+testUUID+"/task-family", `{"family_id":"kubernetes.capacity.traffic_spike","case_type":"positive"}`)
	if w.Code != http.StatusCreated {
		t.Fatalf("status=%d body=%s", w.Code, w.Body.String())
	}
	w = serveCoverage(&fakeCoverageService{}, http.MethodGet, "/v1/workspaces/"+testUUID+"/scenarios/"+testUUID+"/versions/"+testUUID+"/task-family", "")
	if w.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", w.Code, w.Body.String())
	}
}
