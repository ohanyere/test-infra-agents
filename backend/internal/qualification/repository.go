package qualification

import (
	"context"
	"encoding/json"
	"errors"

	"github.com/evalplane/evalplane/backend/internal/identity"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
)

type DBTX interface {
	Exec(context.Context, string, ...any) (pgconn.CommandTag, error)
	Query(context.Context, string, ...any) (pgx.Rows, error)
	QueryRow(context.Context, string, ...any) pgx.Row
}
type Repository struct{}

func NewRepository() *Repository { return &Repository{} }

func (r *Repository) CreatePolicy(ctx context.Context, db DBTX, workspace, user, name string) (Policy, error) {
	var out Policy
	err := db.QueryRow(ctx, `INSERT INTO qualification_policies(workspace_id,name,created_by) VALUES($1,$2,$3) RETURNING id,workspace_id,name,created_by,created_at`, workspace, name, user).Scan(&out.ID, &out.WorkspaceID, &out.Name, &out.CreatedBy, &out.CreatedAt)
	return out, mapDB(err)
}
func (r *Repository) LockPolicy(ctx context.Context, db DBTX, workspace, id string) (Policy, error) {
	var out Policy
	err := db.QueryRow(ctx, `SELECT id,workspace_id,name,created_by,created_at FROM qualification_policies WHERE id=$1 AND workspace_id=$2 FOR UPDATE`, id, workspace).Scan(&out.ID, &out.WorkspaceID, &out.Name, &out.CreatedBy, &out.CreatedAt)
	return out, mapDB(err)
}
func (r *Repository) NextVersion(ctx context.Context, db DBTX, policyID string) (int, error) {
	var n int
	err := db.QueryRow(ctx, `SELECT COALESCE(max(version),0)+1 FROM qualification_policy_versions WHERE policy_id=$1`, policyID).Scan(&n)
	return n, mapDB(err)
}
func (r *Repository) InsertVersion(ctx context.Context, db DBTX, policyID, user string, number int, in CreateVersionInput) (PolicyVersion, error) {
	var out PolicyVersion
	d := in.Definition
	err := db.QueryRow(ctx, `INSERT INTO qualification_policy_versions(policy_id,version,description,minimum_valid_trials,minimum_overall_pass_rate,minimum_per_scenario_pass_rate,maximum_inconclusive_rate,maximum_invalid_platform_rate,maximum_safety_violations,forbidden_safety_codes,require_no_regression,created_by) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12) RETURNING id,policy_id,version,description,minimum_valid_trials,minimum_overall_pass_rate,minimum_per_scenario_pass_rate,maximum_inconclusive_rate,maximum_invalid_platform_rate,maximum_safety_violations,forbidden_safety_codes,require_no_regression,created_by,created_at`, policyID, number, in.Description, d.MinimumValidTrials, d.MinimumOverallPassRate, d.MinimumPerScenarioPassRate, d.MaximumInconclusiveRate, d.MaximumInvalidPlatformRate, d.MaximumSafetyViolations, d.ForbiddenSafetyCodes, d.RequireNoDetectedRegression, user).Scan(&out.ID, &out.PolicyID, &out.Version, &out.Description, &out.Definition.MinimumValidTrials, &out.Definition.MinimumOverallPassRate, &out.Definition.MinimumPerScenarioPassRate, &out.Definition.MaximumInconclusiveRate, &out.Definition.MaximumInvalidPlatformRate, &out.Definition.MaximumSafetyViolations, &out.Definition.ForbiddenSafetyCodes, &out.Definition.RequireNoDetectedRegression, &out.CreatedBy, &out.CreatedAt)
	return out, mapDB(err)
}

func (r *Repository) CreateVersion(ctx context.Context, pool *pgxpool.Pool, workspace, policyID, user string, in CreateVersionInput) (PolicyVersion, error) {
	tx, err := pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return PolicyVersion{}, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if _, err = r.LockPolicy(ctx, tx, workspace, policyID); err != nil {
		return PolicyVersion{}, err
	}
	number, err := r.NextVersion(ctx, tx, policyID)
	if err != nil {
		return PolicyVersion{}, err
	}
	out, err := r.InsertVersion(ctx, tx, policyID, user, number, in)
	if err != nil {
		return PolicyVersion{}, err
	}
	if err = tx.Commit(ctx); err != nil {
		return PolicyVersion{}, err
	}
	return out, nil
}
func (r *Repository) GetPolicy(ctx context.Context, db DBTX, workspace, id string) (Policy, error) {
	var out Policy
	err := db.QueryRow(ctx, `SELECT id,workspace_id,name,created_by,created_at FROM qualification_policies WHERE id=$1 AND workspace_id=$2`, id, workspace).Scan(&out.ID, &out.WorkspaceID, &out.Name, &out.CreatedBy, &out.CreatedAt)
	if err != nil {
		return out, mapDB(err)
	}
	latest, e := r.latestVersion(ctx, db, id)
	if e == nil {
		out.LatestVersion = &latest
	} else if !errors.Is(e, identity.ErrNotFound) {
		return out, e
	}
	return out, nil
}
func (r *Repository) ListPolicies(ctx context.Context, db DBTX, workspace string) ([]Policy, error) {
	rows, err := db.Query(ctx, `SELECT id,workspace_id,name,created_by,created_at FROM qualification_policies WHERE workspace_id=$1 ORDER BY created_at DESC,id DESC`, workspace)
	if err != nil {
		return nil, mapDB(err)
	}
	defer rows.Close()
	out := []Policy{}
	for rows.Next() {
		var p Policy
		if err := rows.Scan(&p.ID, &p.WorkspaceID, &p.Name, &p.CreatedBy, &p.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, p)
	}
	return out, mapDB(rows.Err())
}
func (r *Repository) latestVersion(ctx context.Context, db DBTX, policyID string) (PolicyVersion, error) {
	var out PolicyVersion
	err := scanVersion(db.QueryRow(ctx, `SELECT id,policy_id,version,description,minimum_valid_trials,minimum_overall_pass_rate,minimum_per_scenario_pass_rate,maximum_inconclusive_rate,maximum_invalid_platform_rate,maximum_safety_violations,forbidden_safety_codes,require_no_regression,created_by,created_at FROM qualification_policy_versions WHERE policy_id=$1 ORDER BY version DESC LIMIT 1`, policyID), &out)
	return out, mapDB(err)
}
func (r *Repository) GetVersion(ctx context.Context, db DBTX, workspace, id string) (PolicyVersion, error) {
	var out PolicyVersion
	err := scanVersion(db.QueryRow(ctx, `SELECT v.id,v.policy_id,v.version,v.description,v.minimum_valid_trials,v.minimum_overall_pass_rate,v.minimum_per_scenario_pass_rate,v.maximum_inconclusive_rate,v.maximum_invalid_platform_rate,v.maximum_safety_violations,v.forbidden_safety_codes,v.require_no_regression,v.created_by,v.created_at FROM qualification_policy_versions v JOIN qualification_policies p ON p.id=v.policy_id WHERE v.id=$1 AND p.workspace_id=$2`, id, workspace), &out)
	return out, mapDB(err)
}
func scanVersion(row pgx.Row, out *PolicyVersion) error {
	return row.Scan(&out.ID, &out.PolicyID, &out.Version, &out.Description, &out.Definition.MinimumValidTrials, &out.Definition.MinimumOverallPassRate, &out.Definition.MinimumPerScenarioPassRate, &out.Definition.MaximumInconclusiveRate, &out.Definition.MaximumInvalidPlatformRate, &out.Definition.MaximumSafetyViolations, &out.Definition.ForbiddenSafetyCodes, &out.Definition.RequireNoDetectedRegression, &out.CreatedBy, &out.CreatedAt)
}

func (r *Repository) InsertAssessment(ctx context.Context, db DBTX, user string, in CreateAssessmentInput, out Assessment, facts AssessmentFacts) (Assessment, error) {
	snapshot, _ := json.Marshal(map[string]any{"suite_version_id": facts.SuiteVersionID, "counts": facts.Counts, "scenarios": facts.Scenarios, "regression": facts.Regression})
	manifest, _ := json.Marshal(out.Reproducibility)
	var id string
	err := db.QueryRow(ctx, `INSERT INTO qualification_assessments(workspace_id,policy_version_id,suite_execution_id,agent_version_id,regression_comparison_id,status,evidence_snapshot,reproducibility_manifest,created_by) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9) RETURNING id`, out.WorkspaceID, in.PolicyVersionID, in.SuiteExecutionID, in.AgentVersionID, in.RegressionComparisonID, out.Status, snapshot, manifest, user).Scan(&id)
	if err != nil {
		return Assessment{}, mapDB(err)
	}
	for i, rule := range out.Rules {
		if _, err = db.Exec(ctx, `INSERT INTO qualification_rule_results(assessment_id,rule_type,status,expected,observed,reason,supporting_references,sort_order) VALUES($1,$2,$3,$4,$5,$6,$7,$8)`, id, rule.RuleType, rule.Status, rule.Expected, rule.Observed, rule.Reason, rule.SupportingReferences, i); err != nil {
			return Assessment{}, mapDB(err)
		}
	}
	return r.GetAssessment(ctx, db, out.WorkspaceID, id)
}

func (r *Repository) SaveAssessment(ctx context.Context, pool *pgxpool.Pool, user string, in CreateAssessmentInput, out Assessment, facts AssessmentFacts) (Assessment, error) {
	tx, err := pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return Assessment{}, err
	}
	stored, err := r.InsertAssessment(ctx, tx, user, in, out, facts)
	if err == nil {
		if err = tx.Commit(ctx); err == nil {
			return stored, nil
		}
		return Assessment{}, err
	}
	_ = tx.Rollback(ctx)
	if errors.Is(err, identity.ErrConflict) {
		return r.GetAssessmentByIdentity(ctx, pool, out.WorkspaceID, in)
	}
	return Assessment{}, err
}
func (r *Repository) GetAssessmentByIdentity(ctx context.Context, db DBTX, workspace string, in CreateAssessmentInput) (Assessment, error) {
	var id string
	err := db.QueryRow(ctx, `SELECT id FROM qualification_assessments WHERE workspace_id=$1 AND policy_version_id=$2 AND suite_execution_id=$3 AND agent_version_id=$4 AND regression_comparison_id IS NOT DISTINCT FROM $5::uuid`, workspace, in.PolicyVersionID, in.SuiteExecutionID, in.AgentVersionID, in.RegressionComparisonID).Scan(&id)
	if err != nil {
		return Assessment{}, mapDB(err)
	}
	return r.GetAssessment(ctx, db, workspace, id)
}
func (r *Repository) GetAssessment(ctx context.Context, db DBTX, workspace, id string) (Assessment, error) {
	var out Assessment
	var manifest []byte
	err := db.QueryRow(ctx, `SELECT a.id,a.workspace_id,a.policy_version_id,a.suite_execution_id,se.suite_version_id,a.agent_version_id,a.regression_comparison_id,a.status,a.reproducibility_manifest,a.created_by,a.created_at FROM qualification_assessments a JOIN suite_executions se ON se.id=a.suite_execution_id WHERE a.id=$1 AND a.workspace_id=$2`, id, workspace).Scan(&out.ID, &out.WorkspaceID, &out.PolicyVersionID, &out.SuiteExecutionID, &out.SuiteVersionID, &out.AgentVersionID, &out.RegressionComparisonID, &out.Status, &manifest, &out.CreatedBy, &out.CreatedAt)
	if err != nil {
		return out, mapDB(err)
	}
	if err = json.Unmarshal(manifest, &out.Reproducibility); err != nil {
		return out, err
	}
	rows, err := db.Query(ctx, `SELECT rule_type,status,expected,observed,reason,supporting_references FROM qualification_rule_results WHERE assessment_id=$1 ORDER BY sort_order,id`, id)
	if err != nil {
		return out, mapDB(err)
	}
	defer rows.Close()
	for rows.Next() {
		var rule RuleResult
		if err := rows.Scan(&rule.RuleType, &rule.Status, &rule.Expected, &rule.Observed, &rule.Reason, &rule.SupportingReferences); err != nil {
			return out, err
		}
		out.Rules = append(out.Rules, rule)
	}
	return out, mapDB(rows.Err())
}
func (r *Repository) ListAssessments(ctx context.Context, db DBTX, workspace, execution, policyVersion string) ([]Assessment, error) {
	rows, err := db.Query(ctx, `SELECT id FROM qualification_assessments WHERE workspace_id=$1 AND suite_execution_id=$2 AND ($3='' OR policy_version_id=$3::uuid) ORDER BY agent_version_id,id`, workspace, execution, policyVersion)
	if err != nil {
		return nil, mapDB(err)
	}
	ids := []string{}
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			rows.Close()
			return nil, err
		}
		ids = append(ids, id)
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return nil, mapDB(err)
	}
	rows.Close()
	out := []Assessment{}
	for _, id := range ids {
		a, err := r.GetAssessment(ctx, db, workspace, id)
		if err != nil {
			return nil, err
		}
		out = append(out, a)
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
	var p *pgconn.PgError
	if errors.As(err, &p) {
		switch p.Code {
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
