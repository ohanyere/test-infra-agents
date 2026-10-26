package customercollector

import (
	"bytes"
	"context"
	"crypto/sha256"
	"crypto/subtle"
	"crypto/x509"
	"encoding/hex"
	"encoding/json"
	"encoding/pem"
	"errors"
	"time"

	"github.com/evalplane/evalplane/backend/internal/auth"
	"github.com/evalplane/evalplane/backend/internal/customeractionevidence"
	"github.com/evalplane/evalplane/backend/internal/customerattribution"
	"github.com/evalplane/evalplane/backend/internal/identity"
	"github.com/evalplane/evalplane/backend/internal/kubeaudit"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

type AuditMTLSSource struct {
	ID                 string     `json:"id"`
	WorkspaceID        string     `json:"workspace_id"`
	EnvironmentID      string     `json:"environment_id"`
	CertificateSHA256  string     `json:"certificate_sha256"`
	CertificateSubject string     `json:"certificate_subject"`
	CreatedAt          time.Time  `json:"created_at"`
	ExpiresAt          time.Time  `json:"expires_at"`
	RevokedAt          *time.Time `json:"revoked_at,omitempty"`
}

type AuthenticatedAuditResult struct {
	ID                string    `json:"id"`
	SourceID          string    `json:"source_id"`
	SessionID         string    `json:"session_id"`
	SourceEventID     string    `json:"source_event_id"`
	SourceEventDigest string    `json:"source_event_digest"`
	EvidenceID        string    `json:"evidence_id"`
	Duplicate         bool      `json:"duplicate"`
	AcquiredAt        time.Time `json:"acquired_at"`
}

// ErrNoMatchingAuditSession means a valid webhook event lies outside every
// active session's baseline. The receiver acknowledges it without persisting
// evidence so unrelated cluster operations cannot jam the audit stream.
var ErrNoMatchingAuditSession = errors.New("no active matching audit session")

// NewAuditWebhookIntake is for the isolated mTLS listener. It exposes no
// workspace mutation APIs and has no customer-collector bearer-token path.
func NewAuditWebhookIntake(pool *pgxpool.Pool) *Service {
	return &Service{pool: pool, now: func() time.Time { return time.Now().UTC() }}
}

func validWebhookClientCertificate(cert *x509.Certificate, now time.Time) bool {
	if cert == nil || cert.IsCA || now.Before(cert.NotBefore) || !now.Before(cert.NotAfter) || cert.KeyUsage&x509.KeyUsageDigitalSignature == 0 {
		return false
	}
	for _, use := range cert.ExtKeyUsage {
		if use == x509.ExtKeyUsageClientAuth || use == x509.ExtKeyUsageAny {
			return true
		}
	}
	return false
}

// RegisterAuditMTLSSource pins the exact client certificate configured by the
// customer operator on the API-server audit webhook. Registration alone does
// not prove that the operator installed or confined its private key.
func (s *Service) RegisterAuditMTLSSource(ctx context.Context, p auth.Principal, w, e, certificatePEM string) (AuditMTLSSource, error) {
	role, err := s.role(ctx, p, w)
	if err != nil {
		return AuditMTLSSource{}, err
	}
	if role != "owner" && role != "admin" {
		return AuditMTLSSource{}, auth.ErrForbidden
	}
	if identity.ValidateUUID(e) != nil || len(certificatePEM) > 8192 {
		return AuditMTLSSource{}, validation("invalid source registration")
	}
	block, rest := pem.Decode([]byte(certificatePEM))
	if block == nil || block.Type != "CERTIFICATE" || len(bytes.TrimSpace(rest)) != 0 {
		return AuditMTLSSource{}, validation("one PEM client certificate is required")
	}
	cert, err := x509.ParseCertificate(block.Bytes)
	if err != nil || !validWebhookClientCertificate(cert, s.now()) {
		return AuditMTLSSource{}, validation("certificate must be a valid client-authentication leaf")
	}
	fingerprint := sha256.Sum256(cert.Raw)
	var out AuditMTLSSource
	var revoked *time.Time
	err = s.pool.QueryRow(ctx, `INSERT INTO customer_audit_mtls_sources(workspace_id,environment_id,certificate_sha256,certificate_subject,created_by,expires_at)
	 SELECT $1,e.id,$3,$4,$5,LEAST($6::timestamptz,now()+interval '90 days') FROM customer_kubernetes_environments e WHERE e.workspace_id=$1 AND e.id=$2 AND e.revoked_at IS NULL
	 RETURNING id,workspace_id,environment_id,certificate_subject,created_at,expires_at,revoked_at`, w, e, fingerprint[:], cert.Subject.String(), p.UserID, cert.NotAfter).Scan(&out.ID, &out.WorkspaceID, &out.EnvironmentID, &out.CertificateSubject, &out.CreatedAt, &out.ExpiresAt, &revoked)
	if errors.Is(err, pgx.ErrNoRows) {
		return AuditMTLSSource{}, auth.ErrForbidden
	}
	if err != nil {
		return AuditMTLSSource{}, mapError(err)
	}
	out.CertificateSHA256 = hex.EncodeToString(fingerprint[:])
	out.RevokedAt = revoked
	return out, nil
}

func (s *Service) ListAuditMTLSSources(ctx context.Context, p auth.Principal, w, e string) ([]AuditMTLSSource, error) {
	if _, err := s.role(ctx, p, w); err != nil {
		return nil, err
	}
	if identity.ValidateUUID(e) != nil {
		return nil, auth.ErrForbidden
	}
	rows, err := s.pool.Query(ctx, `SELECT id,workspace_id,environment_id,certificate_sha256,certificate_subject,created_at,expires_at,revoked_at FROM customer_audit_mtls_sources WHERE workspace_id=$1 AND environment_id=$2 ORDER BY created_at DESC`, w, e)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []AuditMTLSSource{}
	for rows.Next() {
		var item AuditMTLSSource
		var fingerprint []byte
		if err = rows.Scan(&item.ID, &item.WorkspaceID, &item.EnvironmentID, &fingerprint, &item.CertificateSubject, &item.CreatedAt, &item.ExpiresAt, &item.RevokedAt); err != nil {
			return nil, err
		}
		item.CertificateSHA256 = hex.EncodeToString(fingerprint)
		out = append(out, item)
	}
	return out, rows.Err()
}

func (s *Service) RevokeAuditMTLSSource(ctx context.Context, p auth.Principal, w, e, sourceID string) error {
	role, err := s.role(ctx, p, w)
	if err != nil {
		return err
	}
	if role != "owner" && role != "admin" {
		return auth.ErrForbidden
	}
	if identity.ValidateUUID(e) != nil || identity.ValidateUUID(sourceID) != nil {
		return auth.ErrForbidden
	}
	tag, err := s.pool.Exec(ctx, `UPDATE customer_audit_mtls_sources SET revoked_at=now() WHERE workspace_id=$1 AND environment_id=$2 AND id=$3 AND revoked_at IS NULL`, w, e, sourceID)
	if err != nil {
		return err
	}
	if tag.RowsAffected() != 1 {
		return auth.ErrForbidden
	}
	return nil
}

// IngestAuthenticatedAuditEvent is reachable only through the dedicated mTLS
// webhook listener. It resolves cluster and session identity from registration
// and durable server-owned records, never from an event or a forwarded token.
func (s *Service) IngestAuthenticatedAuditEvent(ctx context.Context, peer *x509.Certificate, raw []byte) (AuthenticatedAuditResult, error) {
	if len(raw) == 0 || len(raw) > 1<<20 || !validWebhookClientCertificate(peer, s.now()) {
		return AuthenticatedAuditResult{}, auth.ErrForbidden
	}
	events, err := kubeaudit.Parse(bytes.NewReader(raw))
	if err != nil || len(events) != 1 {
		return AuthenticatedAuditResult{}, validation("one complete Kubernetes audit response event is required")
	}
	event := events[0]
	kind, namespace, supported := kubeaudit.CustomerActionTarget(event)
	if !supported {
		return AuthenticatedAuditResult{}, validation("unsupported customer Kubernetes audit mutation")
	}
	fingerprint := sha256.Sum256(peer.Raw)
	digest := sha256.Sum256(raw)
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return AuthenticatedAuditResult{}, err
	}
	defer tx.Rollback(ctx)
	var w, e, cluster, status, sourceID string
	var namespaces, kinds []string
	var revoked *time.Time
	var registered []byte
	var sourceExpiry time.Time
	err = tx.QueryRow(ctx, `SELECT a.id,a.workspace_id,a.environment_id,e.cluster_identifier,e.connection_status,e.namespaces,e.resource_kinds,e.revoked_at,a.certificate_sha256,a.expires_at FROM customer_audit_mtls_sources a JOIN customer_kubernetes_environments e ON e.workspace_id=a.workspace_id AND e.id=a.environment_id WHERE a.certificate_sha256=$1 AND a.revoked_at IS NULL FOR UPDATE OF a,e`, fingerprint[:]).Scan(&sourceID, &w, &e, &cluster, &status, &namespaces, &kinds, &revoked, &registered, &sourceExpiry)
	if errors.Is(err, pgx.ErrNoRows) {
		return AuthenticatedAuditResult{}, auth.ErrForbidden
	}
	if err != nil {
		return AuthenticatedAuditResult{}, err
	}
	if revoked != nil || status == "revoked" || !s.now().Before(sourceExpiry) || subtle.ConstantTimeCompare(fingerprint[:], registered) != 1 || !containsAuditScope(namespaces, namespace) || !containsAuditScope(kinds, kind) {
		return AuthenticatedAuditResult{}, auth.ErrForbidden
	}
	var out AuthenticatedAuditResult
	out.SourceID = sourceID
	out.SourceEventID = event.AuditID
	out.SourceEventDigest = "sha256:" + hex.EncodeToString(digest[:])
	out.AcquiredAt = s.now().UTC()
	var priorDigest []byte
	var priorSource string
	err = tx.QueryRow(ctx, `SELECT id,session_id,evidence_id,source_event_digest,acquired_at,source_id FROM customer_authenticated_audit_events WHERE workspace_id=$1 AND environment_id=$2 AND source_event_id=$3`, w, e, event.AuditID).Scan(&out.ID, &out.SessionID, &out.EvidenceID, &priorDigest, &out.AcquiredAt, &priorSource)
	if err == nil {
		if priorSource != sourceID || subtle.ConstantTimeCompare(priorDigest, digest[:]) != 1 {
			return AuthenticatedAuditResult{}, identity.ErrConflict
		}
		out.Duplicate = true
		if _, err = customerattribution.ReconcileAuthenticatedAuditTx(ctx, tx, out.ID, s.now().UTC()); err != nil {
			return AuthenticatedAuditResult{}, err
		}
		return out, tx.Commit(ctx)
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return AuthenticatedAuditResult{}, err
	}
	rows, err := tx.Query(ctx, `SELECT s.id,s.run_id FROM customer_observation_sessions s JOIN customer_observation_batches b ON b.session_id=s.id AND b.phase='baseline' AND b.sequence=1 WHERE s.workspace_id=$1 AND s.environment_id=$2 AND s.status='active' AND b.captured_at<=$3 AND s.created_at<=$3 ORDER BY s.created_at LIMIT 2 FOR UPDATE OF s`, w, e, event.RequestAt)
	if err != nil {
		return AuthenticatedAuditResult{}, err
	}
	var sessions [][2]string
	for rows.Next() {
		var pair [2]string
		if err = rows.Scan(&pair[0], &pair[1]); err != nil {
			rows.Close()
			return AuthenticatedAuditResult{}, err
		}
		sessions = append(sessions, pair)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return AuthenticatedAuditResult{}, err
	}
	// A deployment monitor retains one direct, pre-action state acquisition on
	// its completed assurance session. Registration alone never supplies it.
	monitored := false
	monitorRows, err := tx.Query(ctx, `SELECT p.session_id,s.run_id,p.baseline_acquired_at FROM assurance_monitoring_plans p JOIN customer_observation_sessions s ON s.id=p.session_id AND s.workspace_id=p.workspace_id AND s.environment_id=p.environment_id JOIN customer_outcome_sources o ON o.id=p.deployment_source_id AND o.workspace_id=p.workspace_id AND o.environment_id=p.environment_id WHERE p.workspace_id=$1 AND p.environment_id=$2 AND p.mode='deployment_audit' AND p.state IN ('initializing','active','running','degraded') AND p.ends_at>now() AND o.revoked_at IS NULL AND o.expires_at>now() AND o.source_kind='kubernetes_state' AND o.namespace=$3 AND o.resource_name=$4 AND $5='Deployment' AND s.status='completed' ORDER BY p.session_id LIMIT 2 FOR UPDATE OF p,s`, w, e, namespace, event.ObjectRef.Name, kind)
	if err != nil {
		return AuthenticatedAuditResult{}, err
	}
	type monitorMatch struct {
		session, run string
		baseline     *time.Time
	}
	monitors := []monitorMatch{}
	for monitorRows.Next() {
		var m monitorMatch
		if err = monitorRows.Scan(&m.session, &m.run, &m.baseline); err != nil {
			monitorRows.Close()
			return AuthenticatedAuditResult{}, err
		}
		monitors = append(monitors, m)
	}
	err = monitorRows.Err()
	monitorRows.Close()
	if err != nil {
		return AuthenticatedAuditResult{}, err
	}
	if len(monitors) > 1 {
		return AuthenticatedAuditResult{}, identity.ErrConflict
	}
	if len(monitors) == 1 {
		m := monitors[0]
		if m.baseline == nil || !m.baseline.Before(event.RequestAt) {
			_, err = tx.Exec(ctx, `INSERT INTO customer_monitoring_audit_gaps(workspace_id,environment_id,session_id,source_id,source_event_id,source_event_digest,event_raw,event_at,reason) VALUES($1,$2,$3,$4,$5,$6,$7,$8,'no pre-action direct baseline') ON CONFLICT(workspace_id,environment_id,source_event_id) DO NOTHING`, w, e, m.session, sourceID, event.AuditID, digest[:], raw, event.RequestAt)
			if err != nil {
				return AuthenticatedAuditResult{}, err
			}
			_, err = tx.Exec(ctx, `UPDATE assurance_monitoring_plans SET integrity_gap=true,state=CASE WHEN state='running' THEN state ELSE 'degraded' END,last_error='authenticated audit arrived without a pre-action baseline',updated_at=now() WHERE session_id=$1`, m.session)
			if err != nil {
				return AuthenticatedAuditResult{}, err
			}
			if err = tx.Commit(ctx); err != nil {
				return AuthenticatedAuditResult{}, err
			}
			return AuthenticatedAuditResult{}, ErrNoMatchingAuditSession
		}
		sessions = append(sessions, [2]string{m.session, m.run})
		monitored = true
	}
	if len(sessions) == 0 {
		return AuthenticatedAuditResult{}, ErrNoMatchingAuditSession
	}
	if len(sessions) != 1 {
		return AuthenticatedAuditResult{}, identity.ErrConflict
	}
	out.SessionID = sessions[0][0]
	runID := sessions[0][1]
	if event.StageAt.After(out.AcquiredAt.Add(30*time.Second)) || event.RequestAt.After(event.StageAt) || event.StageAt.Before(event.RequestAt) {
		return AuthenticatedAuditResult{}, validation("audit event timestamps are invalid")
	}
	binding := customeractionevidence.KubernetesEnvironmentBinding{Scope: customeractionevidence.Scope{WorkspaceID: w, EnvironmentID: e, SessionID: out.SessionID, RunID: runID}, ClusterIdentity: cluster, Namespaces: namespaces, ResourceKinds: kinds}
	evidence, err := customeractionevidence.BuildFromCustomerAuditWebhook(ctx, event, raw, binding, sourceID, hex.EncodeToString(fingerprint[:]), out.AcquiredAt)
	if err != nil {
		return AuthenticatedAuditResult{}, err
	}
	stored, err := customeractionevidence.Store(ctx, tx, evidence)
	if err != nil {
		return AuthenticatedAuditResult{}, err
	}
	out.EvidenceID = stored.ID
	encoded, err := json.Marshal(event)
	if err != nil {
		return AuthenticatedAuditResult{}, err
	}
	err = tx.QueryRow(ctx, `INSERT INTO customer_authenticated_audit_events(workspace_id,environment_id,session_id,run_id,source_id,source_event_id,source_event_digest,evidence_id,event_raw,event_payload,event_at,source_observed_at,acquired_at)
 VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10::jsonb,$11,$12,$13) RETURNING id`, w, e, out.SessionID, runID, sourceID, event.AuditID, digest[:], out.EvidenceID, raw, string(encoded), event.RequestAt, event.StageAt, out.AcquiredAt).Scan(&out.ID)
	if err != nil {
		return AuthenticatedAuditResult{}, mapError(err)
	}
	if _, err = customerattribution.ReconcileAuthenticatedAuditTx(ctx, tx, out.ID, s.now().UTC()); err != nil {
		return AuthenticatedAuditResult{}, err
	}
	if monitored {
		_, err = tx.Exec(ctx, `UPDATE assurance_monitoring_plans SET assessment_baseline_observation_id=baseline_observation_id,integrity_gap=integrity_gap OR (last_audit_at IS NOT NULL AND baseline_acquired_at<=last_audit_at) OR (last_audit_at IS NOT NULL AND $2<last_audit_at),last_error=CASE WHEN last_audit_at IS NOT NULL AND (baseline_acquired_at<=last_audit_at OR $2<last_audit_at) THEN 'overlapping or out-of-order authenticated operations' ELSE last_error END,last_audit_at=GREATEST(COALESCE(last_audit_at,$2),$2),next_due_at=now(),updated_at=now() WHERE session_id=$1 AND mode='deployment_audit'`, out.SessionID, event.RequestAt)
		if err != nil {
			return AuthenticatedAuditResult{}, err
		}
	}
	return out, tx.Commit(ctx)
}
