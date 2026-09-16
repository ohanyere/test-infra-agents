package reliability

import (
	"context"
	"errors"

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

// Repository owns reliability SQL. All queries are workspace-scoped and
// select opaque variant IDs only: hidden variant configs, reference
// remediations, and ground truth are never selected.
type Repository struct{}

// NewRepository creates a reliability Repository.
func NewRepository() *Repository { return &Repository{} }

// platformValidity is the shared platform-invalid classification: a terminal
// trial with platform-class evidence (provisioning/platform run events or
// runtime_failure/telemetry_unavailable findings) is invalid_platform; a
// trial with a deterministic evaluation is valid; anything else is pending.
const platformValidity = `CASE WHEN (EXISTS(SELECT 1 FROM run_events re WHERE re.run_id=r.id AND re.payload->>'classification'='platform' AND re.payload->>'code'<>'cleanup_completed')
		OR EXISTS(SELECT 1 FROM evaluation_findings pf JOIN evaluations pe ON pe.id=pf.evaluation_id AND pe.run_id=r.id WHERE pf.evidence->>'code' IN ('runtime_failure','telemetry_unavailable')))
		AND (COALESCE(e.result,'')<>'' OR r.status IN ('completed','failed','stopped'))
		THEN 'invalid_platform'
		WHEN COALESCE(e.result,'')<>'' THEN 'valid'
		ELSE 'pending' END`

// Semantic environment pins come from the run-owned EnvironmentInstance
// (owner_type='run', owner_id=run id), newest first. Correlated
// sub-selects (not a JOIN) so one run with several instance rows never
// duplicates trial evidence. Empty string means the persisted evidence
// predates instance linkage; comparability then reports
// environment_semantic_unavailable instead of inferring from opaque refs.
const trialColumns = `r.id,COALESCE(seb.suite_execution_id,se.id),COALESCE(seb.scenario_version_id,b.scenario_version_id),r.agent_version_id,
	COALESCE(f.task_family_id,''),COALESCE(f.case_type,''),
	` + platformValidity + `,
	COALESCE(e.result,''),COALESCE(rv.variant_id,''),COALESCE(rv.variant_seed,0),COALESCE(rv.trial_ordinal,r.attempt_number),
	COALESCE(e.evaluator_version,''),COALESCE(r.environment_ref,''),COALESCE(e.safety_violation_count,0),
	COALESCE((SELECT ei.profile_id FROM environment_instances ei WHERE ei.owner_type='run' AND ei.owner_id=r.id ORDER BY ei.created_at DESC LIMIT 1),''),
	COALESCE((SELECT ei.profile_contract_version FROM environment_instances ei WHERE ei.owner_type='run' AND ei.owner_id=r.id ORDER BY ei.created_at DESC LIMIT 1),''),
	COALESCE((SELECT ei.provider_id FROM environment_instances ei WHERE ei.owner_type='run' AND ei.owner_id=r.id ORDER BY ei.created_at DESC LIMIT 1),''),
	COALESCE((SELECT ei.provider_contract_version FROM environment_instances ei WHERE ei.owner_type='run' AND ei.owner_id=r.id ORDER BY ei.created_at DESC LIMIT 1),''),
	COALESCE((SELECT ei.isolation_tier FROM environment_instances ei WHERE ei.owner_type='run' AND ei.owner_id=r.id ORDER BY ei.created_at DESC LIMIT 1),'')`

func scanTrials(rows pgx.Rows) ([]Trial, error) {
	out := []Trial{}
	for rows.Next() {
		var item Trial
		var environmentRef *string
		if err := rows.Scan(&item.RunID, &item.SuiteExecutionID, &item.ScenarioVersionID, &item.AgentVersionID,
			&item.FamilyID, &item.CaseType, &item.Validity, &item.Result, &item.VariantID, &item.VariantSeed,
			&item.Ordinal, &item.EvaluatorVersion, &environmentRef, &item.SafetyViolations,
			&item.EnvironmentProfileID, &item.EnvironmentProfileContract, &item.ProviderID, &item.ProviderContract, &item.IsolationTier); err != nil {
			return nil, err
		}
		if environmentRef != nil {
			item.EnvironmentRef = *environmentRef
		}
		if item.RuntimeContractVersion == "" {
			item.RuntimeContractVersion = RuntimeContractVersion
		}
		out = append(out, item)
	}
	return out, rows.Err()
}

// ListExecutionTrials returns trial evidence for one suite execution,
// optionally restricted to one agent version. Unassigned ScenarioVersions
// are included with empty family fields (family aggregation skips them;
// task-level reliability still reports them).
func (r *Repository) ListExecutionTrials(ctx context.Context, db DBTX, workspaceID, executionID, agentVersionID string) ([]Trial, error) {
	rows, err := db.Query(ctx, `
		SELECT `+trialColumns+`
		FROM suite_executions se
		JOIN suite_execution_batches seb ON seb.suite_execution_id=se.id
		JOIN runs r ON r.batch_id=seb.batch_id
		JOIN evaluation_batches b ON b.id=r.batch_id
		LEFT JOIN evaluations e ON e.run_id=r.id
		LEFT JOIN suite_execution_run_variants rv ON rv.run_id=r.id
		LEFT JOIN scenario_version_task_family f ON f.scenario_version_id=seb.scenario_version_id
		WHERE se.id=$1 AND se.workspace_id=$2 AND ($3='' OR r.agent_version_id=$3::uuid)
		ORDER BY seb.scenario_version_id,r.agent_version_id,r.attempt_number,r.id`, executionID, workspaceID, agentVersionID)
	if err != nil {
		return nil, mapDB(err)
	}
	defer rows.Close()
	return scanTrials(rows)
}

// ListAgentTrials returns trial evidence for one agent version across all
// suite executions in the workspace.
func (r *Repository) ListAgentTrials(ctx context.Context, db DBTX, workspaceID, agentVersionID string) ([]Trial, error) {
	rows, err := db.Query(ctx, `
		SELECT `+trialColumns+`
		FROM suite_executions se
		JOIN suite_execution_batches seb ON seb.suite_execution_id=se.id
		JOIN runs r ON r.batch_id=seb.batch_id
		JOIN evaluation_batches b ON b.id=r.batch_id
		LEFT JOIN evaluations e ON e.run_id=r.id
		LEFT JOIN suite_execution_run_variants rv ON rv.run_id=r.id
		LEFT JOIN scenario_version_task_family f ON f.scenario_version_id=seb.scenario_version_id
		WHERE se.workspace_id=$1 AND r.agent_version_id=$2
		ORDER BY seb.scenario_version_id,r.attempt_number,r.id`, workspaceID, agentVersionID)
	if err != nil {
		return nil, mapDB(err)
	}
	defer rows.Close()
	return scanTrials(rows)
}

// ListScenarioTrials returns trial evidence for one ScenarioVersion,
// optionally restricted to one agent version.
func (r *Repository) ListScenarioTrials(ctx context.Context, db DBTX, workspaceID, scenarioVersionID, agentVersionID string) ([]Trial, error) {
	rows, err := db.Query(ctx, `
		SELECT `+trialColumns+`
		FROM suite_executions se
		JOIN suite_execution_batches seb ON seb.suite_execution_id=se.id
		JOIN runs r ON r.batch_id=seb.batch_id
		JOIN evaluation_batches b ON b.id=r.batch_id
		LEFT JOIN evaluations e ON e.run_id=r.id
		LEFT JOIN suite_execution_run_variants rv ON rv.run_id=r.id
		LEFT JOIN scenario_version_task_family f ON f.scenario_version_id=seb.scenario_version_id
		WHERE se.workspace_id=$1 AND seb.scenario_version_id=$2 AND ($3='' OR r.agent_version_id=$3::uuid)
		ORDER BY r.agent_version_id,r.attempt_number,r.id`, workspaceID, scenarioVersionID, agentVersionID)
	if err != nil {
		return nil, mapDB(err)
	}
	defer rows.Close()
	return scanTrials(rows)
}

// ExecutionSuiteVersion resolves the suite version for an execution.
func (r *Repository) ExecutionSuiteVersion(ctx context.Context, db DBTX, workspaceID, executionID string) (string, string, error) {
	var suiteVersionID, suiteExecutionID string
	err := db.QueryRow(ctx, `SELECT suite_version_id,id FROM suite_executions WHERE id=$1 AND workspace_id=$2`, executionID, workspaceID).Scan(&suiteVersionID, &suiteExecutionID)
	return suiteVersionID, suiteExecutionID, mapDB(err)
}

// ScenarioEligibleVariants counts distinct hidden variants configured for
// one scenario entry across suite versions (the legitimate coverage-ratio
// denominator at execution scope). It counts configured variant rows only,
// never hidden values.
func (r *Repository) ScenarioEligibleVariants(ctx context.Context, db DBTX, suiteVersionID, scenarioVersionID string) (int, bool, error) {
	var count int
	err := db.QueryRow(ctx, `SELECT COALESCE(jsonb_array_length(hidden_variants),0) FROM evaluation_suite_version_scenarios WHERE suite_version_id=$1 AND scenario_version_id=$2`, suiteVersionID, scenarioVersionID).Scan(&count)
	if err != nil {
		return 0, false, mapDB(err)
	}
	return count, count > 0, nil
}

// ScenarioIdentity resolves scenario ownership and assignment metadata for
// one ScenarioVersion.
func (r *Repository) ScenarioIdentity(ctx context.Context, db DBTX, workspaceID, scenarioID, scenarioVersionID string) (familyID string, err error) {
	var owner string
	err = db.QueryRow(ctx, `SELECT s.owner_workspace_id FROM scenario_versions sv JOIN scenarios s ON s.id=sv.scenario_id WHERE sv.id=$1 AND sv.scenario_id=$2`, scenarioVersionID, scenarioID).Scan(&owner)
	if err != nil {
		return "", mapDB(err)
	}
	if owner != workspaceID {
		return "", identity.ErrNotFound
	}
	if lookupErr := db.QueryRow(ctx, `SELECT COALESCE(task_family_id,'') FROM scenario_version_task_family WHERE scenario_version_id=$1`, scenarioVersionID).Scan(&familyID); lookupErr != nil {
		if errors.Is(lookupErr, pgx.ErrNoRows) {
			familyID = ""
		} else {
			return "", mapDB(lookupErr)
		}
	}
	return familyID, nil
}

// FamilyAlternatives counts other ScenarioVersions in the same family that
// have persisted suite-execution evidence in this workspace.
func (r *Repository) FamilyAlternatives(ctx context.Context, db DBTX, workspaceID, familyID, excludeScenarioVersionID string) (int, error) {
	if familyID == "" {
		return 0, nil
	}
	var count int
	err := db.QueryRow(ctx, `SELECT COUNT(DISTINCT f.scenario_version_id)
		FROM scenario_version_task_family f
		JOIN suite_execution_batches seb ON seb.scenario_version_id=f.scenario_version_id
		JOIN suite_executions se ON se.id=seb.suite_execution_id AND se.workspace_id=$1
		WHERE f.task_family_id=$2 AND f.scenario_version_id<>$3`, workspaceID, familyID, excludeScenarioVersionID).Scan(&count)
	return count, mapDB(err)
}

// AgentForVersion resolves the agent owning an agent version in this
// workspace.
func (r *Repository) AgentForVersion(ctx context.Context, db DBTX, workspaceID, agentVersionID string) (string, error) {
	var agentID string
	err := db.QueryRow(ctx, `SELECT a.id FROM agent_versions av JOIN agents a ON a.id=av.agent_id WHERE av.id=$1 AND a.owner_workspace_id=$2`, agentVersionID, workspaceID).Scan(&agentID)
	return agentID, mapDB(err)
}

// AgentVersionOwner resolves the agent owning an agent version in this
// workspace.
func (r *Repository) AgentVersionOwner(ctx context.Context, db DBTX, workspaceID, agentID, agentVersionID string) error {
	var found bool
	err := db.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM agent_versions av JOIN agents a ON a.id=av.agent_id WHERE av.id=$1 AND a.id=$2 AND a.owner_workspace_id=$3)`, agentVersionID, agentID, workspaceID).Scan(&found)
	if err != nil {
		return mapDB(err)
	}
	if !found {
		return identity.ErrNotFound
	}
	return nil
}

// ExecutionAgents lists agent versions participating in an execution.
func (r *Repository) ExecutionAgents(ctx context.Context, db DBTX, workspaceID, executionID string) ([]string, error) {
	rows, err := db.Query(ctx, `SELECT sea.agent_version_id FROM suite_execution_agents sea JOIN suite_executions se ON se.id=sea.suite_execution_id WHERE sea.suite_execution_id=$1 AND se.workspace_id=$2 ORDER BY sea.agent_version_id`, executionID, workspaceID)
	if err != nil {
		return nil, mapDB(err)
	}
	defer rows.Close()
	out := []string{}
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		out = append(out, id)
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
