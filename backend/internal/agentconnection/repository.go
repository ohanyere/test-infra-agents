package agentconnection

import (
	"context"
	"errors"
	"time"

	"github.com/evalplane/evalplane/backend/internal/identity"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

type DBTX interface {
	Exec(context.Context, string, ...any) (pgconn.CommandTag, error)
	QueryRow(context.Context, string, ...any) pgx.Row
}

type Repository struct{}

func NewRepository() *Repository { return &Repository{} }

func (r *Repository) UpsertCredential(ctx context.Context, db DBTX, workspaceID, versionID, authType string, envelope []byte) (Info, error) {
	var out Info
	var has bool
	err := db.QueryRow(ctx, `INSERT INTO agent_version_connections(agent_version_id,auth_type,credential_envelope,credential_updated_at,last_checked_at,last_check_status,last_diagnostic_code)
		SELECT v.id,$3,$4,CASE WHEN $3='bearer' THEN now() ELSE NULL END,NULL,'untested',NULL
		FROM agent_versions v JOIN agents a ON a.id=v.agent_id WHERE v.id=$2 AND a.owner_workspace_id=$1
		ON CONFLICT(agent_version_id) DO UPDATE SET auth_type=EXCLUDED.auth_type,credential_envelope=EXCLUDED.credential_envelope,
		credential_updated_at=EXCLUDED.credential_updated_at,last_checked_at=NULL,last_check_status='untested',last_diagnostic_code=NULL,updated_at=now()
		RETURNING agent_version_id,auth_type,(credential_envelope IS NOT NULL),credential_updated_at,last_checked_at,last_check_status,last_diagnostic_code`, workspaceID, versionID, authType, envelope).
		Scan(&out.AgentVersionID, &out.AuthType, &has, &out.CredentialUpdatedAt, &out.LastCheckedAt, &out.LastCheckStatus, &out.LastDiagnosticCode)
	if err != nil {
		return Info{}, mapError(err)
	}
	out.HasCredential = has
	return out, nil
}

func (r *Repository) Get(ctx context.Context, db DBTX, workspaceID, versionID string) (storedConnection, error) {
	var out storedConnection
	var has bool
	err := db.QueryRow(ctx, `SELECT v.id,v.interface_kind,COALESCE(v.endpoint_url,''),COALESCE(c.auth_type,'none'),
		(c.credential_envelope IS NOT NULL),c.credential_updated_at,c.last_checked_at,COALESCE(c.last_check_status,'untested'),c.last_diagnostic_code,c.credential_envelope
		FROM agent_versions v JOIN agents a ON a.id=v.agent_id LEFT JOIN agent_version_connections c ON c.agent_version_id=v.id
		WHERE v.id=$2 AND a.owner_workspace_id=$1`, workspaceID, versionID).
		Scan(&out.AgentVersionID, &out.Protocol, &out.EndpointURL, &out.AuthType, &has, &out.CredentialUpdatedAt, &out.LastCheckedAt, &out.LastCheckStatus, &out.LastDiagnosticCode, &out.CredentialEnvelope)
	if err != nil {
		return storedConnection{}, mapError(err)
	}
	out.HasCredential = has
	return out, nil
}

func (r *Repository) SetCheck(ctx context.Context, db DBTX, workspaceID, versionID, status, code string, checked time.Time) (Info, error) {
	tag, err := db.Exec(ctx, `INSERT INTO agent_version_connections(agent_version_id,auth_type,last_checked_at,last_check_status,last_diagnostic_code)
		SELECT v.id,'none',$3,$4,$5 FROM agent_versions v JOIN agents a ON a.id=v.agent_id WHERE v.id=$2 AND a.owner_workspace_id=$1
		ON CONFLICT(agent_version_id) DO UPDATE SET last_checked_at=EXCLUDED.last_checked_at,last_check_status=EXCLUDED.last_check_status,last_diagnostic_code=EXCLUDED.last_diagnostic_code,updated_at=now()`, workspaceID, versionID, checked, status, nullableString(code))
	if err != nil {
		return Info{}, mapError(err)
	}
	if tag.RowsAffected() == 0 {
		return Info{}, identity.ErrNotFound
	}
	value, err := r.Get(ctx, db, workspaceID, versionID)
	return value.Info, err
}

func (r *Repository) GetCompatibility(ctx context.Context, db DBTX, workspaceID, versionID, scenarioVersionID string) (declared, required []string, err error) {
	err = db.QueryRow(ctx, `SELECT v.capabilities,COALESCE(sv.observables,'{}'::text[]) || COALESCE(sv.allowed_actions,'{}'::text[])
		FROM agent_versions v JOIN agents a ON a.id=v.agent_id
		JOIN scenario_versions sv ON sv.id=$3 JOIN scenarios s ON s.id=sv.scenario_id
		WHERE v.id=$2 AND a.owner_workspace_id=$1 AND (s.owner_workspace_id=$1 OR (s.kind='built_in' AND s.visibility='public'))`, workspaceID, versionID, scenarioVersionID).Scan(&declared, &required)
	if err != nil {
		return nil, nil, mapError(err)
	}
	return declared, required, nil
}

func nullableString(value string) any {
	if value == "" {
		return nil
	}
	return value
}

func mapError(err error) error {
	if errors.Is(err, pgx.ErrNoRows) {
		return identity.ErrNotFound
	}
	return err
}
