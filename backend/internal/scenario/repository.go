package scenario

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/evalplane/evalplane/backend/internal/identity"
	"github.com/evalplane/evalplane/backend/internal/observablecatalog"
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

const scenarioColumns = `s.id,s.owner_workspace_id,s.slug,s.name,s.kind,s.visibility,s.status,s.created_at,s.updated_at,
	v.id,v.version,v.description,v.category,v.difficulty,v.duration_seconds,v.environment,v.conditions,v.observables,v.expected_behaviors,v.forbidden_behaviors,v.authoring_method,v.agent_task,v.agent_visible_context,v.allowed_actions,v.allowed_resources,v.ground_truth,v.execution_spec,v.source_metadata,v.created_at,v.criteria,f.task_family_id,f.case_type,f.contract_version`
const latestJoin = `LEFT JOIN LATERAL (SELECT sv.id,sv.version,sv.description,sv.category,sv.difficulty,sv.duration_seconds,sv.environment,sv.conditions,sv.observables,sv.expected_behaviors,sv.forbidden_behaviors,sv.authoring_method,sv.agent_task,sv.agent_visible_context,sv.allowed_actions,sv.allowed_resources,sv.ground_truth,sv.execution_spec,sv.source_metadata,sv.created_at,
	COALESCE((SELECT jsonb_agg(jsonb_build_object('id',c.id,'key',c.key,'name',c.name,'metric_key',c.metric_key,'operator',c.operator,'target_value',c.target_value,'unit',c.unit,'sort_order',c.sort_order,'created_at',c.created_at) ORDER BY c.sort_order,c.id) FROM scenario_criteria c WHERE c.scenario_version_id=sv.id),'[]'::jsonb) criteria
	FROM scenario_versions sv WHERE sv.scenario_id=s.id ORDER BY sv.version DESC LIMIT 1) v ON true LEFT JOIN scenario_version_task_family f ON f.scenario_version_id=v.id`

func (r *Repository) Create(ctx context.Context, db DBTX, workspaceID string, in CreateInput) (Scenario, error) {
	var v Scenario
	err := db.QueryRow(ctx, `INSERT INTO scenarios(owner_workspace_id,slug,name,kind,visibility,status)VALUES($1,$2,$3,'custom',$4,'active')RETURNING id,owner_workspace_id,slug,name,kind,visibility,status,created_at,updated_at`, workspaceID, in.Slug, in.Name, in.Visibility).Scan(&v.ID, &v.OwnerWorkspaceID, &v.Slug, &v.Name, &v.Kind, &v.Visibility, &v.Status, &v.CreatedAt, &v.UpdatedAt)
	return v, mapDB(err)
}
func (r *Repository) CreateVersion(ctx context.Context, db DBTX, scenarioID string, number int, in VersionInput) (Version, error) {
	var v Version
	err := db.QueryRow(ctx, `INSERT INTO scenario_versions(scenario_id,version,description,category,difficulty,duration_seconds,environment,conditions,observables,expected_behaviors,forbidden_behaviors,authoring_method,agent_task,agent_visible_context,allowed_actions,allowed_resources,ground_truth,execution_spec,source_metadata)VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15,$16,$17,$18,$19)RETURNING id,version,description,category,difficulty,duration_seconds,environment,conditions,observables,expected_behaviors,forbidden_behaviors,authoring_method,agent_task,agent_visible_context,allowed_actions,allowed_resources,ground_truth,execution_spec,source_metadata,created_at`, scenarioID, number, in.Description, in.Category, in.Difficulty, in.DurationSeconds, in.Environment, in.Conditions, in.Observables, in.ExpectedBehaviors, in.ForbiddenBehaviors, in.AuthoringMethod, in.AgentTask, in.AgentVisibleContext, in.AllowedActions, in.AllowedResources, in.GroundTruth, in.ExecutionSpec, in.SourceMetadata).Scan(&v.ID, &v.Version, &v.Description, &v.Category, &v.Difficulty, &v.DurationSeconds, &v.Environment, &v.Conditions, &v.Observables, &v.ExpectedBehaviors, &v.ForbiddenBehaviors, &v.AuthoringMethod, &v.AgentTask, &v.AgentVisibleContext, &v.AllowedActions, &v.AllowedResources, &v.GroundTruth, &v.ExecutionSpec, &v.SourceMetadata, &v.CreatedAt)
	return v, mapDB(err)
}
func (r *Repository) CreateCriteria(ctx context.Context, db DBTX, versionID string, inputs []CriterionInput) ([]Criterion, error) {
	items := make([]Criterion, 0, len(inputs))
	for _, in := range inputs {
		var v Criterion
		err := db.QueryRow(ctx, `INSERT INTO scenario_criteria(scenario_version_id,key,name,metric_key,operator,target_value,unit,sort_order)VALUES($1,$2,$3,$4,$5,$6,$7,$8)RETURNING id,key,name,metric_key,operator,target_value,unit,sort_order,created_at`, versionID, in.Key, in.Name, in.MetricKey, in.Operator, in.TargetValue, in.Unit, in.SortOrder).Scan(&v.ID, &v.Key, &v.Name, &v.MetricKey, &v.Operator, &v.TargetValue, &v.Unit, &v.SortOrder, &v.CreatedAt)
		if err != nil {
			return nil, mapDB(err)
		}
		items = append(items, v)
	}
	return items, nil
}
func (r *Repository) GetPublic(ctx context.Context, db DBTX, slug string) (Scenario, error) {
	return scanScenario(db.QueryRow(ctx, `SELECT `+scenarioColumns+` FROM scenarios s `+latestJoin+` WHERE s.slug=$1 AND s.kind='built_in' AND s.visibility='public' AND s.status='active' ORDER BY s.owner_workspace_id NULLS FIRST,s.id LIMIT 1`, slug))
}
func (r *Repository) GetWorkspace(ctx context.Context, db DBTX, workspaceID, scenarioID string) (Scenario, error) {
	return scanScenario(db.QueryRow(ctx, `SELECT `+scenarioColumns+` FROM scenarios s `+latestJoin+` WHERE s.id=$1 AND s.owner_workspace_id=$2`, scenarioID, workspaceID))
}
func (r *Repository) ListPublic(ctx context.Context, db DBTX, f Filters) ([]Scenario, error) {
	return list(ctx, db, `s.kind='built_in' AND s.visibility='public' AND s.status='active'`, nil, f)
}
func (r *Repository) ListWorkspace(ctx context.Context, db DBTX, workspaceID string, f Filters) ([]Scenario, error) {
	return list(ctx, db, `s.owner_workspace_id=$1`, []any{workspaceID}, f)
}
func list(ctx context.Context, db DBTX, where string, args []any, f Filters) ([]Scenario, error) {
	if f.Query != "" {
		args = append(args, "%"+f.Query+"%")
		n := len(args)
		where += fmt.Sprintf(` AND (s.name ILIKE $%d OR s.slug ILIKE $%d OR v.description ILIKE $%d)`, n, n, n)
	}
	add := func(clause string, value any) {
		args = append(args, value)
		where += fmt.Sprintf(" AND "+clause, len(args))
	}
	if f.Category != "" {
		add(`v.category=$%d`, f.Category)
	}
	if f.Difficulty != "" {
		add(`v.difficulty=$%d`, f.Difficulty)
	}
	if f.Status != "" && f.Status != "all" {
		add(`s.status=$%d`, f.Status)
	}
	args = append(args, f.Pagination.Limit, f.Pagination.Offset)
	q := fmt.Sprintf(`SELECT %s FROM scenarios s %s WHERE %s ORDER BY s.created_at DESC,s.id DESC LIMIT $%d OFFSET $%d`, scenarioColumns, latestJoin, where, len(args)-1, len(args))
	rows, err := db.Query(ctx, q, args...)
	if err != nil {
		return nil, mapDB(err)
	}
	defer rows.Close()
	items := []Scenario{}
	for rows.Next() {
		v, err := scanScenario(rows)
		if err != nil {
			return nil, err
		}
		items = append(items, v)
	}
	return items, rows.Err()
}
func (r *Repository) Update(ctx context.Context, db DBTX, workspaceID, scenarioID string, in UpdateInput) (Scenario, error) {
	tag, err := db.Exec(ctx, `UPDATE scenarios SET name=COALESCE($3,name),visibility=COALESCE($4,visibility),status=COALESCE($5,status),updated_at=now() WHERE id=$1 AND owner_workspace_id=$2`, scenarioID, workspaceID, in.Name, in.Visibility, in.Status)
	if err != nil {
		return Scenario{}, mapDB(err)
	}
	if tag.RowsAffected() == 0 {
		return Scenario{}, identity.ErrNotFound
	}
	return r.GetWorkspace(ctx, db, workspaceID, scenarioID)
}
func (r *Repository) LockAndNextVersion(ctx context.Context, tx pgx.Tx, workspaceID, scenarioID string) (int, string, error) {
	var status string
	if err := tx.QueryRow(ctx, `SELECT status FROM scenarios WHERE id=$1 AND owner_workspace_id=$2 FOR UPDATE`, scenarioID, workspaceID).Scan(&status); err != nil {
		return 0, "", mapDB(err)
	}
	var next int
	err := tx.QueryRow(ctx, `SELECT COALESCE(MAX(version),0)+1 FROM scenario_versions WHERE scenario_id=$1`, scenarioID).Scan(&next)
	return next, status, mapDB(err)
}
func (r *Repository) ListVersions(ctx context.Context, db DBTX, workspaceID, scenarioID string, p identity.Pagination) ([]Version, error) {
	rows, err := db.Query(ctx, versionSelect+` WHERE sv.scenario_id=$1 AND s.owner_workspace_id=$2 ORDER BY sv.version DESC LIMIT $3 OFFSET $4`, scenarioID, workspaceID, p.Limit, p.Offset)
	if err != nil {
		return nil, mapDB(err)
	}
	defer rows.Close()
	items := []Version{}
	for rows.Next() {
		v, err := scanVersion(rows)
		if err != nil {
			return nil, err
		}
		items = append(items, v)
	}
	return items, rows.Err()
}
func (r *Repository) GetVersion(ctx context.Context, db DBTX, workspaceID, scenarioID, versionID string) (Version, error) {
	return scanVersion(db.QueryRow(ctx, versionSelect+` WHERE sv.id=$1 AND sv.scenario_id=$2 AND s.owner_workspace_id=$3`, versionID, scenarioID, workspaceID))
}

func (r *Repository) GetRunVersion(ctx context.Context, db DBTX, workspaceID, runID string) (Version, error) {
	return scanVersion(db.QueryRow(ctx, versionSelect+` JOIN evaluation_batches b ON b.scenario_version_id=sv.id JOIN runs r ON r.batch_id=b.id WHERE r.id=$1 AND b.workspace_id=$2`, runID, workspaceID))
}

const versionSelect = `SELECT sv.id,sv.version,sv.description,sv.category,sv.difficulty,sv.duration_seconds,sv.environment,sv.conditions,sv.observables,sv.expected_behaviors,sv.forbidden_behaviors,sv.authoring_method,sv.agent_task,sv.agent_visible_context,sv.allowed_actions,sv.allowed_resources,sv.ground_truth,sv.execution_spec,sv.source_metadata,sv.created_at,COALESCE((SELECT jsonb_agg(jsonb_build_object('id',c.id,'key',c.key,'name',c.name,'metric_key',c.metric_key,'operator',c.operator,'target_value',c.target_value,'unit',c.unit,'sort_order',c.sort_order,'created_at',c.created_at) ORDER BY c.sort_order,c.id)FROM scenario_criteria c WHERE c.scenario_version_id=sv.id),'[]'::jsonb),f.task_family_id,f.case_type,f.contract_version FROM scenario_versions sv JOIN scenarios s ON s.id=sv.scenario_id LEFT JOIN scenario_version_task_family f ON f.scenario_version_id=sv.id`

type scanner interface{ Scan(...any) error }

func scanScenario(row scanner) (Scenario, error) {
	var s Scenario
	var id *string
	var number *int
	var d, cat, diff, authoring, task *string
	var dur *int
	var env, cond, visible, resources, ground, execution, metadata, criteria []byte
	var obs, expected, forbidden, actions []string
	var created *time.Time
	var familyID, caseType, familyContract *string
	err := row.Scan(&s.ID, &s.OwnerWorkspaceID, &s.Slug, &s.Name, &s.Kind, &s.Visibility, &s.Status, &s.CreatedAt, &s.UpdatedAt, &id, &number, &d, &cat, &diff, &dur, &env, &cond, &obs, &expected, &forbidden, &authoring, &task, &visible, &actions, &resources, &ground, &execution, &metadata, &created, &criteria, &familyID, &caseType, &familyContract)
	if err != nil {
		return s, mapDB(err)
	}
	if id != nil {
		v := Version{ID: *id, Version: *number, Description: *d, Category: *cat, Difficulty: *diff, DurationSeconds: *dur, Environment: env, Conditions: cond, Observables: observablecatalog.CanonicalizeKnown(obs), ExpectedBehaviors: expected, ForbiddenBehaviors: forbidden, AuthoringMethod: *authoring, AgentTask: *task, AgentVisibleContext: visible, AllowedActions: actions, AllowedResources: resources, GroundTruth: ground, ExecutionSpec: execution, SourceMetadata: metadata, CreatedAt: *created}
		if familyID != nil {
			v.TaskFamilyID = *familyID
		}
		if caseType != nil {
			v.CaseType = *caseType
		}
		if familyContract != nil {
			v.TaskFamilyContractVersion = *familyContract
		}
		if err = json.Unmarshal(criteria, &v.Criteria); err != nil {
			return s, err
		}
		s.LatestVersion = &v
	}
	return s, nil
}
func scanVersion(row scanner) (Version, error) {
	var v Version
	var criteria []byte
	var familyID, caseType, familyContract *string
	err := row.Scan(&v.ID, &v.Version, &v.Description, &v.Category, &v.Difficulty, &v.DurationSeconds, &v.Environment, &v.Conditions, &v.Observables, &v.ExpectedBehaviors, &v.ForbiddenBehaviors, &v.AuthoringMethod, &v.AgentTask, &v.AgentVisibleContext, &v.AllowedActions, &v.AllowedResources, &v.GroundTruth, &v.ExecutionSpec, &v.SourceMetadata, &v.CreatedAt, &criteria, &familyID, &caseType, &familyContract)
	if err != nil {
		return v, mapDB(err)
	}
	v.Observables = observablecatalog.CanonicalizeKnown(v.Observables)
	if familyID != nil {
		v.TaskFamilyID = *familyID
	}
	if caseType != nil {
		v.CaseType = *caseType
	}
	if familyContract != nil {
		v.TaskFamilyContractVersion = *familyContract
	}
	err = json.Unmarshal(criteria, &v.Criteria)
	return v, err
}
func mapDB(err error) error {
	if err == nil {
		return nil
	}
	if errors.Is(err, pgx.ErrNoRows) {
		return identity.ErrNotFound
	}
	var p *pgconn.PgError
	if errors.As(err, &p) && p.Code == "23505" {
		return identity.ErrConflict
	}
	return err
}
