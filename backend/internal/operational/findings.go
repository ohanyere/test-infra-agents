package operational

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"time"

	"github.com/evalplane/evalplane/backend/internal/auth"
	"github.com/evalplane/evalplane/backend/internal/identity"
	"github.com/jackc/pgx/v5"
)

type FindingRecord struct {
	ID                string    `json:"id"`
	EnvironmentID     string    `json:"environment_id"`
	SessionID         string    `json:"session_id"`
	RunID             string    `json:"run_id"`
	ReportRevision    int       `json:"report_revision"`
	Ordinal           int       `json:"ordinal"`
	Disposition       string    `json:"disposition"`
	Requirement       string    `json:"requirement"`
	AffectedResource  string    `json:"affected_resource"`
	AttributionStatus string    `json:"attribution_status"`
	Finding           Finding   `json:"finding"`
	EvaluatedAt       time.Time `json:"evaluated_at"`
}
type NotificationEvent struct {
	ID               string          `json:"id"`
	FindingID        string          `json:"finding_id"`
	EventKind        string          `json:"event_kind"`
	Payload          json.RawMessage `json:"payload"`
	CreatedAt        time.Time       `json:"created_at"`
	DeliveryState    string          `json:"delivery_state"`
	DeliveryAttempts int             `json:"delivery_attempts"`
}

func findingDisposition(f Finding) string {
	if strings.HasPrefix(f.Code, "RECOVERY_") {
		return "recovered_provisional"
	}
	if f.Status == Fail && f.ContractID != "" && f.ProvenanceStatus == "SOURCE_AUTHENTICATED" && len(f.Evidence) > 0 {
		return "violation"
	}
	if f.Status == Fail || strings.Contains(f.Code, "CANDIDATE") {
		return "candidate"
	}
	return "inconclusive"
}
func notificationKind(f Finding, disposition string) string {
	if strings.HasPrefix(f.Code, "ASSURANCE_") && strings.HasSuffix(f.Code, "_REGRESSION") {
		return "assurance_regression"
	}
	if strings.HasPrefix(f.Code, "ASSURANCE_") && strings.HasSuffix(f.Code, "_RECOVERED") {
		return "assurance_recovery"
	}
	if f.Code == "RECONCILIATION_FAILURE" {
		return "reconciliation_failure"
	}
	if f.Code == "REGRESSION_OBSERVED" || f.Code == "INTERMEDIATE_REGRESSION_CANDIDATE" {
		return "stability_regression"
	}
	if disposition == "violation" {
		return "confirmed_violation"
	}
	if disposition == "candidate" {
		return "candidate_violation"
	}
	if f.Code == "OBSERVATION_GAP" || strings.Contains(f.Code, "MISSING") || strings.Contains(f.Code, "UNAVAILABLE") {
		return "evidence_unavailable"
	}
	return ""
}
func (s *Service) persistFindings(ctx context.Context, tx pgx.Tx, w, e, sessionID string, report Report) error {
	for i, f := range report.Findings {
		data, err := json.Marshal(f)
		if err != nil {
			return err
		}
		disposition := findingDisposition(f)
		requirement := f.RuleType
		if requirement == "" {
			requirement = f.Dimension
		}
		var id string
		err = tx.QueryRow(ctx, `INSERT INTO operational_finding_history(workspace_id,environment_id,session_id,run_id,report_revision,ordinal,contract_id,contract_version,disposition,dimension,requirement,affected_resource,provenance_status,attribution_status,finding,evaluated_at) VALUES($1,$2,$3,$4,$5,$6,NULLIF($7,'')::uuid,NULLIF($8,0),$9,$10,$11,$12,$13,$14,$15::jsonb,$16) RETURNING id`, w, e, sessionID, report.RunID, report.Revision, i, f.ContractID, f.ContractVersion, disposition, f.Dimension, requirement, f.AffectedResource, f.ProvenanceStatus, report.Attribution.Status, string(data), report.EvaluatedAt).Scan(&id)
		if err != nil {
			return err
		}
		kind := notificationKind(f, disposition)
		if kind == "" {
			continue
		}
		// Preserve every finding revision, but notify only when the condition
		// changes for this dimension and resource.
		var previous json.RawMessage
		err = tx.QueryRow(ctx, `SELECT finding FROM operational_finding_history WHERE workspace_id=$1 AND environment_id=$2 AND session_id=$3 AND dimension=$4 AND affected_resource=$5 AND report_revision<$6 ORDER BY report_revision DESC,ordinal DESC LIMIT 1`, w, e, sessionID, f.Dimension, f.AffectedResource, report.Revision).Scan(&previous)
		if err != nil && !errors.Is(err, pgx.ErrNoRows) {
			return err
		}
		if err == nil {
			var prior Finding
			if json.Unmarshal(previous, &prior) == nil && prior.Code == f.Code && prior.Status == f.Status && prior.ProvenanceStatus == f.ProvenanceStatus {
				continue
			}
		}
		payload, err := json.Marshal(map[string]any{"finding_id": id, "workspace_id": w, "environment_id": e, "session_id": sessionID, "run_id": report.RunID, "report_revision": report.Revision, "event_kind": kind, "disposition": disposition, "code": f.Code, "provenance": f.ProvenanceStatus, "evidence": f.Evidence})
		if err != nil {
			return err
		}
		var eventID string
		err = tx.QueryRow(ctx, `INSERT INTO assurance_notification_events(workspace_id,environment_id,session_id,finding_id,event_kind,payload) VALUES($1,$2,$3,$4,$5,$6::jsonb) RETURNING id`, w, e, sessionID, id, kind, string(payload)).Scan(&eventID)
		if err != nil {
			return err
		}
		if _, err = tx.Exec(ctx, `INSERT INTO assurance_notification_deliveries(event_id) VALUES($1)`, eventID); err != nil {
			return err
		}
	}
	return nil
}
func (s *Service) ListFindings(ctx context.Context, p auth.Principal, w, e, sessionID string) ([]FindingRecord, error) {
	if _, err := s.role(ctx, p, w); err != nil {
		return nil, err
	}
	if identity.ValidateUUID(e) != nil || identity.ValidateUUID(sessionID) != nil {
		return nil, auth.ErrForbidden
	}
	rows, err := s.pool.Query(ctx, `SELECT id,environment_id,session_id,run_id,report_revision,ordinal,disposition,requirement,affected_resource,attribution_status,finding,evaluated_at FROM operational_finding_history WHERE workspace_id=$1 AND environment_id=$2 AND session_id=$3 ORDER BY report_revision DESC,ordinal LIMIT 500`, w, e, sessionID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []FindingRecord{}
	for rows.Next() {
		var x FindingRecord
		var raw []byte
		if err = rows.Scan(&x.ID, &x.EnvironmentID, &x.SessionID, &x.RunID, &x.ReportRevision, &x.Ordinal, &x.Disposition, &x.Requirement, &x.AffectedResource, &x.AttributionStatus, &raw, &x.EvaluatedAt); err != nil {
			return nil, err
		}
		if err = json.Unmarshal(raw, &x.Finding); err != nil {
			return nil, err
		}
		out = append(out, x)
	}
	return out, rows.Err()
}
func (s *Service) ListNotifications(ctx context.Context, p auth.Principal, w, e string) ([]NotificationEvent, error) {
	if _, err := s.role(ctx, p, w); err != nil {
		return nil, err
	}
	if identity.ValidateUUID(e) != nil {
		return nil, auth.ErrForbidden
	}
	rows, err := s.pool.Query(ctx, `SELECT n.id,n.finding_id,n.event_kind,n.payload,n.created_at,d.state,d.attempts FROM assurance_notification_events n JOIN assurance_notification_deliveries d ON d.event_id=n.id WHERE n.workspace_id=$1 AND n.environment_id=$2 ORDER BY n.created_at DESC,n.id DESC LIMIT 200`, w, e)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []NotificationEvent{}
	for rows.Next() {
		var x NotificationEvent
		if err = rows.Scan(&x.ID, &x.FindingID, &x.EventKind, &x.Payload, &x.CreatedAt, &x.DeliveryState, &x.DeliveryAttempts); err != nil {
			return nil, err
		}
		out = append(out, x)
	}
	return out, rows.Err()
}
func (s *Service) ListStabilityFindings(ctx context.Context, p auth.Principal, w, e, sessionID string) ([]FindingRecord, error) {
	all, err := s.ListFindings(ctx, p, w, e, sessionID)
	if err != nil {
		return nil, err
	}
	out := []FindingRecord{}
	for _, record := range all {
		if record.Finding.Dimension == "temporal_stability" || record.Finding.Dimension == "service_stability" {
			out = append(out, record)
		}
	}
	return out, nil
}
func (s *Service) ListEvidenceReferences(ctx context.Context, p auth.Principal, w, e, sessionID string) ([]EvidenceRef, error) {
	reports, err := s.ListReports(ctx, p, w, e, sessionID)
	if err != nil {
		return nil, err
	}
	out := []EvidenceRef{}
	if len(reports) == 0 {
		return out, nil
	}
	seen := map[string]bool{}
	for _, finding := range reports[len(reports)-1].Findings {
		for _, reference := range finding.Evidence {
			if !seen[reference.Reference] {
				seen[reference.Reference] = true
				out = append(out, reference)
			}
		}
	}
	return out, nil
}
