package customeractionevidence

import (
	"bytes"
	"context"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/evalplane/evalplane/backend/internal/auth"
	"github.com/evalplane/evalplane/backend/internal/identity"
	"github.com/jackc/pgx/v5"
)

var ErrUntrustedInput = errors.New("customer action evidence is not independently authenticated")

// Store requires a transaction so it can serialize evidence insertion against
// session finalization and environment revocation.
func Store(ctx context.Context, tx pgx.Tx, e Evidence) (PersistResult, error) {
	if !hasValidProviderSeal(e) {
		return PersistResult{}, ErrUntrustedInput
	}
	if tx == nil {
		return PersistResult{}, invalid("evidence persistence requires a database transaction")
	}
	if err := Validate(e); err != nil {
		return PersistResult{}, err
	}
	if e.Kind == KindAgentDeclaredAction || e.Trust == TrustUnverifiedAgentClaim ||
		(e.KubernetesAudit != nil && e.KubernetesAudit.VerificationState == "runner_bound_test_source") {
		return PersistResult{}, ErrUntrustedInput
	}
	if e.Source.AuthenticationStatus != "verified" || e.Source.AuthorizationStatus != "authorized" {
		return PersistResult{}, ErrUntrustedInput
	}
	if strings.TrimSpace(e.Source.ID) == "" || strings.TrimSpace(e.Source.EventID) == "" || strings.TrimSpace(e.Source.AuthenticationMethod) == "" {
		return PersistResult{}, invalid("persisted evidence requires source, event, and authentication identifiers")
	}

	var workspaceID, environmentID, runID, sessionStatus, collectorStatus string
	var revokedAt, baselineAt *time.Time
	var databaseNow time.Time
	err := tx.QueryRow(ctx, `SELECT s.workspace_id,s.environment_id,s.run_id,s.status,e.revoked_at,c.status,
		(SELECT captured_at FROM customer_observation_batches WHERE session_id=s.id AND sequence=1 AND phase='baseline'),now()
		FROM customer_observation_sessions s
		JOIN customer_kubernetes_environments e ON e.id=s.environment_id AND e.workspace_id=s.workspace_id
		JOIN customer_kubernetes_collectors c ON c.environment_id=e.id
		WHERE s.id=$1 FOR UPDATE OF s,e,c`, e.Scope.SessionID).
		Scan(&workspaceID, &environmentID, &runID, &sessionStatus, &revokedAt, &collectorStatus, &baselineAt, &databaseNow)
	if errors.Is(err, pgx.ErrNoRows) {
		return PersistResult{}, auth.ErrForbidden
	}
	if err != nil {
		return PersistResult{}, err
	}
	if workspaceID != e.Scope.WorkspaceID || environmentID != e.Scope.EnvironmentID || runID != e.Scope.RunID {
		return PersistResult{}, auth.ErrForbidden
	}
	if revokedAt != nil || collectorStatus != "active" {
		return PersistResult{}, auth.ErrForbidden
	}
	if sessionStatus != "active" {
		return PersistResult{}, identity.ErrConflict
	}
	if baselineAt == nil || e.OccurredAt.IsZero() || e.ObservedAt.IsZero() || e.OccurredAt.Before(*baselineAt) || e.ObservedAt.Before(*baselineAt) || e.OccurredAt.After(databaseNow.Add(30*time.Second)) || e.ObservedAt.After(databaseNow.Add(30*time.Second)) || e.OccurredAt.After(e.ObservedAt.Add(30*time.Second)) {
		return PersistResult{}, fmt.Errorf("%w: action evidence timestamps are outside the active observation window", identity.ErrValidation)
	}
	e.IngestedAt = databaseNow.UTC()
	if err = Validate(e); err != nil {
		return PersistResult{}, err
	}
	encoded, err := json.Marshal(e)
	if err != nil {
		return PersistResult{}, fmt.Errorf("marshal customer action evidence: %w", err)
	}
	digestHex := strings.TrimPrefix(e.Integrity.Digest, "sha256:")
	digest, err := hex.DecodeString(digestHex)
	if err != nil || len(digest) != 32 {
		return PersistResult{}, invalid("payload digest is malformed")
	}
	var result PersistResult
	err = tx.QueryRow(ctx, `INSERT INTO customer_action_evidence(
		contract_version,workspace_id,environment_id,session_id,run_id,evidence_kind,
		source_id,source_identity,source_event_id,action_type,operation,target_kind,target_namespace,target_name,target_uid,
		actor_identity,actor_authority,actor_identity_status,occurred_at,observed_at,authentication_method,credential_scope,
		authentication_status,authorization_status,provenance_verification_status,causality_status,trust_classification,
		correlation_ids,evidence_references,provenance_conflicts,uncertainty,missing_fields,payload_digest,payload)
		VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,NULLIF($10,''),NULLIF($11,''),NULLIF($12,''),NULLIF($13,''),NULLIF($14,''),NULLIF($15,''),
		NULLIF($16,''),NULLIF($17,''),$18,$19,$20,$21,NULLIF($22,''),
		$23,$24,$25,$26,$27,$28::jsonb,$29::jsonb,$30::jsonb,$31::jsonb,$32::jsonb,$33,$34::jsonb)
		ON CONFLICT(session_id,source_id,source_identity,source_event_id) DO NOTHING
		RETURNING id,ingested_at`, e.ContractVersion, e.Scope.WorkspaceID, e.Scope.EnvironmentID, e.Scope.SessionID, e.Scope.RunID, e.Kind,
		e.Source.ID, e.Source.Identity, e.Source.EventID, e.ActionType, e.Operation, e.Target.Kind, e.Target.Namespace, e.Target.Name, e.Target.UID,
		e.Actor.Identity, e.Actor.Authority, e.Actor.Status, nullableTime(e.OccurredAt), nullableTime(e.ObservedAt), e.Source.AuthenticationMethod, e.Source.CredentialScope,
		e.Source.AuthenticationStatus, e.Source.AuthorizationStatus, e.ProvenanceStatus, e.CausalityStatus, e.Trust,
		jsonString(e.CorrelationIDs), jsonString(e.EvidenceRefs), jsonString(e.ProvenanceConflicts), jsonString(e.Uncertainty), jsonString(e.MissingFields), digest, string(encoded)).
		Scan(&result.ID, &result.IngestedAt)
	if err == nil {
		return result, nil
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return PersistResult{}, mapEvidenceDBError(err)
	}
	var storedDigest []byte
	err = tx.QueryRow(ctx, `SELECT id,payload_digest,ingested_at FROM customer_action_evidence
		WHERE session_id=$1 AND source_id=$2 AND source_identity=$3 AND source_event_id=$4`, e.Scope.SessionID, e.Source.ID, e.Source.Identity, e.Source.EventID).
		Scan(&result.ID, &storedDigest, &result.IngestedAt)
	if err != nil {
		return PersistResult{}, mapEvidenceDBError(err)
	}
	if !bytes.Equal(storedDigest, digest) {
		return PersistResult{}, identity.ErrConflict
	}
	result.Duplicate = true
	return result, nil
}

func nullableTime(value time.Time) any {
	if value.IsZero() {
		return nil
	}
	return value.UTC()
}

func jsonString(values []string) string {
	encoded, _ := json.Marshal(values)
	return string(encoded)
}

func mapEvidenceDBError(err error) error {
	if errors.Is(err, pgx.ErrNoRows) {
		return auth.ErrForbidden
	}
	return fmt.Errorf("customer action evidence persistence: %w", err)
}
