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
	"github.com/evalplane/evalplane/backend/internal/qualplan"
)

type fakeQualificationPlanService struct {
	plan      qualplan.Plan
	buildErr  error
	planErr   error
	suiteErr  error
	build     qualplan.SuiteBuild
	suite     evaluationsuite.Suite
	version   evaluationsuite.Version
	gotSubset []string
}

func (f *fakeQualificationPlanService) BuildPlan(_ context.Context, _ auth.Principal, _, _, _ string, subset []string) (qualplan.Plan, error) {
	if f.buildErr != nil {
		return qualplan.Plan{}, f.buildErr
	}
	f.gotSubset = append([]string(nil), subset...)
	if f.plan.ID == "" {
		f.plan = qualplan.Plan{
			ID: "22222222-2222-4222-8222-222222222222", WorkspaceID: testUUID,
			AgentID: testUUID, AgentVersionID: testUUID, State: qualplan.StateBlocked,
			CapabilityIDs: []string{}, PlannerContractVersion: qualplan.PlannerContractVersion,
			CoverageContractVersion: qualplan.CoverageContractVersion,
			Blockers:                []qualplan.Blocker{{Code: qualplan.BlockerNoCapabilityClaims, Detail: "no claims", BlocksSuite: true}},
			Capabilities:            []qualplan.CapabilityPlan{},
		}
	}
	return f.plan, nil
}

func (f *fakeQualificationPlanService) GetPlan(context.Context, auth.Principal, string, string) (qualplan.Plan, error) {
	if f.planErr != nil {
		return qualplan.Plan{}, f.planErr
	}
	return f.plan, nil
}

func (f *fakeQualificationPlanService) BuildSuite(context.Context, auth.Principal, string, string) (qualplan.SuiteBuild, evaluationsuite.Suite, evaluationsuite.Version, error) {
	if f.suiteErr != nil {
		return qualplan.SuiteBuild{}, evaluationsuite.Suite{}, evaluationsuite.Version{}, f.suiteErr
	}
	return f.build, f.suite, f.version, nil
}

func planRouter(service QualificationPlanService) http.Handler {
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	return NewRouter(healthyDatabase{}, logger,
		WithIdentityService(fakeIdentity{}),
		WithAuthentication(fakeAuth{allow: true}, fakeAuth{allow: true}, false, 168*time.Hour, "http://localhost:3000"),
		WithQualificationPlanService(service))
}

func servePlan(service QualificationPlanService, method, path, body string) *httptest.ResponseRecorder {
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
	planRouter(service).ServeHTTP(recorder, r)
	return recorder
}

func TestBuildPlanCreatesPlan(t *testing.T) {
	service := &fakeQualificationPlanService{}
	w := servePlan(service, http.MethodPost, "/v1/workspaces/"+testUUID+"/agents/"+testUUID+"/versions/"+testUUID+"/qualification-plans", `{"capability_ids":["kubernetes.capacity.remediation"]}`)
	if w.Code != http.StatusCreated {
		t.Fatalf("status=%d body=%s", w.Code, w.Body.String())
	}
	if len(service.gotSubset) != 1 || service.gotSubset[0] != "kubernetes.capacity.remediation" {
		t.Fatalf("subset not forwarded: %v", service.gotSubset)
	}
	body := w.Body.String()
	if !strings.Contains(body, "qualification-suite-planner:v1") {
		t.Fatal("plan must pin the planner contract")
	}
	if strings.Contains(body, `"qualified"`) {
		t.Fatal("planner response must never set QUALIFIED")
	}
	for _, leaked := range []string{"ground_truth", "reference_remediation", "hidden_variant", "reference_solution", "kubeconfig", "bearer_token"} {
		if strings.Contains(body, leaked) {
			t.Fatalf("planner response leaks evaluator-only data: %s", leaked)
		}
	}
}

func TestBuildPlanRejectsServerDerivedFields(t *testing.T) {
	for _, body := range []string{
		`{"state":"ready"}`,
		`{"capability_ids":[],"selected_task_ids":["x"]}`,
		`{"certified":true}`,
		`{"coverage_result":"tested"}`,
		`{"qualification_result":"qualified"}`,
	} {
		w := servePlan(&fakeQualificationPlanService{}, http.MethodPost, "/v1/workspaces/"+testUUID+"/agents/"+testUUID+"/versions/"+testUUID+"/qualification-plans", body)
		if w.Code != http.StatusBadRequest {
			t.Fatalf("body %s: status=%d, want 400", body, w.Code)
		}
	}
}

func TestBuildSuiteRejectsCallerTaskIDs(t *testing.T) {
	w := servePlan(&fakeQualificationPlanService{}, http.MethodPost, "/v1/workspaces/"+testUUID+"/qualification-plans/"+testUUID+"/suite", `{"scenario_version_ids":["`+testUUID+`"]}`)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("status=%d, want 400 for caller-supplied task IDs", w.Code)
	}
}

func TestBuildSuiteBlockedPlanFails(t *testing.T) {
	service := &fakeQualificationPlanService{suiteErr: identity.ErrValidation}
	w := servePlan(service, http.MethodPost, "/v1/workspaces/"+testUUID+"/qualification-plans/"+testUUID+"/suite", "")
	if w.Code != http.StatusBadRequest {
		t.Fatalf("status=%d body=%s, want 400", w.Code, w.Body.String())
	}
}

func TestPlanViewerCannotBuild(t *testing.T) {
	service := &fakeQualificationPlanService{buildErr: auth.ErrForbidden, suiteErr: auth.ErrForbidden}
	w := servePlan(service, http.MethodPost, "/v1/workspaces/"+testUUID+"/agents/"+testUUID+"/versions/"+testUUID+"/qualification-plans", `{}`)
	if w.Code != http.StatusForbidden {
		t.Fatalf("build plan status=%d, want 403", w.Code)
	}
	w = servePlan(service, http.MethodPost, "/v1/workspaces/"+testUUID+"/qualification-plans/"+testUUID+"/suite", "")
	if w.Code != http.StatusForbidden {
		t.Fatalf("build suite status=%d, want 403", w.Code)
	}
}

func TestGetPlanReadsBack(t *testing.T) {
	service := &fakeQualificationPlanService{}
	service.plan = qualplan.Plan{ID: testUUID, State: qualplan.StatePartial, PlannerContractVersion: qualplan.PlannerContractVersion, Capabilities: []qualplan.CapabilityPlan{}, Blockers: []qualplan.Blocker{}}
	w := servePlan(service, http.MethodGet, "/v1/workspaces/"+testUUID+"/qualification-plans/"+testUUID, "")
	if w.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", w.Code, w.Body.String())
	}
}
