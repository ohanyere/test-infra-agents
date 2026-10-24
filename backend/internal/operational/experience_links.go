package operational

import (
	"context"
	"time"

	"github.com/evalplane/evalplane/backend/internal/auth"
	"github.com/evalplane/evalplane/backend/internal/identity"
)

// GuidanceOperationLink records a reference supplied by an mTLS-authenticated
// agent execution to the signed customer gateway. It is neither an
// acknowledgement of reading the guidance nor evidence of improvement.
type GuidanceOperationLink struct {
	ID                 string    `json:"id"`
	GuidanceRequestID  string    `json:"guidance_request_id"`
	ExecutionReceiptID string    `json:"execution_receipt_id"`
	AgentVersionID     string    `json:"agent_version_id"`
	SessionID          string    `json:"session_id"`
	RunID              string    `json:"run_id"`
	Relation           string    `json:"relation"`
	AttributionStatus  string    `json:"attribution_status"`
	AssessmentRevision int       `json:"assessment_revision,omitempty"`
	AssessmentStatus   string    `json:"assessment_status,omitempty"`
	LinkedAt           time.Time `json:"linked_at"`
}

func (s *Service) ListGuidanceOperationLinks(ctx context.Context, p auth.Principal, w, e string) ([]GuidanceOperationLink, error) {
	if _, err := s.role(ctx, p, w); err != nil {
		return nil, err
	}
	if identity.ValidateUUID(e) != nil {
		return nil, auth.ErrForbidden
	}
	rows, err := s.pool.Query(ctx, `SELECT l.id,l.guidance_request_id,l.execution_receipt_id,l.agent_version_id,l.session_id,l.run_id,l.relation,
        COALESCE((SELECT d.status FROM customer_attribution_decisions d WHERE d.execution_receipt_id=l.execution_receipt_id ORDER BY d.evaluated_at DESC,d.id DESC LIMIT 1),'INCONCLUSIVE'),
        COALESCE((SELECT r.revision FROM operational_assessment_reports r WHERE r.workspace_id=l.workspace_id AND r.environment_id=l.environment_id AND r.session_id=l.session_id AND r.run_id=l.run_id ORDER BY r.revision DESC LIMIT 1),0),
        COALESCE((SELECT r.report->>'canonical_overall_status' FROM operational_assessment_reports r WHERE r.workspace_id=l.workspace_id AND r.environment_id=l.environment_id AND r.session_id=l.session_id AND r.run_id=l.run_id ORDER BY r.revision DESC LIMIT 1),''),l.linked_at
        FROM operational_guidance_operation_links l WHERE l.workspace_id=$1 AND l.environment_id=$2 ORDER BY l.linked_at DESC,l.id DESC LIMIT 100`, w, e)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []GuidanceOperationLink{}
	for rows.Next() {
		var x GuidanceOperationLink
		if err = rows.Scan(&x.ID, &x.GuidanceRequestID, &x.ExecutionReceiptID, &x.AgentVersionID, &x.SessionID, &x.RunID, &x.Relation, &x.AttributionStatus, &x.AssessmentRevision, &x.AssessmentStatus, &x.LinkedAt); err != nil {
			return nil, err
		}
		out = append(out, x)
	}
	return out, rows.Err()
}
