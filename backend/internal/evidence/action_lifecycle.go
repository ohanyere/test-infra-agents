package evidence

import (
	"context"
	"encoding/json"
	"fmt"
	"github.com/evalplane/evalplane/backend/internal/identity"
	"time"
)

type UpdateActionInput struct {
	RunID, ActionID, Status string
	Authorized              bool
	ResultPayload           json.RawMessage
}

func validActionTransition(from, to string) bool {
	return (from == "proposed" && (to == "allowed" || to == "denied" || to == "failed")) || (from == "allowed" && (to == "executed" || to == "failed" || to == "denied"))
}

// UpdateAgentAction advances one existing proposal. Run ownership is checked in SQL.
func (s *Service) UpdateAgentAction(ctx context.Context, in UpdateActionInput) error {
	if identity.ValidateUUID(in.RunID) != nil || identity.ValidateUUID(in.ActionID) != nil {
		return invalid("invalid action or run id")
	}
	if in.Status != "allowed" && in.Status != "denied" && in.Status != "executed" && in.Status != "failed" {
		return invalid("invalid action transition")
	}
	if (in.Status == "denied" && in.Authorized) || ((in.Status == "allowed" || in.Status == "executed") && !in.Authorized) {
		return invalid("inconsistent authorization")
	}
	payload, ok := validateObject(in.ResultPayload)
	if !ok {
		return invalid("action result must be an object")
	}
	tag, err := s.pool.Exec(ctx, `UPDATE agent_actions SET status=$3,authorized=$4,result_payload=$5,started_at=CASE WHEN $3='allowed' THEN COALESCE(started_at,$6) ELSE started_at END,completed_at=CASE WHEN $3 IN ('denied','executed','failed') THEN COALESCE(completed_at,$6) ELSE completed_at END WHERE id=$1 AND run_id=$2 AND ((status='proposed' AND $3 IN ('allowed','denied','failed')) OR (status='allowed' AND $3 IN ('executed','failed','denied')) OR (status=$3 AND authorized=$4 AND result_payload=$5))`, in.ActionID, in.RunID, in.Status, in.Authorized, payload, time.Now().UTC())
	if err != nil {
		return mapDB(err)
	}
	if tag.RowsAffected() != 1 {
		return fmt.Errorf("%w: action transition rejected", identity.ErrConflict)
	}
	return nil
}
