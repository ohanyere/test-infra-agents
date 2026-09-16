package httpapi

import (
	"context"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/evalplane/evalplane/backend/internal/auth"
	"github.com/evalplane/evalplane/backend/internal/execution"
	"github.com/evalplane/evalplane/backend/internal/identity"
)

type fakeExecutionService struct {
	status   string
	startErr error
	stopErr  error
}

func (f *fakeExecutionService) StartBatch(context.Context, auth.Principal, string, string) (execution.BatchState, error) {
	if f.startErr != nil {
		return execution.BatchState{}, f.startErr
	}
	if f.status != "draft" {
		return execution.BatchState{}, identity.ErrConflict
	}
	f.status = "queued"
	return execution.BatchState{ID: testUUID, WorkspaceID: testUUID, Status: f.status}, nil
}

func (f *fakeExecutionService) StopBatch(context.Context, auth.Principal, string, string) (execution.BatchState, error) {
	if f.stopErr != nil {
		return execution.BatchState{}, f.stopErr
	}
	if f.status == "running" {
		return execution.BatchState{}, identity.ErrConflict
	}
	f.status = "stopped"
	return execution.BatchState{ID: testUUID, WorkspaceID: testUUID, Status: f.status}, nil
}

func executionRouter(authentication fakeAuth, service ExecutionService) http.Handler {
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	return NewRouter(healthyDatabase{}, logger, WithIdentityService(fakeIdentity{}), WithAuthentication(authentication, authentication, false, 168*time.Hour, "http://localhost:3000"), WithExecutionService(service))
}

func TestExecutionStartBatchUnauthenticated(t *testing.T) {
	w := httptest.NewRecorder()
	executionRouter(fakeAuth{authenticateErr: auth.ErrUnauthenticated}, &fakeExecutionService{status: "draft"}).ServeHTTP(w, executionRequest("start"))
	if w.Code != http.StatusUnauthorized {
		t.Fatalf("status=%d body=%s", w.Code, w.Body.String())
	}
}

func TestExecutionViewerCannotStartBatch(t *testing.T) {
	w := httptest.NewRecorder()
	executionRouter(fakeAuth{allow: true}, &fakeExecutionService{status: "draft", startErr: auth.ErrForbidden}).ServeHTTP(w, executionRequest("start"))
	if w.Code != http.StatusForbidden {
		t.Fatalf("status=%d body=%s", w.Code, w.Body.String())
	}
}

func TestExecutionMemberStartsDraftBatchOnce(t *testing.T) {
	service := &fakeExecutionService{status: "draft"}
	router := executionRouter(fakeAuth{allow: true}, service)
	first := httptest.NewRecorder()
	router.ServeHTTP(first, executionRequest("start"))
	if first.Code != http.StatusOK || service.status != "queued" {
		t.Fatalf("first status=%d batch=%s body=%s", first.Code, service.status, first.Body.String())
	}
	second := httptest.NewRecorder()
	router.ServeHTTP(second, executionRequest("start"))
	if second.Code != http.StatusConflict {
		t.Fatalf("second status=%d body=%s", second.Code, second.Body.String())
	}
}

func TestExecutionBatchWorkspaceIsolation(t *testing.T) {
	w := httptest.NewRecorder()
	executionRouter(fakeAuth{allow: true}, &fakeExecutionService{status: "draft", startErr: identity.ErrNotFound}).ServeHTTP(w, executionRequest("start"))
	if w.Code != http.StatusNotFound {
		t.Fatalf("status=%d body=%s", w.Code, w.Body.String())
	}
}

func TestExecutionStopsDraftOrQueuedBatch(t *testing.T) {
	for _, status := range []string{"draft", "queued"} {
		service := &fakeExecutionService{status: status}
		w := httptest.NewRecorder()
		executionRouter(fakeAuth{allow: true}, service).ServeHTTP(w, executionRequest("stop"))
		if w.Code != http.StatusOK || service.status != "stopped" {
			t.Fatalf("from=%s status=%d batch=%s body=%s", status, w.Code, service.status, w.Body.String())
		}
	}
}

func TestExecutionCannotStopRunningBatch(t *testing.T) {
	w := httptest.NewRecorder()
	executionRouter(fakeAuth{allow: true}, &fakeExecutionService{status: "running"}).ServeHTTP(w, executionRequest("stop"))
	if w.Code != http.StatusConflict {
		t.Fatalf("status=%d body=%s", w.Code, w.Body.String())
	}
}

func executionRequest(action string) *http.Request {
	r := httptest.NewRequest(http.MethodPost, "/v1/workspaces/"+testUUID+"/evaluation-batches/"+testUUID+"/"+action, nil)
	r.Header.Set("Content-Type", "application/json")
	r.Header.Set("Origin", "http://localhost:3000")
	r.AddCookie(&http.Cookie{Name: auth.CookieName, Value: "token"})
	return r
}
