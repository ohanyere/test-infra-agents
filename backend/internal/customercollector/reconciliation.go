package customercollector

import (
	"context"
	"errors"
	"time"

	"github.com/evalplane/evalplane/backend/internal/auth"
	"github.com/evalplane/evalplane/backend/internal/identity"
	"github.com/evalplane/evalplane/backend/internal/kubeaudit"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// queueAuditReconciliation appends a new revision when a structurally relevant
// record arrives, including after a prior completed missing-evidence result.
// It never edits a canonical verification result.
func queueAuditReconciliation(ctx context.Context, tx pgx.Tx, record ForwardedAuditResult, event kubeaudit.Event) error {
	if event.ObjectRef.UID == "" {
		return nil
	}
	rows, err := tx.Query(ctx, `UPDATE customer_audit_reconciliation_cases c SET state='reconciliation_pending',result='none',revision=revision+1,next_attempt_at=now(),lease_until=NULL,lease_token=NULL,attempts=0,updated_at=now()
 FROM customer_agent_action_claims a WHERE a.session_id=c.session_id AND a.claim_id=c.claim_id AND c.workspace_id=$1 AND c.environment_id=$2 AND c.session_id=$3
 AND a.resource_kind='Deployment' AND a.resource_namespace=$4 AND a.resource_name=$5 AND a.resource_uid=$6 AND $7::timestamptz BETWEEN a.claimed_started_at-interval '30 seconds' AND a.claimed_completed_at+interval '30 seconds'
 RETURNING c.id,c.revision`, record.WorkspaceID, record.EnvironmentID, record.SessionID, event.ObjectRef.Namespace, event.ObjectRef.Name, event.ObjectRef.UID, event.StageAt)
	if err != nil {
		return err
	}
	type revision struct {
		id      string
		version int
	}
	var revisions []revision
	for rows.Next() {
		var r revision
		if err = rows.Scan(&r.id, &r.version); err != nil {
			rows.Close()
			return err
		}
		revisions = append(revisions, r)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return err
	}
	for _, r := range revisions {
		if _, err = tx.Exec(ctx, `INSERT INTO customer_audit_reconciliation_events(case_id,revision,state,result,reason,audit_record_id)
 VALUES($1,$2,'reconciliation_pending','none','Collector-forwarded audit candidate arrived; API-server origin remains unverified',$3)`, r.id, r.version, record.ID); err != nil {
			return err
		}
	}
	return nil
}

// ProcessNextAuditReconciliation uses a durable lease. A crash after claiming
// work leaves it recoverable when the lease expires; retries are capped at 3.
func ProcessNextAuditReconciliation(ctx context.Context, pool *pgxpool.Pool) (string, bool, error) {
	if pool == nil {
		return "", false, identity.ErrValidation
	}
	tx, err := pool.Begin(ctx)
	if err != nil {
		return "", false, err
	}
	defer tx.Rollback(ctx)
	var caseID, leaseToken string
	var attempts, revision int
	err = tx.QueryRow(ctx, `SELECT id,attempts,revision FROM customer_audit_reconciliation_cases
 WHERE next_attempt_at<=now() AND (state IN ('expected','received','validated','unavailable','rejected','reconciliation_pending') OR (state='acquiring' AND lease_until<now()))
 ORDER BY next_attempt_at,id LIMIT 1 FOR UPDATE SKIP LOCKED`).Scan(&caseID, &attempts, &revision)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", false, nil
	}
	if err != nil {
		return "", false, err
	}
	if attempts >= 3 {
		count, recordID, countErr := auditCandidateCount(ctx, tx, caseID)
		if countErr != nil {
			return "", false, countErr
		}
		_, result, reason := reconciliationDecision(count, attempts)
		_, err = tx.Exec(ctx, `UPDATE customer_audit_reconciliation_cases SET state='reconciliation_completed',result=$2,revision=revision+1,lease_until=NULL,lease_token=NULL,evaluated_at=now(),last_reason=$3,updated_at=now() WHERE id=$1`, caseID, result, reason)
		if err != nil {
			return "", false, err
		}
		_, err = tx.Exec(ctx, `INSERT INTO customer_audit_reconciliation_events(case_id,revision,state,result,reason,audit_record_id) VALUES($1,$2,'reconciliation_completed',$3,$4,$5)`, caseID, revision+1, result, reason, recordID)
		if err != nil {
			return "", false, err
		}
		if err = tx.Commit(ctx); err != nil {
			return "", false, err
		}
		return caseID, true, nil
	}
	err = tx.QueryRow(ctx, `UPDATE customer_audit_reconciliation_cases SET state='acquiring',attempts=attempts+1,revision=revision+1,lease_until=now()+interval '30 seconds',lease_token=gen_random_uuid(),updated_at=now() WHERE id=$1 RETURNING lease_token`, caseID).Scan(&leaseToken)
	if err != nil {
		return "", false, err
	}
	_, err = tx.Exec(ctx, `INSERT INTO customer_audit_reconciliation_events(case_id,revision,state,result,reason) VALUES($1,$2,'acquiring','none','Reconciling available audit candidates')`, caseID, revision+1)
	if err != nil {
		return "", false, err
	}
	if err = tx.Commit(ctx); err != nil {
		return "", false, err
	}
	return finishAuditReconciliation(ctx, pool, caseID, leaseToken)
}

func finishAuditReconciliation(ctx context.Context, pool *pgxpool.Pool, caseID, leaseToken string) (string, bool, error) {
	tx, err := pool.Begin(ctx)
	if err != nil {
		return "", false, err
	}
	defer tx.Rollback(ctx)
	var attempts, revision int
	var currentLease string
	err = tx.QueryRow(ctx, `SELECT attempts,revision,lease_token FROM customer_audit_reconciliation_cases WHERE id=$1 AND state='acquiring' FOR UPDATE`, caseID).Scan(&attempts, &revision, &currentLease)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", false, identity.ErrConflict
	}
	if err != nil {
		return "", false, err
	}
	if currentLease != leaseToken {
		return "", false, identity.ErrConflict
	}
	count, recordID, err := auditCandidateCount(ctx, tx, caseID)
	if err != nil {
		return "", false, err
	}
	state, result, reason := reconciliationDecision(count, attempts)
	next := time.Now().UTC().Add(time.Duration(attempts*30) * time.Second)
	_, err = tx.Exec(ctx, `UPDATE customer_audit_reconciliation_cases SET state=$2,result=$3,revision=revision+1,lease_until=NULL,lease_token=NULL,next_attempt_at=$4,evaluated_at=now(),last_reason=$5,updated_at=now() WHERE id=$1`, caseID, state, result, next, reason)
	if err != nil {
		return "", false, err
	}
	_, err = tx.Exec(ctx, `INSERT INTO customer_audit_reconciliation_events(case_id,revision,state,result,reason,audit_record_id) VALUES($1,$2,$3,$4,$5,$6)`, caseID, revision+1, state, result, reason, recordID)
	if err != nil {
		return "", false, err
	}
	if err = tx.Commit(ctx); err != nil {
		return "", false, err
	}
	return caseID, true, nil
}

type ReconciliationRevision struct {
	CaseID        string    `json:"case_id"`
	ClaimID       string    `json:"claim_id"`
	Revision      int       `json:"revision"`
	State         string    `json:"state"`
	Result        string    `json:"result"`
	Reason        string    `json:"reason"`
	AuditRecordID *string   `json:"audit_record_id,omitempty"`
	EvaluatedAt   time.Time `json:"evaluated_at"`
}

func (s *Service) ListAuditReconciliation(ctx context.Context, p auth.Principal, workspaceID, environmentID, sessionID string) ([]ReconciliationRevision, error) {
	if identity.ValidateUUID(workspaceID) != nil || identity.ValidateUUID(environmentID) != nil || identity.ValidateUUID(sessionID) != nil {
		return nil, auth.ErrForbidden
	}
	if _, err := s.role(ctx, p, workspaceID); err != nil {
		return nil, err
	}
	var exists bool
	if err := s.pool.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM customer_observation_sessions WHERE workspace_id=$1 AND environment_id=$2 AND id=$3)`, workspaceID, environmentID, sessionID).Scan(&exists); err != nil {
		return nil, err
	}
	if !exists {
		return nil, auth.ErrForbidden
	}
	rows, err := s.pool.Query(ctx, `SELECT c.id,c.claim_id,e.revision,e.state,e.result,e.reason,e.audit_record_id,e.evaluated_at
 FROM customer_audit_reconciliation_cases c JOIN customer_audit_reconciliation_events e ON e.case_id=c.id
 WHERE c.workspace_id=$1 AND c.environment_id=$2 AND c.session_id=$3 ORDER BY c.created_at,c.id,e.revision`, workspaceID, environmentID, sessionID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []ReconciliationRevision{}
	for rows.Next() {
		var r ReconciliationRevision
		if err = rows.Scan(&r.CaseID, &r.ClaimID, &r.Revision, &r.State, &r.Result, &r.Reason, &r.AuditRecordID, &r.EvaluatedAt); err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

func reconciliationDecision(count, attempts int) (state, result, reason string) {
	if count == 1 {
		return "reconciliation_completed", "insufficient", "One structurally matching record arrived, but API-server origin and agent causality remain unverified"
	}
	if count > 1 {
		return "reconciliation_completed", "conflicting", "Multiple audit records match the claimed operation; attribution remains unestablished"
	}
	if attempts >= 3 {
		return "reconciliation_completed", "missing", "No matching audit record arrived after three bounded attempts"
	}
	return "unavailable", "none", "No matching audit record has arrived; attribution remains unestablished"
}

func auditCandidateCount(ctx context.Context, tx pgx.Tx, caseID string) (int, *string, error) {
	var count int
	var recordID *string
	err := tx.QueryRow(ctx, `SELECT count(*),(array_agg(r.id::text ORDER BY r.id))[1] FROM customer_kubernetes_audit_records r
 JOIN customer_audit_reconciliation_cases c ON c.id=$1 AND c.workspace_id=r.workspace_id AND c.environment_id=r.environment_id AND c.session_id=r.session_id
 JOIN customer_agent_action_claims a ON a.session_id=c.session_id AND a.claim_id=c.claim_id
 WHERE r.resource_kind=a.resource_kind AND r.resource_namespace=a.resource_namespace AND r.resource_name=a.resource_name AND r.resource_uid=a.resource_uid
 AND r.stage_at BETWEEN a.claimed_started_at-interval '30 seconds' AND a.claimed_completed_at+interval '30 seconds'`, caseID).Scan(&count, &recordID)
	return count, recordID, err
}
