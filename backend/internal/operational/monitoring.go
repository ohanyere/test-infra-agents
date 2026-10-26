package operational

import (
	"context"
	"errors"
	"time"

	"github.com/evalplane/evalplane/backend/internal/auth"
	"github.com/evalplane/evalplane/backend/internal/identity"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

type MonitoringInput struct {
	Mode            string   `json:"mode,omitempty"`
	SourceIDs       []string `json:"source_ids"`
	CadenceSeconds  int      `json:"cadence_seconds"`
	DurationSeconds int      `json:"duration_seconds"`
}
type MonitoringPlan struct {
	Mode                  string     `json:"mode"`
	SessionID             string     `json:"session_id"`
	WorkspaceID           string     `json:"workspace_id"`
	EnvironmentID         string     `json:"environment_id"`
	State                 string     `json:"state"`
	SourceIDs             []string   `json:"source_ids"`
	CadenceSeconds        int        `json:"cadence_seconds"`
	EndsAt                time.Time  `json:"ends_at"`
	NextDueAt             time.Time  `json:"next_due_at"`
	CycleNumber           int        `json:"cycle_number"`
	Attempts              int        `json:"attempts"`
	LastError             string     `json:"last_error"`
	LastSuccessAt         *time.Time `json:"last_success_at,omitempty"`
	BaselineObservationID string     `json:"baseline_observation_id,omitempty"`
	BaselineAcquiredAt    *time.Time `json:"baseline_acquired_at,omitempty"`
	LastAuditAt           *time.Time `json:"last_audit_at,omitempty"`
	IntegrityGap          bool       `json:"integrity_gap"`
}
type MonitoringEvent struct {
	ID            string    `json:"id"`
	CycleID       string    `json:"cycle_id"`
	CycleNumber   int       `json:"cycle_number"`
	Attempt       int       `json:"attempt"`
	SourceID      string    `json:"source_id"`
	Status        string    `json:"status"`
	ObservationID string    `json:"observation_id,omitempty"`
	Reason        string    `json:"reason"`
	RecordedAt    time.Time `json:"recorded_at"`
}
type MonitoringView struct {
	Plan   *MonitoringPlan   `json:"plan"`
	Events []MonitoringEvent `json:"events"`
}

func (s *Service) ConfigureMonitoring(ctx context.Context, p auth.Principal, w, e, session string, in MonitoringInput) (MonitoringPlan, error) {
	var zero MonitoringPlan
	role, err := s.role(ctx, p, w)
	if err != nil {
		return zero, err
	}
	if role != "owner" && role != "admin" {
		return zero, auth.ErrForbidden
	}
	if in.Mode == "" {
		in.Mode = "periodic"
	}
	if identity.ValidateUUID(e) != nil || identity.ValidateUUID(session) != nil || in.CadenceSeconds < 60 || in.CadenceSeconds > 3600 || in.DurationSeconds < in.CadenceSeconds*2 || in.DurationSeconds > 7*24*3600 || (in.DurationSeconds+in.CadenceSeconds-1)/in.CadenceSeconds > 288 || len(in.SourceIDs) == 0 || len(in.SourceIDs) > 6 || (in.Mode != "periodic" && in.Mode != "deployment_audit") || (in.Mode == "deployment_audit" && len(in.SourceIDs) != 1) {
		return zero, identity.ErrValidation
	}
	seen := map[string]bool{}
	for _, id := range in.SourceIDs {
		if identity.ValidateUUID(id) != nil || seen[id] {
			return zero, identity.ErrValidation
		}
		seen[id] = true
	}
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return zero, err
	}
	defer tx.Rollback(ctx)
	var run, status string
	err = tx.QueryRow(ctx, `SELECT s.run_id,s.status FROM customer_observation_sessions s JOIN customer_kubernetes_environments e ON e.id=s.environment_id AND e.workspace_id=s.workspace_id WHERE s.workspace_id=$1 AND s.environment_id=$2 AND s.id=$3 AND e.revoked_at IS NULL FOR UPDATE OF s`, w, e, session).Scan(&run, &status)
	if err != nil || status != "completed" {
		return zero, auth.ErrForbidden
	}
	var verified bool
	if err = tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM verification_results v JOIN runs r ON r.id=v.run_id JOIN evaluation_batches b ON b.id=r.batch_id WHERE v.workspace_id=$1 AND v.run_id=$2 AND ($3<>'deployment_audit' OR (v.overall_status='INCONCLUSIVE' AND b.source_metadata->>'execution_mode'='observation_only')))`, w, run, in.Mode).Scan(&verified); err != nil || !verified {
		return zero, auth.ErrForbidden
	}
	for _, id := range in.SourceIDs {
		var valid bool
		if err = tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM customer_outcome_sources WHERE id=$1 AND workspace_id=$2 AND environment_id=$3 AND revoked_at IS NULL AND expires_at>now() AND ($4<>'deployment_audit' OR source_kind='kubernetes_state'))`, id, w, e, in.Mode).Scan(&valid); err != nil || !valid {
			return zero, auth.ErrForbidden
		}
	}
	if in.Mode == "deployment_audit" {
		var auditRegistered bool
		if err = tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM customer_audit_mtls_sources WHERE workspace_id=$1 AND environment_id=$2 AND revoked_at IS NULL AND expires_at>now())`, w, e).Scan(&auditRegistered); err != nil || !auditRegistered {
			return zero, auth.ErrForbidden
		}
	}
	var existing string
	err = tx.QueryRow(ctx, `SELECT state FROM assurance_monitoring_plans WHERE session_id=$1`, session).Scan(&existing)
	if err == nil {
		return zero, identity.ErrConflict
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return zero, err
	}
	now := s.now().UTC()
	ends := now.Add(time.Duration(in.DurationSeconds) * time.Second)
	state := "active"
	if in.Mode == "deployment_audit" {
		state = "initializing"
	}
	deploymentSource := ""
	if in.Mode == "deployment_audit" {
		deploymentSource = in.SourceIDs[0]
	}
	_, err = tx.Exec(ctx, `INSERT INTO assurance_monitoring_plans(session_id,workspace_id,environment_id,run_id,cadence_seconds,ends_at,next_due_at,created_by,mode,state,deployment_source_id) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,NULLIF($11,'')::uuid)`, session, w, e, run, in.CadenceSeconds, ends, now, p.UserID, in.Mode, state, deploymentSource)
	if err != nil {
		return zero, err
	}
	for _, id := range in.SourceIDs {
		if _, err = tx.Exec(ctx, `INSERT INTO assurance_monitoring_plan_sources(session_id,source_id) VALUES($1,$2)`, session, id); err != nil {
			return zero, err
		}
	}
	if err = tx.Commit(ctx); err != nil {
		return zero, err
	}
	return MonitoringPlan{SessionID: session, WorkspaceID: w, EnvironmentID: e, Mode: in.Mode, State: state, SourceIDs: in.SourceIDs, CadenceSeconds: in.CadenceSeconds, EndsAt: ends, NextDueAt: now}, nil
}

func (s *Service) Monitoring(ctx context.Context, p auth.Principal, w, e, session string) (MonitoringView, error) {
	var out MonitoringView
	out.Events = []MonitoringEvent{}
	if _, err := s.role(ctx, p, w); err != nil {
		return out, err
	}
	if identity.ValidateUUID(e) != nil || identity.ValidateUUID(session) != nil {
		return out, auth.ErrForbidden
	}
	var exists bool
	if err := s.pool.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM customer_observation_sessions WHERE workspace_id=$1 AND environment_id=$2 AND id=$3)`, w, e, session).Scan(&exists); err != nil || !exists {
		return out, auth.ErrForbidden
	}
	var plan MonitoringPlan
	err := s.pool.QueryRow(ctx, `SELECT session_id,workspace_id,environment_id,mode,state,cadence_seconds,ends_at,next_due_at,cycle_number,attempts,last_error,last_success_at,COALESCE(baseline_observation_id::text,''),baseline_acquired_at,last_audit_at,integrity_gap FROM assurance_monitoring_plans WHERE workspace_id=$1 AND environment_id=$2 AND session_id=$3`, w, e, session).Scan(&plan.SessionID, &plan.WorkspaceID, &plan.EnvironmentID, &plan.Mode, &plan.State, &plan.CadenceSeconds, &plan.EndsAt, &plan.NextDueAt, &plan.CycleNumber, &plan.Attempts, &plan.LastError, &plan.LastSuccessAt, &plan.BaselineObservationID, &plan.BaselineAcquiredAt, &plan.LastAuditAt, &plan.IntegrityGap)
	if err == nil {
		rows, qerr := s.pool.Query(ctx, `SELECT source_id FROM assurance_monitoring_plan_sources WHERE session_id=$1 ORDER BY source_id`, session)
		if qerr != nil {
			return out, qerr
		}
		for rows.Next() {
			var id string
			if qerr = rows.Scan(&id); qerr != nil {
				rows.Close()
				return out, qerr
			}
			plan.SourceIDs = append(plan.SourceIDs, id)
		}
		qerr = rows.Err()
		rows.Close()
		if qerr != nil {
			return out, qerr
		}
		out.Plan = &plan
	} else if !errors.Is(err, pgx.ErrNoRows) {
		return out, err
	}
	rows, err := s.pool.Query(ctx, `SELECT id,cycle_id,cycle_number,attempt,source_id,status,COALESCE(observation_id::text,''),reason,recorded_at FROM assurance_monitoring_events WHERE workspace_id=$1 AND environment_id=$2 AND session_id=$3 ORDER BY recorded_at DESC,id DESC LIMIT 1200`, w, e, session)
	if err != nil {
		return out, err
	}
	defer rows.Close()
	for rows.Next() {
		var item MonitoringEvent
		if err = rows.Scan(&item.ID, &item.CycleID, &item.CycleNumber, &item.Attempt, &item.SourceID, &item.Status, &item.ObservationID, &item.Reason, &item.RecordedAt); err != nil {
			return out, err
		}
		out.Events = append(out.Events, item)
	}
	if err = rows.Err(); err != nil {
		return out, err
	}
	for left, right := 0, len(out.Events)-1; left < right; left, right = left+1, right-1 {
		out.Events[left], out.Events[right] = out.Events[right], out.Events[left]
	}
	return out, nil
}

func (s *Service) ChangeMonitoring(ctx context.Context, p auth.Principal, w, e, session, action string) (MonitoringPlan, error) {
	var zero MonitoringPlan
	role, err := s.role(ctx, p, w)
	if err != nil {
		return zero, err
	}
	if role != "owner" && role != "admin" {
		return zero, auth.ErrForbidden
	}
	if identity.ValidateUUID(e) != nil || identity.ValidateUUID(session) != nil {
		return zero, identity.ErrValidation
	}
	var from, to string
	switch action {
	case "suspend":
		from = "active"
		to = "suspended"
	case "resume":
		from = "suspended"
		to = "active"
	case "retry":
		from = "failed"
		to = "active"
	case "cancel":
		to = "cancelled"
	default:
		return zero, identity.ErrValidation
	}
	query := `UPDATE assurance_monitoring_plans SET state=$4,attempts=0,lease_until=NULL,next_due_at=now(),updated_at=now() WHERE workspace_id=$1 AND environment_id=$2 AND session_id=$3 AND state=$5 AND ends_at>now() RETURNING session_id`
	if action == "cancel" {
		query = `UPDATE assurance_monitoring_plans SET state='cancelled',lease_until=NULL,updated_at=now() WHERE workspace_id=$1 AND environment_id=$2 AND session_id=$3 AND state IN ('initializing','active','degraded','suspended','failed') RETURNING session_id`
	}
	var id string
	if action == "cancel" {
		err = s.pool.QueryRow(ctx, query, w, e, session).Scan(&id)
	} else {
		if action == "resume" {
			query = `UPDATE assurance_monitoring_plans SET state=CASE WHEN mode='deployment_audit' AND baseline_observation_id IS NULL THEN 'initializing' WHEN mode='deployment_audit' AND integrity_gap THEN 'degraded' ELSE 'active' END,attempts=0,lease_until=NULL,next_due_at=now(),updated_at=now() WHERE workspace_id=$1 AND environment_id=$2 AND session_id=$3 AND state='suspended' AND ends_at>now() RETURNING session_id`
			err = s.pool.QueryRow(ctx, query, w, e, session).Scan(&id)
		} else {
			err = s.pool.QueryRow(ctx, query, w, e, session, to, from).Scan(&id)
		}
	}
	if errors.Is(err, pgx.ErrNoRows) {
		return zero, auth.ErrForbidden
	}
	if err != nil {
		return zero, err
	}
	view, err := s.Monitoring(ctx, p, w, e, session)
	if err != nil {
		return zero, err
	}
	if view.Plan == nil {
		return zero, auth.ErrForbidden
	}
	return *view.Plan, nil
}

func newCycleID() string { return uuid.NewString() }
