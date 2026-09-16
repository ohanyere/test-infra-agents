package environment

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"github.com/evalplane/evalplane/backend/internal/identity"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

// DBTX is the minimal database surface used by this package.
type DBTX interface {
	Exec(context.Context, string, ...any) (pgconn.CommandTag, error)
	Query(context.Context, string, ...any) (pgx.Rows, error)
	QueryRow(context.Context, string, ...any) pgx.Row
}

// Repository owns environment SQL. Historical certification pins are never
// mutated here; instance rows move forward through validated transitions.
type Repository struct{}

// NewRepository creates an environment Repository.
func NewRepository() *Repository { return &Repository{} }

// UpsertRequirements stores server-reviewed requirements for one exact
// ScenarioVersion.
func (r *Repository) UpsertRequirements(ctx context.Context, db DBTX, workspaceID, versionID string, req Requirements) (Requirements, error) {
	components, _ := json.Marshal(req.RequiredComponents)
	caps, _ := json.Marshal(req.RequiredCapabilities)
	var out ComponentsRow
	err := db.QueryRow(ctx, `INSERT INTO scenario_version_environment_requirements
		(scenario_version_id, workspace_id, required_components, minimum_isolation_tier, required_profile_capabilities, runtime_contract_version, contract_version, updated_at)
		VALUES ($1,$2,(SELECT array_agg(value) FROM jsonb_array_elements_text($3::jsonb) AS value),$4,(SELECT COALESCE(array_agg(value), '{}') FROM jsonb_array_elements_text($5::jsonb) AS value),$6,$7,now())
		ON CONFLICT (scenario_version_id) DO UPDATE SET workspace_id=EXCLUDED.workspace_id,
			required_components=EXCLUDED.required_components, minimum_isolation_tier=EXCLUDED.minimum_isolation_tier,
			required_profile_capabilities=EXCLUDED.required_profile_capabilities, runtime_contract_version=EXCLUDED.runtime_contract_version,
			contract_version=EXCLUDED.contract_version, updated_at=now()
		RETURNING required_components, minimum_isolation_tier, required_profile_capabilities, runtime_contract_version, contract_version`,
		versionID, workspaceID, string(components), req.MinimumIsolation, string(caps), req.RuntimeContract, RequirementsContractVersion).
		Scan(&out.Components, &out.MinimumIsolation, &out.Capabilities, &out.RuntimeContract, &out.ContractVersion)
	if err != nil {
		return Requirements{}, mapDB(err)
	}
	return out.Requirements(), nil
}

// ComponentsRow is the stored requirements shape.
type ComponentsRow struct {
	Components       []string
	MinimumIsolation string
	Capabilities     []string
	RuntimeContract  string
	ContractVersion  string
}

// Requirements converts the row to validated requirements.
func (c ComponentsRow) Requirements() Requirements {
	if c.ContractVersion == "" {
		c.ContractVersion = RequirementsContractVersion
	}
	if c.RuntimeContract == "" {
		c.RuntimeContract = DefaultRuntimeContract
	}
	if c.Capabilities == nil {
		c.Capabilities = []string{}
	}
	return Requirements{
		RequiredComponents:   append([]string(nil), c.Components...),
		MinimumIsolation:     c.MinimumIsolation,
		RequiredCapabilities: append([]string(nil), c.Capabilities...),
		RuntimeContract:      c.RuntimeContract,
		ContractVersion:      c.ContractVersion,
	}
}

// GetRequirements loads requirements for one ScenarioVersion.
func (r *Repository) GetRequirements(ctx context.Context, db DBTX, versionID string) (ComponentsRow, error) {
	var out ComponentsRow
	err := db.QueryRow(ctx, `SELECT required_components, minimum_isolation_tier, required_profile_capabilities, runtime_contract_version, contract_version
		FROM scenario_version_environment_requirements WHERE scenario_version_id=$1`, versionID).
		Scan(&out.Components, &out.MinimumIsolation, &out.Capabilities, &out.RuntimeContract, &out.ContractVersion)
	if err != nil {
		return ComponentsRow{}, mapDB(err)
	}
	return out, nil
}

// InsertInstance creates one provisioning instance row.
func (r *Repository) InsertInstance(ctx context.Context, db DBTX, in EnvironmentInstance) (EnvironmentInstance, error) {
	var out EnvironmentInstance
	err := db.QueryRow(ctx, `INSERT INTO environment_instances
		(workspace_id, owner_type, owner_id, profile_id, profile_contract_version, provider_id, provider_contract_version,
		 isolation_tier, status, lease_expires_at, provider_ref)
		VALUES ($1,$2,NULLIF($3,'')::uuid,$4,$5,$6,$7,$8,$9,$10,$11)
		RETURNING id, workspace_id, owner_type, owner_id::text, profile_id, profile_contract_version, provider_id,
			provider_contract_version, isolation_tier, status, lease_expires_at, created_at, ready_at, destroyed_at, provider_ref`,
		in.WorkspaceID, in.OwnerType, in.OwnerID, in.ProfileID, in.ProfileContractVersion, in.ProviderID,
		in.ProviderContractVersion, in.IsolationTier, StateProvisioning, in.LeaseExpiresAt, in.ProviderRef).
		Scan(&out.ID, &out.WorkspaceID, &out.OwnerType, &out.OwnerID, &out.ProfileID, &out.ProfileContractVersion,
			&out.ProviderID, &out.ProviderContractVersion, &out.IsolationTier, &out.Status, &out.LeaseExpiresAt,
			&out.CreatedAt, &out.ReadyAt, &out.DestroyedAt, &out.ProviderRef)
	if err != nil {
		return EnvironmentInstance{}, mapDB(err)
	}
	return out, nil
}

// GetInstance loads one instance scoped by workspace. Provider references
// returned are opaque handles only; no credentials are stored or returned.
func (r *Repository) GetInstance(ctx context.Context, db DBTX, workspaceID, environmentID string) (EnvironmentInstance, error) {
	return scanInstance(db.QueryRow(ctx, instanceSelect+` WHERE id=$1 AND workspace_id=$2`, environmentID, workspaceID))
}

// ListInstances lists workspace instances newest-first.
func (r *Repository) ListInstances(ctx context.Context, db DBTX, workspaceID string, limit int) ([]EnvironmentInstance, error) {
	if limit < 1 || limit > 100 {
		limit = 50
	}
	rows, err := db.Query(ctx, instanceSelect+` WHERE workspace_id=$1 ORDER BY created_at DESC, id DESC LIMIT $2`, workspaceID, limit)
	if err != nil {
		return nil, mapDB(err)
	}
	defer rows.Close()
	out := []EnvironmentInstance{}
	for rows.Next() {
		var item EnvironmentInstance
		var id, workspace, ownerType, ownerID, profileID, profileContract, providerID, providerContract, isolation, status, providerRef string
		var lease *time.Time
		var created time.Time
		var ready, destroyed *time.Time
		if err := rows.Scan(&id, &workspace, &ownerType, &ownerID, &profileID, &profileContract, &providerID, &providerContract, &isolation, &status, &lease, &created, &ready, &destroyed, &providerRef); err != nil {
			return nil, err
		}
		item = EnvironmentInstance{ID: id, WorkspaceID: workspace, OwnerType: ownerType, OwnerID: ownerID, ProfileID: profileID, ProfileContractVersion: profileContract, ProviderID: providerID, ProviderContractVersion: providerContract, IsolationTier: isolation, Status: status, LeaseExpiresAt: lease, CreatedAt: created, ReadyAt: ready, DestroyedAt: destroyed, ProviderRef: providerRef}
		out = append(out, item)
	}
	return out, mapDB(rows.Err())
}

// TransitionInstance moves one instance forward through a validated
// transition. Destroyed → destroying/destroyed is allowed (idempotent
// repeated destroy); destroyed never returns to ready.
func (r *Repository) TransitionInstance(ctx context.Context, db DBTX, workspaceID, environmentID, to string, providerRef string) (EnvironmentInstance, error) {
	current, err := r.GetInstance(ctx, db, workspaceID, environmentID)
	if err != nil {
		return EnvironmentInstance{}, err
	}
	if !ValidStateTransition(current.Status, to) {
		return EnvironmentInstance{}, errValidation("invalid environment transition from " + quote(current.Status) + " to " + quote(to))
	}
	// Never allow destroyed environments back to ready (defense in depth;
	// ValidStateTransition already rejects it).
	if current.Status == StateDestroyed && to == StateReady {
		return EnvironmentInstance{}, errValidation("destroyed environments cannot return to ready")
	}
	ref := current.ProviderRef
	if providerRef != "" {
		ref = providerRef
	}
	return scanInstance(db.QueryRow(ctx, `UPDATE environment_instances SET status=$3, provider_ref=$4,
			ready_at=CASE WHEN $3='ready' THEN COALESCE(ready_at, now()) ELSE ready_at END,
			destroyed_at=CASE WHEN $3='destroyed' THEN COALESCE(destroyed_at, now()) ELSE destroyed_at END
		WHERE id=$1 AND workspace_id=$2
		RETURNING id, workspace_id, owner_type, owner_id::text, profile_id, profile_contract_version, provider_id,
			provider_contract_version, isolation_tier, status, lease_expires_at, created_at, ready_at, destroyed_at, provider_ref`,
		environmentID, workspaceID, to, ref))
}

// GetByOwner loads the newest instance for one execution owner scoped by
// workspace. Run environments use owner_type=run and owner_id=run ID, so
// cancelled, abandoned, and crashed runs remain discoverable and
// cleanup-capable through the existing lease/recovery path. No
// cross-workspace reuse: the workspace predicate is mandatory.
func (r *Repository) GetByOwner(ctx context.Context, db DBTX, workspaceID, ownerType, ownerID string) (EnvironmentInstance, error) {
	return scanInstance(db.QueryRow(ctx, instanceSelect+` WHERE workspace_id=$1 AND owner_type=$2 AND owner_id::text=$3 ORDER BY created_at DESC, id DESC LIMIT 1`, workspaceID, ownerType, ownerID))
}

// ListByOwner lists all instances for one execution owner newest-first.
func (r *Repository) ListByOwner(ctx context.Context, db DBTX, workspaceID, ownerType, ownerID string) ([]EnvironmentInstance, error) {
	rows, err := db.Query(ctx, instanceSelect+` WHERE workspace_id=$1 AND owner_type=$2 AND owner_id::text=$3 ORDER BY created_at DESC, id DESC`, workspaceID, ownerType, ownerID)
	if err != nil {
		return nil, mapDB(err)
	}
	defer rows.Close()
	out := []EnvironmentInstance{}
	for rows.Next() {
		var item EnvironmentInstance
		var id, workspace, ownerType, ownerID, profileID, profileContract, providerID, providerContract, isolation, status, providerRef string
		var lease *time.Time
		var created time.Time
		var ready, destroyed *time.Time
		if err := rows.Scan(&id, &workspace, &ownerType, &ownerID, &profileID, &profileContract, &providerID, &providerContract, &isolation, &status, &lease, &created, &ready, &destroyed, &providerRef); err != nil {
			return nil, err
		}
		item = EnvironmentInstance{ID: id, WorkspaceID: workspace, OwnerType: ownerType, OwnerID: ownerID, ProfileID: profileID, ProfileContractVersion: profileContract, ProviderID: providerID, ProviderContractVersion: providerContract, IsolationTier: isolation, Status: status, LeaseExpiresAt: lease, CreatedAt: created, ReadyAt: ready, DestroyedAt: destroyed, ProviderRef: providerRef}
		out = append(out, item)
	}
	return out, mapDB(rows.Err())
}

const instanceSelect = `SELECT id, workspace_id, owner_type, owner_id::text, profile_id, profile_contract_version, provider_id,
	provider_contract_version, isolation_tier, status, lease_expires_at, created_at, ready_at, destroyed_at, provider_ref
	FROM environment_instances`

func scanInstance(row pgx.Row) (EnvironmentInstance, error) {
	var out EnvironmentInstance
	err := row.Scan(&out.ID, &out.WorkspaceID, &out.OwnerType, &out.OwnerID, &out.ProfileID, &out.ProfileContractVersion,
		&out.ProviderID, &out.ProviderContractVersion, &out.IsolationTier, &out.Status, &out.LeaseExpiresAt,
		&out.CreatedAt, &out.ReadyAt, &out.DestroyedAt, &out.ProviderRef)
	if err != nil {
		return EnvironmentInstance{}, mapDB(err)
	}
	return out, nil
}

func mapDB(err error) error {
	if err == nil {
		return nil
	}
	if errors.Is(err, pgx.ErrNoRows) {
		return identity.ErrNotFound
	}
	var pgErr interface{ SQLState() string }
	_ = pgErr
	return mapPgError(err)
}
