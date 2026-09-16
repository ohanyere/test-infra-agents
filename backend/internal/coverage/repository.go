package coverage

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

// Assignment is the explicit ScenarioVersion -> task family mapping with an
// explicit case type. Set once; frozen once evaluation evidence exists.
type Assignment struct {
	ScenarioVersionID string    `json:"scenario_version_id"`
	FamilyID          string    `json:"family_id"`
	CaseType          string    `json:"case_type"`
	ContractVersion   string    `json:"contract_version"`
	AssignedAt        time.Time `json:"assigned_at"`
}

// PersistedAssessment is a stored Assessment with its exact evidence scope.
// Historical rows retain the exact contract versions used so catalog v2 never
// rewrites them and reproduction never depends on mutable current state.
type PersistedAssessment struct {
	ID string `json:"id"`
	Assessment
	WorkspaceID      string    `json:"workspace_id"`
	AgentID          string    `json:"agent_id"`
	AgentVersionID   string    `json:"agent_version_id"`
	SuiteExecutionID string    `json:"suite_execution_id"`
	ScenarioVersions []string  `json:"scenario_version_ids"`
	RunIDs           []string  `json:"run_ids"`
	CreatedAt        time.Time `json:"created_at"`
}

// Repository owns coverage SQL.
type Repository struct{}

// NewRepository creates a coverage Repository.
func NewRepository() *Repository { return &Repository{} }

// GetAssignment returns the family assignment for one ScenarioVersion.
func (r *Repository) GetAssignment(ctx context.Context, db DBTX, scenarioVersionID string) (Assignment, error) {
	var out Assignment
	err := db.QueryRow(ctx, `SELECT scenario_version_id,task_family_id,case_type,contract_version,assigned_at FROM scenario_version_task_family WHERE scenario_version_id=$1`, scenarioVersionID).
		Scan(&out.ScenarioVersionID, &out.FamilyID, &out.CaseType, &out.ContractVersion, &out.AssignedAt)
	return out, mapDB(err)
}

// ListAssignments returns assignments for many ScenarioVersions.
func (r *Repository) ListAssignments(ctx context.Context, db DBTX, scenarioVersionIDs []string) (map[string]Assignment, error) {
	out := make(map[string]Assignment, len(scenarioVersionIDs))
	if len(scenarioVersionIDs) == 0 {
		return out, nil
	}
	rows, err := db.Query(ctx, `SELECT scenario_version_id,task_family_id,case_type,contract_version,assigned_at FROM scenario_version_task_family WHERE scenario_version_id = ANY($1)`, scenarioVersionIDs)
	if err != nil {
		return nil, mapDB(err)
	}
	defer rows.Close()
	for rows.Next() {
		var item Assignment
		if err := rows.Scan(&item.ScenarioVersionID, &item.FamilyID, &item.CaseType, &item.ContractVersion, &item.AssignedAt); err != nil {
			return nil, err
		}
		out[item.ScenarioVersionID] = item
	}
	return out, mapDB(rows.Err())
}

// UpsertAssignment creates or (before evidence exists) updates the assignment.
// The evidence-freeze trigger is the backstop; callers check evidence first
// to return domain errors instead of raw SQL errors.
func (r *Repository) UpsertAssignment(ctx context.Context, db DBTX, scenarioVersionID, familyID, caseType, assignedBy string) (Assignment, error) {
	var out Assignment
	err := db.QueryRow(ctx, `INSERT INTO scenario_version_task_family (scenario_version_id,task_family_id,case_type,assigned_by)
		VALUES ($1,$2,$3,NULLIF($4,'')::uuid)
		ON CONFLICT (scenario_version_id) DO UPDATE SET task_family_id=EXCLUDED.task_family_id,case_type=EXCLUDED.case_type,assigned_at=now()
		RETURNING scenario_version_id,task_family_id,case_type,contract_version,assigned_at`,
		scenarioVersionID, familyID, caseType, assignedBy).
		Scan(&out.ScenarioVersionID, &out.FamilyID, &out.CaseType, &out.ContractVersion, &out.AssignedAt)
	return out, mapDB(err)
}

// ScenarioEvidenceExists reports whether evaluation evidence references the
// ScenarioVersion (runs via batches, or suite execution links).
func (r *Repository) ScenarioEvidenceExists(ctx context.Context, db DBTX, scenarioVersionID string) (bool, error) {
	var exists bool
	err := db.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM evaluation_batches WHERE scenario_version_id=$1 UNION ALL SELECT 1 FROM suite_execution_batches WHERE scenario_version_id=$1 LIMIT 1)`, scenarioVersionID).Scan(&exists)
	return exists, mapDB(err)
}

// ScenarioWorkspace owns the workspace check: the scenario must belong to the
// workspace before an assignment is accepted.
func (r *Repository) ScenarioWorkspace(ctx context.Context, db DBTX, scenarioID, scenarioVersionID string) (string, error) {
	var workspaceID string
	err := db.QueryRow(ctx, `SELECT s.owner_workspace_id FROM scenario_versions sv JOIN scenarios s ON s.id=sv.scenario_id WHERE sv.id=$1 AND sv.scenario_id=$2`, scenarioVersionID, scenarioID).Scan(&workspaceID)
	return workspaceID, mapDB(err)
}

// TrialInput is one joined trial row for assessment.
type TrialInput struct {
	RunID             string
	ScenarioVersionID string
	FamilyID          string
	CaseType          string
	Validity          string
	EvaluationResult  string
	VariantID         string
}

// ListExecutionTrials returns joined trial evidence for one agent version in
// one suite execution. Only rows with an explicit family assignment are
// returned; unassigned ScenarioVersions never contribute coverage (no silent
// inference from names or specs). Hidden evaluator-only variant values are
// never selected: only the opaque variant_id is exposed.
func (r *Repository) ListExecutionTrials(ctx context.Context, db DBTX, workspaceID, executionID, agentVersionID string) ([]TrialInput, error) {
	rows, err := db.Query(ctx, `
		SELECT r.id,seb.scenario_version_id,COALESCE(f.task_family_id,''),COALESCE(f.case_type,''),
			CASE WHEN (EXISTS(SELECT 1 FROM run_events re WHERE re.run_id=r.id AND re.payload->>'classification'='platform' AND re.payload->>'code'<>'cleanup_completed')
				OR EXISTS(SELECT 1 FROM evaluation_findings pf JOIN evaluations pe ON pe.id=pf.evaluation_id AND pe.run_id=r.id WHERE pf.evidence->>'code' IN ('runtime_failure','telemetry_unavailable')))
				AND (COALESCE(e.result,'')<>'' OR r.status IN ('completed','failed','stopped'))
				THEN 'invalid_platform'
				WHEN COALESCE(e.result,'')<>'' THEN 'valid'
				ELSE 'pending' END,
			COALESCE(e.result,''),
			COALESCE(rv.variant_id,'')
		FROM suite_executions se
		JOIN suite_execution_batches seb ON seb.suite_execution_id=se.id
		JOIN runs r ON r.batch_id=seb.batch_id
		LEFT JOIN evaluations e ON e.run_id=r.id
		LEFT JOIN suite_execution_run_variants rv ON rv.run_id=r.id
		LEFT JOIN scenario_version_task_family f ON f.scenario_version_id=seb.scenario_version_id
		WHERE se.id=$1 AND se.workspace_id=$2 AND r.agent_version_id=$3
		ORDER BY seb.scenario_version_id,r.attempt_number,r.id`, executionID, workspaceID, agentVersionID)
	if err != nil {
		return nil, mapDB(err)
	}
	defer rows.Close()
	out := []TrialInput{}
	for rows.Next() {
		var item TrialInput
		if err := rows.Scan(&item.RunID, &item.ScenarioVersionID, &item.FamilyID, &item.CaseType, &item.Validity, &item.EvaluationResult, &item.VariantID); err != nil {
			return nil, err
		}
		if item.FamilyID == "" {
			continue
		}
		out = append(out, item)
	}
	return out, mapDB(rows.Err())
}

// InsertAssessment persists one assessment. Idempotent: the unique scope
// constraint makes concurrent retries safe via ON CONFLICT DO NOTHING.
func (r *Repository) InsertAssessment(ctx context.Context, db DBTX, workspaceID, agentID, agentVersionID, executionID string, assessment Assessment, scenarioVersionIDs, runIDs []string) (PersistedAssessment, error) {
	families, err := json.Marshal(assessment.Families)
	if err != nil {
		return PersistedAssessment{}, err
	}
	missing, err := json.Marshal(assessment.MissingRequirements)
	if err != nil {
		return PersistedAssessment{}, err
	}
	if scenarioVersionIDs == nil {
		scenarioVersionIDs = []string{}
	}
	if runIDs == nil {
		runIDs = []string{}
	}
	var out PersistedAssessment
	out.Assessment = assessment
	out.WorkspaceID = workspaceID
	out.AgentID = agentID
	out.AgentVersionID = agentVersionID
	out.SuiteExecutionID = executionID
	err = db.QueryRow(ctx, `INSERT INTO capability_coverage_assessments
		(workspace_id,agent_id,agent_version_id,capability_id,suite_execution_id,test_status,required_family_count,covered_family_count,valid_trial_count,distinct_variant_count,families,missing_requirements,scenario_version_ids,run_ids)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14)
		ON CONFLICT (agent_version_id,capability_id,suite_execution_id,coverage_contract_version,task_family_contract_version,capability_catalog_version) DO NOTHING
		RETURNING id,created_at`,
		workspaceID, agentID, agentVersionID, assessment.CapabilityID, executionID, assessment.TestStatus,
		assessment.RequiredFamilyCount, assessment.CoveredFamilyCount, assessment.ValidTrialCount, assessment.DistinctVariantCount,
		families, missing, scenarioVersionIDs, runIDs).
		Scan(&out.ID, &out.CreatedAt)
	if err != nil {
		return PersistedAssessment{}, mapDB(err)
	}
	out.ScenarioVersions = append([]string(nil), scenarioVersionIDs...)
	out.RunIDs = append([]string(nil), runIDs...)
	return out, nil
}

// GetAssessment returns one persisted assessment by exact scope.
func (r *Repository) GetAssessment(ctx context.Context, db DBTX, workspaceID, agentVersionID, capabilityID, executionID string) (PersistedAssessment, error) {
	var out PersistedAssessment
	var families, missing []byte
	err := db.QueryRow(ctx, `SELECT id,workspace_id,agent_id,agent_version_id,capability_id,suite_execution_id,
		capability_catalog_version,task_family_contract_version,coverage_contract_version,test_status,
		required_family_count,covered_family_count,valid_trial_count,distinct_variant_count,
		families,missing_requirements,scenario_version_ids,run_ids,created_at
		FROM capability_coverage_assessments
		WHERE workspace_id=$1 AND agent_version_id=$2 AND capability_id=$3 AND suite_execution_id=$4
		ORDER BY created_at DESC LIMIT 1`, workspaceID, agentVersionID, capabilityID, executionID).
		Scan(&out.ID, &out.WorkspaceID, &out.AgentID, &out.AgentVersionID, &out.CapabilityID, &out.SuiteExecutionID,
			&out.CapabilityCatalogVersion, &out.TaskFamilyContractVersion, &out.CoverageContractVersion, &out.TestStatus,
			&out.RequiredFamilyCount, &out.CoveredFamilyCount, &out.ValidTrialCount, &out.DistinctVariantCount,
			&families, &missing, &out.ScenarioVersions, &out.RunIDs, &out.CreatedAt)
	if err != nil {
		return PersistedAssessment{}, mapDB(err)
	}
	out.ClaimStatus = ClaimStatusClaimed
	if err := json.Unmarshal(families, &out.Families); err != nil {
		return PersistedAssessment{}, err
	}
	if err := json.Unmarshal(missing, &out.MissingRequirements); err != nil {
		return PersistedAssessment{}, err
	}
	if out.Families == nil {
		out.Families = []FamilyCoverage{}
	}
	if out.MissingRequirements == nil {
		out.MissingRequirements = []MissingRequirement{}
	}
	return out, nil
}

// ListLatestAssessments returns the latest persisted assessment per capability
// for one agent version. Each row keeps its own exact evidence scope; the
// caller must surface suite_execution_id so history is never silently merged.
func (r *Repository) ListLatestAssessments(ctx context.Context, db DBTX, workspaceID, agentVersionID string) ([]PersistedAssessment, error) {
	rows, err := db.Query(ctx, `SELECT DISTINCT ON (capability_id) id,workspace_id,agent_id,agent_version_id,capability_id,suite_execution_id,
		capability_catalog_version,task_family_contract_version,coverage_contract_version,test_status,
		required_family_count,covered_family_count,valid_trial_count,distinct_variant_count,
		families,missing_requirements,scenario_version_ids,run_ids,created_at
		FROM capability_coverage_assessments
		WHERE workspace_id=$1 AND agent_version_id=$2
		ORDER BY capability_id,created_at DESC`, workspaceID, agentVersionID)
	if err != nil {
		return nil, mapDB(err)
	}
	defer rows.Close()
	out := []PersistedAssessment{}
	for rows.Next() {
		var item PersistedAssessment
		var families, missing []byte
		if err := rows.Scan(&item.ID, &item.WorkspaceID, &item.AgentID, &item.AgentVersionID, &item.CapabilityID, &item.SuiteExecutionID,
			&item.CapabilityCatalogVersion, &item.TaskFamilyContractVersion, &item.CoverageContractVersion, &item.TestStatus,
			&item.RequiredFamilyCount, &item.CoveredFamilyCount, &item.ValidTrialCount, &item.DistinctVariantCount,
			&families, &missing, &item.ScenarioVersions, &item.RunIDs, &item.CreatedAt); err != nil {
			return nil, err
		}
		item.ClaimStatus = ClaimStatusClaimed
		if err := json.Unmarshal(families, &item.Families); err != nil {
			return nil, err
		}
		if err := json.Unmarshal(missing, &item.MissingRequirements); err != nil {
			return nil, err
		}
		if item.Families == nil {
			item.Families = []FamilyCoverage{}
		}
		if item.MissingRequirements == nil {
			item.MissingRequirements = []MissingRequirement{}
		}
		out = append(out, item)
	}
	return out, mapDB(rows.Err())
}

// ListExecutionAssessments returns persisted assessments for one execution.
func (r *Repository) ListExecutionAssessments(ctx context.Context, db DBTX, workspaceID, executionID, agentVersionID string) ([]PersistedAssessment, error) {
	rows, err := db.Query(ctx, `SELECT id,workspace_id,agent_id,agent_version_id,capability_id,suite_execution_id,
		capability_catalog_version,task_family_contract_version,coverage_contract_version,test_status,
		required_family_count,covered_family_count,valid_trial_count,distinct_variant_count,
		families,missing_requirements,scenario_version_ids,run_ids,created_at
		FROM capability_coverage_assessments
		WHERE workspace_id=$1 AND suite_execution_id=$2 AND agent_version_id=$3
		ORDER BY capability_id`, workspaceID, executionID, agentVersionID)
	if err != nil {
		return nil, mapDB(err)
	}
	defer rows.Close()
	out := []PersistedAssessment{}
	for rows.Next() {
		var item PersistedAssessment
		var families, missing []byte
		if err := rows.Scan(&item.ID, &item.WorkspaceID, &item.AgentID, &item.AgentVersionID, &item.CapabilityID, &item.SuiteExecutionID,
			&item.CapabilityCatalogVersion, &item.TaskFamilyContractVersion, &item.CoverageContractVersion, &item.TestStatus,
			&item.RequiredFamilyCount, &item.CoveredFamilyCount, &item.ValidTrialCount, &item.DistinctVariantCount,
			&families, &missing, &item.ScenarioVersions, &item.RunIDs, &item.CreatedAt); err != nil {
			return nil, err
		}
		item.ClaimStatus = ClaimStatusClaimed
		if err := json.Unmarshal(families, &item.Families); err != nil {
			return nil, err
		}
		if err := json.Unmarshal(missing, &item.MissingRequirements); err != nil {
			return nil, err
		}
		if item.Families == nil {
			item.Families = []FamilyCoverage{}
		}
		if item.MissingRequirements == nil {
			item.MissingRequirements = []MissingRequirement{}
		}
		out = append(out, item)
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
