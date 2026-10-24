package operational

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/evalplane/evalplane/backend/internal/auth"
	"github.com/evalplane/evalplane/backend/internal/identity"
	"github.com/jackc/pgx/v5"
)

type ApprovalInput struct {
	ActionID   string    `json:"action_id"`
	Resource   string    `json:"resource"`
	Decision   string    `json:"decision"`
	Reason     string    `json:"reason"`
	ValidUntil time.Time `json:"valid_until"`
}

// RevokeApproval appends a customer decision; the original approval remains
// intact for historical reports and pre-action audit review.
func (s *Service) RevokeApproval(ctx context.Context, p auth.Principal, w, e, sessionID, approvalID, reason string) error {
	role, err := s.role(ctx, p, w)
	if err != nil {
		return err
	}
	if role != "owner" && role != "admin" {
		return auth.ErrForbidden
	}
	if identity.ValidateUUID(e) != nil || identity.ValidateUUID(sessionID) != nil || identity.ValidateUUID(approvalID) != nil || len(reason) > 1000 {
		return identity.ErrValidation
	}
	var id string
	err = s.pool.QueryRow(ctx, `INSERT INTO operational_approval_revocations(workspace_id,environment_id,session_id,approval_id,revoked_by,reason)
	 SELECT $1,$2,$3,a.id,$5,$6 FROM operational_approval_records a WHERE a.workspace_id=$1 AND a.environment_id=$2 AND a.session_id=$3 AND a.id=$4
	 ON CONFLICT(approval_id) DO NOTHING RETURNING id`, w, e, sessionID, approvalID, p.UserID, reason).Scan(&id)
	if errors.Is(err, pgx.ErrNoRows) {
		return auth.ErrForbidden
	}
	return err
}

// RecordApproval is a customer-user decision scoped to one observation session
// and action ID. It never attests that the action was executed.
func (s *Service) RecordApproval(ctx context.Context, p auth.Principal, w, e, sessionID string, in ApprovalInput) (ApprovalRecord, error) {
	var out ApprovalRecord
	role, err := s.role(ctx, p, w)
	if err != nil {
		return out, err
	}
	if role != "owner" && role != "admin" {
		return out, auth.ErrForbidden
	}
	if identity.ValidateUUID(e) != nil || identity.ValidateUUID(sessionID) != nil || len(in.ActionID) == 0 || len(in.ActionID) > 200 || len(in.Resource) > 256 || len(in.Reason) > 1000 || (in.Decision != "approved" && in.Decision != "denied") {
		return out, identity.ErrValidation
	}
	parts := strings.Split(in.Resource, "/")
	if len(parts) != 2 || parts[0] == "" || parts[1] == "" {
		return out, identity.ErrValidation
	}
	now := s.now().UTC()
	if !in.ValidUntil.After(now) || in.ValidUntil.After(now.Add(24*time.Hour)) {
		return out, identity.ErrValidation
	}
	var runID, status string
	var namespaces []string
	err = s.pool.QueryRow(ctx, `SELECT s.run_id,s.status,e.namespaces FROM customer_observation_sessions s JOIN customer_kubernetes_environments e ON e.id=s.environment_id AND e.workspace_id=s.workspace_id WHERE s.workspace_id=$1 AND s.environment_id=$2 AND s.id=$3 AND e.revoked_at IS NULL`, w, e, sessionID).Scan(&runID, &status, &namespaces)
	if errors.Is(err, pgx.ErrNoRows) {
		return out, auth.ErrForbidden
	}
	if err != nil {
		return out, err
	}
	if status != "active" {
		return out, auth.ErrForbidden
	}
	allowed := false
	for _, ns := range namespaces {
		if ns == parts[0] {
			allowed = true
			break
		}
	}
	if !allowed {
		return out, auth.ErrForbidden
	}
	err = s.pool.QueryRow(ctx, `INSERT INTO operational_approval_records(workspace_id,environment_id,session_id,run_id,action_id,decision,resource,approved_by,reason,valid_until) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10) RETURNING id,recorded_at`, w, e, sessionID, runID, in.ActionID, in.Decision, in.Resource, p.UserID, in.Reason, in.ValidUntil).Scan(&out.ID, &out.RecordedAt)
	if err != nil {
		return out, err
	}
	out.ActionID = in.ActionID
	out.Resource = in.Resource
	out.Decision = in.Decision
	out.ApprovedBy = p.UserID
	out.ValidUntil = in.ValidUntil
	return out, nil
}

func (s *Service) ListApprovals(ctx context.Context, p auth.Principal, w, e, sessionID string) ([]ApprovalRecord, error) {
	if _, err := s.role(ctx, p, w); err != nil {
		return nil, err
	}
	if identity.ValidateUUID(e) != nil || identity.ValidateUUID(sessionID) != nil {
		return nil, auth.ErrForbidden
	}
	rows, err := s.pool.Query(ctx, `SELECT a.id,a.action_id,a.resource,a.decision,a.approved_by,a.recorded_at,a.valid_until,r.revoked_at FROM operational_approval_records a LEFT JOIN operational_approval_revocations r ON r.approval_id=a.id WHERE a.workspace_id=$1 AND a.environment_id=$2 AND a.session_id=$3 ORDER BY a.recorded_at,a.id`, w, e, sessionID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []ApprovalRecord{}
	for rows.Next() {
		var a ApprovalRecord
		if err = rows.Scan(&a.ID, &a.ActionID, &a.Resource, &a.Decision, &a.ApprovedBy, &a.RecordedAt, &a.ValidUntil, &a.RevokedAt); err != nil {
			return nil, err
		}
		out = append(out, a)
	}
	return out, rows.Err()
}
