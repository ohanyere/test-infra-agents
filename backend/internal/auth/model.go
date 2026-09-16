package auth

import (
	"context"
	"errors"
	"time"

	"github.com/evalplane/evalplane/backend/internal/identity"
)

var (
	ErrUnauthenticated    = errors.New("unauthenticated")
	ErrInvalidCredentials = errors.New("invalid credentials")
	ErrForbidden          = errors.New("forbidden")
)

const CookieName = "evalplane_session"

type Principal struct {
	UserID    string
	SessionID string
}
type principalKey struct{}

func ContextWithPrincipal(ctx context.Context, p Principal) context.Context {
	return context.WithValue(ctx, principalKey{}, p)
}
func PrincipalFromContext(ctx context.Context) (Principal, bool) {
	p, ok := ctx.Value(principalKey{}).(Principal)
	return p, ok
}

type SignupInput struct {
	FullName, Email, Password string
	Organization              identity.OrganizationInput
	Workspace                 identity.WorkspaceInput
	UserAgent, IPAddress      string
}
type SignupResult struct {
	User         identity.User         `json:"user"`
	Organization identity.Organization `json:"organization"`
	Workspace    identity.Workspace    `json:"workspace"`
	RawToken     string                `json:"-"`
	ExpiresAt    time.Time             `json:"-"`
}
type LoginInput struct{ Email, Password, UserAgent, IPAddress string }
type LoginResult struct {
	User      identity.User `json:"user"`
	RawToken  string        `json:"-"`
	ExpiresAt time.Time     `json:"-"`
}
type Session struct {
	ID, UserID                       string
	TokenHash                        []byte
	CreatedAt, ExpiresAt, LastSeenAt time.Time
	RevokedAt                        *time.Time
}
type WorkspaceAccess struct {
	ID           string                       `json:"id"`
	Name         string                       `json:"name"`
	Slug         string                       `json:"slug"`
	Organization identity.OrganizationSummary `json:"organization"`
	Role         string                       `json:"role"`
}
type MeResult struct {
	User       identity.User     `json:"user"`
	Workspaces []WorkspaceAccess `json:"workspaces"`
}
