package customercollector

import (
	"bytes"
	"context"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/evalplane/evalplane/backend/internal/auth"
	"github.com/evalplane/evalplane/backend/internal/identity"
	"github.com/evalplane/evalplane/backend/internal/kubeaudit"
	"github.com/jackc/pgx/v5"
)

const AuditCandidateTrust = "INSUFFICIENT_PROVENANCE"

type ForwardedAuditInput struct {
	ClaimedClusterIdentity string          `json:"claimed_cluster_identity"`
	Event                  json.RawMessage `json:"event"`
}

type ForwardedAuditResult struct {
	ID                            string    `json:"id"`
	WorkspaceID                   string    `json:"workspace_id"`
	EnvironmentID                 string    `json:"environment_id"`
	SessionID                     string    `json:"session_id"`
	RunID                         string    `json:"run_id"`
	SourceEventID                 string    `json:"source_event_id"`
	SourceEventDigest             string    `json:"source_event_digest"`
	CollectorAuthenticationStatus string    `json:"collector_authentication_status"`
	AuditOriginStatus             string    `json:"audit_origin_status"`
	TrustClassification           string    `json:"trust_classification"`
	Duplicate                     bool      `json:"duplicate"`
	AcquiredAt                    time.Time `json:"acquired_at"`
	PersistedAt                   time.Time `json:"persisted_at"`
}

// IngestForwardedAudit authenticates the registered collector transport. The
// forwarded JSON remains unverified as an API-server-origin record and cannot
// enter customer_action_evidence or establish an agent identity.
func (s *Service) IngestForwardedAudit(ctx context.Context, sessionID, token string, in ForwardedAuditInput) (ForwardedAuditResult, error) {
	if identity.ValidateUUID(sessionID) != nil || !validToken(token) {
		return ForwardedAuditResult{}, auth.ErrForbidden
	}
	if len(in.Event) == 0 || len(in.Event) > 1<<20 {
		return ForwardedAuditResult{}, validation("audit event must be between 1 byte and 1 MiB")
	}
	events, err := kubeaudit.Parse(bytes.NewReader(in.Event))
	if err != nil || len(events) != 1 {
		return ForwardedAuditResult{}, validation("a single complete Kubernetes audit event is required")
	}
	event := events[0]
	if event.Stage != "ResponseComplete" || (event.Verb != "patch" && event.Verb != "update") || event.ObjectRef.APIGroup != "apps" || event.ObjectRef.APIVersion != "v1" || event.ObjectRef.Resource != "deployments" || event.Response.Code < 200 || event.Response.Code >= 300 {
		return ForwardedAuditResult{}, validation("only successful Deployment mutations are accepted")
	}
	if len(event.AuditID) > 256 || len(event.ObjectRef.Namespace) > 63 || len(event.ObjectRef.Name) > 253 || len(event.ObjectRef.UID) > 128 || event.ObjectRef.Namespace == "" || event.ObjectRef.Name == "" || event.RequestAt.After(event.StageAt) {
		return ForwardedAuditResult{}, validation("audit resource or timestamps are invalid")
	}
	encoded, err := json.Marshal(event)
	if err != nil {
		return ForwardedAuditResult{}, err
	}
	digest := sha256.Sum256(encoded)
	digestText := "sha256:" + hex.EncodeToString(digest[:])
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return ForwardedAuditResult{}, err
	}
	defer tx.Rollback(ctx)
	var out ForwardedAuditResult
	var status, collectorStatus, registeredCluster, sourceKind, bindingStatus string
	var namespaces, kinds []string
	var storedHash []byte
	var credentialVersion int64
	var revokedAt, completedAt *time.Time
	var createdAt, dbNow time.Time
	err = tx.QueryRow(ctx, `SELECT s.workspace_id,s.environment_id,s.run_id,s.status,s.created_at,s.completed_at,e.cluster_identifier,e.namespaces,e.resource_kinds,e.revoked_at,c.credential_hash,c.credential_version,c.status,a.source_kind,a.cluster_binding_status,now()
 FROM customer_observation_sessions s JOIN customer_kubernetes_environments e ON e.id=s.environment_id AND e.workspace_id=s.workspace_id
 JOIN customer_kubernetes_collectors c ON c.environment_id=e.id
 JOIN customer_audit_source_registrations a ON a.environment_id=e.id AND a.workspace_id=e.workspace_id
 WHERE s.id=$1 FOR UPDATE OF s,e,c`, sessionID).Scan(&out.WorkspaceID, &out.EnvironmentID, &out.RunID, &status, &createdAt, &completedAt, &registeredCluster, &namespaces, &kinds, &revokedAt, &storedHash, &credentialVersion, &collectorStatus, &sourceKind, &bindingStatus, &dbNow)
	if errors.Is(err, pgx.ErrNoRows) {
		return ForwardedAuditResult{}, auth.ErrForbidden
	}
	if err != nil {
		return ForwardedAuditResult{}, err
	}
	if revokedAt != nil || collectorStatus != "active" || subtle.ConstantTimeCompare(tokenDigest(token), storedHash) != 1 || (status != "active" && status != "completed") || sourceKind != "collector_forwarded_audit_unverified" || bindingStatus != "registered_unverified" {
		return ForwardedAuditResult{}, auth.ErrForbidden
	}
	if in.ClaimedClusterIdentity != registeredCluster || !containsAuditScope(namespaces, event.ObjectRef.Namespace) || !containsAuditScope(kinds, "Deployment") {
		return ForwardedAuditResult{}, auth.ErrForbidden
	}
	if event.RequestAt.Before(createdAt) || event.StageAt.Before(createdAt) || event.StageAt.After(dbNow.Add(30*time.Second)) || (completedAt != nil && event.StageAt.After(*completedAt)) {
		return ForwardedAuditResult{}, validation("audit event is outside the session mutation window")
	}
	out.SessionID = sessionID
	out.SourceEventID = event.AuditID
	out.SourceEventDigest = digestText
	out.CollectorAuthenticationStatus = "verified"
	out.AuditOriginStatus = "unverified"
	out.TrustClassification = AuditCandidateTrust
	out.AcquiredAt = dbNow.UTC()
	err = tx.QueryRow(ctx, `INSERT INTO customer_kubernetes_audit_records(workspace_id,environment_id,session_id,run_id,collector_credential_version,registered_cluster_identity,source_event_id,source_event_digest,operation,resource_kind,resource_namespace,resource_name,resource_uid,kubernetes_principal,request_received_at,stage_at,acquired_at,collector_authentication_status,audit_origin_status,trust_classification,event_payload)
 VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,'Deployment',$10,$11,$12,$13,$14,$15,$16,'verified','unverified','INSUFFICIENT_PROVENANCE',$17::jsonb)
 ON CONFLICT DO NOTHING RETURNING id,persisted_at`, out.WorkspaceID, out.EnvironmentID, sessionID, out.RunID, credentialVersion, registeredCluster, event.AuditID, digestText, event.Verb, event.ObjectRef.Namespace, event.ObjectRef.Name, event.ObjectRef.UID, event.User.Username, event.RequestAt, event.StageAt, dbNow, string(encoded)).Scan(&out.ID, &out.PersistedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		var priorSession, priorDigest string
		err = tx.QueryRow(ctx, `SELECT id,session_id,source_event_digest,acquired_at,persisted_at FROM customer_kubernetes_audit_records WHERE workspace_id=$1 AND environment_id=$2 AND source_event_id=$3`, out.WorkspaceID, out.EnvironmentID, event.AuditID).Scan(&out.ID, &priorSession, &priorDigest, &out.AcquiredAt, &out.PersistedAt)
		if err != nil {
			return ForwardedAuditResult{}, err
		}
		if priorSession != sessionID || priorDigest != digestText {
			return ForwardedAuditResult{}, identity.ErrConflict
		}
		out.Duplicate = true
	} else if err != nil {
		return ForwardedAuditResult{}, fmt.Errorf("persist forwarded audit candidate: %w", err)
	}
	if !out.Duplicate {
		if err = queueAuditReconciliation(ctx, tx, out, event); err != nil {
			return ForwardedAuditResult{}, err
		}
	}
	if err = tx.Commit(ctx); err != nil {
		return ForwardedAuditResult{}, err
	}
	return out, nil
}

func containsAuditScope(values []string, target string) bool {
	for _, v := range values {
		if v == target {
			return true
		}
	}
	return false
}

// ListForwardedAudits exposes metadata only to a member of the owning workspace.
func (s *Service) ListForwardedAudits(ctx context.Context, p auth.Principal, workspaceID, environmentID, sessionID string) ([]ForwardedAuditResult, error) {
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
	rows, err := s.pool.Query(ctx, `SELECT id,workspace_id,environment_id,session_id,run_id,source_event_id,source_event_digest,collector_authentication_status,audit_origin_status,trust_classification,acquired_at,persisted_at
 FROM customer_kubernetes_audit_records WHERE workspace_id=$1 AND environment_id=$2 AND session_id=$3 ORDER BY persisted_at,id`, workspaceID, environmentID, sessionID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []ForwardedAuditResult{}
	for rows.Next() {
		var r ForwardedAuditResult
		if err = rows.Scan(&r.ID, &r.WorkspaceID, &r.EnvironmentID, &r.SessionID, &r.RunID, &r.SourceEventID, &r.SourceEventDigest, &r.CollectorAuthenticationStatus, &r.AuditOriginStatus, &r.TrustClassification, &r.AcquiredAt, &r.PersistedAt); err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}
