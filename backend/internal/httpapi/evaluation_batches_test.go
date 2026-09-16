package httpapi

import (
	"context"
	"github.com/evalplane/evalplane/backend/internal/auth"
	"github.com/evalplane/evalplane/backend/internal/evaluationbatch"
	"github.com/evalplane/evalplane/backend/internal/identity"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

type fakeBatchService struct{ writeErr, readErr error }

func (f fakeBatchService) Create(context.Context, auth.Principal, string, evaluationbatch.CreateInput) (evaluationbatch.Batch, error) {
	return evaluationbatch.Batch{ID: testUUID}, f.writeErr
}
func (f fakeBatchService) List(context.Context, auth.Principal, string, evaluationbatch.Filters) ([]evaluationbatch.Batch, error) {
	return []evaluationbatch.Batch{}, f.readErr
}
func (f fakeBatchService) Get(context.Context, auth.Principal, string, string) (evaluationbatch.Batch, error) {
	return evaluationbatch.Batch{}, f.readErr
}
func (f fakeBatchService) ListRuns(context.Context, auth.Principal, string, string, evaluationbatch.Filters) ([]evaluationbatch.Run, error) {
	return []evaluationbatch.Run{}, f.readErr
}
func (f fakeBatchService) GetRun(context.Context, auth.Principal, string, string) (evaluationbatch.Run, error) {
	return evaluationbatch.Run{}, f.readErr
}
func batchRouter(a fakeAuth, s fakeBatchService) http.Handler {
	l := slog.New(slog.NewTextHandler(io.Discard, nil))
	return NewRouter(healthyDatabase{}, l, WithIdentityService(fakeIdentity{}), WithAuthentication(a, a, false, 168*time.Hour, "http://localhost:3000"), WithEvaluationBatchService(s))
}
func TestEvaluationBatchUnauthenticated(t *testing.T) {
	w := httptest.NewRecorder()
	batchRouter(fakeAuth{authenticateErr: auth.ErrUnauthenticated}, fakeBatchService{}).ServeHTTP(w, httptest.NewRequest("GET", "/v1/workspaces/"+testUUID+"/evaluation-batches", nil))
	if w.Code != 401 {
		t.Fatalf("status=%d", w.Code)
	}
}
func TestEvaluationBatchViewerCanRead(t *testing.T) {
	paths := []string{"/v1/workspaces/" + testUUID + "/evaluation-batches", "/v1/workspaces/" + testUUID + "/evaluation-batches/" + testUUID + "/runs"}
	for _, path := range paths {
		r := httptest.NewRequest("GET", path, nil)
		r.AddCookie(&http.Cookie{Name: auth.CookieName, Value: "token"})
		w := httptest.NewRecorder()
		batchRouter(fakeAuth{allow: true}, fakeBatchService{}).ServeHTTP(w, r)
		if w.Code != 200 {
			t.Fatalf("%s status=%d body=%s", path, w.Code, w.Body.String())
		}
	}
}
func TestEvaluationBatchViewerCannotCreate(t *testing.T) {
	r := batchCreateRequest()
	w := httptest.NewRecorder()
	batchRouter(fakeAuth{allow: true}, fakeBatchService{writeErr: auth.ErrForbidden}).ServeHTTP(w, r)
	if w.Code != 403 {
		t.Fatalf("status=%d body=%s", w.Code, w.Body.String())
	}
}
func TestEvaluationBatchMemberCanCreate(t *testing.T) {
	r := batchCreateRequest()
	w := httptest.NewRecorder()
	batchRouter(fakeAuth{allow: true}, fakeBatchService{}).ServeHTTP(w, r)
	if w.Code != 201 {
		t.Fatalf("status=%d body=%s", w.Code, w.Body.String())
	}
}
func TestEvaluationBatchInvalidIDs(t *testing.T) {
	paths := []string{
		"/v1/workspaces/bad/evaluation-batches",
		"/v1/workspaces/" + testUUID + "/evaluation-batches/bad",
		"/v1/workspaces/" + testUUID + "/evaluation-batches/bad/runs",
		"/v1/workspaces/" + testUUID + "/runs/bad",
	}
	for _, path := range paths {
		r := httptest.NewRequest("GET", path, nil)
		r.AddCookie(&http.Cookie{Name: auth.CookieName, Value: "token"})
		w := httptest.NewRecorder()
		batchRouter(fakeAuth{allow: true}, fakeBatchService{}).ServeHTTP(w, r)
		if w.Code != 400 {
			t.Fatalf("%s status=%d", path, w.Code)
		}
	}
}

func TestEvaluationBatchWorkspaceIsolation(t *testing.T) {
	paths := []string{
		"/v1/workspaces/" + testUUID + "/evaluation-batches/" + testUUID,
		"/v1/workspaces/" + testUUID + "/evaluation-batches/" + testUUID + "/runs",
		"/v1/workspaces/" + testUUID + "/runs/" + testUUID,
	}
	for _, path := range paths {
		r := httptest.NewRequest("GET", path, nil)
		r.AddCookie(&http.Cookie{Name: auth.CookieName, Value: "token"})
		w := httptest.NewRecorder()
		batchRouter(fakeAuth{allow: true}, fakeBatchService{readErr: identity.ErrNotFound}).ServeHTTP(w, r)
		if w.Code != 404 {
			t.Fatalf("%s status=%d", path, w.Code)
		}
	}
}
func batchCreateRequest() *http.Request {
	body := `{"scenario_version_id":"11111111-1111-4111-8111-111111111111","agent_version_ids":["22222222-2222-4222-8222-222222222222"],"runs_per_agent":3,"environment_mode":"isolated","config":{}}`
	r := httptest.NewRequest("POST", "/v1/workspaces/"+testUUID+"/evaluation-batches", strings.NewReader(body))
	r.Header.Set("Content-Type", "application/json")
	r.Header.Set("Origin", "http://localhost:3000")
	r.AddCookie(&http.Cookie{Name: auth.CookieName, Value: "token"})
	return r
}
