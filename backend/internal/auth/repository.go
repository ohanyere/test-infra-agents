package auth

import (
	"context"
	"errors"
	"fmt"
	"github.com/evalplane/evalplane/backend/internal/identity"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"time"
)

type Repository struct{}

func NewRepository() *Repository { return &Repository{} }
func (r *Repository) CreateCredential(ctx context.Context, db identity.DBTX, userID, passwordHash string) error {
	_, err := db.Exec(ctx, `INSERT INTO password_credentials (user_id,password_hash) VALUES ($1,$2)`, userID, passwordHash)
	return mapError(err)
}
func (r *Repository) GetCredentialByEmail(ctx context.Context, db identity.DBTX, email string) (identity.User, string, error) {
	var u identity.User
	var hash string
	err := db.QueryRow(ctx, `SELECT u.id,u.email,u.full_name,u.created_at,u.updated_at,c.password_hash FROM users u JOIN password_credentials c ON c.user_id=u.id WHERE lower(u.email)=lower($1)`, email).Scan(&u.ID, &u.Email, &u.FullName, &u.CreatedAt, &u.UpdatedAt, &hash)
	return u, hash, mapError(err)
}
func (r *Repository) CreateSession(ctx context.Context, db identity.DBTX, userID string, digest []byte, expires time.Time, userAgent, ip string) (Session, error) {
	var s Session
	err := db.QueryRow(ctx, `INSERT INTO sessions (user_id,token_hash,expires_at,user_agent,ip_address) VALUES ($1,$2,$3,NULLIF($4,''),NULLIF($5,'')::inet) RETURNING id,user_id,token_hash,created_at,expires_at,last_seen_at,revoked_at`, userID, digest, expires, userAgent, ip).Scan(&s.ID, &s.UserID, &s.TokenHash, &s.CreatedAt, &s.ExpiresAt, &s.LastSeenAt, &s.RevokedAt)
	return s, mapError(err)
}
func (r *Repository) GetActiveSession(ctx context.Context, db identity.DBTX, digest []byte) (Session, error) {
	var s Session
	err := db.QueryRow(ctx, `SELECT id,user_id,token_hash,created_at,expires_at,last_seen_at,revoked_at FROM sessions WHERE token_hash=$1 AND revoked_at IS NULL AND expires_at>now()`, digest).Scan(&s.ID, &s.UserID, &s.TokenHash, &s.CreatedAt, &s.ExpiresAt, &s.LastSeenAt, &s.RevokedAt)
	return s, mapError(err)
}
func (r *Repository) TouchSession(ctx context.Context, db identity.DBTX, id string) error {
	_, err := db.Exec(ctx, `UPDATE sessions SET last_seen_at=now() WHERE id=$1 AND last_seen_at<now()-interval '15 minutes'`, id)
	return mapError(err)
}
func (r *Repository) RevokeSession(ctx context.Context, db identity.DBTX, id string) error {
	_, err := db.Exec(ctx, `UPDATE sessions SET revoked_at=COALESCE(revoked_at,now()) WHERE id=$1`, id)
	return mapError(err)
}
func (r *Repository) DeleteExpiredSessions(ctx context.Context, db identity.DBTX) (int64, error) {
	tag, err := db.Exec(ctx, `DELETE FROM sessions WHERE expires_at<=now() OR revoked_at IS NOT NULL`)
	if err != nil {
		return 0, mapError(err)
	}
	return tag.RowsAffected(), nil
}
func (r *Repository) ListWorkspaceAccess(ctx context.Context, db identity.DBTX, userID string) ([]WorkspaceAccess, error) {
	rows, err := db.Query(ctx, `SELECT w.id,w.name,w.slug,o.id,o.name,o.slug,m.role FROM workspace_members m JOIN workspaces w ON w.id=m.workspace_id JOIN organizations o ON o.id=w.organization_id WHERE m.user_id=$1 ORDER BY w.name,w.id`, userID)
	if err != nil {
		return nil, mapError(err)
	}
	defer rows.Close()
	items := make([]WorkspaceAccess, 0)
	for rows.Next() {
		var v WorkspaceAccess
		if err := rows.Scan(&v.ID, &v.Name, &v.Slug, &v.Organization.ID, &v.Organization.Name, &v.Organization.Slug, &v.Role); err != nil {
			return nil, err
		}
		items = append(items, v)
	}
	return items, rows.Err()
}
func (r *Repository) GetRole(ctx context.Context, db identity.DBTX, workspaceID, userID string) (string, error) {
	var role string
	err := db.QueryRow(ctx, `SELECT role FROM workspace_members WHERE workspace_id=$1 AND user_id=$2`, workspaceID, userID).Scan(&role)
	return role, mapError(err)
}
func (r *Repository) OrganizationAccessible(ctx context.Context, db identity.DBTX, organizationID, userID string) error {
	var exists bool
	err := db.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM workspace_members m JOIN workspaces w ON w.id=m.workspace_id WHERE w.organization_id=$1 AND m.user_id=$2)`, organizationID, userID).Scan(&exists)
	if err != nil {
		return mapError(err)
	}
	if !exists {
		return ErrForbidden
	}
	return nil
}
func (r *Repository) ListOrganizationsForUser(ctx context.Context, db identity.DBTX, userID string, p identity.Pagination) ([]identity.Organization, error) {
	rows, err := db.Query(ctx, `SELECT DISTINCT o.id,o.name,o.slug,o.created_at,o.updated_at FROM organizations o JOIN workspaces w ON w.organization_id=o.id JOIN workspace_members m ON m.workspace_id=w.id WHERE m.user_id=$1 ORDER BY o.created_at DESC,o.id LIMIT $2 OFFSET $3`, userID, p.Limit, p.Offset)
	if err != nil {
		return nil, mapError(err)
	}
	defer rows.Close()
	items := make([]identity.Organization, 0)
	for rows.Next() {
		var v identity.Organization
		if err := rows.Scan(&v.ID, &v.Name, &v.Slug, &v.CreatedAt, &v.UpdatedAt); err != nil {
			return nil, err
		}
		items = append(items, v)
	}
	return items, rows.Err()
}
func (r *Repository) ListWorkspacesForUser(ctx context.Context, db identity.DBTX, organizationID, userID string, p identity.Pagination) ([]identity.Workspace, error) {
	rows, err := db.Query(ctx, `SELECT w.id,o.id,o.name,o.slug,w.name,w.slug,w.created_at,w.updated_at FROM workspaces w JOIN organizations o ON o.id=w.organization_id JOIN workspace_members m ON m.workspace_id=w.id WHERE w.organization_id=$1 AND m.user_id=$2 ORDER BY w.created_at DESC,w.id LIMIT $3 OFFSET $4`, organizationID, userID, p.Limit, p.Offset)
	if err != nil {
		return nil, mapError(err)
	}
	defer rows.Close()
	items := make([]identity.Workspace, 0)
	for rows.Next() {
		var v identity.Workspace
		if err := rows.Scan(&v.ID, &v.Organization.ID, &v.Organization.Name, &v.Organization.Slug, &v.Name, &v.Slug, &v.CreatedAt, &v.UpdatedAt); err != nil {
			return nil, err
		}
		items = append(items, v)
	}
	return items, rows.Err()
}
func mapError(err error) error {
	if err == nil {
		return nil
	}
	if errors.Is(err, pgx.ErrNoRows) {
		return identity.ErrNotFound
	}
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) {
		if pgErr.Code == "23505" {
			return identity.ErrConflict
		}
		if pgErr.Code == "23503" {
			return identity.ErrNotFound
		}
	}
	return fmt.Errorf("auth database operation: %w", err)
}
