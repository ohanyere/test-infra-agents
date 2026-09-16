package httpapi

import (
	"context"
	"errors"
	"github.com/evalplane/evalplane/backend/internal/auth"
	"github.com/evalplane/evalplane/backend/internal/identity"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

const testUUID = "11111111-1111-4111-8111-111111111111"

type fakeIdentity struct{}

func (fakeIdentity) GetOrganization(context.Context, string) (identity.Organization, error) {
	return identity.Organization{ID: testUUID, Name: "Acme", Slug: "acme"}, nil
}
func (fakeIdentity) GetWorkspace(context.Context, string) (identity.Workspace, error) {
	return identity.Workspace{ID: testUUID, Name: "Infra", Slug: "infra"}, nil
}
func (fakeIdentity) ListMembers(context.Context, string, identity.Pagination) ([]identity.Membership, error) {
	return []identity.Membership{}, nil
}

type fakeAuth struct {
	authenticateErr error
	signupFn        func(auth.SignupInput) (auth.SignupResult, error)
	loginFn         func(auth.LoginInput) (auth.LoginResult, error)
	allow           bool
}

func (f fakeAuth) Signup(_ context.Context, in auth.SignupInput) (auth.SignupResult, error) {
	return f.signupFn(in)
}
func (f fakeAuth) Login(_ context.Context, in auth.LoginInput) (auth.LoginResult, error) {
	return f.loginFn(in)
}
func (f fakeAuth) Authenticate(context.Context, string) (auth.Principal, error) {
	if f.authenticateErr != nil {
		return auth.Principal{}, f.authenticateErr
	}
	return auth.Principal{UserID: testUUID, SessionID: testUUID}, nil
}
func (fakeAuth) Logout(context.Context, auth.Principal) error { return nil }
func (fakeAuth) Me(context.Context, auth.Principal) (auth.MeResult, error) {
	return auth.MeResult{}, nil
}
func (f fakeAuth) CanViewWorkspace(context.Context, auth.Principal, string) error {
	if !f.allow {
		return auth.ErrForbidden
	}
	return nil
}
func (f fakeAuth) CanViewOrganization(context.Context, auth.Principal, string) error {
	if !f.allow {
		return auth.ErrForbidden
	}
	return nil
}
func (fakeAuth) ListOrganizations(context.Context, auth.Principal, identity.Pagination) ([]identity.Organization, error) {
	return []identity.Organization{}, nil
}
func (fakeAuth) ListWorkspaces(context.Context, auth.Principal, string, identity.Pagination) ([]identity.Workspace, error) {
	return []identity.Workspace{}, nil
}
func (f fakeAuth) AddMember(context.Context, auth.Principal, string, string, string) (identity.Membership, error) {
	if !f.allow {
		return identity.Membership{}, auth.ErrForbidden
	}
	return identity.Membership{}, nil
}
func (fakeAuth) ChangeMemberRole(context.Context, auth.Principal, string, string, string) (identity.Membership, error) {
	return identity.Membership{}, nil
}
func (fakeAuth) RemoveMember(context.Context, auth.Principal, string, string) error { return nil }
func (f fakeAuth) UpdateWorkspaceName(context.Context, auth.Principal, string, string) (identity.Workspace, error) {
	if !f.allow {
		return identity.Workspace{}, auth.ErrForbidden
	}
	return identity.Workspace{ID: testUUID, Name: "Platform Engineering"}, nil
}

type healthyDatabase struct{}

func (healthyDatabase) Ping(context.Context) error { return nil }
func testRouter(authentication fakeAuth, cookieSecure bool) http.Handler {
	return NewRouter(healthyDatabase{}, slog.New(slog.NewTextHandler(io.Discard, nil)), WithIdentityService(fakeIdentity{}), WithAuthentication(authentication, authentication, cookieSecure, 168*time.Hour, "http://localhost:3000"))
}
func TestSignupMalformedJSON(t *testing.T) {
	f := fakeAuth{signupFn: func(auth.SignupInput) (auth.SignupResult, error) { t.Fatal("called"); return auth.SignupResult{}, nil }}
	r := httptest.NewRequest(http.MethodPost, "/v1/auth/signup", strings.NewReader(`{"email":`))
	r.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	testRouter(f, false).ServeHTTP(w, r)
	assertError(t, w, 400, "validation_error")
}
func TestSignupCookieFlags(t *testing.T) {
	expires := time.Now().Add(168 * time.Hour)
	f := fakeAuth{signupFn: func(auth.SignupInput) (auth.SignupResult, error) {
		return auth.SignupResult{RawToken: "raw-token", ExpiresAt: expires}, nil
	}}
	r := httptest.NewRequest(http.MethodPost, "/v1/auth/signup", strings.NewReader(`{"full_name":"Francis","email":"f@example.com","password":"long-password","organization":{"name":"Acme","slug":"acme"},"workspace":{"name":"Infra","slug":"infra"}}`))
	r.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	testRouter(f, true).ServeHTTP(w, r)
	if w.Code != 201 {
		t.Fatalf("status=%d body=%s", w.Code, w.Body.String())
	}
	cookies := w.Result().Cookies()
	if len(cookies) != 1 {
		t.Fatal("missing cookie")
	}
	c := cookies[0]
	if !c.HttpOnly || !c.Secure || c.SameSite != http.SameSiteLaxMode || c.Path != "/" || c.MaxAge <= 0 {
		t.Fatalf("cookie flags: %+v", c)
	}
}
func TestLoginUsesSameInvalidCredentialsError(t *testing.T) {
	for _, err := range []error{auth.ErrInvalidCredentials, fmtDatabaseError{}} {
		f := fakeAuth{loginFn: func(auth.LoginInput) (auth.LoginResult, error) { return auth.LoginResult{}, err }}
		r := httptest.NewRequest(http.MethodPost, "/v1/auth/login", strings.NewReader(`{"email":"x@example.com","password":"bad"}`))
		r.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		testRouter(f, false).ServeHTTP(w, r)
		if errors.Is(err, auth.ErrInvalidCredentials) {
			assertError(t, w, 401, "invalid_credentials")
		} else {
			assertError(t, w, 500, "internal_error")
		}
	}
}

type fmtDatabaseError struct{}

func (fmtDatabaseError) Error() string { return "secret constraint data" }
func TestProtectedRouteRequiresSession(t *testing.T) {
	w := httptest.NewRecorder()
	testRouter(fakeAuth{authenticateErr: auth.ErrUnauthenticated}, false).ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/v1/workspaces/"+testUUID, nil))
	assertError(t, w, 401, "unauthenticated")
}
func TestUnrelatedWorkspaceForbidden(t *testing.T) {
	r := httptest.NewRequest(http.MethodGet, "/v1/workspaces/"+testUUID, nil)
	r.AddCookie(&http.Cookie{Name: auth.CookieName, Value: "token"})
	w := httptest.NewRecorder()
	testRouter(fakeAuth{}, false).ServeHTTP(w, r)
	assertError(t, w, 403, "forbidden")
}

func TestUpdateWorkspaceRejectsInvalidUUID(t *testing.T) {
	r := httptest.NewRequest(http.MethodPatch, "/v1/workspaces/not-a-uuid", strings.NewReader(`{"name":"Platform Engineering"}`))
	r.Header.Set("Content-Type", "application/json")
	r.Header.Set("Origin", "http://localhost:3000")
	r.AddCookie(&http.Cookie{Name: auth.CookieName, Value: "token"})
	w := httptest.NewRecorder()
	testRouter(fakeAuth{allow: true}, false).ServeHTTP(w, r)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("status=%d body=%s", w.Code, w.Body.String())
	}
}

func TestUpdateWorkspaceReturnsUpdatedWorkspace(t *testing.T) {
	r := httptest.NewRequest(http.MethodPatch, "/v1/workspaces/"+testUUID, strings.NewReader(`{"name":"Platform Engineering"}`))
	r.Header.Set("Content-Type", "application/json")
	r.Header.Set("Origin", "http://localhost:3000")
	r.AddCookie(&http.Cookie{Name: auth.CookieName, Value: "token"})
	w := httptest.NewRecorder()
	testRouter(fakeAuth{allow: true}, false).ServeHTTP(w, r)
	if w.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", w.Code, w.Body.String())
	}
	if !strings.Contains(w.Body.String(), `"name":"Platform Engineering"`) {
		t.Fatalf("body=%s", w.Body.String())
	}
}
func TestUnauthorizedMemberMutationIsForbidden(t *testing.T) {
	r := httptest.NewRequest(http.MethodPost, "/v1/workspaces/"+testUUID+"/members", strings.NewReader(`{"user_id":"`+testUUID+`","role":"member"}`))
	r.AddCookie(&http.Cookie{Name: auth.CookieName, Value: "token"})
	r.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	testRouter(fakeAuth{}, false).ServeHTTP(w, r)
	assertError(t, w, http.StatusForbidden, "forbidden")
}
func TestOriginRejectedForAuthenticatedMutation(t *testing.T) {
	r := httptest.NewRequest(http.MethodPost, "/v1/auth/logout", nil)
	r.AddCookie(&http.Cookie{Name: auth.CookieName, Value: "token"})
	r.Header.Set("Content-Type", "application/json")
	r.Header.Set("Origin", "https://evil.example")
	w := httptest.NewRecorder()
	testRouter(fakeAuth{allow: true}, false).ServeHTTP(w, r)
	assertError(t, w, 403, "forbidden")
}
func TestLogoutClearsCookie(t *testing.T) {
	r := httptest.NewRequest(http.MethodPost, "/v1/auth/logout", nil)
	r.AddCookie(&http.Cookie{Name: auth.CookieName, Value: "token"})
	r.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	testRouter(fakeAuth{allow: true}, false).ServeHTTP(w, r)
	if w.Code != 204 {
		t.Fatalf("status=%d", w.Code)
	}
	c := w.Result().Cookies()[0]
	if c.MaxAge != -1 || c.Value != "" || !c.HttpOnly || c.Path != "/" {
		t.Fatalf("cookie not cleared: %+v", c)
	}
}
func TestBootstrapAndDirectCreationRemoved(t *testing.T) {
	for _, path := range []string{"/v1/bootstrap", "/v1/users", "/v1/organizations"} {
		r := httptest.NewRequest(http.MethodPost, path, strings.NewReader(`{}`))
		r.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		testRouter(fakeAuth{}, false).ServeHTTP(w, r)
		if w.Code != http.StatusMethodNotAllowed && w.Code != http.StatusNotFound {
			t.Fatalf("path=%s status=%d", path, w.Code)
		}
	}
}
func TestCORSAllowedOrigin(t *testing.T) {
	r := httptest.NewRequest(http.MethodGet, "/healthz", nil)
	r.Header.Set("Origin", "http://localhost:3000")
	w := httptest.NewRecorder()
	testRouter(fakeAuth{}, false).ServeHTTP(w, r)
	if w.Header().Get("Access-Control-Allow-Origin") != "http://localhost:3000" || w.Header().Get("Access-Control-Allow-Credentials") != "true" || !strings.Contains(w.Header().Get("Vary"), "Origin") {
		t.Fatalf("CORS headers: %v", w.Header())
	}
	if w.Header().Get("Access-Control-Allow-Origin") == "*" {
		t.Fatal("wildcard origin returned")
	}
}
func TestCORSPreflight(t *testing.T) {
	r := httptest.NewRequest(http.MethodOptions, "/v1/auth/login", nil)
	r.Header.Set("Origin", "http://localhost:3000")
	r.Header.Set("Access-Control-Request-Method", "POST")
	r.Header.Set("Access-Control-Request-Headers", "content-type")
	w := httptest.NewRecorder()
	testRouter(fakeAuth{}, false).ServeHTTP(w, r)
	if w.Code != http.StatusNoContent || !strings.Contains(w.Header().Get("Access-Control-Allow-Methods"), "POST") || !strings.Contains(w.Header().Get("Access-Control-Allow-Headers"), "Content-Type") {
		t.Fatalf("status=%d headers=%v", w.Code, w.Header())
	}
}
func TestCORSDisallowedOrigin(t *testing.T) {
	r := httptest.NewRequest(http.MethodGet, "/healthz", nil)
	r.Header.Set("Origin", "https://evil.example")
	w := httptest.NewRecorder()
	testRouter(fakeAuth{}, false).ServeHTTP(w, r)
	if w.Header().Get("Access-Control-Allow-Origin") != "" || w.Header().Get("Access-Control-Allow-Credentials") != "" {
		t.Fatalf("disallowed headers=%v", w.Header())
	}
}
func assertError(t *testing.T, w *httptest.ResponseRecorder, status int, code string) {
	t.Helper()
	if w.Code != status || !strings.Contains(w.Body.String(), `"code":"`+code+`"`) {
		t.Fatalf("status=%d body=%s", w.Code, w.Body.String())
	}
	if w.Header().Get("Content-Type") != "application/json" {
		t.Fatal("missing JSON content type")
	}
	if strings.Contains(w.Body.String(), "constraint") {
		t.Fatal("internal error leaked")
	}
}
