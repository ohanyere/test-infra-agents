package httpapi

import (
	"context"
	"errors"
	"github.com/evalplane/evalplane/backend/internal/auth"
	"github.com/evalplane/evalplane/backend/internal/identity"
	"net"
	"net/http"
	"time"
)

type AuthService interface {
	Signup(context.Context, auth.SignupInput) (auth.SignupResult, error)
	Login(context.Context, auth.LoginInput) (auth.LoginResult, error)
	Authenticate(context.Context, string) (auth.Principal, error)
	Logout(context.Context, auth.Principal) error
	Me(context.Context, auth.Principal) (auth.MeResult, error)
}
type authHandlers struct {
	service      AuthService
	cookieSecure bool
	ttl          time.Duration
}
type signupRequest struct {
	FullName     string              `json:"full_name"`
	Email        string              `json:"email"`
	Password     string              `json:"password"`
	Organization organizationRequest `json:"organization"`
	Workspace    workspaceRequest    `json:"workspace"`
}
type loginRequest struct {
	Email    string `json:"email"`
	Password string `json:"password"`
}
type signupResponse struct {
	User         identity.User         `json:"user"`
	Organization identity.Organization `json:"organization"`
	Workspace    workspaceAuthResponse `json:"workspace"`
}
type workspaceAuthResponse struct {
	ID   string `json:"id"`
	Name string `json:"name"`
	Slug string `json:"slug"`
}

func (h authHandlers) signup(w http.ResponseWriter, r *http.Request) {
	var request signupRequest
	if err := decodeJSON(w, r, &request); err != nil {
		writeAPIError(w, http.StatusBadRequest, "validation_error", "request body must contain one valid JSON document with known fields")
		return
	}
	result, err := h.service.Signup(r.Context(), auth.SignupInput{FullName: request.FullName, Email: request.Email, Password: request.Password, Organization: identity.OrganizationInput{Name: request.Organization.Name, Slug: request.Organization.Slug}, Workspace: identity.WorkspaceInput{Name: request.Workspace.Name, Slug: request.Workspace.Slug}, UserAgent: r.UserAgent(), IPAddress: clientIP(r)})
	if err != nil {
		writeAuthError(w, err)
		return
	}
	h.setCookie(w, result.RawToken, result.ExpiresAt)
	writeJSON(w, http.StatusCreated, signupResponse{User: result.User, Organization: result.Organization, Workspace: workspaceAuthResponse{ID: result.Workspace.ID, Name: result.Workspace.Name, Slug: result.Workspace.Slug}})
}
func (h authHandlers) login(w http.ResponseWriter, r *http.Request) {
	var request loginRequest
	if err := decodeJSON(w, r, &request); err != nil {
		writeAPIError(w, http.StatusBadRequest, "validation_error", "request body must contain one valid JSON document with known fields")
		return
	}
	result, err := h.service.Login(r.Context(), auth.LoginInput{Email: request.Email, Password: request.Password, UserAgent: r.UserAgent(), IPAddress: clientIP(r)})
	if err != nil {
		writeAuthError(w, err)
		return
	}
	h.setCookie(w, result.RawToken, result.ExpiresAt)
	writeJSON(w, http.StatusOK, map[string]any{"user": result.User})
}
func (h authHandlers) logout(w http.ResponseWriter, r *http.Request) {
	p, _ := auth.PrincipalFromContext(r.Context())
	if err := h.service.Logout(r.Context(), p); err != nil {
		writeAuthError(w, err)
		return
	}
	http.SetCookie(w, &http.Cookie{Name: auth.CookieName, Value: "", Path: "/", HttpOnly: true, Secure: h.cookieSecure, SameSite: http.SameSiteLaxMode, MaxAge: -1, Expires: time.Unix(1, 0)})
	w.WriteHeader(http.StatusNoContent)
}
func (h authHandlers) me(w http.ResponseWriter, r *http.Request) {
	p, _ := auth.PrincipalFromContext(r.Context())
	result, err := h.service.Me(r.Context(), p)
	if err != nil {
		writeAuthError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, result)
}
func (h authHandlers) setCookie(w http.ResponseWriter, token string, expires time.Time) {
	http.SetCookie(w, &http.Cookie{Name: auth.CookieName, Value: token, Path: "/", HttpOnly: true, Secure: h.cookieSecure, SameSite: http.SameSiteLaxMode, MaxAge: int(h.ttl.Seconds()), Expires: expires})
}
func clientIP(r *http.Request) string {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err == nil {
		return host
	}
	return ""
}
func writeAuthError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, auth.ErrInvalidCredentials):
		writeAPIError(w, http.StatusUnauthorized, "invalid_credentials", "invalid email or password")
	case errors.Is(err, auth.ErrUnauthenticated):
		writeAPIError(w, http.StatusUnauthorized, "unauthenticated", "authentication required")
	case errors.Is(err, auth.ErrForbidden):
		writeAPIError(w, http.StatusForbidden, "forbidden", "you do not have permission to perform this action")
	default:
		writeDomainError(w, err)
	}
}
