package evaluationbatch

import (
	"context"
	"errors"
	"fmt"
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
func (r *Repository) LoadUsableScenario(ctx context.Context, db DBTX, w, versionID string) (ScenarioRef, error) {
	var v ScenarioRef
	e := db.QueryRow(ctx, `SELECT s.id,sv.id,s.name,sv.version FROM scenario_versions sv JOIN scenarios s ON s.id=sv.scenario_id WHERE sv.id=$1 AND s.status='active' AND (s.owner_workspace_id=$2 OR (s.kind='built_in' AND s.visibility='public'))`, versionID, w).Scan(&v.ScenarioID, &v.ScenarioVersionID, &v.Name, &v.Version)
	return v, mapDB(e)
}
func (r *Repository) LoadUsableAgents(ctx context.Context, db DBTX, w string, versionIDs []string) ([]AgentRef, error) {
	rows, e := db.Query(ctx, `SELECT a.id,av.id,a.name,av.version_label FROM unnest($1::uuid[]) WITH ORDINALITY AS selected(version_id,position) JOIN agent_versions av ON av.id=selected.version_id JOIN agents a ON a.id=av.agent_id WHERE a.status='active' AND (a.owner_workspace_id=$2 OR a.visibility='public') ORDER BY selected.position`, versionIDs, w)
	if e != nil {
		return nil, mapDB(e)
	}
	defer rows.Close()
	agents := make([]AgentRef, 0, len(versionIDs))
	for rows.Next() {
		var v AgentRef
		if e = rows.Scan(&v.AgentID, &v.AgentVersionID, &v.Name, &v.VersionLabel); e != nil {
			return nil, e
		}
		agents = append(agents, v)
	}
	if e = rows.Err(); e != nil {
		return nil, mapDB(e)
	}
	if len(agents) != len(versionIDs) {
		return nil, identity.ErrNotFound
	}
	return agents, nil
}
func (r *Repository) CreateBatch(ctx context.Context, db DBTX, w, user string, in CreateInput) (Batch, error) {
	var v Batch
	e := db.QueryRow(ctx, `INSERT INTO evaluation_batches(workspace_id,scenario_version_id,created_by,status,runs_per_agent,environment_mode,config)VALUES($1,$2,$3,'draft',$4,$5,$6)RETURNING id,workspace_id,status,runs_per_agent,environment_mode,config,created_by,created_at,started_at,completed_at`, w, in.ScenarioVersionID, user, in.RunsPerAgent, in.EnvironmentMode, in.Config).Scan(&v.ID, &v.WorkspaceID, &v.Status, &v.RunsPerAgent, &v.EnvironmentMode, &v.Config, &v.CreatedBy, &v.CreatedAt, &v.StartedAt, &v.CompletedAt)
	return v, mapDB(e)
}
func (r *Repository) AddAgentsAndRuns(ctx context.Context, db DBTX, batchID string, agentVersionIDs []string, repetitions int) error {
	if _, e := db.Exec(ctx, `INSERT INTO evaluation_batch_agents(batch_id,agent_version_id) SELECT $1,agent_version_id FROM unnest($2::uuid[]) AS selected(agent_version_id)`, batchID, agentVersionIDs); e != nil {
		return mapDB(e)
	}
	if _, e := db.Exec(ctx, `INSERT INTO runs(batch_id,agent_version_id,attempt_number,status) SELECT $1,agent_version_id,attempt_number,'queued' FROM unnest($2::uuid[]) AS selected(agent_version_id) CROSS JOIN generate_series(1,$3) AS attempts(attempt_number)`, batchID, agentVersionIDs, repetitions); e != nil {
		return mapDB(e)
	}
	return nil
}

const batchSelect = `SELECT b.id,b.workspace_id,b.status,b.runs_per_agent,b.environment_mode,b.config,b.created_by,b.created_at,b.started_at,b.completed_at,s.id,sv.id,s.name,sv.version,(SELECT count(*) FROM evaluation_batch_agents ba WHERE ba.batch_id=b.id),(SELECT count(*) FROM runs r WHERE r.batch_id=b.id) FROM evaluation_batches b JOIN scenario_versions sv ON sv.id=b.scenario_version_id JOIN scenarios s ON s.id=sv.scenario_id`

func (r *Repository) GetBatch(ctx context.Context, db DBTX, w, id string) (Batch, error) {
	v, e := scanBatch(db.QueryRow(ctx, batchSelect+` WHERE b.id=$1 AND b.workspace_id=$2`, id, w))
	if e != nil {
		return v, e
	}
	v.Agents, e = r.ListAgents(ctx, db, id)
	return v, e
}
func (r *Repository) ListBatches(ctx context.Context, db DBTX, w string, f Filters) ([]Batch, error) {
	args := []any{w}
	where := "b.workspace_id=$1"
	if f.Status != "all" {
		args = append(args, f.Status)
		where += " AND b.status=$2"
	}
	args = append(args, f.Pagination.Limit, f.Pagination.Offset)
	q := fmt.Sprintf(batchSelect+` WHERE %s ORDER BY b.created_at DESC,b.id DESC LIMIT $%d OFFSET $%d`, where, len(args)-1, len(args))
	rows, e := db.Query(ctx, q, args...)
	if e != nil {
		return nil, mapDB(e)
	}
	defer rows.Close()
	out := []Batch{}
	for rows.Next() {
		v, e := scanBatch(rows)
		if e != nil {
			return nil, e
		}
		out = append(out, v)
	}
	return out, rows.Err()
}
func (r *Repository) ListAgents(ctx context.Context, db DBTX, batchID string) ([]AgentRef, error) {
	rows, e := db.Query(ctx, `SELECT a.id,av.id,a.name,av.version_label FROM evaluation_batch_agents ba JOIN agent_versions av ON av.id=ba.agent_version_id JOIN agents a ON a.id=av.agent_id WHERE ba.batch_id=$1 ORDER BY a.name,av.version_label,av.id`, batchID)
	if e != nil {
		return nil, mapDB(e)
	}
	defer rows.Close()
	out := []AgentRef{}
	for rows.Next() {
		var v AgentRef
		if e = rows.Scan(&v.AgentID, &v.AgentVersionID, &v.Name, &v.VersionLabel); e != nil {
			return nil, e
		}
		out = append(out, v)
	}
	return out, rows.Err()
}
func (r *Repository) ListRuns(ctx context.Context, db DBTX, w, batchID string, f Filters) ([]Run, error) {
	args := []any{batchID, w}
	where := "r.batch_id=$1 AND b.workspace_id=$2"
	if f.Status != "all" {
		args = append(args, f.Status)
		where += fmt.Sprintf(" AND r.status=$%d", len(args))
	}
	args = append(args, f.Pagination.Limit, f.Pagination.Offset)
	q := fmt.Sprintf(`SELECT r.id,r.batch_id,r.attempt_number,r.status,r.environment_ref,r.created_at,r.started_at,r.completed_at,a.id,av.id,a.name,av.version_label FROM runs r JOIN evaluation_batches b ON b.id=r.batch_id JOIN agent_versions av ON av.id=r.agent_version_id JOIN agents a ON a.id=av.agent_id WHERE %s ORDER BY a.id,av.id,r.attempt_number LIMIT $%d OFFSET $%d`, where, len(args)-1, len(args))
	rows, e := db.Query(ctx, q, args...)
	if e != nil {
		return nil, mapDB(e)
	}
	defer rows.Close()
	out := []Run{}
	for rows.Next() {
		v, e := scanRun(rows)
		if e != nil {
			return nil, e
		}
		out = append(out, v)
	}
	return out, rows.Err()
}
func (r *Repository) GetRun(ctx context.Context, db DBTX, w, id string) (Run, error) {
	v := Run{Scenario: &ScenarioRef{}}
	e := db.QueryRow(ctx, `SELECT r.id,r.batch_id,r.attempt_number,r.status,r.environment_ref,r.created_at,r.started_at,r.completed_at,a.id,av.id,a.name,av.version_label,s.id,sv.id,s.name,sv.version FROM runs r JOIN evaluation_batches b ON b.id=r.batch_id JOIN agent_versions av ON av.id=r.agent_version_id JOIN agents a ON a.id=av.agent_id JOIN scenario_versions sv ON sv.id=b.scenario_version_id JOIN scenarios s ON s.id=sv.scenario_id WHERE r.id=$1 AND b.workspace_id=$2`, id, w).Scan(&v.ID, &v.BatchID, &v.AttemptNumber, &v.Status, &v.EnvironmentRef, &v.CreatedAt, &v.StartedAt, &v.CompletedAt, &v.Agent.AgentID, &v.Agent.AgentVersionID, &v.Agent.Name, &v.Agent.VersionLabel, &v.Scenario.ScenarioID, &v.Scenario.ScenarioVersionID, &v.Scenario.Name, &v.Scenario.Version)
	return v, mapDB(e)
}

type scanner interface{ Scan(...any) error }

func scanBatch(row scanner) (Batch, error) {
	var v Batch
	e := row.Scan(&v.ID, &v.WorkspaceID, &v.Status, &v.RunsPerAgent, &v.EnvironmentMode, &v.Config, &v.CreatedBy, &v.CreatedAt, &v.StartedAt, &v.CompletedAt, &v.Scenario.ScenarioID, &v.Scenario.ScenarioVersionID, &v.Scenario.Name, &v.Scenario.Version, &v.AgentCount, &v.RunCount)
	return v, mapDB(e)
}
func scanRun(row scanner) (Run, error) {
	var v Run
	e := row.Scan(&v.ID, &v.BatchID, &v.AttemptNumber, &v.Status, &v.EnvironmentRef, &v.CreatedAt, &v.StartedAt, &v.CompletedAt, &v.Agent.AgentID, &v.Agent.AgentVersionID, &v.Agent.Name, &v.Agent.VersionLabel)
	return v, mapDB(e)
}
func mapDB(e error) error {
	if e == nil {
		return nil
	}
	if errors.Is(e, pgx.ErrNoRows) {
		return identity.ErrNotFound
	}
	var p *pgconn.PgError
	if errors.As(e, &p) {
		if p.Code == "23505" {
			return identity.ErrConflict
		}
		if p.Code == "23503" {
			return identity.ErrNotFound
		}
	}
	return e
}
