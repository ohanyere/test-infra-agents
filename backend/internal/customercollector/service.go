package customercollector

import (
	"context"
	"crypto/subtle"
	"encoding/json"
	"errors"
	"strings"
	"time"

	"github.com/evalplane/evalplane/backend/internal/auth"
	"github.com/evalplane/evalplane/backend/internal/identity"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
)

type roleRepository interface {
	GetRole(context.Context, identity.DBTX, string, string) (string, error)
}
type Finalizer interface {
	FinalizeRun(context.Context, string, string) (string, error)
}

type Service struct {
	pool      *pgxpool.Pool
	roles     roleRepository
	finalizer Finalizer
	now       func() time.Time
}

func NewService(pool *pgxpool.Pool, roles roleRepository, finalizer Finalizer) (*Service, error) {
	if pool == nil || roles == nil || finalizer == nil {
		return nil, errors.New("customer collector requires database, membership, and canonical finalizer")
	}
	return &Service{pool: pool, roles: roles, finalizer: finalizer, now: func() time.Time { return time.Now().UTC() }}, nil
}

type RegisteredEnvironment struct {
	Environment         Environment `json:"environment"`
	CollectorCredential string      `json:"collector_credential"`
}
type IngestResult struct {
	Accepted           bool      `json:"accepted"`
	Duplicate          bool      `json:"duplicate"`
	SessionID          string    `json:"session_id"`
	RunID              string    `json:"run_id"`
	Sequence           int64     `json:"sequence"`
	ReceivedAt         time.Time `json:"received_at"`
	VerificationStatus string    `json:"verification_status,omitempty"`
}

func (s *Service) SubmitActionClaim(ctx context.Context, p auth.Principal, workspaceID, environmentID, sessionID string, input SubmitActionClaimInput) (ActionClaim, error) {
	if identity.ValidateUUID(workspaceID) != nil || identity.ValidateUUID(environmentID) != nil || identity.ValidateUUID(sessionID) != nil {
		return ActionClaim{}, validation("workspace, environment, and session identifiers must be UUIDs")
	}
	role, err := s.role(ctx, p, workspaceID)
	if err != nil {
		return ActionClaim{}, err
	}
	if role == "viewer" {
		return ActionClaim{}, auth.ErrForbidden
	}
	input, err = validateActionClaim(input)
	if err != nil {
		return ActionClaim{}, err
	}
	tx, err := s.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return ActionClaim{}, err
	}
	defer tx.Rollback(ctx)
	var derived ActionClaim
	var sessionStatus string
	var observationWindow int
	var baselineAt, finalAt *time.Time
	err = tx.QueryRow(ctx, `SELECT s.workspace_id,s.environment_id,s.id,s.run_id,r.agent_version_id,s.status,s.observation_window_seconds,
		(SELECT captured_at FROM customer_observation_batches WHERE session_id=s.id AND sequence=1 AND phase='baseline'),
		(SELECT captured_at FROM customer_observation_batches WHERE session_id=s.id AND phase='final')
		FROM customer_observation_sessions s
		JOIN customer_kubernetes_environments e ON e.id=s.environment_id AND e.workspace_id=s.workspace_id
		JOIN runs r ON r.id=s.run_id
		JOIN evaluation_batches b ON b.id=r.batch_id AND b.workspace_id=s.workspace_id
		JOIN agent_versions av ON av.id=r.agent_version_id
		JOIN agents a ON a.id=av.agent_id AND a.owner_workspace_id=s.workspace_id
		WHERE s.workspace_id=$1 AND e.id=$2 AND s.id=$3
		FOR UPDATE OF s`, workspaceID, environmentID, sessionID).
		Scan(&derived.WorkspaceID, &derived.EnvironmentID, &derived.SessionID, &derived.RunID, &derived.AgentVersionID, &sessionStatus, &observationWindow, &baselineAt, &finalAt)
	if err != nil {
		return ActionClaim{}, auth.ErrForbidden
	}
	if derived.AgentVersionID != input.AgentVersionID {
		return ActionClaim{}, auth.ErrForbidden
	}
	derived.ContractVersion = input.ContractVersion
	derived.ClaimID = input.ClaimID
	derived.IdempotencyKey = input.IdempotencyKey
	derived.ActionType = input.ActionType
	derived.Resource = input.Resource
	derived.StartedAt = input.StartedAt
	derived.CompletedAt = input.CompletedAt
	derived.Outcome = input.Outcome
	derived.ExternalTrace = input.ExternalTrace
	derived.SubmitterUserID = p.UserID
	derived.Trust = ActionClaimTrust
	var databaseNow time.Time
	if err = tx.QueryRow(ctx, `SELECT now()`).Scan(&databaseNow); err != nil {
		return ActionClaim{}, err
	}
	derived.SubmittedAt = databaseNow.UTC()

	// Either identifier may be retried. Locking the session serializes claim
	// insertion with other claims and the collector's final-session transition.
	prior, priorErr := scanActionClaim(tx.QueryRow(ctx, `SELECT contract_version,workspace_id,environment_id,session_id,run_id,agent_version_id,claim_id,idempotency_key,action_type,resource_kind,resource_namespace,resource_name,resource_uid,claimed_started_at,claimed_completed_at,claimed_outcome,COALESCE(external_trace_reference,''),submitted_at,submitter_user_id,trust_classification
		FROM customer_agent_action_claims WHERE session_id=$1 AND (claim_id=$2 OR idempotency_key=$3) FOR UPDATE`, sessionID, input.ClaimID, input.IdempotencyKey))
	if priorErr == nil {
		if !sameActionClaim(prior, derived) {
			return ActionClaim{}, identity.ErrConflict
		}
		prior.Duplicate = true
		if err = tx.Commit(ctx); err != nil {
			return ActionClaim{}, err
		}
		return prior, nil
	}
	if !errors.Is(priorErr, pgx.ErrNoRows) {
		return ActionClaim{}, priorErr
	}
	if sessionStatus != "active" && sessionStatus != "completed" {
		return ActionClaim{}, identity.ErrConflict
	}
	if err = validateActionClaimClock(input, databaseNow.UTC()); err != nil {
		return ActionClaim{}, err
	}
	if err = validateActionClaimWindow(input, baselineAt, finalAt, databaseNow.UTC(), observationWindow); err != nil {
		return ActionClaim{}, err
	}
	_, err = tx.Exec(ctx, `INSERT INTO customer_agent_action_claims(contract_version,workspace_id,environment_id,session_id,run_id,agent_version_id,claim_id,idempotency_key,action_type,resource_kind,resource_namespace,resource_name,resource_uid,claimed_started_at,claimed_completed_at,claimed_outcome,external_trace_reference,submitted_at,submitter_user_id,trust_classification)
		VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15,$16,NULLIF($17,''),$18,$19,$20)`, derived.ContractVersion, derived.WorkspaceID, derived.EnvironmentID, derived.SessionID, derived.RunID, derived.AgentVersionID, derived.ClaimID, derived.IdempotencyKey, derived.ActionType, derived.Resource.Kind, derived.Resource.Namespace, derived.Resource.Name, derived.Resource.UID, derived.StartedAt, derived.CompletedAt, derived.Outcome, derived.ExternalTrace, derived.SubmittedAt, derived.SubmitterUserID, derived.Trust)
	if err != nil {
		return ActionClaim{}, mapError(err)
	}
	if err = tx.Commit(ctx); err != nil {
		return ActionClaim{}, err
	}
	return derived, nil
}

func (s *Service) ListActionClaims(ctx context.Context, p auth.Principal, workspaceID, environmentID, sessionID string) ([]ActionClaim, error) {
	if _, err := s.role(ctx, p, workspaceID); err != nil {
		return nil, err
	}
	rows, err := s.pool.Query(ctx, `SELECT contract_version,workspace_id,environment_id,session_id,run_id,agent_version_id,claim_id,idempotency_key,action_type,resource_kind,resource_namespace,resource_name,resource_uid,claimed_started_at,claimed_completed_at,claimed_outcome,COALESCE(external_trace_reference,''),submitted_at,submitter_user_id,trust_classification
		FROM customer_agent_action_claims WHERE workspace_id=$1 AND environment_id=$2 AND session_id=$3 ORDER BY submitted_at,id`, workspaceID, environmentID, sessionID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	claims := make([]ActionClaim, 0)
	for rows.Next() {
		claim, scanErr := scanActionClaim(rows)
		if scanErr != nil {
			return nil, scanErr
		}
		claims = append(claims, claim)
	}
	if err = rows.Err(); err != nil {
		return nil, err
	}
	// An empty result must not reveal whether the supplied session belongs to
	// another workspace/environment.
	if len(claims) == 0 {
		var exists bool
		if err = s.pool.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM customer_observation_sessions WHERE workspace_id=$1 AND environment_id=$2 AND id=$3)`, workspaceID, environmentID, sessionID).Scan(&exists); err != nil {
			return nil, err
		}
		if !exists {
			return nil, auth.ErrForbidden
		}
	}
	return claims, nil
}

type actionClaimScanner interface{ Scan(...any) error }

func scanActionClaim(row actionClaimScanner) (ActionClaim, error) {
	var claim ActionClaim
	err := row.Scan(&claim.ContractVersion, &claim.WorkspaceID, &claim.EnvironmentID, &claim.SessionID, &claim.RunID, &claim.AgentVersionID, &claim.ClaimID, &claim.IdempotencyKey, &claim.ActionType, &claim.Resource.Kind, &claim.Resource.Namespace, &claim.Resource.Name, &claim.Resource.UID, &claim.StartedAt, &claim.CompletedAt, &claim.Outcome, &claim.ExternalTrace, &claim.SubmittedAt, &claim.SubmitterUserID, &claim.Trust)
	return claim, err
}

func sameActionClaim(left, right ActionClaim) bool {
	return left.ContractVersion == right.ContractVersion && left.WorkspaceID == right.WorkspaceID && left.EnvironmentID == right.EnvironmentID && left.SessionID == right.SessionID && left.RunID == right.RunID && left.AgentVersionID == right.AgentVersionID && left.ClaimID == right.ClaimID && left.IdempotencyKey == right.IdempotencyKey && left.ActionType == right.ActionType && left.Resource == right.Resource && left.StartedAt.Equal(right.StartedAt) && left.CompletedAt.Equal(right.CompletedAt) && left.Outcome == right.Outcome && left.ExternalTrace == right.ExternalTrace && left.SubmitterUserID == right.SubmitterUserID && left.Trust == right.Trust
}

func (s *Service) Register(ctx context.Context, p auth.Principal, workspaceID string, input CreateEnvironmentInput) (RegisteredEnvironment, error) {
	if err := identity.ValidateUUID(workspaceID); err != nil {
		return RegisteredEnvironment{}, validation("workspace_id must be a UUID")
	}
	role, err := s.role(ctx, p, workspaceID)
	if err != nil {
		return RegisteredEnvironment{}, err
	}
	if role == "viewer" {
		return RegisteredEnvironment{}, auth.ErrForbidden
	}
	input, err = normalizeEnvironment(input)
	if err != nil {
		return RegisteredEnvironment{}, err
	}
	token, hash, err := newCredential()
	if err != nil {
		return RegisteredEnvironment{}, err
	}
	tx, err := s.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return RegisteredEnvironment{}, err
	}
	defer tx.Rollback(ctx)
	var out Environment
	err = tx.QueryRow(ctx, `INSERT INTO customer_kubernetes_environments(workspace_id,name,cluster_identifier,namespaces,resource_kinds,environment_tier,created_by)
		VALUES($1,$2,$3,$4,ARRAY['Deployment','Pod']::TEXT[],$5,$6)
		RETURNING id,workspace_id,name,cluster_identifier,namespaces,resource_kinds,environment_tier,connection_status,revoked_at,created_at,updated_at`, workspaceID, input.Name, input.ClusterIdentifier, input.Namespaces, input.Tier, p.UserID).
		Scan(&out.ID, &out.WorkspaceID, &out.Name, &out.ClusterIdentifier, &out.Namespaces, &out.ResourceKinds, &out.Tier, &out.ConnectionStatus, &out.RevokedAt, &out.CreatedAt, &out.UpdatedAt)
	if err != nil {
		return RegisteredEnvironment{}, mapError(err)
	}
	if _, err = tx.Exec(ctx, `INSERT INTO customer_kubernetes_collectors(environment_id,credential_hash) VALUES($1,$2)`, out.ID, hash); err != nil {
		return RegisteredEnvironment{}, mapError(err)
	}
	if err = tx.Commit(ctx); err != nil {
		return RegisteredEnvironment{}, err
	}
	return RegisteredEnvironment{Environment: out, CollectorCredential: token}, nil
}

func (s *Service) List(ctx context.Context, p auth.Principal, workspaceID string) ([]Environment, error) {
	if _, err := s.role(ctx, p, workspaceID); err != nil {
		return nil, err
	}
	rows, err := s.pool.Query(ctx, `SELECT id,workspace_id,name,cluster_identifier,namespaces,resource_kinds,environment_tier,connection_status,revoked_at,created_at,updated_at FROM customer_kubernetes_environments WHERE workspace_id=$1 ORDER BY created_at DESC,id`, workspaceID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Environment{}
	for rows.Next() {
		var v Environment
		if err = rows.Scan(&v.ID, &v.WorkspaceID, &v.Name, &v.ClusterIdentifier, &v.Namespaces, &v.ResourceKinds, &v.Tier, &v.ConnectionStatus, &v.RevokedAt, &v.CreatedAt, &v.UpdatedAt); err != nil {
			return nil, err
		}
		out = append(out, v)
	}
	return out, rows.Err()
}

func (s *Service) Rotate(ctx context.Context, p auth.Principal, workspaceID, environmentID string) (RegisteredEnvironment, error) {
	role, err := s.role(ctx, p, workspaceID)
	if err != nil {
		return RegisteredEnvironment{}, err
	}
	if role == "viewer" {
		return RegisteredEnvironment{}, auth.ErrForbidden
	}
	token, hash, err := newCredential()
	if err != nil {
		return RegisteredEnvironment{}, err
	}
	tx, err := s.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return RegisteredEnvironment{}, err
	}
	defer tx.Rollback(ctx)
	var out Environment
	err = tx.QueryRow(ctx, `WITH updated AS (UPDATE customer_kubernetes_collectors c SET credential_hash=$3,credential_version=credential_version+1,status='active',last_seen_at=NULL,rotated_at=now(),revoked_at=NULL FROM customer_kubernetes_environments e WHERE c.environment_id=e.id AND e.id=$1 AND e.workspace_id=$2 AND e.revoked_at IS NULL RETURNING e.id,e.workspace_id,e.name,e.cluster_identifier,e.namespaces,e.resource_kinds,e.environment_tier,e.revoked_at,e.created_at,e.updated_at) SELECT id,workspace_id,name,cluster_identifier,namespaces,resource_kinds,environment_tier,'pending',revoked_at,created_at,now() FROM updated`, environmentID, workspaceID, hash).Scan(&out.ID, &out.WorkspaceID, &out.Name, &out.ClusterIdentifier, &out.Namespaces, &out.ResourceKinds, &out.Tier, &out.ConnectionStatus, &out.RevokedAt, &out.CreatedAt, &out.UpdatedAt)
	if err != nil {
		return RegisteredEnvironment{}, mapError(err)
	}
	if _, err = tx.Exec(ctx, `UPDATE customer_kubernetes_environments SET connection_status='pending',updated_at=now() WHERE id=$1 AND workspace_id=$2 AND revoked_at IS NULL`, environmentID, workspaceID); err != nil {
		return RegisteredEnvironment{}, err
	}
	if err = tx.Commit(ctx); err != nil {
		return RegisteredEnvironment{}, err
	}
	return RegisteredEnvironment{Environment: out, CollectorCredential: token}, nil
}

func (s *Service) Revoke(ctx context.Context, p auth.Principal, workspaceID, environmentID string) error {
	role, err := s.role(ctx, p, workspaceID)
	if err != nil {
		return err
	}
	if role == "viewer" {
		return auth.ErrForbidden
	}
	tx, err := s.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	_, err = tx.Exec(ctx, `UPDATE customer_kubernetes_collectors c SET status='revoked',revoked_at=now() FROM customer_kubernetes_environments e WHERE e.id=$1 AND e.workspace_id=$2 AND c.environment_id=e.id`, environmentID, workspaceID)
	if err != nil {
		return err
	}
	_, err = tx.Exec(ctx, `UPDATE customer_kubernetes_environments SET connection_status='revoked',revoked_at=now(),updated_at=now() WHERE id=$1 AND workspace_id=$2`, environmentID, workspaceID)
	if err != nil {
		return err
	}
	_, err = tx.Exec(ctx, `UPDATE customer_observation_sessions SET status='revoked' WHERE environment_id=$1 AND status='active'`, environmentID)
	if err != nil {
		return err
	}
	return tx.Commit(ctx)
}

func (s *Service) StartSession(ctx context.Context, p auth.Principal, workspaceID, environmentID string, in CreateSessionInput) (Session, error) {
	role, err := s.role(ctx, p, workspaceID)
	if err != nil {
		return Session{}, err
	}
	if role == "viewer" {
		return Session{}, auth.ErrForbidden
	}
	if identity.ValidateUUID(in.AgentVersionID) != nil || identity.ValidateUUID(in.ScenarioVersionID) != nil {
		return Session{}, validation("agent_version_id and scenario_version_id must be UUIDs")
	}
	tx, err := s.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return Session{}, err
	}
	defer tx.Rollback(ctx)
	// Source metadata marks the observation-only trust boundary. The canonical
	// verifier requires attribution for these runs; collector state alone cannot
	// prove which actor caused a change.
	metadata := `{"source":"customer_collector","execution_mode":"observation_only","action_attribution":"required"}`
	var batchID, runID, identityJSON string
	var observationWindow int
	err = tx.QueryRow(ctx, `SELECT sv.duration_seconds FROM scenario_versions sv JOIN scenarios sc ON sc.id=sv.scenario_id
		WHERE sv.id=$1 AND (sc.owner_workspace_id=$2 OR sc.visibility='public')`, in.ScenarioVersionID, workspaceID).Scan(&observationWindow)
	if err != nil {
		return Session{}, mapError(err)
	}
	err = tx.QueryRow(ctx, `INSERT INTO evaluation_batches(workspace_id,scenario_version_id,created_by,status,runs_per_agent,environment_mode,config,source_metadata)
		VALUES($1,$2,$3,'running',1,'isolated','{"customer_observation":true}'::jsonb,$4::jsonb) RETURNING id`, workspaceID, in.ScenarioVersionID, p.UserID, metadata).Scan(&batchID)
	if err != nil {
		return Session{}, mapError(err)
	}
	err = tx.QueryRow(ctx, `SELECT av.implementation_identity FROM agent_versions av JOIN agents a ON a.id=av.agent_id WHERE av.id=$1 AND a.owner_workspace_id=$2`, in.AgentVersionID, workspaceID).Scan(&identityJSON)
	if err != nil {
		return Session{}, mapError(err)
	}
	if _, err = tx.Exec(ctx, `INSERT INTO evaluation_batch_agents(batch_id,agent_version_id) VALUES($1,$2)`, batchID, in.AgentVersionID); err != nil {
		return Session{}, mapError(err)
	}
	err = tx.QueryRow(ctx, `INSERT INTO runs(batch_id,agent_version_id,attempt_number,status,created_at,started_at,agent_implementation_identity)
		VALUES($1,$2,1,'running',now(),now(),$3::jsonb) RETURNING id`, batchID, in.AgentVersionID, identityJSON).Scan(&runID)
	if err != nil {
		return Session{}, mapError(err)
	}
	var out Session
	err = tx.QueryRow(ctx, `INSERT INTO customer_observation_sessions(workspace_id,environment_id,run_id,created_by,observation_window_seconds) SELECT $1,e.id,$3,$4,$5 FROM customer_kubernetes_environments e WHERE e.id=$2 AND e.workspace_id=$1 AND e.revoked_at IS NULL AND e.connection_status<>'revoked' RETURNING id,workspace_id,environment_id,run_id,status,observation_window_seconds,created_at`, workspaceID, environmentID, runID, p.UserID, observationWindow).Scan(&out.ID, &out.WorkspaceID, &out.EnvironmentID, &out.RunID, &out.Status, &out.ObservationWindowSeconds, &out.CreatedAt)
	if err != nil {
		return Session{}, mapError(err)
	}
	if err = tx.Commit(ctx); err != nil {
		return Session{}, err
	}
	return out, nil
}

func (s *Service) Ingest(ctx context.Context, sessionID, token string, in EvidenceBatch) (IngestResult, error) {
	if identity.ValidateUUID(sessionID) != nil || !validToken(token) {
		return IngestResult{}, auth.ErrForbidden
	}
	now := s.now().UTC()
	if err := validateBatch(in, now); err != nil {
		return IngestResult{}, err
	}
	tx, err := s.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return IngestResult{}, err
	}
	defer tx.Rollback(ctx)
	var received time.Time
	if err = tx.QueryRow(ctx, `SELECT now()`).Scan(&received); err != nil {
		return IngestResult{}, err
	}
	var workspaceID, environmentID, runID, status string
	var expected int64
	var observationWindowSeconds int
	var lastCaptured *time.Time
	var namespaces, kinds []string
	var storedHash []byte
	var lastPhase string
	var batchCount int
	err = tx.QueryRow(ctx, `SELECT s.workspace_id,s.environment_id,s.run_id,s.status,s.next_sequence,s.last_captured_at,s.observation_window_seconds,e.namespaces,e.resource_kinds,c.credential_hash,
		COALESCE((SELECT phase FROM customer_observation_batches b WHERE b.session_id=s.id ORDER BY sequence DESC LIMIT 1),''),
		(SELECT count(*) FROM customer_observation_batches b WHERE b.session_id=s.id)
		FROM customer_observation_sessions s JOIN customer_kubernetes_environments e ON e.id=s.environment_id JOIN customer_kubernetes_collectors c ON c.environment_id=e.id
		WHERE s.id=$1 AND e.revoked_at IS NULL AND c.status='active' AND c.revoked_at IS NULL FOR UPDATE OF s,c`, sessionID).Scan(&workspaceID, &environmentID, &runID, &status, &expected, &lastCaptured, &observationWindowSeconds, &namespaces, &kinds, &storedHash, &lastPhase, &batchCount)
	if err != nil {
		return IngestResult{}, auth.ErrForbidden
	}
	digest := tokenDigest(token)
	if subtle.ConstantTimeCompare(digest, storedHash) != 1 {
		return IngestResult{}, auth.ErrForbidden
	}
	if status == "completed" {
		observationJSON, marshalErr := json.Marshal(in.Observation)
		if marshalErr != nil {
			return IngestResult{}, validation("observation cannot be serialized")
		}
		var exists bool
		if err = tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM customer_observation_batches WHERE session_id=$1 AND batch_id=$2 AND sequence=$3 AND phase='final' AND schema_version=$4 AND captured_at=$5 AND payload->'observation'=$6::jsonb)`, sessionID, in.BatchID, in.Sequence, in.SchemaVersion, in.CapturedAt.UTC(), string(observationJSON)).Scan(&exists); err != nil {
			return IngestResult{}, err
		}
		if !exists {
			return IngestResult{}, identity.ErrConflict
		}
		if err = tx.Commit(ctx); err != nil {
			return IngestResult{}, err
		}
		statusValue, finalErr := s.finalizer.FinalizeRun(ctx, workspaceID, runID)
		return IngestResult{Accepted: finalErr == nil, Duplicate: true, SessionID: sessionID, RunID: runID, Sequence: in.Sequence, ReceivedAt: received, VerificationStatus: statusValue}, finalErr
	}
	if status != "active" {
		return IngestResult{}, identity.ErrConflict
	}
	if err = validateOrder(expected, int64(batchCount), lastPhase, lastCaptured, in); err != nil {
		return IngestResult{}, err
	}
	if err = resourceScope(kinds, namespaces, in); err != nil {
		return IngestResult{}, err
	}
	if in.Phase == "final" {
		var baselineReceived time.Time
		if err = tx.QueryRow(ctx, `SELECT received_at FROM customer_observation_batches WHERE session_id=$1 AND sequence=1 AND phase='baseline'`, sessionID).Scan(&baselineReceived); err != nil {
			return IngestResult{}, mapError(err)
		}
		if err = validateObservationWindow(received, baselineReceived, observationWindowSeconds); err != nil {
			return IngestResult{}, err
		}
	}
	// Lock the target run and append the evidence in the same transaction as
	// sequence consumption. Database uniqueness plus row locking prevents replay.
	var runStatus string
	if err = tx.QueryRow(ctx, `SELECT r.status FROM runs r JOIN evaluation_batches b ON b.id=r.batch_id WHERE r.id=$1 AND b.workspace_id=$2 FOR UPDATE OF r`, runID, workspaceID).Scan(&runStatus); err != nil {
		return IngestResult{}, auth.ErrForbidden
	}
	if runStatus != "running" {
		return IngestResult{}, identity.ErrConflict
	}
	var eventSequence int64
	if err = tx.QueryRow(ctx, `SELECT COALESCE(MAX(sequence),0)+1 FROM run_events WHERE run_id=$1`, runID).Scan(&eventSequence); err != nil {
		return IngestResult{}, err
	}
	payload, _ := json.Marshal(map[string]any{"observation": in.Observation, "source_provider": ProviderID, "collector_environment_id": environmentID, "collector_session_id": sessionID, "collector_sequence": in.Sequence, "collector_batch_id": in.BatchID, "schema_version": in.SchemaVersion, "captured_at": in.CapturedAt.UTC(), "received_at": received})
	summary := map[string]string{"baseline": "verification_baseline_captured", "stability": "stability_observation_collected", "final": "final_observation_collected"}[in.Phase]
	if _, err = tx.Exec(ctx, `INSERT INTO customer_observation_batches(session_id,environment_id,batch_id,sequence,schema_version,phase,source_identity,captured_at,payload) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9::jsonb)`, sessionID, environmentID, in.BatchID, in.Sequence, in.SchemaVersion, in.Phase, "collector:"+environmentID, in.CapturedAt.UTC(), string(payload)); err != nil {
		return IngestResult{}, mapError(err)
	}
	if _, err = tx.Exec(ctx, `INSERT INTO run_events(run_id,sequence,event_type,occurred_at,summary,payload) VALUES($1,$2,'observation',$3,$4,$5::jsonb)`, runID, eventSequence, received, summary, string(payload)); err != nil {
		return IngestResult{}, mapError(err)
	}
	if in.Phase == "final" {
		if _, err = tx.Exec(ctx, `UPDATE customer_observation_sessions SET status='completed',next_sequence=next_sequence+1,last_captured_at=$2,completed_at=now() WHERE id=$1`, sessionID, in.CapturedAt.UTC()); err != nil {
			return IngestResult{}, err
		}
		if _, err = tx.Exec(ctx, `UPDATE runs SET status='completed',completed_at=now() WHERE id=$1 AND status='running'`, runID); err != nil {
			return IngestResult{}, err
		}
		if _, err = tx.Exec(ctx, `UPDATE evaluation_batches SET status='completed',completed_at=now() WHERE id=(SELECT batch_id FROM runs WHERE id=$1)`, runID); err != nil {
			return IngestResult{}, err
		}
	} else if _, err = tx.Exec(ctx, `UPDATE customer_observation_sessions SET next_sequence=next_sequence+1,last_captured_at=$2 WHERE id=$1`, sessionID, in.CapturedAt.UTC()); err != nil {
		return IngestResult{}, err
	}
	if _, err = tx.Exec(ctx, `UPDATE customer_kubernetes_collectors SET last_seen_at=now() WHERE environment_id=$1`, environmentID); err != nil {
		return IngestResult{}, err
	}
	if _, err = tx.Exec(ctx, `UPDATE customer_kubernetes_environments SET connection_status='healthy',updated_at=now() WHERE id=$1`, environmentID); err != nil {
		return IngestResult{}, err
	}
	if err = tx.Commit(ctx); err != nil {
		return IngestResult{}, err
	}
	out := IngestResult{Accepted: true, SessionID: sessionID, RunID: runID, Sequence: in.Sequence, ReceivedAt: received}
	if in.Phase == "final" {
		status, finalizeErr := s.finalizer.FinalizeRun(ctx, workspaceID, runID)
		if finalizeErr != nil {
			return out, finalizeErr
		}
		out.VerificationStatus = status
	}
	return out, nil
}

func (s *Service) role(ctx context.Context, p auth.Principal, w string) (string, error) {
	role, err := s.roles.GetRole(ctx, s.pool, w, p.UserID)
	if errors.Is(err, identity.ErrNotFound) {
		return "", auth.ErrForbidden
	}
	return role, err
}
func validation(message string) error {
	return errors.Join(identity.ErrValidation, errors.New(message))
}
func mapError(err error) error {
	if errors.Is(err, pgx.ErrNoRows) {
		return identity.ErrNotFound
	}
	var postgresError *pgconn.PgError
	if errors.As(err, &postgresError) && (postgresError.Code == "23505" || postgresError.Code == "23503") {
		return errors.Join(identity.ErrConflict, errors.New("customer observation identity conflicts with existing data"))
	}
	return err
}

// ParseBearer accepts exactly one opaque collector credential.
func ParseBearer(header string) (string, bool) {
	parts := strings.Fields(header)
	if len(parts) != 2 || !strings.EqualFold(parts[0], "Bearer") || !validToken(parts[1]) {
		return "", false
	}
	return parts[1], true
}
