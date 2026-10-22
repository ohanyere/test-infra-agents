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

// EvidenceDetail deliberately exposes scoped metadata, not raw audit bodies,
// signed receipt payloads, source credentials, or private keys.
type EvidenceDetail struct {
	Reference  string    `json:"reference"`
	Kind       string    `json:"kind"`
	Summary    string    `json:"summary"`
	ObservedAt time.Time `json:"observed_at,omitempty"`
	Digest     string    `json:"digest,omitempty"`
}

func (s *Service) ResolveEvidence(ctx context.Context, p auth.Principal, w, e, sessionID, reference string) (EvidenceDetail, error) {
	var out EvidenceDetail
	if _, err := s.role(ctx, p, w); err != nil {
		return out, err
	}
	if identity.ValidateUUID(e) != nil || identity.ValidateUUID(sessionID) != nil || len(reference) > 256 {
		return out, auth.ErrForbidden
	}
	var exists bool
	if err := s.pool.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM customer_observation_sessions WHERE workspace_id=$1 AND environment_id=$2 AND id=$3)`, w, e, sessionID).Scan(&exists); err != nil {
		return out, err
	}
	if !exists {
		return out, auth.ErrForbidden
	}
	parts := strings.SplitN(reference, ":", 2)
	if len(parts) != 2 || parts[1] == "" {
		return out, identity.ErrValidation
	}
	out.Reference = reference
	out.Kind = parts[0]
	var err error
	switch parts[0] {
	case "audit":
		err = s.pool.QueryRow(ctx, `SELECT event_at,'Authenticated API-server audit event '||source_event_id,encode(source_event_digest,'hex') FROM customer_authenticated_audit_events WHERE workspace_id=$1 AND environment_id=$2 AND session_id=$3 AND source_event_id=$4`, w, e, sessionID, parts[1]).Scan(&out.ObservedAt, &out.Summary, &out.Digest)
		if errors.Is(err, pgx.ErrNoRows) {
			err = s.pool.QueryRow(ctx, `SELECT stage_at,'Forwarded audit candidate; API-server origin unverified: '||source_event_id,source_event_digest FROM customer_kubernetes_audit_records WHERE workspace_id=$1 AND environment_id=$2 AND session_id=$3 AND source_event_id=$4`, w, e, sessionID, parts[1]).Scan(&out.ObservedAt, &out.Summary, &out.Digest)
		}
	case "receipt", "gateway_preflight", "approval", "attribution", "direct_outcome", "claim", "metric_sample", "customer_observation_batch", "reconciliation":
		if identity.ValidateUUID(parts[1]) != nil {
			return EvidenceDetail{}, identity.ErrValidation
		}
		switch parts[0] {
		case "metric_sample":
			err = s.pool.QueryRow(ctx, `SELECT sampled_at,'Forwarded service metric sample; source origin unverified',source_digest FROM customer_service_metric_samples WHERE workspace_id=$1 AND environment_id=$2 AND session_id=$3 AND id=$4`, w, e, sessionID, parts[1]).Scan(&out.ObservedAt, &out.Summary, &out.Digest)
		case "customer_observation_batch":
			err = s.pool.QueryRow(ctx, `SELECT captured_at,'Customer collector observation batch; source origin requires separate assessment' FROM customer_observation_batches WHERE workspace_id=$1 AND environment_id=$2 AND session_id=$3 AND batch_id=$4`, w, e, sessionID, parts[1]).Scan(&out.ObservedAt, &out.Summary)
		case "reconciliation":
			err = s.pool.QueryRow(ctx, `SELECT COALESCE(evaluated_at,created_at),'Audit evidence reconciliation case '||state||' ('||result||')' FROM customer_audit_reconciliation_cases WHERE workspace_id=$1 AND environment_id=$2 AND session_id=$3 AND id=$4`, w, e, sessionID, parts[1]).Scan(&out.ObservedAt, &out.Summary)
		case "claim":
			err = s.pool.QueryRow(ctx, `SELECT submitted_at,'Unverified agent claim '||action_type||' '||resource_namespace||'/'||resource_name||' ('||claimed_outcome||')' FROM customer_agent_action_claims WHERE workspace_id=$1 AND environment_id=$2 AND session_id=$3 AND claim_id=$4`, w, e, sessionID, parts[1]).Scan(&out.ObservedAt, &out.Summary)
		case "receipt":
			err = s.pool.QueryRow(ctx, `SELECT authenticated_at,'Signed execution receipt for '||operation||' '||resource_namespace||'/'||resource_name,encode(payload_digest,'hex') FROM customer_execution_receipts WHERE workspace_id=$1 AND environment_id=$2 AND session_id=$3 AND id=$4`, w, e, sessionID, parts[1]).Scan(&out.ObservedAt, &out.Summary, &out.Digest)
		case "gateway_preflight":
			err = s.pool.QueryRow(ctx, `SELECT decided_at,'Connected gateway '||decision||' '||operation||' '||resource_namespace||'/'||resource_name||' ('||reason_code||')' FROM customer_gateway_preflight_decisions WHERE workspace_id=$1 AND environment_id=$2 AND session_id=$3 AND id=$4`, w, e, sessionID, parts[1]).Scan(&out.ObservedAt, &out.Summary)
		case "approval":
			err = s.pool.QueryRow(ctx, `SELECT recorded_at,'Customer approval decision '||decision||' for '||resource FROM operational_approval_records WHERE workspace_id=$1 AND environment_id=$2 AND session_id=$3 AND id=$4`, w, e, sessionID, parts[1]).Scan(&out.ObservedAt, &out.Summary)
		case "attribution":
			err = s.pool.QueryRow(ctx, `SELECT evaluated_at,'Independent attribution decision '||status||' ('||reason_code||')',encode(decision_digest,'hex') FROM customer_attribution_decisions WHERE workspace_id=$1 AND environment_id=$2 AND session_id=$3 AND id=$4`, w, e, sessionID, parts[1]).Scan(&out.ObservedAt, &out.Summary, &out.Digest)
		case "direct_outcome":
			err = s.pool.QueryRow(ctx, `SELECT observed_at,'Direct source observation '||source_kind,encode(source_digest,'hex') FROM customer_direct_outcome_observations WHERE workspace_id=$1 AND environment_id=$2 AND session_id=$3 AND id=$4`, w, e, sessionID, parts[1]).Scan(&out.ObservedAt, &out.Summary, &out.Digest)
		}
	default:
		return EvidenceDetail{}, identity.ErrValidation
	}
	if errors.Is(err, pgx.ErrNoRows) {
		return EvidenceDetail{}, auth.ErrForbidden
	}
	return out, err
}
