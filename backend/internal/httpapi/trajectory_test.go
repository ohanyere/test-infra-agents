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
	"github.com/evalplane/evalplane/backend/internal/identity"
	"github.com/evalplane/evalplane/backend/internal/trajectory"
)

type fakeTrajectoryService struct {
	readErr    error
	trajectory trajectory.Trajectory
	evidence   trajectory.EvidenceDetail
	suite      trajectory.SuiteTrajectorySummary
	compare    trajectory.ComparisonSummary
}

func (f fakeTrajectoryService) GetTrajectory(context.Context, auth.Principal, string, string, trajectory.Filters) (trajectory.Trajectory, error) {
	out := f.trajectory
	if out.Contract == "" {
		out.Contract = trajectory.ContractVersion
	}
	return out, f.readErr
}

func (f fakeTrajectoryService) GetEvidence(context.Context, auth.Principal, string, string, string) (trajectory.EvidenceDetail, error) {
	return f.evidence, f.readErr
}

func (f fakeTrajectoryService) SuiteSummary(context.Context, auth.Principal, string, string) (trajectory.SuiteTrajectorySummary, error) {
	out := f.suite
	if out.Contract == "" {
		out.Contract = trajectory.ContractVersion
	}
	return out, f.readErr
}

func (f fakeTrajectoryService) Compare(context.Context, auth.Principal, string, string, string) (trajectory.ComparisonSummary, error) {
	out := f.compare
	if out.Contract == "" {
		out.Contract = trajectory.ContractVersion
	}
	return out, f.readErr
}

func trajectoryRouter(authentication fakeAuth, service fakeTrajectoryService) http.Handler {
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	return NewRouter(healthyDatabase{}, logger, WithIdentityService(fakeIdentity{}), WithAuthentication(authentication, authentication, false, 168*time.Hour, "http://localhost:3000"), WithTrajectoryService(service))
}

func trajectoryPath(suffix string) string {
	return "/v1/workspaces/" + testUUID + "/runs/" + testUUID + "/" + suffix
}

func authenticatedTrajectoryRequest(path string) *http.Request {
	r := httptest.NewRequest(http.MethodGet, path, nil)
	r.AddCookie(&http.Cookie{Name: auth.CookieName, Value: "token"})
	return r
}

func TestTrajectoryReadRequiresAuthentication(t *testing.T) {
	w := httptest.NewRecorder()
	trajectoryRouter(fakeAuth{authenticateErr: auth.ErrUnauthenticated}, fakeTrajectoryService{}).ServeHTTP(w, httptest.NewRequest(http.MethodGet, trajectoryPath("trajectory"), nil))
	if w.Code != http.StatusUnauthorized {
		t.Fatalf("status=%d body=%s", w.Code, w.Body.String())
	}
}

func TestTrajectoryViewerCanRead(t *testing.T) {
	paths := []string{trajectoryPath("trajectory"), trajectoryPath("evidence/some-evidence"), "/v1/workspaces/" + testUUID + "/suite-executions/" + testUUID + "/trajectory-summary"}
	for _, path := range paths {
		r := authenticatedTrajectoryRequest(path)
		w := httptest.NewRecorder()
		trajectoryRouter(fakeAuth{allow: true}, fakeTrajectoryService{}).ServeHTTP(w, r)
		if w.Code != http.StatusOK {
			t.Fatalf("%s status=%d body=%s", path, w.Code, w.Body.String())
		}
	}
}

func TestTrajectoryContractPinnedInAPI(t *testing.T) {
	r := authenticatedTrajectoryRequest(trajectoryPath("trajectory"))
	w := httptest.NewRecorder()
	trajectoryRouter(fakeAuth{allow: true}, fakeTrajectoryService{}).ServeHTTP(w, r)
	if w.Code != http.StatusOK {
		t.Fatalf("status=%d", w.Code)
	}
	var got trajectory.Trajectory
	if err := json.Unmarshal(w.Body.Bytes(), &got); err != nil || got.Contract != trajectory.ContractVersion {
		t.Fatalf("contract=%+v err=%v", got, err)
	}
}

func TestTrajectoryCrossWorkspaceDenied(t *testing.T) {
	r := authenticatedTrajectoryRequest(trajectoryPath("trajectory"))
	w := httptest.NewRecorder()
	trajectoryRouter(fakeAuth{allow: true}, fakeTrajectoryService{readErr: identity.ErrNotFound}).ServeHTTP(w, r)
	if w.Code != http.StatusNotFound {
		t.Fatalf("status=%d body=%s", w.Code, w.Body.String())
	}
}

func TestTrajectoryFilterValidation(t *testing.T) {
	for _, suffix := range []string{"trajectory?phase=bogus", "trajectory?event_type=bogus", "trajectory?actor=bogus", "trajectory?status=bogus"} {
		r := authenticatedTrajectoryRequest(trajectoryPath(suffix))
		w := httptest.NewRecorder()
		trajectoryRouter(fakeAuth{allow: true}, fakeTrajectoryService{}).ServeHTTP(w, r)
		if w.Code != http.StatusBadRequest {
			t.Fatalf("%s status=%d body=%s", suffix, w.Code, w.Body.String())
		}
	}
}

func TestTrajectoryPaginationStable(t *testing.T) {
	for _, suffix := range []string{"trajectory?limit=0", "trajectory?limit=101", "trajectory?offset=-1"} {
		r := authenticatedTrajectoryRequest(trajectoryPath(suffix))
		w := httptest.NewRecorder()
		trajectoryRouter(fakeAuth{allow: true}, fakeTrajectoryService{}).ServeHTTP(w, r)
		if w.Code != http.StatusBadRequest {
			t.Fatalf("%s status=%d body=%s", suffix, w.Code, w.Body.String())
		}
	}
}

func TestTrajectoryCompareValidation(t *testing.T) {
	r := authenticatedTrajectoryRequest(trajectoryPath("trajectory") + "?compare_with=not-a-uuid")
	w := httptest.NewRecorder()
	trajectoryRouter(fakeAuth{allow: true}, fakeTrajectoryService{}).ServeHTTP(w, r)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("status=%d body=%s", w.Code, w.Body.String())
	}
}
