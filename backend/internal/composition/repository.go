package composition

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"github.com/evalplane/evalplane/backend/internal/identity"
	"github.com/evalplane/evalplane/backend/internal/worldspec"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

type DBTX interface {
	Exec(context.Context, string, ...any) (pgconn.CommandTag, error)
	QueryRow(context.Context, string, ...any) pgx.Row
}
type StoredWorldSpec struct {
	ID, WorkspaceID, ScenarioVersionID, ContractVersion, CompilerID, SpecHash string
	Spec                                                                      worldspec.Spec
	CreatedAt                                                                 time.Time
}
type StoredPlan struct {
	ID, WorkspaceID, WorldSpecID string
	Plan                         Plan
	CreatedAt                    time.Time
}
type Repository struct{}

func NewRepository() *Repository { return &Repository{} }
func (r *Repository) InsertWorldSpec(ctx context.Context, db DBTX, workspaceID, scenarioVersionID string, s worldspec.Spec) (StoredWorldSpec, error) {
	n, err := worldspec.Normalize(s)
	if err != nil {
		return StoredWorldSpec{}, err
	}
	h, _ := worldspec.Hash(n)
	doc, _ := json.Marshal(n)
	var out StoredWorldSpec
	var raw []byte
	err = db.QueryRow(ctx, `INSERT INTO world_specs(workspace_id,scenario_version_id,contract_version,compiler_id,normalized_spec,spec_hash) VALUES(NULLIF($1,'')::uuid,$2,$3,$4,$5,$6) RETURNING id,COALESCE(workspace_id::text,''),scenario_version_id,contract_version,compiler_id,normalized_spec,spec_hash,created_at`, workspaceID, scenarioVersionID, n.ContractVersion, n.CompilerID, doc, h).Scan(&out.ID, &out.WorkspaceID, &out.ScenarioVersionID, &out.ContractVersion, &out.CompilerID, &raw, &out.SpecHash, &out.CreatedAt)
	if err == nil {
		err = json.Unmarshal(raw, &out.Spec)
	}
	return out, mapDB(err)
}
func (r *Repository) GetWorldSpec(ctx context.Context, db DBTX, workspaceID, scenarioVersionID string) (StoredWorldSpec, error) {
	var out StoredWorldSpec
	var raw []byte
	err := db.QueryRow(ctx, `SELECT id,COALESCE(workspace_id::text,''),scenario_version_id,contract_version,compiler_id,normalized_spec,spec_hash,created_at FROM world_specs WHERE scenario_version_id=$1 AND (workspace_id=$2 OR workspace_id IS NULL)`, scenarioVersionID, workspaceID).Scan(&out.ID, &out.WorkspaceID, &out.ScenarioVersionID, &out.ContractVersion, &out.CompilerID, &raw, &out.SpecHash, &out.CreatedAt)
	if err == nil {
		err = json.Unmarshal(raw, &out.Spec)
	}
	return out, mapDB(err)
}
func (r *Repository) InsertPlan(ctx context.Context, db DBTX, workspaceID, worldSpecID string, p Plan) (StoredPlan, error) {
	h, err := Hash(p)
	if err != nil {
		return StoredPlan{}, err
	}
	if p.PlanHash != "" && p.PlanHash != h {
		return StoredPlan{}, errors.New("composition plan hash mismatch")
	}
	p.PlanHash = h
	doc, _ := json.Marshal(p)
	var out StoredPlan
	var raw []byte
	err = db.QueryRow(ctx, `INSERT INTO composition_plans(workspace_id,scenario_version_id,world_spec_id,world_spec_hash,contract_version,normalized_plan,plan_hash,profile_id,isolation_tier,minimum_fidelity,selected_fidelity) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11) RETURNING id,workspace_id,world_spec_id,normalized_plan,created_at`, workspaceID, p.ScenarioVersionID, worldSpecID, p.WorldSpecHash, p.ContractVersion, doc, p.PlanHash, p.ProfileID, p.IsolationTier, p.MinimumFidelity, p.SelectedFidelity).Scan(&out.ID, &out.WorkspaceID, &out.WorldSpecID, &raw, &out.CreatedAt)
	if err == nil {
		err = json.Unmarshal(raw, &out.Plan)
	}
	return out, mapDB(err)
}
func (r *Repository) GetPlan(ctx context.Context, db DBTX, workspaceID, planID string) (StoredPlan, error) {
	var out StoredPlan
	var raw []byte
	err := db.QueryRow(ctx, `SELECT id,workspace_id,world_spec_id,normalized_plan,created_at FROM composition_plans WHERE id=$1 AND workspace_id=$2`, planID, workspaceID).Scan(&out.ID, &out.WorkspaceID, &out.WorldSpecID, &raw, &out.CreatedAt)
	if err == nil {
		err = json.Unmarshal(raw, &out.Plan)
	}
	return out, mapDB(err)
}
func mapDB(err error) error {
	if errors.Is(err, pgx.ErrNoRows) {
		return identity.ErrNotFound
	}
	if err == nil {
		return nil
	}
	var p *pgconn.PgError
	if errors.As(err, &p) && p.Code == "23505" {
		return identity.ErrConflict
	}
	return err
}
