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
	"github.com/evalplane/evalplane/backend/internal/identity"
	"github.com/evalplane/evalplane/backend/internal/verification"
)

type fakeVerificationService struct {
	result verification.Result
	err    error
}

func (f fakeVerificationService) Get(context.Context, auth.Principal, string, string) (verification.Result, error) {
	return f.result, f.err
}

func verificationRouter(authentication fakeAuth, service fakeVerificationService) http.Handler {
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	return NewRouter(healthyDatabase{}, logger, WithIdentityService(fakeIdentity{}), WithAuthentication(authentication, authentication, false, 168*time.Hour, "http://localhost:3000"), WithVerificationService(service))
}

func TestVerificationWorkspaceIsolation(t *testing.T) {
	request := authenticatedEvidenceRequest(evidencePath("verification"))
	response := httptest.NewRecorder()
	verificationRouter(fakeAuth{allow: true}, fakeVerificationService{err: identity.ErrNotFound}).ServeHTTP(response, request)
	if response.Code != http.StatusNotFound {
		t.Fatalf("status=%d body=%s", response.Code, response.Body.String())
	}
}

func TestVerificationRequiresAuthentication(t *testing.T) {
	request := httptest.NewRequest(http.MethodGet, evidencePath("verification"), nil)
	response := httptest.NewRecorder()
	verificationRouter(fakeAuth{authenticateErr: auth.ErrUnauthenticated}, fakeVerificationService{}).ServeHTTP(response, request)
	if response.Code != http.StatusUnauthorized {
		t.Fatalf("status=%d body=%s", response.Code, response.Body.String())
	}
}
