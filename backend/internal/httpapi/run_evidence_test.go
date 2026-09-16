package httpapi

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/evalplane/evalplane/backend/internal/auth"
	"github.com/evalplane/evalplane/backend/internal/evidence"
	"github.com/evalplane/evalplane/backend/internal/identity"
)

type fakeEvidenceService struct {
	readErr    error
	evaluation evidence.Evaluation
}

func (f fakeEvidenceService) ListRunEvents(context.Context, auth.Principal, string, string, evidence.EventFilters) ([]evidence.RunEvent, error) {
	return []evidence.RunEvent{}, f.readErr
}

func (f fakeEvidenceService) ListAgentActions(context.Context, auth.Principal, string, string, evidence.ActionFilters) ([]evidence.AgentAction, error) {
	return []evidence.AgentAction{}, f.readErr
}

func (f fakeEvidenceService) GetEvaluation(context.Context, auth.Principal, string, string) (evidence.Evaluation, error) {
	return f.evaluation, f.readErr
}

func evidenceRouter(authentication fakeAuth, service fakeEvidenceService) http.Handler {
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	return NewRouter(healthyDatabase{}, logger, WithIdentityService(fakeIdentity{}), WithAuthentication(authentication, authentication, false, 168*time.Hour, "http://localhost:3000"), WithEvidenceService(service))
}

func TestRunEventReadRequiresAuthentication(t *testing.T) {
	w := httptest.NewRecorder()
	evidenceRouter(fakeAuth{authenticateErr: auth.ErrUnauthenticated}, fakeEvidenceService{}).ServeHTTP(w, httptest.NewRequest(http.MethodGet, evidencePath("events"), nil))
	if w.Code != http.StatusUnauthorized {
		t.Fatalf("status=%d body=%s", w.Code, w.Body.String())
	}
}

func TestEvidenceWorkspaceMemberCanRead(t *testing.T) {
	for _, suffix := range []string{"events", "actions", "evaluation"} {
		r := authenticatedEvidenceRequest(evidencePath(suffix))
		w := httptest.NewRecorder()
		evidenceRouter(fakeAuth{allow: true}, fakeEvidenceService{evaluation: evidence.Evaluation{ID: testUUID, RunID: testUUID, Result: "pass", Criteria: []evidence.CriterionResult{}, Findings: []evidence.Finding{}}}).ServeHTTP(w, r)
		if w.Code != http.StatusOK {
			t.Fatalf("%s status=%d body=%s", suffix, w.Code, w.Body.String())
		}
	}
}

func TestEvidenceWorkspaceIsolation(t *testing.T) {
	r := authenticatedEvidenceRequest(evidencePath("events"))
	w := httptest.NewRecorder()
	evidenceRouter(fakeAuth{allow: true}, fakeEvidenceService{readErr: identity.ErrNotFound}).ServeHTTP(w, r)
	if w.Code != http.StatusNotFound {
		t.Fatalf("status=%d body=%s", w.Code, w.Body.String())
	}
}

func TestRunEventFilterValidation(t *testing.T) {
	r := authenticatedEvidenceRequest(evidencePath("events") + "?event_type=invalid")
	w := httptest.NewRecorder()
	evidenceRouter(fakeAuth{allow: true}, fakeEvidenceService{}).ServeHTTP(w, r)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("status=%d body=%s", w.Code, w.Body.String())
	}
}

func TestAgentActionFilterValidation(t *testing.T) {
	r := authenticatedEvidenceRequest(evidencePath("actions") + "?status=invalid")
	w := httptest.NewRecorder()
	evidenceRouter(fakeAuth{allow: true}, fakeEvidenceService{}).ServeHTTP(w, r)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("status=%d body=%s", w.Code, w.Body.String())
	}
}

func TestMissingEvaluation(t *testing.T) {
	r := authenticatedEvidenceRequest(evidencePath("evaluation"))
	w := httptest.NewRecorder()
	evidenceRouter(fakeAuth{allow: true}, fakeEvidenceService{readErr: identity.ErrNotFound}).ServeHTTP(w, r)
	if w.Code != http.StatusNotFound {
		t.Fatalf("status=%d body=%s", w.Code, w.Body.String())
	}
}

func TestStoredEvaluationReturned(t *testing.T) {
	score := 92.0
	stored := evidence.Evaluation{ID: testUUID, RunID: testUUID, Result: "pass", Score: &score, Summary: "Recovered", Criteria: []evidence.CriterionResult{{ScenarioCriterionID: testUUID, Key: "recovery", Passed: true}}, Findings: []evidence.Finding{{ID: testUUID, Kind: "strength", Summary: "Fast recovery"}}}
	r := authenticatedEvidenceRequest(evidencePath("evaluation"))
	w := httptest.NewRecorder()
	evidenceRouter(fakeAuth{allow: true}, fakeEvidenceService{evaluation: stored}).ServeHTTP(w, r)
	if w.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", w.Code, w.Body.String())
	}
	var got evidence.Evaluation
	if err := json.Unmarshal(w.Body.Bytes(), &got); err != nil || got.Result != "pass" || got.Score == nil || *got.Score != 92 || len(got.Criteria) != 1 || len(got.Findings) != 1 {
		t.Fatalf("evaluation=%+v error=%v", got, err)
	}
}

func evidencePath(suffix string) string {
	return "/v1/workspaces/" + testUUID + "/runs/" + testUUID + "/" + suffix
}

func authenticatedEvidenceRequest(path string) *http.Request {
	r := httptest.NewRequest(http.MethodGet, path, nil)
	r.AddCookie(&http.Cookie{Name: auth.CookieName, Value: "token"})
	return r
}
