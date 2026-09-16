package identity

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

type DBTX interface {
	Exec(context.Context, string, ...any) (pgconn.CommandTag, error)
	Query(context.Context, string, ...any) (pgx.Rows, error)
	QueryRow(context.Context, string, ...any) pgx.Row
}

type Repository struct{}

func NewRepository() *Repository { return &Repository{} }

func (r *Repository) CreateOrganization(ctx context.Context, db DBTX, in OrganizationInput) (Organization, error) {
	var v Organization
	err := db.QueryRow(ctx, `INSERT INTO organizations (name, slug) VALUES ($1,$2) RETURNING id,name,slug,created_at,updated_at`, in.Name, in.Slug).
		Scan(&v.ID, &v.Name, &v.Slug, &v.CreatedAt, &v.UpdatedAt)
	return v, mapDatabaseError(err)
}

func (r *Repository) GetOrganization(ctx context.Context, db DBTX, id string) (Organization, error) {
	var v Organization
	err := db.QueryRow(ctx, `SELECT id,name,slug,created_at,updated_at FROM organizations WHERE id=$1`, id).
		Scan(&v.ID, &v.Name, &v.Slug, &v.CreatedAt, &v.UpdatedAt)
	return v, mapDatabaseError(err)
}

func (r *Repository) ListOrganizations(ctx context.Context, db DBTX, p Pagination) ([]Organization, error) {
	rows, err := db.Query(ctx, `SELECT id,name,slug,created_at,updated_at FROM organizations ORDER BY created_at DESC,id LIMIT $1 OFFSET $2`, p.Limit, p.Offset)
	if err != nil {
		return nil, mapDatabaseError(err)
	}
	defer rows.Close()
	items := make([]Organization, 0)
	for rows.Next() {
		var v Organization
		if err := rows.Scan(&v.ID, &v.Name, &v.Slug, &v.CreatedAt, &v.UpdatedAt); err != nil {
			return nil, fmt.Errorf("scan organization: %w", err)
		}
		items = append(items, v)
	}
	return items, rows.Err()
}

func (r *Repository) CreateWorkspace(ctx context.Context, db DBTX, organizationID string, in WorkspaceInput) (Workspace, error) {
	var v Workspace
	err := db.QueryRow(ctx, `INSERT INTO workspaces (organization_id,name,slug) VALUES ($1,$2,$3) RETURNING id,name,slug,created_at,updated_at`, organizationID, in.Name, in.Slug).
		Scan(&v.ID, &v.Name, &v.Slug, &v.CreatedAt, &v.UpdatedAt)
	if err != nil {
		return v, mapDatabaseError(err)
	}
	o, err := r.GetOrganization(ctx, db, organizationID)
	if err != nil {
		return v, err
	}
	v.Organization = OrganizationSummary{ID: o.ID, Name: o.Name, Slug: o.Slug}
	return v, nil
}

func (r *Repository) GetWorkspace(ctx context.Context, db DBTX, id string) (Workspace, error) {
	var v Workspace
	err := db.QueryRow(ctx, `SELECT w.id,o.id,o.name,o.slug,w.name,w.slug,w.created_at,w.updated_at FROM workspaces w JOIN organizations o ON o.id=w.organization_id WHERE w.id=$1`, id).
		Scan(&v.ID, &v.Organization.ID, &v.Organization.Name, &v.Organization.Slug, &v.Name, &v.Slug, &v.CreatedAt, &v.UpdatedAt)
	return v, mapDatabaseError(err)
}
func (r *Repository) UpdateWorkspaceName(ctx context.Context, db DBTX, id, name string) (Workspace, error) {
	var v Workspace
	err := db.QueryRow(ctx, `UPDATE workspaces w SET name=$2,updated_at=now() FROM organizations o WHERE w.id=$1 AND o.id=w.organization_id RETURNING w.id,o.id,o.name,o.slug,w.name,w.slug,w.created_at,w.updated_at`, id, name).Scan(&v.ID, &v.Organization.ID, &v.Organization.Name, &v.Organization.Slug, &v.Name, &v.Slug, &v.CreatedAt, &v.UpdatedAt)
	return v, mapDatabaseError(err)
}

func (r *Repository) ListWorkspaces(ctx context.Context, db DBTX, organizationID string, p Pagination) ([]Workspace, error) {
	rows, err := db.Query(ctx, `SELECT w.id,o.id,o.name,o.slug,w.name,w.slug,w.created_at,w.updated_at FROM workspaces w JOIN organizations o ON o.id=w.organization_id WHERE w.organization_id=$1 ORDER BY w.created_at DESC,w.id LIMIT $2 OFFSET $3`, organizationID, p.Limit, p.Offset)
	if err != nil {
		return nil, mapDatabaseError(err)
	}
	defer rows.Close()
	items := make([]Workspace, 0)
	for rows.Next() {
		var v Workspace
		if err := rows.Scan(&v.ID, &v.Organization.ID, &v.Organization.Name, &v.Organization.Slug, &v.Name, &v.Slug, &v.CreatedAt, &v.UpdatedAt); err != nil {
			return nil, err
		}
		items = append(items, v)
	}
	return items, rows.Err()
}

func (r *Repository) CreateUser(ctx context.Context, db DBTX, in UserInput) (User, error) {
	var v User
	err := db.QueryRow(ctx, `INSERT INTO users (email,full_name) VALUES ($1,$2) RETURNING id,email,full_name,created_at,updated_at`, in.Email, in.FullName).Scan(&v.ID, &v.Email, &v.FullName, &v.CreatedAt, &v.UpdatedAt)
	return v, mapDatabaseError(err)
}
func (r *Repository) GetUser(ctx context.Context, db DBTX, id string) (User, error) {
	var v User
	err := db.QueryRow(ctx, `SELECT id,email,full_name,created_at,updated_at FROM users WHERE id=$1`, id).Scan(&v.ID, &v.Email, &v.FullName, &v.CreatedAt, &v.UpdatedAt)
	return v, mapDatabaseError(err)
}

func (r *Repository) CreateMembership(ctx context.Context, db DBTX, workspaceID, userID, role string) (Membership, error) {
	var m Membership
	m.WorkspaceID = workspaceID
	err := db.QueryRow(ctx, `INSERT INTO workspace_members (workspace_id,user_id,role) VALUES ($1,$2,$3) RETURNING role,joined_at`, workspaceID, userID, role).Scan(&m.Role, &m.JoinedAt)
	if err != nil {
		return m, mapDatabaseError(err)
	}
	u, err := r.GetUser(ctx, db, userID)
	if err != nil {
		return m, err
	}
	m.User = UserSummary{ID: u.ID, Email: u.Email, FullName: u.FullName}
	return m, nil
}
func (r *Repository) ListMembers(ctx context.Context, db DBTX, workspaceID string, p Pagination) ([]Membership, error) {
	rows, err := db.Query(ctx, `SELECT u.id,u.email,u.full_name,m.role,m.joined_at FROM workspace_members m JOIN users u ON u.id=m.user_id WHERE m.workspace_id=$1 ORDER BY m.joined_at,u.id LIMIT $2 OFFSET $3`, workspaceID, p.Limit, p.Offset)
	if err != nil {
		return nil, mapDatabaseError(err)
	}
	defer rows.Close()
	items := make([]Membership, 0)
	for rows.Next() {
		var m Membership
		m.WorkspaceID = workspaceID
		if err := rows.Scan(&m.User.ID, &m.User.Email, &m.User.FullName, &m.Role, &m.JoinedAt); err != nil {
			return nil, err
		}
		items = append(items, m)
	}
	return items, rows.Err()
}
func (r *Repository) UpdateMembership(ctx context.Context, db DBTX, workspaceID, userID, role string) (Membership, error) {
	var m Membership
	m.WorkspaceID = workspaceID
	err := db.QueryRow(ctx, `UPDATE workspace_members SET role=$3 WHERE workspace_id=$1 AND user_id=$2 RETURNING role,joined_at`, workspaceID, userID, role).Scan(&m.Role, &m.JoinedAt)
	if err != nil {
		return m, mapDatabaseError(err)
	}
	u, err := r.GetUser(ctx, db, userID)
	if err != nil {
		return m, err
	}
	m.User = UserSummary{ID: u.ID, Email: u.Email, FullName: u.FullName}
	return m, nil
}
func (r *Repository) DeleteMembership(ctx context.Context, db DBTX, workspaceID, userID string) error {
	tag, err := db.Exec(ctx, `DELETE FROM workspace_members WHERE workspace_id=$1 AND user_id=$2`, workspaceID, userID)
	if err != nil {
		return mapDatabaseError(err)
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

func mapDatabaseError(err error) error {
	if err == nil {
		return nil
	}
	if errors.Is(err, pgx.ErrNoRows) {
		return ErrNotFound
	}
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) {
		switch pgErr.Code {
		case "23505":
			return ErrConflict
		case "23503":
			return ErrNotFound
		}
	}
	return fmt.Errorf("database operation: %w", err)
}
