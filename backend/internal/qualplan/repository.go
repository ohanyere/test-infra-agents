package qualplan

import (
	"context"
	"encoding/json"
	"errors"

	"github.com/evalplane/evalplane/backend/internal/identity"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

// DBTX is the minimal database surface used by this package. Both
// *pgxpool.Pool and pgx.Tx satisfy it.
type DBTX interface {
	Exec(context.Context, string, ...any) (pgconn.CommandTag, error)
	Query(context.Context, string, ...any) (pgx.Rows, error)
	QueryRow(context.Context, string, ...any) pgx.Row
}

// Repository owns qualification-plan SQL. Plans are immutable once written:
// the migration installs a trigger rejecting UPDATE/DELETE, and suite links
// live in a separate mapping table so builds never mutate a plan.
type Repository struct{}

func NewRepository() *Repository { return &Repository{} }

// FindScenarioIDBySlug returns the workspace-owned scenario for a bridge
// slug, or identity.ErrNotFound when no materialization exists yet.
func (r *Repository) FindScenarioIDBySlug(ctx context.Context, db DBTX, workspaceID, slug string) (string, error) {
	var id string
	err := db.QueryRow(ctx, `SELECT id FROM scenarios WHERE owner_workspace_id=$1 AND slug=$2`, workspaceID, slug).Scan(&id)
	return id, mapDB(err)
}

// InsertPlan persists one server-derived plan.
func (r *Repository) InsertPlan(ctx context.Context, db DBTX, workspaceID, agentID, versionID string, capabilityIDs []string, outcome PlanOutcome, createdBy string) (Plan, error) {
	body, _ := json.Marshal(map[string]any{
		"capabilities": outcome.Capabilities,
		"blockers":     outcome.Blockers,
	})
	var createdByParam any
	if createdBy != "" {
		createdByParam = createdBy
	}
	var out Plan
	err := db.QueryRow(ctx, `INSERT INTO qualification_plans
		(workspace_id, agent_id, agent_version_id, capability_ids,
		 coverage_contract_version, task_family_contract_version, capability_catalog_version,
		 planner_contract_version, certification_contract_version, evaluator_version,
		 environment_profile_contract_version, state, plan, created_by)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,NULLIF($14,'')::uuid)
		RETURNING id, workspace_id, agent_id, agent_version_id, capability_ids,
		 coverage_contract_version, task_family_contract_version, capability_catalog_version,
		 planner_contract_version, certification_contract_version, evaluator_version,
		 environment_profile_contract_version, state, plan, created_by, created_at`,
		workspaceID, agentID, versionID, capabilityIDs,
		CoverageContractVersion, TaskFamilyContractVersion, CapabilityCatalogVersion,
		PlannerContractVersion, CertificationContractVersion, EvaluatorVersion,
		EnvironmentProfileContractVersion, outcome.State, string(body), createdByParam).
		Scan(&out.ID, &out.WorkspaceID, &out.AgentID, &out.AgentVersionID, &out.CapabilityIDs,
			&out.CoverageContractVersion, &out.TaskFamilyContractVersion, &out.CapabilityCatalogVersion,
			&out.PlannerContractVersion, &out.CertificationContract, &out.EvaluatorVersion,
			&out.EnvironmentProfileContract, &out.State, &body, &out.CreatedBy, &out.CreatedAt)
	if err != nil {
		return Plan{}, mapDB(err)
	}
	return r.rehydrate(out, body)
}

// GetPlan reads one persisted plan with its suite-build link, if any.
func (r *Repository) GetPlan(ctx context.Context, db DBTX, workspaceID, planID string) (Plan, error) {
	var out Plan
	var body json.RawMessage
	err := db.QueryRow(ctx, `SELECT id, workspace_id, agent_id, agent_version_id, capability_ids,
		 coverage_contract_version, task_family_contract_version, capability_catalog_version,
		 planner_contract_version, certification_contract_version, evaluator_version,
		 environment_profile_contract_version, state, plan, created_by, created_at
		FROM qualification_plans WHERE id=$1 AND workspace_id=$2`, planID, workspaceID).
		Scan(&out.ID, &out.WorkspaceID, &out.AgentID, &out.AgentVersionID, &out.CapabilityIDs,
			&out.CoverageContractVersion, &out.TaskFamilyContractVersion, &out.CapabilityCatalogVersion,
			&out.PlannerContractVersion, &out.CertificationContract, &out.EvaluatorVersion,
			&out.EnvironmentProfileContract, &out.State, &body, &out.CreatedBy, &out.CreatedAt)
	if err != nil {
		return Plan{}, mapDB(err)
	}
	plan, err := r.rehydrate(out, body)
	if err != nil {
		return Plan{}, err
	}
	suiteID, suiteVersionID, ok, linkErr := r.getBuild(ctx, db, planID)
	if linkErr != nil {
		return Plan{}, linkErr
	}
	if ok {
		plan.GeneratedSuiteID = &suiteID
		plan.GeneratedSuiteVersionID = &suiteVersionID
	}
	return plan, nil
}

// RecordSuiteBuild links a generated suite version to its READY plan. Plans
// stay immutable: the link is a separate row, never an UPDATE.
func (r *Repository) RecordSuiteBuild(ctx context.Context, db DBTX, planID, suiteID, suiteVersionID string) error {
	_, err := db.Exec(ctx, `INSERT INTO qualification_plan_suites (plan_id, suite_id, suite_version_id)
		VALUES ($1,$2,$3) ON CONFLICT (plan_id) DO NOTHING`, planID, suiteID, suiteVersionID)
	return mapDB(err)
}

func (r *Repository) getBuild(ctx context.Context, db DBTX, planID string) (string, string, bool, error) {
	var suiteID, suiteVersionID string
	err := db.QueryRow(ctx, `SELECT suite_id, suite_version_id FROM qualification_plan_suites WHERE plan_id=$1`, planID).Scan(&suiteID, &suiteVersionID)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", "", false, nil
	}
	if err != nil {
		return "", "", false, mapDB(err)
	}
	return suiteID, suiteVersionID, true, nil
}

// PolicyAvailable reports whether the workspace owns at least one
// qualification policy (informational for planning; policies decide
// QUALIFIED later and are never replaced here).
func (r *Repository) PolicyAvailable(ctx context.Context, db DBTX, workspaceID string) (bool, error) {
	var exists bool
	err := db.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM qualification_policies WHERE workspace_id=$1)`, workspaceID).Scan(&exists)
	return exists, mapDB(err)
}

func (r *Repository) rehydrate(out Plan, body json.RawMessage) (Plan, error) {
	var decoded struct {
		Capabilities []CapabilityPlan `json:"capabilities"`
		Blockers     []Blocker        `json:"blockers"`
	}
	if len(body) > 0 {
		if err := json.Unmarshal(body, &decoded); err != nil {
			return Plan{}, err
		}
	}
	out.Capabilities = decoded.Capabilities
	if out.Capabilities == nil {
		out.Capabilities = []CapabilityPlan{}
	}
	out.Blockers = decoded.Blockers
	if out.Blockers == nil {
		out.Blockers = []Blocker{}
	}
	out.QualificationPolicyAvail = true
	for _, blocker := range out.Blockers {
		if blocker.Code == BlockerQualificationPolicyUnavailable {
			out.QualificationPolicyAvail = false
		}
	}
	return out, nil
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
