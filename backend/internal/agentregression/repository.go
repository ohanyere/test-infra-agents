package agentregression

import (
	"context"
	"encoding/json"
	"errors"

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

func (r *Repository) Create(ctx context.Context, db DBTX, workspaceID, userID string, executionID string, in CreateInput) (Comparison, error) {
	var out Comparison
	err := db.QueryRow(ctx, `
		INSERT INTO agent_regression_comparisons(workspace_id,suite_execution_id,suite_version_id,baseline_agent_version_id,candidate_agent_version_id,created_by)
		SELECT $1,se.id,se.suite_version_id,$3,$4,$5
		FROM suite_executions se
		WHERE se.id=$2 AND se.workspace_id=$1 AND se.suite_version_id=$6
		  AND (SELECT count(*) FROM suite_execution_agents sea WHERE sea.suite_execution_id=se.id)=2
		  AND EXISTS(SELECT 1 FROM suite_execution_agents sea WHERE sea.suite_execution_id=se.id AND sea.agent_version_id=$3)
		  AND EXISTS(SELECT 1 FROM suite_execution_agents sea WHERE sea.suite_execution_id=se.id AND sea.agent_version_id=$4)
		RETURNING id,workspace_id,suite_execution_id,suite_version_id,baseline_agent_version_id,candidate_agent_version_id,created_by,created_at`,
		workspaceID, executionID, in.BaselineAgentVersionID, in.CandidateAgentVersionID, userID, in.SuiteVersionID).
		Scan(&out.ID, &out.WorkspaceID, &out.SuiteExecutionID, &out.SuiteVersionID, &out.BaselineAgentVersionID, &out.CandidateAgentVersionID, &out.CreatedBy, &out.CreatedAt)
	if err != nil {
		return out, mapDB(err)
	}
	return r.Get(ctx, db, workspaceID, out.ID)
}

func (r *Repository) Get(ctx context.Context, db DBTX, workspaceID, comparisonID string) (Comparison, error) {
	var out Comparison
	err := db.QueryRow(ctx, `SELECT c.id,c.workspace_id,c.suite_execution_id,c.suite_version_id,c.baseline_agent_version_id,c.candidate_agent_version_id,c.created_by,c.created_at,ba.id,ba.name,bv.id,bv.version_label,bv.tool_access_mode,ca.id,ca.name,cv.id,cv.version_label,cv.tool_access_mode FROM agent_regression_comparisons c JOIN agent_versions bv ON bv.id=c.baseline_agent_version_id JOIN agents ba ON ba.id=bv.agent_id JOIN agent_versions cv ON cv.id=c.candidate_agent_version_id JOIN agents ca ON ca.id=cv.agent_id WHERE c.id=$1 AND c.workspace_id=$2`, comparisonID, workspaceID).
		Scan(&out.ID, &out.WorkspaceID, &out.SuiteExecutionID, &out.SuiteVersionID, &out.BaselineAgentVersionID, &out.CandidateAgentVersionID, &out.CreatedBy, &out.CreatedAt, &out.Baseline.AgentID, &out.Baseline.AgentName, &out.Baseline.AgentVersionID, &out.Baseline.VersionLabel, &out.Baseline.ToolAccessMode, &out.Candidate.AgentID, &out.Candidate.AgentName, &out.Candidate.AgentVersionID, &out.Candidate.VersionLabel, &out.Candidate.ToolAccessMode)
	return out, mapDB(err)
}

func (r *Repository) ListTrials(ctx context.Context, db DBTX, workspaceID, comparisonID string) ([]TrialRow, error) {
	rows, err := db.Query(ctx, `
		SELECT rv.scenario_version_id,rv.agent_version_id,r.id,COALESCE(e.id::text,''),rv.trial_ordinal,rv.variant_id,rv.variant_seed,r.status,COALESCE(e.result,''),COALESCE(e.evaluator_version,''),
			EXISTS(SELECT 1 FROM run_events re WHERE re.run_id=r.id AND re.payload->>'classification'='platform' AND re.payload->>'code'<>'cleanup_completed') OR
			EXISTS(SELECT 1 FROM evaluation_findings pf WHERE pf.evaluation_id=e.id AND pf.evidence->>'code' IN ('runtime_failure','telemetry_unavailable')),
			COALESCE((SELECT array_agg(sf.evidence->>'code' ORDER BY sf.evidence->>'code') FROM evaluation_findings sf WHERE sf.evaluation_id=e.id AND sf.kind='safety'),'{}'::text[])
		FROM agent_regression_comparisons c
		JOIN suite_execution_run_variants rv ON rv.suite_execution_id=c.suite_execution_id
		JOIN runs r ON r.id=rv.run_id
		LEFT JOIN evaluations e ON e.run_id=r.id
		WHERE c.id=$1 AND c.workspace_id=$2 AND rv.agent_version_id IN (c.baseline_agent_version_id,c.candidate_agent_version_id)
		ORDER BY rv.scenario_version_id,rv.trial_ordinal,rv.agent_version_id`, comparisonID, workspaceID)
	if err != nil {
		return nil, mapDB(err)
	}
	defer rows.Close()
	out := []TrialRow{}
	for rows.Next() {
		var row TrialRow
		if err := rows.Scan(&row.ScenarioVersionID, &row.AgentVersionID, &row.RunID, &row.EvaluationID, &row.TrialOrdinal, &row.VariantID, &row.VariantSeed, &row.RunStatus, &row.EvaluationResult, &row.EvaluatorVersion, &row.PlatformInvalid, &row.SafetyCodes); err != nil {
			return nil, err
		}
		out = append(out, row)
	}
	return out, mapDB(rows.Err())
}

func (r *Repository) ListCriteria(ctx context.Context, db DBTX, workspaceID, comparisonID string) ([]CriterionRow, error) {
	rows, err := db.Query(ctx, `
		SELECT rv.scenario_version_id,rv.agent_version_id,rv.trial_ordinal,rv.variant_id,c.key,c.operator,c.target_value,c.unit,COALESCE(cr.evidence->>'status',CASE WHEN cr.passed THEN 'passed' ELSE 'failed' END),COALESCE(cr.evidence->>'observed_state',''),cr.observed_value
		FROM agent_regression_comparisons arc
		JOIN suite_execution_run_variants rv ON rv.suite_execution_id=arc.suite_execution_id
		JOIN evaluations e ON e.run_id=rv.run_id
		JOIN evaluation_criterion_results cr ON cr.evaluation_id=e.id
		JOIN scenario_criteria c ON c.id=cr.scenario_criterion_id
		WHERE arc.id=$1 AND arc.workspace_id=$2 AND rv.agent_version_id IN (arc.baseline_agent_version_id,arc.candidate_agent_version_id)
		ORDER BY rv.scenario_version_id,rv.trial_ordinal,c.key,rv.agent_version_id`, comparisonID, workspaceID)
	if err != nil {
		return nil, mapDB(err)
	}
	defer rows.Close()
	out := []CriterionRow{}
	for rows.Next() {
		var row CriterionRow
		var target, observed []byte
		if err := rows.Scan(&row.ScenarioVersionID, &row.AgentVersionID, &row.TrialOrdinal, &row.VariantID, &row.CriterionKey, &row.Operator, &target, &row.Unit, &row.Status, &row.ObservedState, &observed); err != nil {
			return nil, err
		}
		row.TargetValue = json.RawMessage(target)
		row.ObservedValue = json.RawMessage(observed)
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
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) {
		switch pgErr.Code {
		case "23505":
			return identity.ErrConflict
		case "23503":
			return identity.ErrNotFound
		case "23514":
			return identity.ErrValidation
		}
	}
	return err
}
