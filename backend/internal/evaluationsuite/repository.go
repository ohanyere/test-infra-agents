package evaluationsuite

import (
	"context"
	"encoding/json"
	"errors"
	"math"
	"strconv"

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

func (r *Repository) CreateSuite(ctx context.Context, db DBTX, workspaceID, userID string, in CreateSuiteInput) (Suite, error) {
	var out Suite
	err := db.QueryRow(ctx, `
		INSERT INTO evaluation_suites(workspace_id,name,created_by)
		VALUES($1,$2,$3)
		RETURNING id,workspace_id,name,created_by,created_at`, workspaceID, in.Name, userID).
		Scan(&out.ID, &out.WorkspaceID, &out.Name, &out.CreatedBy, &out.CreatedAt)
	return out, mapDB(err)
}

func (r *Repository) LockSuite(ctx context.Context, db DBTX, workspaceID, suiteID string) (Suite, error) {
	var out Suite
	err := db.QueryRow(ctx, `
		SELECT id,workspace_id,name,created_by,created_at
		FROM evaluation_suites
		WHERE id=$1 AND workspace_id=$2
		FOR UPDATE`, suiteID, workspaceID).
		Scan(&out.ID, &out.WorkspaceID, &out.Name, &out.CreatedBy, &out.CreatedAt)
	return out, mapDB(err)
}

func (r *Repository) NextVersion(ctx context.Context, db DBTX, suiteID string) (int, error) {
	var version int
	err := db.QueryRow(ctx, `SELECT COALESCE(MAX(version),0)+1 FROM evaluation_suite_versions WHERE suite_id=$1`, suiteID).Scan(&version)
	return version, mapDB(err)
}

func (r *Repository) InsertVersion(ctx context.Context, db DBTX, suiteID, userID string, number int, in CreateVersionInput) (Version, error) {
	var out Version
	err := db.QueryRow(ctx, `
		INSERT INTO evaluation_suite_versions(suite_id,version,description,metadata,created_by)
		VALUES($1,$2,$3,$4,$5)
		RETURNING id,suite_id,version,description,metadata,created_by,created_at`,
		suiteID, number, in.Description, in.Metadata, userID).
		Scan(&out.ID, &out.SuiteID, &out.Version, &out.Description, &out.Metadata, &out.CreatedBy, &out.CreatedAt)
	return out, mapDB(err)
}

func (r *Repository) ScenarioSupportsHiddenVariants(ctx context.Context, db DBTX, scenarioVersionID string) (bool, error) {
	var supported bool
	err := db.QueryRow(ctx, `SELECT COALESCE(execution_spec->>'type','')='traffic_spike' FROM scenario_versions WHERE id=$1`, scenarioVersionID).Scan(&supported)
	return supported, mapDB(err)
}

func (r *Repository) InsertVersionScenarios(ctx context.Context, db DBTX, suiteVersionID string, scenarios []ScenarioEntry) error {
	for _, scenario := range scenarios {
		hiddenVariants, err := json.Marshal(scenario.HiddenVariants)
		if err != nil {
			return err
		}
		if _, err := db.Exec(ctx, `
			INSERT INTO evaluation_suite_version_scenarios(suite_version_id,scenario_version_id,trial_count,sort_order,hidden_variants)
			VALUES($1,$2,$3,$4,$5)`, suiteVersionID, scenario.ScenarioVersionID, scenario.TrialCount, scenario.SortOrder, hiddenVariants); err != nil {
			return mapDB(err)
		}
	}
	return nil
}

func (r *Repository) GetSuite(ctx context.Context, db DBTX, workspaceID, suiteID string) (Suite, error) {
	var out Suite
	err := db.QueryRow(ctx, `
		SELECT id,workspace_id,name,created_by,created_at
		FROM evaluation_suites WHERE id=$1 AND workspace_id=$2`, suiteID, workspaceID).
		Scan(&out.ID, &out.WorkspaceID, &out.Name, &out.CreatedBy, &out.CreatedAt)
	if err != nil {
		return out, mapDB(err)
	}
	latest, err := r.getLatestVersion(ctx, db, suiteID)
	if err == nil {
		out.LatestVersion = &latest
	} else if !errors.Is(err, identity.ErrNotFound) {
		return out, err
	}
	return out, nil
}

func (r *Repository) ListSuites(ctx context.Context, db DBTX, workspaceID string, filters Filters) ([]Suite, error) {
	rows, err := db.Query(ctx, `
		SELECT id,workspace_id,name,created_by,created_at
		FROM evaluation_suites
		WHERE workspace_id=$1
		ORDER BY created_at DESC,id DESC LIMIT $2 OFFSET $3`, workspaceID, filters.Pagination.Limit, filters.Pagination.Offset)
	if err != nil {
		return nil, mapDB(err)
	}
	defer rows.Close()
	out := []Suite{}
	for rows.Next() {
		var suite Suite
		if err := rows.Scan(&suite.ID, &suite.WorkspaceID, &suite.Name, &suite.CreatedBy, &suite.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, suite)
	}
	return out, mapDB(rows.Err())
}

func (r *Repository) getLatestVersion(ctx context.Context, db DBTX, suiteID string) (Version, error) {
	var out Version
	err := db.QueryRow(ctx, `
		SELECT id,suite_id,version,description,metadata,created_by,created_at
		FROM evaluation_suite_versions WHERE suite_id=$1
		ORDER BY version DESC LIMIT 1`, suiteID).
		Scan(&out.ID, &out.SuiteID, &out.Version, &out.Description, &out.Metadata, &out.CreatedBy, &out.CreatedAt)
	if err != nil {
		return out, mapDB(err)
	}
	out.Scenarios, err = r.ListVersionScenarios(ctx, db, out.ID)
	return out, err
}

func (r *Repository) GetVersion(ctx context.Context, db DBTX, workspaceID, versionID string) (Version, error) {
	var out Version
	err := db.QueryRow(ctx, `
		SELECT sv.id,sv.suite_id,sv.version,sv.description,sv.metadata,sv.created_by,sv.created_at
		FROM evaluation_suite_versions sv
		JOIN evaluation_suites s ON s.id=sv.suite_id
		WHERE sv.id=$1 AND s.workspace_id=$2`, versionID, workspaceID).
		Scan(&out.ID, &out.SuiteID, &out.Version, &out.Description, &out.Metadata, &out.CreatedBy, &out.CreatedAt)
	if err != nil {
		return out, mapDB(err)
	}
	out.Scenarios, err = r.ListVersionScenarios(ctx, db, out.ID)
	return out, err
}

func (r *Repository) ListVersionScenarios(ctx context.Context, db DBTX, suiteVersionID string) ([]ScenarioEntry, error) {
	rows, err := db.Query(ctx, `
		SELECT sv.id,s.id,s.name,sv.version,m.trial_count,m.sort_order,m.hidden_variants
		FROM evaluation_suite_version_scenarios m
		JOIN scenario_versions sv ON sv.id=m.scenario_version_id
		JOIN scenarios s ON s.id=sv.scenario_id
		WHERE m.suite_version_id=$1
		ORDER BY m.sort_order,sv.id`, suiteVersionID)
	if err != nil {
		return nil, mapDB(err)
	}
	defer rows.Close()
	out := []ScenarioEntry{}
	for rows.Next() {
		var item ScenarioEntry
		var hiddenVariants []byte
		if err := rows.Scan(&item.ScenarioVersionID, &item.ScenarioID, &item.ScenarioName, &item.ScenarioVersion, &item.TrialCount, &item.SortOrder, &hiddenVariants); err != nil {
			return nil, err
		}
		if err := json.Unmarshal(hiddenVariants, &item.HiddenVariants); err != nil {
			return nil, err
		}
		out = append(out, item)
	}
	return out, mapDB(rows.Err())
}

func (r *Repository) CreateExecution(ctx context.Context, db DBTX, workspaceID, userID, suiteVersionID string) (Execution, error) {
	var out Execution
	err := db.QueryRow(ctx, `
		INSERT INTO suite_executions(workspace_id,suite_version_id,status,created_by)
		VALUES($1,$2,'queued',$3)
		RETURNING id,workspace_id,suite_version_id,status,created_by,created_at,started_at,completed_at`,
		workspaceID, suiteVersionID, userID).
		Scan(&out.ID, &out.WorkspaceID, &out.SuiteVersionID, &out.Status, &out.CreatedBy, &out.CreatedAt, &out.StartedAt, &out.CompletedAt)
	return out, mapDB(err)
}

func (r *Repository) AddExecutionAgents(ctx context.Context, db DBTX, executionID string, agentVersionIDs []string) error {
	_, err := db.Exec(ctx, `
		INSERT INTO suite_execution_agents(suite_execution_id,agent_version_id)
		SELECT $1,agent_version_id FROM unnest($2::uuid[]) AS selected(agent_version_id)`, executionID, agentVersionIDs)
	return mapDB(err)
}

func (r *Repository) LinkBatch(ctx context.Context, db DBTX, executionID, batchID, scenarioVersionID string) error {
	_, err := db.Exec(ctx, `
		INSERT INTO suite_execution_batches(suite_execution_id,batch_id,scenario_version_id)
		VALUES($1,$2,$3)`, executionID, batchID, scenarioVersionID)
	return mapDB(err)
}

type RunAssignmentInput struct {
	RunID             string
	AgentVersionID    string
	ScenarioVersionID string
	TrialOrdinal      int
}

func (r *Repository) ListRunsForAssignment(ctx context.Context, db DBTX, executionID string) ([]RunAssignmentInput, error) {
	rows, err := db.Query(ctx, `
		SELECT r.id,r.agent_version_id,seb.scenario_version_id,r.attempt_number
		FROM suite_execution_batches seb
		JOIN runs r ON r.batch_id=seb.batch_id
		WHERE seb.suite_execution_id=$1
		ORDER BY seb.scenario_version_id,r.agent_version_id,r.attempt_number,r.id`, executionID)
	if err != nil {
		return nil, mapDB(err)
	}
	defer rows.Close()
	out := []RunAssignmentInput{}
	for rows.Next() {
		var item RunAssignmentInput
		if err := rows.Scan(&item.RunID, &item.AgentVersionID, &item.ScenarioVersionID, &item.TrialOrdinal); err != nil {
			return nil, err
		}
		out = append(out, item)
	}
	return out, mapDB(rows.Err())
}

func (r *Repository) InsertVariantAssignments(ctx context.Context, db DBTX, assignments []VariantAssignment) error {
	for _, assignment := range assignments {
		hiddenConfig, err := json.Marshal(assignment.HiddenConfig)
		if err != nil {
			return err
		}
		if _, err = db.Exec(ctx, `
			INSERT INTO suite_execution_run_variants(run_id,suite_execution_id,suite_version_id,scenario_version_id,agent_version_id,trial_ordinal,variant_id,variant_seed,hidden_config)
			VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9)
			ON CONFLICT(run_id) DO NOTHING`, assignment.RunID, assignment.SuiteExecutionID, assignment.SuiteVersionID, assignment.ScenarioVersionID, assignment.AgentVersionID, assignment.TrialOrdinal, assignment.VariantID, assignment.VariantSeed, hiddenConfig); err != nil {
			return mapDB(err)
		}
	}
	return nil
}

func (r *Repository) QueueBatch(ctx context.Context, db DBTX, workspaceID, batchID string) error {
	tag, err := db.Exec(ctx, `UPDATE evaluation_batches SET status='queued' WHERE id=$1 AND workspace_id=$2 AND status='draft'`, batchID, workspaceID)
	if err != nil {
		return mapDB(err)
	}
	if tag.RowsAffected() != 1 {
		return identity.ErrConflict
	}
	return nil
}

func (r *Repository) ReconcileExecution(ctx context.Context, db DBTX, workspaceID, executionID string) error {
	tag, err := db.Exec(ctx, `
		WITH state AS (
			SELECT se.id,
				CASE
					WHEN bool_or(b.status='running') THEN 'running'
					WHEN bool_or(b.status='queued') THEN CASE WHEN bool_or(b.status IN ('completed','failed','stopped')) THEN 'running' ELSE 'queued' END
					WHEN bool_or(b.status='failed') THEN 'failed'
					WHEN bool_or(b.status='stopped') THEN 'stopped'
					ELSE 'completed'
				END AS status
			FROM suite_executions se
			JOIN suite_execution_batches seb ON seb.suite_execution_id=se.id
			JOIN evaluation_batches b ON b.id=seb.batch_id
			WHERE se.id=$1 AND se.workspace_id=$2
			GROUP BY se.id
		)
		UPDATE suite_executions se
		SET status=state.status,
			started_at=CASE WHEN state.status<>'queued' THEN COALESCE(se.started_at,now()) ELSE se.started_at END,
			completed_at=CASE WHEN state.status IN ('completed','failed','stopped') THEN COALESCE(se.completed_at,now()) ELSE NULL END
		FROM state WHERE se.id=state.id`, executionID, workspaceID)
	if err != nil {
		return mapDB(err)
	}
	if tag.RowsAffected() != 1 {
		return identity.ErrNotFound
	}
	return nil
}

func (r *Repository) GetExecution(ctx context.Context, db DBTX, workspaceID, executionID string) (Execution, error) {
	var out Execution
	err := db.QueryRow(ctx, `
		SELECT id,workspace_id,suite_version_id,status,created_by,created_at,started_at,completed_at
		FROM suite_executions WHERE id=$1 AND workspace_id=$2`, executionID, workspaceID).
		Scan(&out.ID, &out.WorkspaceID, &out.SuiteVersionID, &out.Status, &out.CreatedBy, &out.CreatedAt, &out.StartedAt, &out.CompletedAt)
	if err != nil {
		return out, mapDB(err)
	}
	rows, err := db.Query(ctx, `SELECT agent_version_id FROM suite_execution_agents WHERE suite_execution_id=$1 ORDER BY agent_version_id`, executionID)
	if err != nil {
		return out, mapDB(err)
	}
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			rows.Close()
			return out, err
		}
		out.AgentVersionIDs = append(out.AgentVersionIDs, id)
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return out, mapDB(err)
	}
	rows.Close()
	rows, err = db.Query(ctx, `SELECT batch_id FROM suite_execution_batches WHERE suite_execution_id=$1 ORDER BY scenario_version_id`, executionID)
	if err != nil {
		return out, mapDB(err)
	}
	defer rows.Close()
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return out, err
		}
		out.BatchIDs = append(out.BatchIDs, id)
	}
	return out, mapDB(rows.Err())
}

type TrialRow struct {
	AgentVersionID    string
	ScenarioVersionID string
	RunStatus         string
	EvaluationResult  string
	PlatformInvalid   bool
	SafetyViolations  int
	SafetyCodes       []string
	RunID             string
	TrialOrdinal      int
	VariantID         string
	VariantSeed       int64
	EvaluatorVersion  string
}

func (r *Repository) ListTrialRows(ctx context.Context, db DBTX, workspaceID, executionID string) ([]TrialRow, error) {
	rows, err := db.Query(ctx, `
		SELECT r.agent_version_id,seb.scenario_version_id,r.status,COALESCE(e.result,''),
			EXISTS(SELECT 1 FROM run_events re WHERE re.run_id=r.id AND re.payload->>'classification'='platform' AND re.payload->>'code'<>'cleanup_completed') OR
			EXISTS(SELECT 1 FROM evaluation_findings pf WHERE pf.evaluation_id=e.id AND pf.evidence->>'code' IN ('runtime_failure','telemetry_unavailable')),
			COALESCE(e.safety_violation_count,0),
			COALESCE((SELECT array_agg(sf.evidence->>'code' ORDER BY sf.evidence->>'code') FROM evaluation_findings sf WHERE sf.evaluation_id=e.id AND sf.kind='safety'),'{}'::text[]),
			r.id,COALESCE(rv.trial_ordinal,r.attempt_number),COALESCE(rv.variant_id,''),COALESCE(rv.variant_seed,0),COALESCE(e.evaluator_version,'')
		FROM suite_executions se
		JOIN suite_execution_batches seb ON seb.suite_execution_id=se.id
		JOIN runs r ON r.batch_id=seb.batch_id
		LEFT JOIN evaluations e ON e.run_id=r.id
		LEFT JOIN suite_execution_run_variants rv ON rv.run_id=r.id
		WHERE se.id=$1 AND se.workspace_id=$2
		ORDER BY r.agent_version_id,seb.scenario_version_id,r.attempt_number,r.id`, executionID, workspaceID)
	if err != nil {
		return nil, mapDB(err)
	}
	defer rows.Close()
	out := []TrialRow{}
	for rows.Next() {
		var row TrialRow
		if err := rows.Scan(&row.AgentVersionID, &row.ScenarioVersionID, &row.RunStatus, &row.EvaluationResult, &row.PlatformInvalid, &row.SafetyViolations, &row.SafetyCodes, &row.RunID, &row.TrialOrdinal, &row.VariantID, &row.VariantSeed, &row.EvaluatorVersion); err != nil {
			return nil, err
		}
		out = append(out, row)
	}
	return out, mapDB(rows.Err())
}

type CriterionObservationRow struct {
	AgentVersionID    string
	ScenarioVersionID string
	CriterionID       string
	CriterionKey      string
	Operator          string
	TargetValue       json.RawMessage
	Unit              *string
	ObservedValue     float64
	PlatformInvalid   bool
}

func (r *Repository) ListCriterionObservationRows(ctx context.Context, db DBTX, workspaceID, executionID string) ([]CriterionObservationRow, error) {
	rows, err := db.Query(ctx, `
		SELECT r.agent_version_id,seb.scenario_version_id,c.id,c.key,c.operator,c.target_value,c.unit,cr.observed_value,
			EXISTS(SELECT 1 FROM run_events re WHERE re.run_id=r.id AND re.payload->>'classification'='platform' AND re.payload->>'code'<>'cleanup_completed') OR
			EXISTS(SELECT 1 FROM evaluation_findings pf WHERE pf.evaluation_id=e.id AND pf.evidence->>'code' IN ('runtime_failure','telemetry_unavailable'))
		FROM suite_executions se
		JOIN suite_execution_batches seb ON seb.suite_execution_id=se.id
		JOIN runs r ON r.batch_id=seb.batch_id
		JOIN evaluations e ON e.run_id=r.id
		JOIN evaluation_criterion_results cr ON cr.evaluation_id=e.id
		JOIN scenario_criteria c ON c.id=cr.scenario_criterion_id
		WHERE se.id=$1 AND se.workspace_id=$2
			AND cr.evidence->>'observed_state'='present'
			AND cr.evidence->>'status' IN ('passed','failed')
		ORDER BY r.agent_version_id,seb.scenario_version_id,c.id,r.attempt_number,r.id`, executionID, workspaceID)
	if err != nil {
		return nil, mapDB(err)
	}
	defer rows.Close()
	out := []CriterionObservationRow{}
	for rows.Next() {
		var row CriterionObservationRow
		var observed []byte
		if err := rows.Scan(&row.AgentVersionID, &row.ScenarioVersionID, &row.CriterionID, &row.CriterionKey, &row.Operator, &row.TargetValue, &row.Unit, &observed, &row.PlatformInvalid); err != nil {
			return nil, err
		}
		value, err := strconv.ParseFloat(string(observed), 64)
		if err != nil || math.IsNaN(value) || math.IsInf(value, 0) {
			continue
		}
		row.ObservedValue = value
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
