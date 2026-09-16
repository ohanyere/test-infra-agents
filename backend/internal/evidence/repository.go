package evidence

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

func (r *Repository) LockRun(ctx context.Context, db DBTX, runID string) error {
	var id string
	err := db.QueryRow(ctx, `SELECT id FROM runs WHERE id=$1 FOR UPDATE`, runID).Scan(&id)
	return mapDB(err)
}

func (r *Repository) NextEventSequence(ctx context.Context, db DBTX, runID string) (int64, error) {
	var sequence int64
	err := db.QueryRow(ctx, `SELECT COALESCE(MAX(sequence),0)+1 FROM run_events WHERE run_id=$1`, runID).Scan(&sequence)
	return sequence, mapDB(err)
}

func (r *Repository) FindRunEventBySummary(ctx context.Context, db DBTX, runID, summary string) (RunEvent, bool, error) {
	var out RunEvent
	err := db.QueryRow(ctx, `SELECT id,run_id,sequence,event_type,occurred_at,summary,payload,created_at FROM run_events WHERE run_id=$1 AND summary=$2 ORDER BY sequence LIMIT 1`, runID, summary).Scan(&out.ID, &out.RunID, &out.Sequence, &out.EventType, &out.OccurredAt, &out.Summary, &out.Payload, &out.CreatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return out, false, nil
	}
	return out, err == nil, mapDB(err)
}

func (r *Repository) InsertRunEvent(ctx context.Context, db DBTX, in AppendRunEventInput, sequence int64) (RunEvent, error) {
	var out RunEvent
	err := db.QueryRow(ctx, `INSERT INTO run_events(run_id,sequence,event_type,occurred_at,summary,payload) VALUES($1,$2,$3,$4,$5,$6) RETURNING id,run_id,sequence,event_type,occurred_at,summary,payload,created_at`, in.RunID, sequence, in.EventType, in.OccurredAt, in.Summary, in.Payload).Scan(&out.ID, &out.RunID, &out.Sequence, &out.EventType, &out.OccurredAt, &out.Summary, &out.Payload, &out.CreatedAt)
	return out, mapDB(err)
}

func (r *Repository) EnsureRunWorkspace(ctx context.Context, db DBTX, workspaceID, runID string) error {
	var id string
	err := db.QueryRow(ctx, `SELECT r.id FROM runs r JOIN evaluation_batches b ON b.id=r.batch_id WHERE r.id=$1 AND b.workspace_id=$2`, runID, workspaceID).Scan(&id)
	return mapDB(err)
}

func (r *Repository) ListRunEvents(ctx context.Context, db DBTX, workspaceID, runID string, filters EventFilters) ([]RunEvent, error) {
	args := []any{runID, workspaceID}
	filter := ""
	if filters.EventType != "all" {
		args = append(args, filters.EventType)
		filter = fmt.Sprintf(" AND e.event_type=$%d", len(args))
	}
	args = append(args, filters.Pagination.Limit, filters.Pagination.Offset)
	query := fmt.Sprintf(`SELECT e.id,e.run_id,e.sequence,e.event_type,e.occurred_at,e.summary,e.payload,e.created_at FROM run_events e JOIN runs r ON r.id=e.run_id JOIN evaluation_batches b ON b.id=r.batch_id WHERE e.run_id=$1 AND b.workspace_id=$2%s ORDER BY e.sequence ASC LIMIT $%d OFFSET $%d`, filter, len(args)-1, len(args))
	rows, err := db.Query(ctx, query, args...)
	if err != nil {
		return nil, mapDB(err)
	}
	defer rows.Close()
	items := make([]RunEvent, 0)
	for rows.Next() {
		var out RunEvent
		if err = rows.Scan(&out.ID, &out.RunID, &out.Sequence, &out.EventType, &out.OccurredAt, &out.Summary, &out.Payload, &out.CreatedAt); err != nil {
			return nil, mapDB(err)
		}
		items = append(items, out)
	}
	return items, mapDB(rows.Err())
}

func (r *Repository) EventBelongsToRun(ctx context.Context, db DBTX, runID string, eventID int64) (bool, error) {
	var belongs bool
	err := db.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM run_events WHERE id=$1 AND run_id=$2)`, eventID, runID).Scan(&belongs)
	return belongs, mapDB(err)
}

func (r *Repository) InsertAgentAction(ctx context.Context, db DBTX, in RecordAgentActionInput) (AgentAction, error) {
	var out AgentAction
	err := db.QueryRow(ctx, `INSERT INTO agent_actions(run_id,run_event_id,action_type,target_type,target_ref,status,authorized,request_payload,result_payload,started_at,completed_at) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11) RETURNING id,run_id,run_event_id,action_type,target_type,target_ref,status,authorized,request_payload,result_payload,started_at,completed_at,created_at`, in.RunID, in.RunEventID, in.ActionType, in.TargetType, in.TargetRef, in.Status, in.Authorized, in.RequestPayload, in.ResultPayload, in.StartedAt, in.CompletedAt).Scan(&out.ID, &out.RunID, &out.RunEventID, &out.ActionType, &out.TargetType, &out.TargetRef, &out.Status, &out.Authorized, &out.RequestPayload, &out.ResultPayload, &out.StartedAt, &out.CompletedAt, &out.CreatedAt)
	return out, mapDB(err)
}

func (r *Repository) ListAgentActions(ctx context.Context, db DBTX, workspaceID, runID string, filters ActionFilters) ([]AgentAction, error) {
	args := []any{runID, workspaceID}
	filter := ""
	if filters.Status != "all" {
		args = append(args, filters.Status)
		filter = fmt.Sprintf(" AND a.status=$%d", len(args))
	}
	args = append(args, filters.Pagination.Limit, filters.Pagination.Offset)
	query := fmt.Sprintf(`SELECT a.id,a.run_id,a.run_event_id,a.action_type,a.target_type,a.target_ref,a.status,a.authorized,a.request_payload,a.result_payload,a.started_at,a.completed_at,a.created_at FROM agent_actions a JOIN runs r ON r.id=a.run_id JOIN evaluation_batches b ON b.id=r.batch_id WHERE a.run_id=$1 AND b.workspace_id=$2%s ORDER BY a.created_at ASC,a.id ASC LIMIT $%d OFFSET $%d`, filter, len(args)-1, len(args))
	rows, err := db.Query(ctx, query, args...)
	if err != nil {
		return nil, mapDB(err)
	}
	defer rows.Close()
	items := make([]AgentAction, 0)
	for rows.Next() {
		var out AgentAction
		if err = rows.Scan(&out.ID, &out.RunID, &out.RunEventID, &out.ActionType, &out.TargetType, &out.TargetRef, &out.Status, &out.Authorized, &out.RequestPayload, &out.ResultPayload, &out.StartedAt, &out.CompletedAt, &out.CreatedAt); err != nil {
			return nil, mapDB(err)
		}
		items = append(items, out)
	}
	return items, mapDB(rows.Err())
}

func (r *Repository) LockRunForEvaluation(ctx context.Context, db DBTX, runID string) (string, string, error) {
	var status, scenarioVersionID string
	err := db.QueryRow(ctx, `SELECT r.status,b.scenario_version_id FROM runs r JOIN evaluation_batches b ON b.id=r.batch_id WHERE r.id=$1 FOR UPDATE OF r`, runID).Scan(&status, &scenarioVersionID)
	return status, scenarioVersionID, mapDB(err)
}

func (r *Repository) ListCriterionIDs(ctx context.Context, db DBTX, scenarioVersionID string) ([]string, error) {
	rows, err := db.Query(ctx, `SELECT id FROM scenario_criteria WHERE scenario_version_id=$1 ORDER BY id`, scenarioVersionID)
	if err != nil {
		return nil, mapDB(err)
	}
	defer rows.Close()
	ids := make([]string, 0)
	for rows.Next() {
		var id string
		if err = rows.Scan(&id); err != nil {
			return nil, mapDB(err)
		}
		ids = append(ids, id)
	}
	return ids, mapDB(rows.Err())
}

func (r *Repository) InsertEvaluation(ctx context.Context, db DBTX, in CreateEvaluationInput) (Evaluation, error) {
	var out Evaluation
	if in.EvaluatorVersion == "" {
		in.EvaluatorVersion = "deterministic-evaluator:v1"
	}
	err := db.QueryRow(ctx, `INSERT INTO evaluations(run_id,result,score,summary,recovery_duration_ms,availability_pct,peak_error_rate_pct,safety_violation_count,secondary_incident_count,agent_action_count,completed_at,evaluator_version) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12) RETURNING id,run_id,result,evaluator_version,score,summary,recovery_duration_ms,availability_pct,peak_error_rate_pct,safety_violation_count,secondary_incident_count,agent_action_count,created_at,completed_at`, in.RunID, in.Result, in.Score, in.Summary, in.RecoveryDurationMS, in.AvailabilityPct, in.PeakErrorRatePct, in.SafetyViolationCount, in.SecondaryIncidentCount, in.AgentActionCount, in.CompletedAt, in.EvaluatorVersion).Scan(&out.ID, &out.RunID, &out.Result, &out.EvaluatorVersion, &out.Score, &out.Summary, &out.RecoveryDurationMS, &out.AvailabilityPct, &out.PeakErrorRatePct, &out.SafetyViolationCount, &out.SecondaryIncidentCount, &out.AgentActionCount, &out.CreatedAt, &out.CompletedAt)
	return out, mapDB(err)
}

func (r *Repository) InsertCriterionResult(ctx context.Context, db DBTX, evaluationID string, in CriterionResultInput) error {
	_, err := db.Exec(ctx, `INSERT INTO evaluation_criterion_results(evaluation_id,scenario_criterion_id,passed,observed_value,evidence) VALUES($1,$2,$3,$4,$5)`, evaluationID, in.ScenarioCriterionID, in.Passed, in.ObservedValue, in.Evidence)
	return mapDB(err)
}

func (r *Repository) InsertFinding(ctx context.Context, db DBTX, evaluationID string, in FindingInput) error {
	_, err := db.Exec(ctx, `INSERT INTO evaluation_findings(evaluation_id,kind,summary,evidence,sort_order) VALUES($1,$2,$3,$4,$5)`, evaluationID, in.Kind, in.Summary, in.Evidence, in.SortOrder)
	return mapDB(err)
}

func (r *Repository) GetEvaluation(ctx context.Context, db DBTX, workspaceID, runID string) (Evaluation, error) {
	var out Evaluation
	err := db.QueryRow(ctx, `SELECT e.id,e.run_id,e.result,e.evaluator_version,e.score,e.summary,e.recovery_duration_ms,e.availability_pct,e.peak_error_rate_pct,e.safety_violation_count,e.secondary_incident_count,e.agent_action_count,e.created_at,e.completed_at FROM evaluations e JOIN runs r ON r.id=e.run_id JOIN evaluation_batches b ON b.id=r.batch_id WHERE e.run_id=$1 AND b.workspace_id=$2`, runID, workspaceID).Scan(&out.ID, &out.RunID, &out.Result, &out.EvaluatorVersion, &out.Score, &out.Summary, &out.RecoveryDurationMS, &out.AvailabilityPct, &out.PeakErrorRatePct, &out.SafetyViolationCount, &out.SecondaryIncidentCount, &out.AgentActionCount, &out.CreatedAt, &out.CompletedAt)
	return out, mapDB(err)
}

func (r *Repository) ListEvaluationCriteria(ctx context.Context, db DBTX, evaluationID string) ([]CriterionResult, error) {
	rows, err := db.Query(ctx, `SELECT cr.id,cr.scenario_criterion_id,c.key,c.name,c.metric_key,c.operator,c.target_value,c.unit,cr.passed,cr.observed_value,cr.evidence,cr.created_at FROM evaluation_criterion_results cr JOIN scenario_criteria c ON c.id=cr.scenario_criterion_id WHERE cr.evaluation_id=$1 ORDER BY c.sort_order ASC,c.id ASC`, evaluationID)
	if err != nil {
		return nil, mapDB(err)
	}
	defer rows.Close()
	items := make([]CriterionResult, 0)
	for rows.Next() {
		var out CriterionResult
		if err = rows.Scan(&out.ID, &out.ScenarioCriterionID, &out.Key, &out.Name, &out.MetricKey, &out.Operator, &out.TargetValue, &out.Unit, &out.Passed, &out.ObservedValue, &out.Evidence, &out.CreatedAt); err != nil {
			return nil, mapDB(err)
		}
		items = append(items, out)
	}
	return items, mapDB(rows.Err())
}

func (r *Repository) ListEvaluationFindings(ctx context.Context, db DBTX, evaluationID string) ([]Finding, error) {
	rows, err := db.Query(ctx, `SELECT id,kind,summary,evidence,sort_order,created_at FROM evaluation_findings WHERE evaluation_id=$1 ORDER BY sort_order ASC,created_at ASC,id ASC`, evaluationID)
	if err != nil {
		return nil, mapDB(err)
	}
	defer rows.Close()
	items := make([]Finding, 0)
	for rows.Next() {
		var out Finding
		if err = rows.Scan(&out.ID, &out.Kind, &out.Summary, &out.Evidence, &out.SortOrder, &out.CreatedAt); err != nil {
			return nil, mapDB(err)
		}
		items = append(items, out)
	}
	return items, mapDB(rows.Err())
}

func mapDB(err error) error {
	if err == nil {
		return nil
	}
	if errors.Is(err, pgx.ErrNoRows) {
		return identity.ErrNotFound
	}
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) {
		switch pgErr.Code {
		case "23505":
			return identity.ErrConflict
		case "23503":
			return identity.ErrNotFound
		}
	}
	return fmt.Errorf("evidence database operation: %w", err)
}
