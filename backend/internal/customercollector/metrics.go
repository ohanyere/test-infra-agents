package customercollector

import (
	"context"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"encoding/json"
	"errors"
	"math"
	"time"

	"github.com/evalplane/evalplane/backend/internal/auth"
	"github.com/evalplane/evalplane/backend/internal/identity"
	"github.com/jackc/pgx/v5"
)

const MetricCandidateTrust = "COLLECTOR_AUTHENTICATED_METRICS_UNVERIFIED"

type MetricCandidateInput struct {
	SampleID               string    `json:"sample_id"`
	ClaimedClusterIdentity string    `json:"claimed_cluster_identity"`
	Namespace              string    `json:"namespace"`
	Service                string    `json:"service"`
	SampledAt              time.Time `json:"sampled_at"`
	ErrorRatePct           *float64  `json:"error_rate_pct,omitempty"`
	RequestSuccessRatePct  *float64  `json:"request_success_rate_pct,omitempty"`
	P95LatencyMS           *float64  `json:"p95_latency_ms,omitempty"`
}
type MetricCandidateResult struct {
	ID            string    `json:"id"`
	SampleID      string    `json:"sample_id"`
	SessionID     string    `json:"session_id"`
	WorkspaceID   string    `json:"workspace_id"`
	EnvironmentID string    `json:"environment_id"`
	Trust         string    `json:"trust"`
	Duplicate     bool      `json:"duplicate"`
	AcquiredAt    time.Time `json:"acquired_at"`
	PersistedAt   time.Time `json:"persisted_at"`
}

func validMetricCandidate(in MetricCandidateInput) bool {
	if identity.ValidateUUID(in.SampleID) != nil || in.ClaimedClusterIdentity == "" || !validResourceName(in.Service, in.Namespace) || in.SampledAt.IsZero() {
		return false
	}
	if in.ErrorRatePct == nil && in.RequestSuccessRatePct == nil && in.P95LatencyMS == nil {
		return false
	}
	for _, p := range []*float64{in.ErrorRatePct, in.RequestSuccessRatePct} {
		if p != nil && (math.IsNaN(*p) || math.IsInf(*p, 0) || *p < 0 || *p > 100) {
			return false
		}
	}
	if p := in.P95LatencyMS; p != nil && (math.IsNaN(*p) || math.IsInf(*p, 0) || *p < 0) {
		return false
	}
	return true
}

// IngestMetricCandidate authenticates collector transport and registered scope,
// but does not authenticate Prometheus origin or service-level semantics.
func (s *Service) IngestMetricCandidate(ctx context.Context, sessionID, token string, in MetricCandidateInput) (MetricCandidateResult, error) {
	var out MetricCandidateResult
	if identity.ValidateUUID(sessionID) != nil || !validToken(token) {
		return out, auth.ErrForbidden
	}
	if !validMetricCandidate(in) {
		return out, identity.ErrValidation
	}
	data, err := json.Marshal(in)
	if err != nil {
		return out, err
	}
	digest := sha256.Sum256(data)
	digestText := "sha256:" + hex.EncodeToString(digest[:])
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return out, err
	}
	defer tx.Rollback(ctx)
	var runID, status, cluster, collectorStatus string
	var namespaces []string
	var hash []byte
	var version int64
	var createdAt, dbNow time.Time
	var completedAt, revokedAt *time.Time
	err = tx.QueryRow(ctx, `SELECT s.workspace_id,s.environment_id,s.run_id,s.status,s.created_at,s.completed_at,e.cluster_identifier,e.namespaces,e.revoked_at,c.credential_hash,c.credential_version,c.status,now()
 FROM customer_observation_sessions s JOIN customer_kubernetes_environments e ON e.id=s.environment_id AND e.workspace_id=s.workspace_id JOIN customer_kubernetes_collectors c ON c.environment_id=e.id WHERE s.id=$1 FOR UPDATE OF s,e,c`, sessionID).Scan(&out.WorkspaceID, &out.EnvironmentID, &runID, &status, &createdAt, &completedAt, &cluster, &namespaces, &revokedAt, &hash, &version, &collectorStatus, &dbNow)
	if errors.Is(err, pgx.ErrNoRows) {
		return out, auth.ErrForbidden
	}
	if err != nil {
		return out, err
	}
	if revokedAt != nil || collectorStatus != "active" || subtle.ConstantTimeCompare(tokenDigest(token), hash) != 1 || (status != "active" && status != "completed") || cluster != in.ClaimedClusterIdentity || !containsAuditScope(namespaces, in.Namespace) {
		return out, auth.ErrForbidden
	}
	if in.SampledAt.Before(createdAt) || in.SampledAt.After(dbNow.Add(30*time.Second)) || (completedAt != nil && in.SampledAt.After(*completedAt)) {
		return out, identity.ErrValidation
	}
	out.SessionID = sessionID
	out.SampleID = in.SampleID
	out.Trust = MetricCandidateTrust
	out.AcquiredAt = dbNow.UTC()
	err = tx.QueryRow(ctx, `INSERT INTO customer_service_metric_samples(workspace_id,environment_id,session_id,run_id,sample_id,collector_credential_version,namespace,service_name,sampled_at,acquired_at,error_rate_pct,request_success_rate_pct,p95_latency_ms,source_kind,source_digest)
 VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,'collector_forwarded_metrics_unverified',$14) ON CONFLICT DO NOTHING RETURNING id,persisted_at`, out.WorkspaceID, out.EnvironmentID, sessionID, runID, in.SampleID, version, in.Namespace, in.Service, in.SampledAt, dbNow, in.ErrorRatePct, in.RequestSuccessRatePct, in.P95LatencyMS, digestText).Scan(&out.ID, &out.PersistedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		var priorSession, priorDigest string
		err = tx.QueryRow(ctx, `SELECT id,session_id,source_digest,acquired_at,persisted_at FROM customer_service_metric_samples WHERE workspace_id=$1 AND environment_id=$2 AND sample_id=$3`, out.WorkspaceID, out.EnvironmentID, in.SampleID).Scan(&out.ID, &priorSession, &priorDigest, &out.AcquiredAt, &out.PersistedAt)
		if err != nil {
			return out, err
		}
		if priorSession != sessionID || priorDigest != digestText {
			return out, identity.ErrConflict
		}
		out.Duplicate = true
	} else if err != nil {
		return out, err
	}
	if err = tx.Commit(ctx); err != nil {
		return out, err
	}
	return out, nil
}
