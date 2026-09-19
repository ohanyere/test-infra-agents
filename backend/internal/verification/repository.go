package verification

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/evalplane/evalplane/backend/internal/evidence"
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

func (r *Repository) LoadInput(ctx context.Context, db DBTX, workspaceID, runID string) (BuildInput, error) {
	var in BuildInput
	var evaluationID, evaluationStatus, evaluationSummary string
	var environmentID, planID, planHash *string
	err := db.QueryRow(ctx, `SELECT b.workspace_id,r.id,r.agent_version_id,b.scenario_version_id,r.environment_instance_id,r.composition_plan_id,cp.plan_hash,sv.agent_task,b.source_metadata,r.started_at,r.completed_at,e.id,e.result,e.summary,e.safety_violation_count
		FROM runs r JOIN evaluation_batches b ON b.id=r.batch_id JOIN scenario_versions sv ON sv.id=b.scenario_version_id JOIN evaluations e ON e.run_id=r.id LEFT JOIN composition_plans cp ON cp.id=r.composition_plan_id
		WHERE b.workspace_id=$1 AND r.id=$2`, workspaceID, runID).Scan(&in.WorkspaceID, &in.RunID, &in.AgentVersionID, &in.ScenarioVersionID, &environmentID, &planID, &planHash, &in.Objective, &in.SourceMetadata, &in.RunStartedAt, &in.RunCompletedAt, &evaluationID, &evaluationStatus, &evaluationSummary, &in.Evaluation.SafetyViolations)
	if err != nil {
		return in, mapError(err)
	}
	if environmentID != nil {
		in.EnvironmentInstanceID = *environmentID
	}
	if planID != nil {
		in.CompositionPlanID = *planID
	}
	if planHash != nil {
		in.CompositionPlanHash = *planHash
	}
	in.Evaluation.Status = evaluationStatus
	in.Evaluation.Summary = evaluationSummary
	rows, err := db.Query(ctx, `SELECT key,metric_key,operator,target_value FROM scenario_criteria WHERE scenario_version_id=$1 ORDER BY sort_order,id`, in.ScenarioVersionID)
	if err != nil {
		return in, err
	}
	for rows.Next() {
		var c Criterion
		if err = rows.Scan(&c.Key, &c.Metric, &c.Operator, &c.Target); err != nil {
			rows.Close()
			return in, err
		}
		in.Criteria = append(in.Criteria, c)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return in, err
	}
	rows, err = db.Query(ctx, `SELECT id,run_id,sequence,event_type,occurred_at,summary,payload,created_at FROM run_events WHERE run_id=$1 ORDER BY sequence`, runID)
	if err != nil {
		return in, err
	}
	for rows.Next() {
		var e evidence.RunEvent
		if err = rows.Scan(&e.ID, &e.RunID, &e.Sequence, &e.EventType, &e.OccurredAt, &e.Summary, &e.Payload, &e.CreatedAt); err != nil {
			rows.Close()
			return in, err
		}
		in.Events = append(in.Events, e)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return in, err
	}
	rows, err = db.Query(ctx, `SELECT id,run_id,run_event_id,action_type,target_type,target_ref,status,authorized,request_payload,result_payload,started_at,completed_at,created_at FROM agent_actions WHERE run_id=$1 ORDER BY created_at,id`, runID)
	if err != nil {
		return in, err
	}
	for rows.Next() {
		var a evidence.AgentAction
		if err = rows.Scan(&a.ID, &a.RunID, &a.RunEventID, &a.ActionType, &a.TargetType, &a.TargetRef, &a.Status, &a.Authorized, &a.RequestPayload, &a.ResultPayload, &a.StartedAt, &a.CompletedAt, &a.CreatedAt); err != nil {
			rows.Close()
			return in, err
		}
		in.Actions = append(in.Actions, a)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return in, err
	}
	rows, err = db.Query(ctx, `SELECT kind,summary,evidence FROM evaluation_findings WHERE evaluation_id=$1 ORDER BY sort_order,id`, evaluationID)
	if err != nil {
		return in, err
	}
	for rows.Next() {
		var f Finding
		var raw json.RawMessage
		if err = rows.Scan(&f.Type, &f.Summary, &raw); err != nil {
			rows.Close()
			return in, err
		}
		f.Severity = "warning"
		if f.Type == "safety_violation" || in.Evaluation.SafetyViolations > 0 {
			f.Severity = "error"
			f.Blocking = true
		}
		var refs struct {
			EvidenceReferences []string `json:"evidence_references"`
		}
		_ = json.Unmarshal(raw, &refs)
		f.Evidence = refs.EvidenceReferences
		in.Evaluation.Findings = append(in.Evaluation.Findings, f)
	}
	err = rows.Err()
	rows.Close()
	return in, err
}

func (r *Repository) Insert(ctx context.Context, db DBTX, v Result) (Result, error) {
	b, _ := json.Marshal(v.Baseline)
	f, _ := json.Marshal(v.Final)
	intent, _ := json.Marshal(v.Intent)
	actions, _ := json.Marshal(v.ActionInspection)
	diff, _ := json.Marshal(v.InfrastructureDiff)
	outcome, _ := json.Marshal(v.Outcome)
	side, _ := json.Marshal(v.SideEffects)
	stability, _ := json.Marshal(v.Stability)
	risks, _ := json.Marshal(v.ForwardRisks)
	err := db.QueryRow(ctx, `INSERT INTO verification_results(workspace_id,run_id,agent_version_id,scenario_version_id,environment_instance_id,composition_plan_id,composition_plan_hash,verifier_contract_version,snapshot_contract_version,baseline_snapshot,final_snapshot,intent_result,action_inspection,infrastructure_diff,outcome_result,side_effect_result,stability_result,forward_risk_findings,platform_validity,overall_status,reason,source_metadata,evidence_started_at,evidence_completed_at)
		VALUES($1,$2,$3,$4,NULLIF($5,''),NULLIF($6,''),NULLIF($7,''),$8,$9,$10,$11,$12,$13,$14,$15,$16,$17,$18,$19,$20,$21,$22,$23,$24) ON CONFLICT(run_id) DO NOTHING RETURNING id,created_at`, v.WorkspaceID, v.RunID, v.AgentVersionID, v.ScenarioVersionID, v.EnvironmentInstanceID, v.CompositionPlanID, v.CompositionPlanHash, v.VerifierContractVersion, v.SnapshotContractVersion, b, f, intent, actions, diff, outcome, side, stability, risks, v.Validity, v.OverallStatus, v.Reason, v.SourceMetadata, v.EvidenceStartedAt, v.EvidenceCompletedAt).Scan(&v.ID, &v.CreatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return r.Get(ctx, db, v.WorkspaceID, v.RunID)
	}
	return v, mapError(err)
}
func (r *Repository) Get(ctx context.Context, db DBTX, workspaceID, runID string) (Result, error) {
	var v Result
	var env, plan, hash *string
	var b, f, i, a, d, o, s, st, risks json.RawMessage
	err := db.QueryRow(ctx, `SELECT id,workspace_id,run_id,agent_version_id,scenario_version_id,environment_instance_id,composition_plan_id,composition_plan_hash,verifier_contract_version,snapshot_contract_version,baseline_snapshot,final_snapshot,intent_result,action_inspection,infrastructure_diff,outcome_result,side_effect_result,stability_result,forward_risk_findings,platform_validity,overall_status,reason,source_metadata,evidence_started_at,evidence_completed_at,created_at FROM verification_results WHERE workspace_id=$1 AND run_id=$2`, workspaceID, runID).Scan(&v.ID, &v.WorkspaceID, &v.RunID, &v.AgentVersionID, &v.ScenarioVersionID, &env, &plan, &hash, &v.VerifierContractVersion, &v.SnapshotContractVersion, &b, &f, &i, &a, &d, &o, &s, &st, &risks, &v.Validity, &v.OverallStatus, &v.Reason, &v.SourceMetadata, &v.EvidenceStartedAt, &v.EvidenceCompletedAt, &v.CreatedAt)
	if err != nil {
		return v, mapError(err)
	}
	if env != nil {
		v.EnvironmentInstanceID = *env
	}
	if plan != nil {
		v.CompositionPlanID = *plan
	}
	if hash != nil {
		v.CompositionPlanHash = *hash
	}
	for _, pair := range []struct {
		raw json.RawMessage
		dst any
	}{{b, &v.Baseline}, {f, &v.Final}, {i, &v.Intent}, {a, &v.ActionInspection}, {d, &v.InfrastructureDiff}, {o, &v.Outcome}, {s, &v.SideEffects}, {st, &v.Stability}, {risks, &v.ForwardRisks}} {
		if err = json.Unmarshal(pair.raw, pair.dst); err != nil {
			return v, fmt.Errorf("decode verification result: %w", err)
		}
	}
	return v, nil
}
func mapError(err error) error {
	if errors.Is(err, pgx.ErrNoRows) {
		return identity.ErrNotFound
	}
	return err
}
