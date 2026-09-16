package auth

import (
	"context"
	"errors"
	"fmt"
	"github.com/evalplane/evalplane/backend/internal/identity"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"strings"
	"time"
)

type Service struct {
	pool           *pgxpool.Pool
	repo           *Repository
	identityRepo   *identity.Repository
	ttl            time.Duration
	passwordParams PasswordParams
	dummyHash      string
}

func NewService(pool *pgxpool.Pool, repo *Repository, identityRepo *identity.Repository, ttl time.Duration, params PasswordParams) (*Service, error) {
	dummy, err := HashPassword("dummy-password-never-used", params)
	if err != nil {
		return nil, err
	}
	return &Service{pool: pool, repo: repo, identityRepo: identityRepo, ttl: ttl, passwordParams: params, dummyHash: dummy}, nil
}
func (s *Service) Signup(ctx context.Context, in SignupInput) (out SignupResult, err error) {
	in.FullName = strings.TrimSpace(in.FullName)
	in.Email = strings.ToLower(strings.TrimSpace(in.Email))
	userInput, err := identity.ValidateUser(identity.UserInput{Email: in.Email, FullName: in.FullName})
	if err != nil {
		return out, err
	}
	in.Email = userInput.Email
	in.FullName = userInput.FullName
	if in.Organization.Slug == "" {
		in.Organization.Slug, err = identity.GenerateSlug(in.Organization.Name)
	}
	if err == nil {
		in.Organization, err = identity.ValidateOrganization(in.Organization)
	}
	if err != nil {
		return out, err
	}
	if in.Workspace.Slug == "" {
		in.Workspace.Slug, err = identity.GenerateSlug(in.Workspace.Name)
	}
	if err == nil {
		in.Workspace, err = identity.ValidateWorkspace(in.Workspace)
	}
	if err != nil {
		return out, err
	}
	if err = ValidatePassword(in.Password); err != nil {
		return out, fmt.Errorf("%w: %s", identity.ErrValidation, err)
	}
	passwordHash, err := HashPassword(in.Password, s.passwordParams)
	if err != nil {
		return out, err
	}
	raw, digest, err := GenerateSessionToken()
	if err != nil {
		return out, err
	}
	expires := time.Now().Add(s.ttl)
	tx, err := s.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return out, err
	}
	defer func() {
		if err != nil {
			_ = tx.Rollback(ctx)
		}
	}()
	out.Organization, err = s.identityRepo.CreateOrganization(ctx, tx, in.Organization)
	if err != nil {
		return out, err
	}
	out.Workspace, err = s.identityRepo.CreateWorkspace(ctx, tx, out.Organization.ID, in.Workspace)
	if err != nil {
		return out, err
	}
	out.User, err = s.identityRepo.CreateUser(ctx, tx, userInput)
	if err != nil {
		return out, err
	}
	if err = s.repo.CreateCredential(ctx, tx, out.User.ID, passwordHash); err != nil {
		return out, err
	}
	if _, err = s.identityRepo.CreateMembership(ctx, tx, out.Workspace.ID, out.User.ID, "owner"); err != nil {
		return out, err
	}
	if _, err = s.repo.CreateSession(ctx, tx, out.User.ID, digest, expires, in.UserAgent, in.IPAddress); err != nil {
		return out, err
	}
	if err = tx.Commit(ctx); err != nil {
		return out, err
	}
	out.RawToken = raw
	out.ExpiresAt = expires
	return out, nil
}
func (s *Service) Login(ctx context.Context, in LoginInput) (LoginResult, error) {
	in.Email = strings.ToLower(strings.TrimSpace(in.Email))
	u, hash, err := s.repo.GetCredentialByEmail(ctx, s.pool, in.Email)
	if err != nil {
		_, _ = VerifyPassword(in.Password, s.dummyHash)
		if errors.Is(err, identity.ErrNotFound) {
			return LoginResult{}, ErrInvalidCredentials
		}
		return LoginResult{}, err
	}
	valid, verifyErr := VerifyPassword(in.Password, hash)
	if verifyErr != nil || !valid {
		return LoginResult{}, ErrInvalidCredentials
	}
	raw, digest, err := GenerateSessionToken()
	if err != nil {
		return LoginResult{}, err
	}
	expires := time.Now().Add(s.ttl)
	if _, err = s.repo.CreateSession(ctx, s.pool, u.ID, digest, expires, in.UserAgent, in.IPAddress); err != nil {
		return LoginResult{}, err
	}
	return LoginResult{User: u, RawToken: raw, ExpiresAt: expires}, nil
}
func (s *Service) Authenticate(ctx context.Context, raw string) (Principal, error) {
	if raw == "" {
		return Principal{}, ErrUnauthenticated
	}
	session, err := s.repo.GetActiveSession(ctx, s.pool, TokenDigest(raw))
	if err != nil {
		if errors.Is(err, identity.ErrNotFound) {
			return Principal{}, ErrUnauthenticated
		}
		return Principal{}, err
	}
	if _, err := s.identityRepo.GetUser(ctx, s.pool, session.UserID); err != nil {
		if errors.Is(err, identity.ErrNotFound) {
			return Principal{}, ErrUnauthenticated
		}
		return Principal{}, err
	}
	_ = s.repo.TouchSession(ctx, s.pool, session.ID)
	return Principal{UserID: session.UserID, SessionID: session.ID}, nil
}
func (s *Service) Logout(ctx context.Context, p Principal) error {
	return s.repo.RevokeSession(ctx, s.pool, p.SessionID)
}
func (s *Service) Me(ctx context.Context, p Principal) (MeResult, error) {
	u, err := s.identityRepo.GetUser(ctx, s.pool, p.UserID)
	if err != nil {
		return MeResult{}, err
	}
	workspaces, err := s.repo.ListWorkspaceAccess(ctx, s.pool, p.UserID)
	return MeResult{User: u, Workspaces: workspaces}, err
}
func (s *Service) CleanupSessions(ctx context.Context) (int64, error) {
	return s.repo.DeleteExpiredSessions(ctx, s.pool)
}
func (s *Service) CanViewWorkspace(ctx context.Context, p Principal, workspaceID string) error {
	_, err := s.repo.GetRole(ctx, s.pool, workspaceID, p.UserID)
	if errors.Is(err, identity.ErrNotFound) {
		return ErrForbidden
	}
	return err
}
func (s *Service) UpdateWorkspaceName(ctx context.Context, p Principal, workspaceID, name string) (result identity.Workspace, err error) {
	name = strings.TrimSpace(name)
	if name == "" || len(name) > 200 {
		return result, fmt.Errorf("%w: workspace name is required and must be at most 200 characters", identity.ErrValidation)
	}
	tx, err := s.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return result, err
	}
	defer func() {
		if err != nil {
			_ = tx.Rollback(ctx)
		}
	}()
	role, err := s.repo.GetRole(ctx, tx, workspaceID, p.UserID)
	if errors.Is(err, identity.ErrNotFound) {
		return result, ErrForbidden
	}
	if err != nil {
		return result, err
	}
	if role != "owner" && role != "admin" {
		return result, ErrForbidden
	}
	result, err = s.identityRepo.UpdateWorkspaceName(ctx, tx, workspaceID, name)
	if err != nil {
		return result, err
	}
	err = tx.Commit(ctx)
	return result, err
}
func (s *Service) CanViewOrganization(ctx context.Context, p Principal, organizationID string) error {
	return s.repo.OrganizationAccessible(ctx, s.pool, organizationID, p.UserID)
}
func (s *Service) ListOrganizations(ctx context.Context, p Principal, page identity.Pagination) ([]identity.Organization, error) {
	return s.repo.ListOrganizationsForUser(ctx, s.pool, p.UserID, page)
}
func (s *Service) ListWorkspaces(ctx context.Context, p Principal, organizationID string, page identity.Pagination) ([]identity.Workspace, error) {
	if err := s.CanViewOrganization(ctx, p, organizationID); err != nil {
		return nil, err
	}
	return s.repo.ListWorkspacesForUser(ctx, s.pool, organizationID, p.UserID, page)
}
func (s *Service) AddMember(ctx context.Context, p Principal, workspaceID, userID, role string) (identity.Membership, error) {
	if err := identity.ValidateRole(role); err != nil {
		return identity.Membership{}, err
	}
	actor, err := s.repo.GetRole(ctx, s.pool, workspaceID, p.UserID)
	if err != nil {
		return identity.Membership{}, ErrForbidden
	}
	if actor != "owner" && actor != "admin" {
		return identity.Membership{}, ErrForbidden
	}
	if role == "owner" && actor != "owner" {
		return identity.Membership{}, ErrForbidden
	}
	return s.identityRepo.CreateMembership(ctx, s.pool, workspaceID, userID, role)
}
func (s *Service) ChangeMemberRole(ctx context.Context, p Principal, workspaceID, userID, newRole string) (result identity.Membership, err error) {
	if err = identity.ValidateRole(newRole); err != nil {
		return result, err
	}
	tx, err := s.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return result, err
	}
	defer func() {
		if err != nil {
			_ = tx.Rollback(ctx)
		}
	}()
	actorRole, targetRole, ownerCount, err := lockRoles(ctx, tx, workspaceID, p.UserID, userID)
	if err != nil {
		return result, err
	}
	if actorRole != "owner" && actorRole != "admin" {
		return result, ErrForbidden
	}
	if actorRole == "admin" && (targetRole == "owner" || newRole == "owner") {
		return result, ErrForbidden
	}
	if targetRole == "owner" && newRole != "owner" && ownerCount <= 1 {
		return result, identity.ErrConflict
	}
	result, err = s.identityRepo.UpdateMembership(ctx, tx, workspaceID, userID, newRole)
	if err != nil {
		return result, err
	}
	err = tx.Commit(ctx)
	return result, err
}
func (s *Service) RemoveMember(ctx context.Context, p Principal, workspaceID, userID string) (err error) {
	tx, err := s.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return err
	}
	defer func() {
		if err != nil {
			_ = tx.Rollback(ctx)
		}
	}()
	actorRole, targetRole, ownerCount, err := lockRoles(ctx, tx, workspaceID, p.UserID, userID)
	if err != nil {
		return err
	}
	if actorRole != "owner" && actorRole != "admin" {
		return ErrForbidden
	}
	if actorRole == "admin" && targetRole == "owner" {
		return ErrForbidden
	}
	if targetRole == "owner" && ownerCount <= 1 {
		return identity.ErrConflict
	}
	if err = s.identityRepo.DeleteMembership(ctx, tx, workspaceID, userID); err != nil {
		return err
	}
	return tx.Commit(ctx)
}
func lockRoles(ctx context.Context, tx pgx.Tx, workspaceID, actorID, targetID string) (actorRole, targetRole string, ownerCount int, err error) {
	rows, err := tx.Query(ctx, `SELECT user_id,role FROM workspace_members WHERE workspace_id=$1 ORDER BY user_id FOR UPDATE`, workspaceID)
	if err != nil {
		return "", "", 0, err
	}
	defer rows.Close()
	for rows.Next() {
		var id, role string
		if err = rows.Scan(&id, &role); err != nil {
			return
		}
		if id == actorID {
			actorRole = role
		}
		if id == targetID {
			targetRole = role
		}
		if role == "owner" {
			ownerCount++
		}
	}
	if err = rows.Err(); err != nil {
		return
	}
	if actorRole == "" {
		err = ErrForbidden
	} else if targetRole == "" {
		err = identity.ErrNotFound
	}
	return
}
