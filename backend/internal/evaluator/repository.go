package evaluator

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/evalplane/evalplane/backend/internal/evidence"
	"github.com/evalplane/evalplane/backend/internal/identity"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
)

type Repository struct{ pool *pgxpool.Pool }

func NewRepository(pool *pgxpool.Pool) *Repository { return &Repository{pool: pool} }

func (r *Repository) LoadEvaluationInput(ctx context.Context, workspaceID, runID string) (EvaluationInput, error) {
	var input EvaluationInput
	var requirementsJSON, criteriaJSON []byte
	err := r.pool.QueryRow(ctx, `SELECT r.id,r.batch_id,r.agent_version_id,r.agent_implementation_identity,r.attempt_number,r.status,r.environment_ref,r.created_at,r.started_at,r.completed_at,sv.id,sv.version,sv.description,sv.category,sv.difficulty,sv.duration_seconds,sv.environment,sv.conditions,sv.observables,sv.expected_behaviors,sv.forbidden_behaviors,sv.authoring_method,sv.agent_task,sv.agent_visible_context,sv.allowed_actions,sv.allowed_resources,sv.ground_truth,sv.execution_spec,sv.source_metadata,sv.evidence_requirements,sv.created_at,COALESCE((SELECT jsonb_agg(jsonb_build_object('id',c.id,'key',c.key,'name',c.name,'metric_key',c.metric_key,'operator',c.operator,'target_value',c.target_value,'unit',c.unit,'sort_order',c.sort_order,'created_at',c.created_at) ORDER BY c.sort_order,c.id) FROM scenario_criteria c WHERE c.scenario_version_id=sv.id),'[]'::jsonb) FROM runs r JOIN evaluation_batches b ON b.id=r.batch_id JOIN scenario_versions sv ON sv.id=b.scenario_version_id WHERE r.id=$1 AND b.workspace_id=$2`, runID, workspaceID).Scan(
		&input.Run.ID, &input.Run.BatchID, &input.Run.Agent.AgentVersionID, &input.Run.AgentImplementationIdentity, &input.Run.AttemptNumber, &input.Run.Status, &input.Run.EnvironmentRef, &input.Run.CreatedAt, &input.Run.StartedAt, &input.Run.CompletedAt,
		&input.ScenarioVersion.ID, &input.ScenarioVersion.Version, &input.ScenarioVersion.Description, &input.ScenarioVersion.Category, &input.ScenarioVersion.Difficulty, &input.ScenarioVersion.DurationSeconds, &input.ScenarioVersion.Environment, &input.ScenarioVersion.Conditions, &input.ScenarioVersion.Observables, &input.ScenarioVersion.ExpectedBehaviors, &input.ScenarioVersion.ForbiddenBehaviors, &input.ScenarioVersion.AuthoringMethod, &input.ScenarioVersion.AgentTask, &input.ScenarioVersion.AgentVisibleContext, &input.ScenarioVersion.AllowedActions, &input.ScenarioVersion.AllowedResources, &input.ScenarioVersion.GroundTruth, &input.ScenarioVersion.ExecutionSpec, &input.ScenarioVersion.SourceMetadata, &requirementsJSON, &input.ScenarioVersion.CreatedAt, &criteriaJSON)
	if err != nil {
		return input, mapRepositoryError(err)
	}
	if err = json.Unmarshal(requirementsJSON, &input.ScenarioVersion.EvidenceRequirements); err != nil {
		return input, err
	}
	if err = json.Unmarshal(criteriaJSON, &input.ScenarioVersion.Criteria); err != nil {
		return input, err
	}
	input.Events, err = r.loadEvents(ctx, workspaceID, runID)
	if err != nil {
		return input, err
	}
	input.Actions, err = r.loadActions(ctx, workspaceID, runID)
	return input, err
}

func (r *Repository) loadEvents(ctx context.Context, workspaceID, runID string) ([]evidence.RunEvent, error) {
	rows, err := r.pool.Query(ctx, `SELECT e.id,e.run_id,e.sequence,e.event_type,e.occurred_at,e.summary,e.payload,e.created_at FROM run_events e JOIN runs r ON r.id=e.run_id JOIN evaluation_batches b ON b.id=r.batch_id WHERE e.run_id=$1 AND b.workspace_id=$2 ORDER BY e.sequence,e.id`, runID, workspaceID)
	if err != nil {
		return nil, mapRepositoryError(err)
	}
	defer rows.Close()
	items := []evidence.RunEvent{}
	for rows.Next() {
		var item evidence.RunEvent
		if err = rows.Scan(&item.ID, &item.RunID, &item.Sequence, &item.EventType, &item.OccurredAt, &item.Summary, &item.Payload, &item.CreatedAt); err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func (r *Repository) loadActions(ctx context.Context, workspaceID, runID string) ([]evidence.AgentAction, error) {
	rows, err := r.pool.Query(ctx, `SELECT a.id,a.run_id,a.run_event_id,a.action_type,a.target_type,a.target_ref,a.status,a.authorized,a.request_payload,a.result_payload,a.started_at,a.completed_at,a.created_at FROM agent_actions a JOIN runs r ON r.id=a.run_id JOIN evaluation_batches b ON b.id=r.batch_id WHERE a.run_id=$1 AND b.workspace_id=$2 ORDER BY a.created_at,a.id`, runID, workspaceID)
	if err != nil {
		return nil, mapRepositoryError(err)
	}
	defer rows.Close()
	items := []evidence.AgentAction{}
	for rows.Next() {
		var item evidence.AgentAction
		if err = rows.Scan(&item.ID, &item.RunID, &item.RunEventID, &item.ActionType, &item.TargetType, &item.TargetRef, &item.Status, &item.Authorized, &item.RequestPayload, &item.ResultPayload, &item.StartedAt, &item.CompletedAt, &item.CreatedAt); err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func (r *Repository) ReconcileEvaluation(ctx context.Context, workspaceID string, input EvaluationInput, result EvaluationResult) (err error) {
	tx, err := r.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return err
	}
	defer func() {
		if err != nil {
			_ = tx.Rollback(ctx)
		}
	}()
	var status, scenarioVersionID string
	if err = tx.QueryRow(ctx, `SELECT r.status,b.scenario_version_id FROM runs r JOIN evaluation_batches b ON b.id=r.batch_id WHERE r.id=$1 AND b.workspace_id=$2 FOR UPDATE OF r`, input.Run.ID, workspaceID).Scan(&status, &scenarioVersionID); err != nil {
		return mapRepositoryError(err)
	}
	if status != "completed" && status != "failed" && status != "stopped" {
		return ErrRunNotTerminal
	}
	if scenarioVersionID != result.ScenarioVersionID {
		return fmt.Errorf("%w: run scenario version changed", identity.ErrConflict)
	}
	var evaluationID string
	summary, _ := json.Marshal(result.SummaryFacts)
	err = tx.QueryRow(ctx, `INSERT INTO evaluations(run_id,result,score,summary,availability_pct,peak_error_rate_pct,safety_violation_count,secondary_incident_count,agent_action_count,completed_at,evaluator_version) VALUES($1,$2,NULL,$3,$4,$5,$6,$7,$8,$9,$10) ON CONFLICT(run_id) DO UPDATE SET result=EXCLUDED.result,score=NULL,summary=EXCLUDED.summary,recovery_duration_ms=NULL,availability_pct=EXCLUDED.availability_pct,peak_error_rate_pct=EXCLUDED.peak_error_rate_pct,safety_violation_count=EXCLUDED.safety_violation_count,secondary_incident_count=EXCLUDED.secondary_incident_count,agent_action_count=EXCLUDED.agent_action_count,completed_at=EXCLUDED.completed_at WHERE evaluations.evaluator_version=EXCLUDED.evaluator_version RETURNING id`, input.Run.ID, result.Status, string(summary), result.AvailabilityPct, result.PeakErrorRatePct, result.SafetyViolationCount, result.SecondaryIncidentCount, result.AgentActionCount, input.Run.CompletedAt, ContractVersion).Scan(&evaluationID)
	if errors.Is(err, pgx.ErrNoRows) {
		return fmt.Errorf("%w: evaluation uses a different evaluator contract", identity.ErrConflict)
	}
	if err != nil {
		return mapRepositoryError(err)
	}
	if _, err = tx.Exec(ctx, `DELETE FROM evaluation_criterion_results WHERE evaluation_id=$1`, evaluationID); err != nil {
		return err
	}
	if _, err = tx.Exec(ctx, `DELETE FROM evaluation_findings WHERE evaluation_id=$1`, evaluationID); err != nil {
		return err
	}
	for _, criterion := range result.CriterionResults {
		if _, err = tx.Exec(ctx, `INSERT INTO evaluation_criterion_results(evaluation_id,scenario_criterion_id,passed,observed_value,evidence) VALUES($1,$2,$3,$4,$5)`, evaluationID, criterion.ScenarioCriterionID, criterion.Status == CriterionPassed, criterion.ObservedValue, criterionEvidence(criterion)); err != nil {
			return mapRepositoryError(err)
		}
	}
	for index, finding := range result.Findings {
		if _, err = tx.Exec(ctx, `INSERT INTO evaluation_findings(evaluation_id,kind,summary,evidence,sort_order) VALUES($1,$2,$3,$4,$5)`, evaluationID, findingKind(finding), finding.Message, findingEvidence(finding), index); err != nil {
			return mapRepositoryError(err)
		}
	}
	return tx.Commit(ctx)
}

func mapRepositoryError(err error) error {
	if errors.Is(err, pgx.ErrNoRows) {
		return identity.ErrNotFound
	}
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) && pgErr.Code == "23505" {
		return identity.ErrConflict
	}
	return err
}

var _ Store = (*Repository)(nil)
