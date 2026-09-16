package incidentsource

import (
	"context"
	"errors"
	"time"

	"github.com/evalplane/evalplane/backend/internal/identity"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

type Store interface {
	Create(context.Context, string, string, string, []byte, []byte) (Connection, error)
	List(context.Context, string) ([]Connection, error)
	Get(context.Context, string, string) (StoredConnection, error)
	RotateCredentials(context.Context, string, string, []byte) (Connection, error)
	SetCheck(context.Context, string, string, string, string, time.Time) (Connection, error)
}

type Repository struct{ pool *pgxpool.Pool }

func NewRepository(pool *pgxpool.Pool) *Repository { return &Repository{pool: pool} }

func (r *Repository) Create(ctx context.Context, workspaceID, provider, displayName string, config, envelope []byte) (Connection, error) {
	var out Connection
	var configured bool
	err := r.pool.QueryRow(ctx, `INSERT INTO incident_source_connections
		(workspace_id,provider,display_name,provider_config,credential_envelope)
		VALUES($1,$2,$3,$4,$5)
		RETURNING id,workspace_id,provider,display_name,provider_config,(credential_envelope IS NOT NULL),status,
		last_diagnostic_code,last_checked_at,credential_updated_at,created_at,updated_at`, workspaceID, provider, displayName, config, envelope).
		Scan(&out.ID, &out.WorkspaceID, &out.Provider, &out.DisplayName, &out.ProviderConfig, &configured, &out.Status,
			&out.LastDiagnosticCode, &out.LastCheckedAt, &out.CredentialUpdatedAt, &out.CreatedAt, &out.UpdatedAt)
	if err != nil {
		return Connection{}, mapStoreError(err)
	}
	out.AuthConfigured = configured
	out.ConnectorContract = contractFor(provider)
	return out, nil
}

func (r *Repository) List(ctx context.Context, workspaceID string) ([]Connection, error) {
	rows, err := r.pool.Query(ctx, `SELECT id,workspace_id,provider,display_name,provider_config,(credential_envelope IS NOT NULL),status,
		last_diagnostic_code,last_checked_at,credential_updated_at,created_at,updated_at
		FROM incident_source_connections WHERE workspace_id=$1 AND disabled_at IS NULL ORDER BY created_at,id`, workspaceID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := []Connection{}
	for rows.Next() {
		var out Connection
		if err = rows.Scan(&out.ID, &out.WorkspaceID, &out.Provider, &out.DisplayName, &out.ProviderConfig, &out.AuthConfigured,
			&out.Status, &out.LastDiagnosticCode, &out.LastCheckedAt, &out.CredentialUpdatedAt, &out.CreatedAt, &out.UpdatedAt); err != nil {
			return nil, err
		}
		out.ConnectorContract = contractFor(out.Provider)
		items = append(items, out)
	}
	return items, rows.Err()
}

func (r *Repository) Get(ctx context.Context, workspaceID, id string) (StoredConnection, error) {
	var out StoredConnection
	err := r.pool.QueryRow(ctx, `SELECT id,workspace_id,provider,display_name,provider_config,(credential_envelope IS NOT NULL),status,
		last_diagnostic_code,last_checked_at,credential_updated_at,created_at,updated_at,credential_envelope,disabled_at
		FROM incident_source_connections WHERE id=$2 AND workspace_id=$1`, workspaceID, id).
		Scan(&out.ID, &out.WorkspaceID, &out.Provider, &out.DisplayName, &out.ProviderConfig, &out.AuthConfigured,
			&out.Status, &out.LastDiagnosticCode, &out.LastCheckedAt, &out.CredentialUpdatedAt, &out.CreatedAt, &out.UpdatedAt,
			&out.CredentialEnvelope, &out.DisabledAt)
	if err != nil {
		return StoredConnection{}, mapStoreError(err)
	}
	out.ConnectorContract = contractFor(out.Provider)
	return out, nil
}

func (r *Repository) RotateCredentials(ctx context.Context, workspaceID, id string, envelope []byte) (Connection, error) {
	tag, err := r.pool.Exec(ctx, `UPDATE incident_source_connections SET credential_envelope=$3,credential_envelope_version=1,
		credential_updated_at=now(),status='untested',last_checked_at=NULL,last_diagnostic_code=NULL,updated_at=now()
		WHERE workspace_id=$1 AND id=$2 AND disabled_at IS NULL`, workspaceID, id, envelope)
	if err != nil {
		return Connection{}, mapStoreError(err)
	}
	if tag.RowsAffected() == 0 {
		return Connection{}, identity.ErrNotFound
	}
	stored, err := r.Get(ctx, workspaceID, id)
	return stored.Connection, err
}

func (r *Repository) SetCheck(ctx context.Context, workspaceID, id, status, code string, checked time.Time) (Connection, error) {
	tag, err := r.pool.Exec(ctx, `UPDATE incident_source_connections SET status=$3,last_diagnostic_code=$4,last_checked_at=$5,updated_at=now()
		WHERE workspace_id=$1 AND id=$2 AND disabled_at IS NULL`, workspaceID, id, status, nullable(code), checked)
	if err != nil {
		return Connection{}, mapStoreError(err)
	}
	if tag.RowsAffected() == 0 {
		return Connection{}, identity.ErrNotFound
	}
	stored, err := r.Get(ctx, workspaceID, id)
	return stored.Connection, err
}

func mapStoreError(err error) error {
	if errors.Is(err, pgx.ErrNoRows) {
		return identity.ErrNotFound
	}
	return err
}

func nullable(value string) any {
	if value == "" {
		return nil
	}
	return value
}

func contractFor(provider string) string {
	if provider == ProviderPagerDuty {
		return PagerDutyContract
	}
	if provider == ProviderDatadog {
		return DatadogContract
	}
	return ""
}
