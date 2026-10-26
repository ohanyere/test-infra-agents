package customeractionevidence

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/evalplane/evalplane/backend/internal/auth"
	"github.com/evalplane/evalplane/backend/internal/identity"
	"github.com/jackc/pgx/v5"
)

// ResolveRunnerTestBinding reads the registration and session created by trusted
// CI setup. Caller-supplied environment, cluster, and run identifiers have no role.
// This is a disposable test-source path, separate from customer Store.
func ResolveRunnerTestBinding(ctx context.Context, tx pgx.Tx, workspaceID, sessionID, collectorIdentity string) (KubernetesEnvironmentBinding, error) {
	return resolveRunnerTestBinding(ctx, tx, workspaceID, sessionID, collectorIdentity, true)
}

func resolveRunnerTestBinding(ctx context.Context, tx pgx.Tx, workspaceID, sessionID, collectorIdentity string, requireActive bool) (KubernetesEnvironmentBinding, error) {
	if tx == nil || identity.ValidateUUID(workspaceID) != nil || identity.ValidateUUID(sessionID) != nil || !strings.HasPrefix(collectorIdentity, "github-actions:") {
		return KubernetesEnvironmentBinding{}, auth.ErrForbidden
	}
	var b KubernetesEnvironmentBinding
	var active bool
	var status, registeredCollector, policy string
	err := tx.QueryRow(ctx, `SELECT s.workspace_id,s.registration_id,s.id,s.run_id,r.cluster_identity,r.namespaces,r.resource_kinds,r.collector_identity,r.trust_policy,r.active,s.status
 FROM runner_audit_test_sessions s JOIN runner_audit_test_registrations r ON r.id=s.registration_id AND r.workspace_id=s.workspace_id
 WHERE s.workspace_id=$1 AND s.id=$2 FOR UPDATE OF s,r`, workspaceID, sessionID).
		Scan(&b.Scope.WorkspaceID, &b.Scope.EnvironmentID, &b.Scope.SessionID, &b.Scope.RunID, &b.ClusterIdentity, &b.Namespaces, &b.ResourceKinds, &registeredCollector, &policy, &active, &status)
	if errors.Is(err, pgx.ErrNoRows) {
		return KubernetesEnvironmentBinding{}, auth.ErrForbidden
	}
	if err != nil {
		return KubernetesEnvironmentBinding{}, err
	}
	if !active || (requireActive && status != "active") || (!requireActive && status != "active" && status != "completed") || policy != "runner_bound_test_source" || registeredCollector != collectorIdentity {
		return KubernetesEnvironmentBinding{}, auth.ErrForbidden
	}
	return b, nil
}

// StoreRunnerTestEvidence accepts only sealed, conservatively classified Kind
// audit evidence. It cannot write the customer evidence table.
func StoreRunnerTestEvidence(ctx context.Context, tx pgx.Tx, workspaceID, sessionID, collectorIdentity string, e Evidence) (PersistResult, error) {
	if !hasValidProviderSeal(e) {
		return PersistResult{}, ErrUntrustedInput
	}
	if err := Validate(e); err != nil {
		return PersistResult{}, err
	}
	b, err := ResolveRunnerTestBinding(ctx, tx, workspaceID, sessionID, collectorIdentity)
	if err != nil {
		return PersistResult{}, err
	}
	if !validRunnerTestEvidence(e, b, collectorIdentity) {
		return PersistResult{}, ErrUntrustedInput
	}
	m := e.KubernetesAudit
	if e.OccurredAt.IsZero() || e.ObservedAt.IsZero() || e.OccurredAt.After(e.ObservedAt) || e.ObservedAt.After(time.Now().UTC().Add(30*time.Second)) {
		return PersistResult{}, invalid("runner audit timestamps are invalid")
	}
	payload, err := json.Marshal(e)
	if err != nil {
		return PersistResult{}, err
	}
	var result PersistResult
	err = tx.QueryRow(ctx, `INSERT INTO runner_kubernetes_audit_evidence(workspace_id,registration_id,session_id,run_id,cluster_identity,source_event_id,source_event_digest,payload_digest,payload)
 VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9::jsonb) ON CONFLICT DO NOTHING RETURNING id,ingested_at`, b.Scope.WorkspaceID, b.Scope.EnvironmentID, b.Scope.SessionID, b.Scope.RunID, b.ClusterIdentity, e.Source.EventID, m.SourceEventDigest, e.Integrity.Digest, string(payload)).Scan(&result.ID, &result.IngestedAt)
	if err == nil {
		return result, nil
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return PersistResult{}, fmt.Errorf("store runner audit evidence: %w", err)
	}
	var storedSession, storedDigest, storedSourceDigest string
	err = tx.QueryRow(ctx, `SELECT id,session_id,payload_digest,source_event_digest,ingested_at FROM runner_kubernetes_audit_evidence WHERE workspace_id=$1 AND cluster_identity=$2 AND source_event_id=$3`, b.Scope.WorkspaceID, b.ClusterIdentity, e.Source.EventID).Scan(&result.ID, &storedSession, &storedDigest, &storedSourceDigest, &result.IngestedAt)
	if err != nil {
		return PersistResult{}, err
	}
	if storedSession != sessionID || storedDigest != e.Integrity.Digest || storedSourceDigest != m.SourceEventDigest {
		return PersistResult{}, identity.ErrConflict
	}
	result.Duplicate = true
	return result, nil
}

func GetRunnerTestEvidence(ctx context.Context, tx pgx.Tx, workspaceID, sessionID, collectorIdentity, evidenceID string) (Evidence, error) {
	if identity.ValidateUUID(evidenceID) != nil {
		return Evidence{}, auth.ErrForbidden
	}
	b, err := resolveRunnerTestBinding(ctx, tx, workspaceID, sessionID, collectorIdentity, false)
	if err != nil {
		return Evidence{}, err
	}
	var payload []byte
	var payloadDigest, sourceDigest string
	err = tx.QueryRow(ctx, `SELECT payload,payload_digest,source_event_digest FROM runner_kubernetes_audit_evidence WHERE id=$1 AND workspace_id=$2 AND registration_id=$3 AND session_id=$4 AND run_id=$5`, evidenceID, b.Scope.WorkspaceID, b.Scope.EnvironmentID, b.Scope.SessionID, b.Scope.RunID).Scan(&payload, &payloadDigest, &sourceDigest)
	if errors.Is(err, pgx.ErrNoRows) {
		return Evidence{}, auth.ErrForbidden
	}
	if err != nil {
		return Evidence{}, err
	}
	var e Evidence
	if err = json.Unmarshal(payload, &e); err != nil {
		return Evidence{}, err
	}
	if err = Validate(e); err != nil {
		return Evidence{}, err
	}
	if e.Integrity.Digest != payloadDigest || e.KubernetesAudit == nil || e.KubernetesAudit.SourceEventDigest != sourceDigest || !validRunnerTestEvidence(e, b, collectorIdentity) {
		return Evidence{}, ErrUntrustedInput
	}
	return e, nil
}

func validRunnerTestEvidence(e Evidence, b KubernetesEnvironmentBinding, collectorIdentity string) bool {
	m := e.KubernetesAudit
	return e.Scope == b.Scope && m != nil && e.Source.ID == KubernetesAuditProviderID && e.Source.Identity == b.ClusterIdentity && m.ClusterIdentity == b.ClusterIdentity && m.ProvisioningIdentity == collectorIdentity && m.VerificationState == "runner_bound_test_source" && e.Kind == KindAuthenticatedActionRecord && e.Trust == TrustInsufficientProvenance && e.Source.AuthenticationStatus == "unknown" && e.Source.AuthorizationStatus == "unknown" && e.Source.IdentityStatus == "asserted" && e.ProvenanceStatus == "insufficient" && e.Actor.Status == "unknown" && e.Actor.Identity == "" && e.CausalityStatus == "unproven" && e.Source.AuthenticationMethod == "runner-bound-test-acquisition" && e.Source.EventID != "" && m.SourceEventDigest != "" && !m.AcquiredAt.IsZero() && len(m.ProvenanceLimitations) > 0 && containsString(b.Namespaces, e.Target.Namespace) && containsString(b.ResourceKinds, e.Target.Kind)
}
