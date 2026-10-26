package agent

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"time"

	"github.com/evalplane/evalplane/backend/internal/competency"
	"github.com/evalplane/evalplane/backend/internal/identity"
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

const agentColumns = `a.id,a.owner_workspace_id,a.slug,a.name,a.description,a.visibility,a.status,a.created_at,a.updated_at,
 v.id,v.version_label,v.interface_kind,v.tool_access_mode,v.endpoint_url,v.capabilities,v.implementation_identity,v.metadata,v.created_at`

const latestVersionJoin = `LEFT JOIN LATERAL (
	SELECT id,version_label,interface_kind,tool_access_mode,endpoint_url,capabilities,implementation_identity,metadata,created_at
	FROM agent_versions WHERE agent_id=a.id ORDER BY created_at DESC,id DESC LIMIT 1
) v ON true`

func (r *Repository) Create(ctx context.Context, db DBTX, workspaceID string, in CreateInput) (Agent, error) {
	var value Agent
	err := db.QueryRow(ctx, `INSERT INTO agents (owner_workspace_id,slug,name,description,visibility,status)
		VALUES ($1,$2,$3,$4,$5,'active') RETURNING id,owner_workspace_id,slug,name,description,visibility,status,created_at,updated_at`,
		workspaceID, in.Slug, in.Name, in.Description, in.Visibility).Scan(&value.ID, &value.OwnerWorkspaceID, &value.Slug, &value.Name, &value.Description, &value.Visibility, &value.Status, &value.CreatedAt, &value.UpdatedAt)
	return value, mapDatabaseError(err)
}

func (r *Repository) CreateVersion(ctx context.Context, db DBTX, agentID string, in VersionInput) (Version, error) {
	var value Version
	err := db.QueryRow(ctx, `INSERT INTO agent_versions (agent_id,version_label,interface_kind,tool_access_mode,endpoint_url,capabilities,implementation_identity,metadata)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8) RETURNING id,version_label,interface_kind,tool_access_mode,COALESCE(endpoint_url,''),capabilities,implementation_identity,metadata,created_at`,
		agentID, in.VersionLabel, in.InterfaceKind, in.ToolAccessMode, nullableEndpoint(in.EndpointURL), in.Capabilities, in.ImplementationIdentity, in.Metadata).Scan(&value.ID, &value.VersionLabel, &value.InterfaceKind, &value.ToolAccessMode, &value.EndpointURL, &value.Capabilities, &value.ImplementationIdentity, &value.Metadata, &value.CreatedAt)
	if err != nil {
		return value, mapDatabaseError(err)
	}
	if len(in.ClaimedCapabilityIDs) > 0 {
		for _, capabilityID := range in.ClaimedCapabilityIDs {
			if _, err := db.Exec(ctx, `INSERT INTO agent_version_capability_claims (agent_version_id,capability_id,catalog_version) VALUES ($1,$2,$3)`,
				value.ID, capabilityID, competency.ContractVersion); err != nil {
				return Version{}, mapDatabaseError(err)
			}
		}
	}
	claims, err := listClaimsForVersions(ctx, db, []string{value.ID})
	if err != nil {
		return Version{}, err
	}
	applyClaims(&value, claims[value.ID])
	return value, nil
}

func (r *Repository) GetPublicBySlug(ctx context.Context, db DBTX, slug string) (Agent, error) {
	row := db.QueryRow(ctx, `SELECT `+agentColumns+` FROM agents a `+latestVersionJoin+`
		WHERE a.slug=$1 AND a.visibility='public' AND a.status='active'
		ORDER BY a.owner_workspace_id NULLS FIRST,a.created_at DESC,a.id DESC LIMIT 1`, slug)
	value, err := scanAgent(row)
	if err != nil {
		return value, err
	}
	if err := r.attachAgentClaims(ctx, db, &value); err != nil {
		return Agent{}, err
	}
	return value, nil
}

func (r *Repository) GetWorkspaceAgent(ctx context.Context, db DBTX, workspaceID, agentID string) (Agent, error) {
	row := db.QueryRow(ctx, `SELECT `+agentColumns+` FROM agents a `+latestVersionJoin+` WHERE a.id=$1 AND a.owner_workspace_id=$2`, agentID, workspaceID)
	value, err := scanAgent(row)
	if err != nil {
		return value, err
	}
	if err := r.attachAgentClaims(ctx, db, &value); err != nil {
		return Agent{}, err
	}
	return value, nil
}

func (r *Repository) ListPublic(ctx context.Context, db DBTX, filters Filters) ([]Agent, error) {
	return listAgents(ctx, db, `a.visibility='public' AND a.status='active'`, nil, filters)
}

func (r *Repository) ListWorkspace(ctx context.Context, db DBTX, workspaceID string, filters Filters) ([]Agent, error) {
	return listAgents(ctx, db, `a.owner_workspace_id=$1`, []any{workspaceID}, filters)
}

func listAgents(ctx context.Context, db DBTX, baseWhere string, args []any, filters Filters) ([]Agent, error) {
	where := baseWhere
	add := func(clause string, value any) {
		args = append(args, value)
		where += fmt.Sprintf(" AND "+clause, len(args))
	}
	if filters.Query != "" {
		args = append(args, "%"+filters.Query+"%")
		where += fmt.Sprintf(` AND (a.name ILIKE $%d OR a.slug ILIKE $%d OR a.description ILIKE $%d)`, len(args), len(args), len(args))
	}
	if filters.Capability != "" {
		add(`$%d = ANY(COALESCE(v.capabilities,'{}'::text[]))`, filters.Capability)
	}
	if filters.InterfaceKind != "" {
		add(`v.interface_kind=$%d`, filters.InterfaceKind)
	}
	if filters.Status != "" && filters.Status != "all" {
		add(`a.status=$%d`, filters.Status)
	}
	args = append(args, filters.Pagination.Limit, filters.Pagination.Offset)
	query := fmt.Sprintf(`SELECT %s FROM agents a %s WHERE %s ORDER BY a.created_at DESC,a.id DESC LIMIT $%d OFFSET $%d`, agentColumns, latestVersionJoin, where, len(args)-1, len(args))
	rows, err := db.Query(ctx, query, args...)
	if err != nil {
		return nil, mapDatabaseError(err)
	}
	defer rows.Close()
	items := make([]Agent, 0)
	for rows.Next() {
		value, err := scanAgent(rows)
		if err != nil {
			return nil, err
		}
		items = append(items, value)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	if err := attachAgentsClaims(ctx, db, items); err != nil {
		return nil, err
	}
	return items, nil
}

func (r *Repository) Update(ctx context.Context, db DBTX, workspaceID, agentID string, in UpdateInput) (Agent, error) {
	tag, err := db.Exec(ctx, `UPDATE agents SET
		name=COALESCE($3,name),description=COALESCE($4,description),visibility=COALESCE($5,visibility),status=COALESCE($6,status),updated_at=now()
		WHERE id=$1 AND owner_workspace_id=$2`, agentID, workspaceID, in.Name, in.Description, in.Visibility, in.Status)
	if err != nil {
		return Agent{}, mapDatabaseError(err)
	}
	if tag.RowsAffected() == 0 {
		return Agent{}, identity.ErrNotFound
	}
	return r.GetWorkspaceAgent(ctx, db, workspaceID, agentID)
}

func (r *Repository) ListVersions(ctx context.Context, db DBTX, workspaceID, agentID string, page identity.Pagination) ([]Version, error) {
	rows, err := db.Query(ctx, `SELECT v.id,v.version_label,v.interface_kind,v.tool_access_mode,COALESCE(v.endpoint_url,''),v.capabilities,v.implementation_identity,v.metadata,v.created_at
		FROM agent_versions v JOIN agents a ON a.id=v.agent_id WHERE v.agent_id=$1 AND a.owner_workspace_id=$2
		ORDER BY v.created_at DESC,v.id DESC LIMIT $3 OFFSET $4`, agentID, workspaceID, page.Limit, page.Offset)
	if err != nil {
		return nil, mapDatabaseError(err)
	}
	defer rows.Close()
	items := make([]Version, 0)
	for rows.Next() {
		var value Version
		if err := rows.Scan(&value.ID, &value.VersionLabel, &value.InterfaceKind, &value.ToolAccessMode, &value.EndpointURL, &value.Capabilities, &value.ImplementationIdentity, &value.Metadata, &value.CreatedAt); err != nil {
			return nil, err
		}
		items = append(items, value)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	if err := attachVersionsClaims(ctx, db, items); err != nil {
		return nil, err
	}
	return items, nil
}

func (r *Repository) GetVersion(ctx context.Context, db DBTX, workspaceID, agentID, versionID string) (Version, error) {
	var value Version
	err := db.QueryRow(ctx, `SELECT v.id,v.version_label,v.interface_kind,v.tool_access_mode,COALESCE(v.endpoint_url,''),v.capabilities,v.implementation_identity,v.metadata,v.created_at
		FROM agent_versions v JOIN agents a ON a.id=v.agent_id
		WHERE v.id=$1 AND v.agent_id=$2 AND a.owner_workspace_id=$3`, versionID, agentID, workspaceID).
		Scan(&value.ID, &value.VersionLabel, &value.InterfaceKind, &value.ToolAccessMode, &value.EndpointURL, &value.Capabilities, &value.ImplementationIdentity, &value.Metadata, &value.CreatedAt)
	if err != nil {
		return value, mapDatabaseError(err)
	}
	claims, err := listClaimsForVersions(ctx, db, []string{value.ID})
	if err != nil {
		return Version{}, err
	}
	applyClaims(&value, claims[value.ID])
	return value, nil
}

type rowScanner interface{ Scan(...any) error }

func scanAgent(row rowScanner) (Agent, error) {
	var value Agent
	var versionID, versionLabel, interfaceKind, toolAccessMode *string
	var endpointURL *string
	var capabilities []string
	var implementationIdentity []byte
	var metadata []byte
	var versionCreatedAt *time.Time
	err := row.Scan(&value.ID, &value.OwnerWorkspaceID, &value.Slug, &value.Name, &value.Description, &value.Visibility, &value.Status, &value.CreatedAt, &value.UpdatedAt,
		&versionID, &versionLabel, &interfaceKind, &toolAccessMode, &endpointURL, &capabilities, &implementationIdentity, &metadata, &versionCreatedAt)
	if err != nil {
		return value, mapDatabaseError(err)
	}
	if versionID != nil {
		if capabilities == nil {
			capabilities = []string{}
		}
		value.LatestVersion = &Version{ID: *versionID, VersionLabel: *versionLabel, InterfaceKind: *interfaceKind, ToolAccessMode: stringValue(toolAccessMode), EndpointURL: stringValue(endpointURL), Capabilities: capabilities, ClaimedCapabilities: []string{}, Metadata: metadata, CreatedAt: *versionCreatedAt}
		if err := json.Unmarshal(implementationIdentity, &value.LatestVersion.ImplementationIdentity); err != nil {
			return value, fmt.Errorf("decode agent implementation identity: %w", err)
		}
	}
	return value, nil
}

type claimRow struct {
	capabilityID   string
	catalogVersion string
	claimedAt      time.Time
}

func listClaimsForVersions(ctx context.Context, db DBTX, versionIDs []string) (map[string][]claimRow, error) {
	out := make(map[string][]claimRow, len(versionIDs))
	if len(versionIDs) == 0 {
		return out, nil
	}
	rows, err := db.Query(ctx, `SELECT agent_version_id,capability_id,catalog_version,claimed_at FROM agent_version_capability_claims WHERE agent_version_id = ANY($1) ORDER BY capability_id`, versionIDs)
	if err != nil {
		return nil, mapDatabaseError(err)
	}
	defer rows.Close()
	for rows.Next() {
		var versionID string
		var row claimRow
		if err := rows.Scan(&versionID, &row.capabilityID, &row.catalogVersion, &row.claimedAt); err != nil {
			return nil, err
		}
		out[versionID] = append(out[versionID], row)
	}
	return out, rows.Err()
}

func applyClaims(value *Version, rows []claimRow) {
	claimed := make([]string, 0, len(rows))
	catalogVersion := ""
	for _, row := range rows {
		claimed = append(claimed, row.capabilityID)
		if catalogVersion == "" {
			catalogVersion = row.catalogVersion
		}
	}
	sort.Strings(claimed)
	value.ClaimedCapabilities = claimed
	if value.ClaimedCapabilities == nil {
		value.ClaimedCapabilities = []string{}
	}
	if value.Capabilities == nil {
		value.Capabilities = []string{}
	}
	value.CapabilityCatalogVersion = catalogVersion
	if len(claimed) > 0 {
		value.ClaimStatus = competency.ClaimStatusClaimed
		if value.CapabilityCatalogVersion == "" {
			value.CapabilityCatalogVersion = competency.ContractVersion
		}
	} else {
		value.ClaimStatus = ""
	}
}

func (r *Repository) attachAgentClaims(ctx context.Context, db DBTX, value *Agent) error {
	if value.LatestVersion == nil {
		return nil
	}
	return attachAgentsClaims(ctx, db, []Agent{*value})
}

func attachAgentsClaims(ctx context.Context, db DBTX, items []Agent) error {
	ids := make([]string, 0, len(items))
	for _, item := range items {
		if item.LatestVersion != nil {
			ids = append(ids, item.LatestVersion.ID)
		}
	}
	claims, err := listClaimsForVersions(ctx, db, ids)
	if err != nil {
		return err
	}
	for i := range items {
		if items[i].LatestVersion != nil {
			applyClaims(items[i].LatestVersion, claims[items[i].LatestVersion.ID])
		}
	}
	return nil
}

func attachVersionsClaims(ctx context.Context, db DBTX, items []Version) error {
	ids := make([]string, 0, len(items))
	for _, item := range items {
		ids = append(ids, item.ID)
	}
	claims, err := listClaimsForVersions(ctx, db, ids)
	if err != nil {
		return err
	}
	for i := range items {
		applyClaims(&items[i], claims[items[i].ID])
	}
	return nil
}

// ListVersionClaims returns immutable claims for one version, enforcing
// workspace ownership of the parent agent. Old versions simply return zero
// claims ("No capability claims declared").
func (r *Repository) ListVersionClaims(ctx context.Context, db DBTX, workspaceID, agentID, versionID string) ([]CapabilityClaim, error) {
	var agentExists string
	if err := db.QueryRow(ctx, `SELECT v.id FROM agent_versions v JOIN agents a ON a.id=v.agent_id WHERE v.id=$1 AND v.agent_id=$2 AND a.owner_workspace_id=$3`, versionID, agentID, workspaceID).Scan(&agentExists); err != nil {
		return nil, mapDatabaseError(err)
	}
	rows, err := db.Query(ctx, `SELECT capability_id,catalog_version,claimed_at FROM agent_version_capability_claims WHERE agent_version_id=$1 ORDER BY capability_id`, versionID)
	if err != nil {
		return nil, mapDatabaseError(err)
	}
	defer rows.Close()
	items := make([]CapabilityClaim, 0)
	for rows.Next() {
		var claim CapabilityClaim
		if err := rows.Scan(&claim.CapabilityID, &claim.CatalogVersion, &claim.ClaimedAt); err != nil {
			return nil, err
		}
		if meta, ok := competency.Get(claim.CapabilityID); ok {
			claim.Domain = meta.Domain
			claim.Family = meta.Family
			claim.DisplayName = meta.DisplayName
			claim.Description = meta.Description
			if claim.CatalogVersion == "" {
				claim.CatalogVersion = meta.CatalogVersion
			}
		}
		claim.ClaimStatus = competency.ClaimStatusClaimed
		claim.TestStatus = "untested"
		claim.QualificationStatus = nil
		items = append(items, claim)
	}
	return items, rows.Err()
}

func nullableEndpoint(value string) any {
	if value == "" {
		return nil
	}
	return value
}
func stringValue(value *string) string {
	if value == nil {
		return ""
	}
	return *value
}

func mapDatabaseError(err error) error {
	if err == nil {
		return nil
	}
	if errors.Is(err, pgx.ErrNoRows) {
		return identity.ErrNotFound
	}
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) && pgErr.Code == "23505" {
		return identity.ErrConflict
	}
	return err
}
