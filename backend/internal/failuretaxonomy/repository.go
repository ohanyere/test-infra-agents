package failuretaxonomy

import (
	"context"
	"errors"

	"github.com/evalplane/evalplane/backend/internal/identity"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

type DBTX interface {
	Query(context.Context, string, ...any) (pgx.Rows, error)
	QueryRow(context.Context, string, ...any) pgx.Row
}
type Repository struct{}

func NewRepository() *Repository { return &Repository{} }
func (r *Repository) List(ctx context.Context, db DBTX, workspaceID, executionID string) ([]Occurrence, error) {
	var exists bool
	if err := db.QueryRow(ctx, `SELECT true FROM suite_executions WHERE id=$1 AND workspace_id=$2`, executionID, workspaceID).Scan(&exists); err != nil {
		return nil, mapDB(err)
	}
	rows, err := db.Query(ctx, `
		SELECT f.id::text,r.id,r.agent_version_id,seb.scenario_version_id,
			CASE WHEN f.evidence->>'code' IN ('criterion_failed','criterion_inconclusive') AND COALESCE(f.evidence->>'criterion_key','')<>'' THEN f.evidence->>'code'||':'||f.evidence->>'criterion_key' ELSE f.evidence->>'code' END,
			EXISTS(SELECT 1 FROM run_events pi WHERE pi.run_id=r.id AND pi.payload->>'classification'='platform' AND pi.payload->>'code'<>'cleanup_completed')
		FROM suite_executions se JOIN suite_execution_batches seb ON seb.suite_execution_id=se.id JOIN runs r ON r.batch_id=seb.batch_id JOIN evaluations e ON e.run_id=r.id JOIN evaluation_findings f ON f.evaluation_id=e.id
		WHERE se.id=$1 AND se.workspace_id=$2 AND COALESCE(f.evidence->>'code','')<>''
		UNION ALL
		SELECT 'event:'||re.id::text,r.id,r.agent_version_id,seb.scenario_version_id,re.payload->>'code',(re.payload->>'classification'='platform')
		FROM suite_executions se JOIN suite_execution_batches seb ON seb.suite_execution_id=se.id JOIN runs r ON r.batch_id=seb.batch_id JOIN run_events re ON re.run_id=r.id
		WHERE se.id=$1 AND se.workspace_id=$2 AND COALESCE(re.payload->>'code','')<>'' AND re.payload->>'code'<>'cleanup_completed'
		  AND (re.payload->>'classification'='platform' OR re.payload->>'code' IN ('worker_lease_expired','run_abandoned','worker_recovery_cleanup_failed','runtime_failure','telemetry_unavailable','agent_gateway_timeout','invalid_agent_response','action_failed','run_timeout','run_stopped','max_agent_steps_reached','invalid_action_parameters','unsupported_action'))
		  AND NOT EXISTS(SELECT 1 FROM evaluations e JOIN evaluation_findings f ON f.evaluation_id=e.id WHERE e.run_id=r.id AND f.evidence->>'code'=re.payload->>'code')
		ORDER BY 3,4,5,1`, executionID, workspaceID)
	if err != nil {
		return nil, mapDB(err)
	}
	defer rows.Close()
	out := []Occurrence{}
	for rows.Next() {
		var row Occurrence
		if err := rows.Scan(&row.SourceID, &row.RunID, &row.AgentVersionID, &row.ScenarioVersionID, &row.Code, &row.PlatformInvalid); err != nil {
			return nil, err
		}
		out = append(out, row)
	}
	return out, mapDB(rows.Err())
}
func mapDB(err error) error {
	if err == nil {
		return nil
	}
	if errors.Is(err, pgx.ErrNoRows) {
		return identity.ErrNotFound
	}
	var p *pgconn.PgError
	if errors.As(err, &p) && p.Code == "23503" {
		return identity.ErrNotFound
	}
	return err
}
