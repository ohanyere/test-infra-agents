package operational

import (
	"context"
	"errors"
	"time"

	"github.com/evalplane/evalplane/backend/internal/auth"
	"github.com/evalplane/evalplane/backend/internal/identity"
	"github.com/jackc/pgx/v5"
)

type SessionSummary struct {
	ID                    string     `json:"id"`
	RunID                 string     `json:"run_id"`
	Status                string     `json:"status"`
	CreatedAt             time.Time  `json:"created_at"`
	LatestRevision        int        `json:"latest_revision"`
	LatestCanonicalStatus string     `json:"latest_canonical_status"`
	LastAssessedAt        *time.Time `json:"last_assessed_at,omitempty"`
	EvaluationState       string     `json:"evaluation_state"`
	EvaluationAttempts    int        `json:"evaluation_attempts"`
	NextEvaluationAt      *time.Time `json:"next_evaluation_at,omitempty"`
	ObservationBatches    int        `json:"observation_batches"`
	AuditCandidates       int        `json:"audit_candidates"`
	MonitoringState       string     `json:"monitoring_state"`
	MonitoringMode        string     `json:"monitoring_mode"`
	MonitoringBaselineAt  *time.Time `json:"monitoring_baseline_at,omitempty"`
	MonitoringNextDueAt   *time.Time `json:"monitoring_next_due_at,omitempty"`
	MonitoringLastSuccess *time.Time `json:"monitoring_last_success_at,omitempty"`
	MonitoringLastError   string     `json:"monitoring_last_error"`
}
type EvidenceSourceStatus struct {
	Kind                string     `json:"kind"`
	Trust               string     `json:"trust"`
	Limitation          string     `json:"limitation"`
	Enabled             bool       `json:"enabled"`
	Configured          bool       `json:"configured"`
	Reachability        string     `json:"reachability"`
	EvidenceCount       int        `json:"evidence_count"`
	LastReceivedAt      *time.Time `json:"last_received_at,omitempty"`
	OriginAuthenticated bool       `json:"origin_authenticated"`
}
type EnvironmentStatus struct {
	WorkspaceID        string                 `json:"workspace_id"`
	EnvironmentID      string                 `json:"environment_id"`
	Name               string                 `json:"name"`
	ClusterIdentifier  string                 `json:"cluster_identifier"`
	ConnectionStatus   string                 `json:"connection_status"`
	ActiveRequirements []Contract             `json:"active_requirements"`
	Sessions           []SessionSummary       `json:"sessions"`
	EvidenceSources    []EvidenceSourceStatus `json:"evidence_sources"`
}

func (s *Service) ListSessions(ctx context.Context, p auth.Principal, w, e string) ([]SessionSummary, error) {
	if _, err := s.role(ctx, p, w); err != nil {
		return nil, err
	}
	if identity.ValidateUUID(e) != nil {
		return nil, auth.ErrForbidden
	}
	var exists bool
	if err := s.pool.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM customer_kubernetes_environments WHERE workspace_id=$1 AND id=$2)`, w, e).Scan(&exists); err != nil {
		return nil, err
	}
	if !exists {
		return nil, auth.ErrForbidden
	}
	rows, err := s.pool.Query(ctx, `SELECT s.id,s.run_id,s.status,s.created_at,COALESCE(r.revision,0),COALESCE(r.report->>'canonical_overall_status',''),r.evaluated_at,COALESCE(j.state,'not_scheduled'),COALESCE(j.attempts,0),j.next_attempt_at,(SELECT count(*) FROM customer_observation_batches b WHERE b.session_id=s.id),(SELECT count(*) FROM customer_kubernetes_audit_records a WHERE a.session_id=s.id),COALESCE(m.state,'not_configured'),COALESCE(m.mode,'none'),m.baseline_acquired_at,m.next_due_at,m.last_success_at,COALESCE(m.last_error,'') FROM customer_observation_sessions s LEFT JOIN LATERAL (SELECT revision,report,evaluated_at FROM operational_assessment_reports WHERE workspace_id=s.workspace_id AND run_id=s.run_id ORDER BY revision DESC LIMIT 1) r ON true LEFT JOIN assurance_evaluation_jobs j ON j.session_id=s.id LEFT JOIN assurance_monitoring_plans m ON m.session_id=s.id AND m.workspace_id=s.workspace_id AND m.environment_id=s.environment_id WHERE s.workspace_id=$1 AND s.environment_id=$2 ORDER BY s.created_at DESC,s.id DESC LIMIT 100`, w, e)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []SessionSummary{}
	for rows.Next() {
		var x SessionSummary
		if err = rows.Scan(&x.ID, &x.RunID, &x.Status, &x.CreatedAt, &x.LatestRevision, &x.LatestCanonicalStatus, &x.LastAssessedAt, &x.EvaluationState, &x.EvaluationAttempts, &x.NextEvaluationAt, &x.ObservationBatches, &x.AuditCandidates, &x.MonitoringState, &x.MonitoringMode, &x.MonitoringBaselineAt, &x.MonitoringNextDueAt, &x.MonitoringLastSuccess, &x.MonitoringLastError); err != nil {
			return nil, err
		}
		out = append(out, x)
	}
	return out, rows.Err()
}
func (s *Service) EnvironmentStatus(ctx context.Context, p auth.Principal, w, e string) (EnvironmentStatus, error) {
	out := EnvironmentStatus{WorkspaceID: w, EnvironmentID: e}
	if _, err := s.role(ctx, p, w); err != nil {
		return EnvironmentStatus{}, err
	}
	if identity.ValidateUUID(e) != nil {
		return EnvironmentStatus{}, auth.ErrForbidden
	}
	err := s.pool.QueryRow(ctx, `SELECT name,cluster_identifier,connection_status FROM customer_kubernetes_environments WHERE workspace_id=$1 AND id=$2`, w, e).Scan(&out.Name, &out.ClusterIdentifier, &out.ConnectionStatus)
	if errors.Is(err, pgx.ErrNoRows) {
		return EnvironmentStatus{}, auth.ErrForbidden
	}
	if err != nil {
		return EnvironmentStatus{}, err
	}
	var collectorStatus string
	var lastSeen, observationReceived, auditReceived, metricReceived *time.Time
	var auditEnabled bool
	var observationCount, auditCount, metricCount int
	err = s.pool.QueryRow(ctx, `SELECT c.status,c.last_seen_at,EXISTS(SELECT 1 FROM customer_audit_source_registrations a WHERE a.workspace_id=e.workspace_id AND a.environment_id=e.id),
 (SELECT count(*) FROM customer_observation_batches b WHERE b.environment_id=e.id),(SELECT max(b.received_at) FROM customer_observation_batches b WHERE b.environment_id=e.id),
 (SELECT count(*) FROM customer_kubernetes_audit_records a WHERE a.workspace_id=e.workspace_id AND a.environment_id=e.id),(SELECT max(a.persisted_at) FROM customer_kubernetes_audit_records a WHERE a.workspace_id=e.workspace_id AND a.environment_id=e.id),
 (SELECT count(*) FROM customer_service_metric_samples m WHERE m.workspace_id=e.workspace_id AND m.environment_id=e.id),(SELECT max(m.persisted_at) FROM customer_service_metric_samples m WHERE m.workspace_id=e.workspace_id AND m.environment_id=e.id)
 FROM customer_kubernetes_environments e JOIN customer_kubernetes_collectors c ON c.environment_id=e.id WHERE e.workspace_id=$1 AND e.id=$2`, w, e).Scan(&collectorStatus, &lastSeen, &auditEnabled, &observationCount, &observationReceived, &auditCount, &auditReceived, &metricCount, &metricReceived)
	if err != nil {
		return EnvironmentStatus{}, err
	}
	reachability := "not_checked"
	if lastSeen != nil {
		reachability = "last_seen_stale"
		if s.now().Sub(*lastSeen) <= 5*time.Minute {
			reachability = "recently_seen"
		}
	}
	collectorConfigured := collectorStatus == "active" && out.ConnectionStatus != "revoked"
	var trustedSources, trustedEvents, executionIssuers, attributedEvents int
	var trustedLast *time.Time
	err = s.pool.QueryRow(ctx, `SELECT
	 (SELECT count(*) FROM customer_audit_mtls_sources a WHERE a.workspace_id=$1 AND a.environment_id=$2 AND a.revoked_at IS NULL AND a.expires_at>now()),
 (SELECT count(*) FROM customer_authenticated_audit_events a WHERE a.workspace_id=$1 AND a.environment_id=$2),
 (SELECT max(a.acquired_at) FROM customer_authenticated_audit_events a WHERE a.workspace_id=$1 AND a.environment_id=$2),
	 (SELECT count(*) FROM customer_execution_issuers i WHERE i.workspace_id=$1 AND i.environment_id=$2 AND i.revoked_at IS NULL AND i.expires_at>now()),
	 (SELECT count(*) FROM (SELECT DISTINCT ON (d.audit_event_id) d.status FROM customer_attribution_decisions d WHERE d.workspace_id=$1 AND d.environment_id=$2 ORDER BY d.audit_event_id,d.evaluated_at DESC,d.id DESC) latest WHERE latest.status='ATTRIBUTED')`, w, e).Scan(&trustedSources, &trustedEvents, &trustedLast, &executionIssuers, &attributedEvents)
	if err != nil {
		return EnvironmentStatus{}, err
	}
	out.EvidenceSources = []EvidenceSourceStatus{
		{Kind: "kubernetes_observation", Trust: "COLLECTOR_AUTHENTICATED_SOURCE_UNVERIFIED", Limitation: "Collector authentication does not prove Kubernetes API-server origin.", Enabled: collectorConfigured, Configured: collectorConfigured, Reachability: reachability, EvidenceCount: observationCount, LastReceivedAt: observationReceived},
		{Kind: "kubernetes_audit", Trust: "INSUFFICIENT_PROVENANCE", Limitation: "Forwarded audit records are candidates; API-server origin is unverified.", Enabled: auditEnabled && collectorConfigured, Configured: auditEnabled && collectorConfigured, Reachability: "not_checked", EvidenceCount: auditCount, LastReceivedAt: auditReceived},
		{Kind: "authenticated_kubernetes_audit", Trust: "PINNED_DIRECT_MTLS_SOURCE", Limitation: "A pinned client certificate authenticates its holder. API-server key confinement and deployment must be verified by the customer operator.", Enabled: trustedSources > 0, Configured: trustedSources > 0, Reachability: "not_checked", EvidenceCount: trustedEvents, LastReceivedAt: trustedLast, OriginAuthenticated: trustedEvents > 0},
		{Kind: "service_metrics", Trust: "COLLECTOR_AUTHENTICATED_METRICS_UNVERIFIED", Limitation: "Forwarded metric origin is unverified; no metric forwarder is installed by the Kubernetes snapshot Job.", Enabled: collectorConfigured, Configured: false, Reachability: "not_checked", EvidenceCount: metricCount, LastReceivedAt: metricReceived},
		{Kind: "agent_attribution", Trust: "SIGNED_EXECUTION_RECEIPT_REQUIRED", Limitation: "A Kubernetes principal or guidance request alone does not establish AI-agent causality. A separately operated issuer and matching authenticated audit event are required.", Enabled: executionIssuers > 0, Configured: executionIssuers > 0, Reachability: "not_checked", EvidenceCount: attributedEvents},
	}
	history, err := s.ListContracts(ctx, p, w, e)
	if err != nil {
		return EnvironmentStatus{}, err
	}
	out.ActiveRequirements = activeContracts(history, s.now().UTC())
	out.Sessions, err = s.ListSessions(ctx, p, w, e)
	return out, err
}
