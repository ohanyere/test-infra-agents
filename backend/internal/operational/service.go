package operational

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/evalplane/evalplane/backend/internal/agentgateway"
	"github.com/evalplane/evalplane/backend/internal/auth"
	"github.com/evalplane/evalplane/backend/internal/customerattribution"
	"github.com/evalplane/evalplane/backend/internal/identity"
	"github.com/evalplane/evalplane/backend/internal/kubeaudit"
	"github.com/evalplane/evalplane/backend/internal/verification"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

type roleRepository interface {
	GetRole(context.Context, identity.DBTX, string, string) (string, error)
}
type canonicalReader interface {
	Get(context.Context, auth.Principal, string, string) (verification.Result, error)
	GetInternal(context.Context, string, string) (verification.Result, error)
}
type Service struct {
	pool      *pgxpool.Pool
	roles     roleRepository
	canonical canonicalReader
	now       func() time.Time
}

func NewService(pool *pgxpool.Pool, roles roleRepository, canonical canonicalReader) *Service {
	return &Service{pool: pool, roles: roles, canonical: canonical, now: time.Now}
}

type ContractInput struct {
	ContractID  string    `json:"contract_id,omitempty"`
	Service     string    `json:"service,omitempty"`
	Active      bool      `json:"active"`
	State       string    `json:"state,omitempty"`
	EffectiveAt time.Time `json:"effective_at"`
	Rules       []Rule    `json:"rules"`
}

func (s *Service) role(ctx context.Context, p auth.Principal, w string) (string, error) {
	if identity.ValidateUUID(w) != nil {
		return "", auth.ErrForbidden
	}
	r, e := s.roles.GetRole(ctx, s.pool, w, p.UserID)
	if errors.Is(e, identity.ErrNotFound) {
		return "", auth.ErrForbidden
	}
	return r, e
}
func validRule(r Rule) bool {
	if r.WindowSeconds < 0 || r.WindowSeconds > 86400 || r.GraceSeconds < 0 || r.GraceSeconds > 3600 || r.FreshnessSeconds < 0 || r.FreshnessSeconds > 3600 {
		return false
	}
	switch r.Type {
	case "min_available_replicas":
		return r.Namespace != "" && r.ResourceName != "" && r.Minimum != nil && *r.Minimum >= 0
	case "max_http_5xx_pct", "max_p95_latency_ms":
		return r.Maximum != nil && *r.Maximum >= 0 && (r.Type != "max_http_5xx_pct" || *r.Maximum <= 100)
	case "min_request_success_pct":
		return r.Minimum != nil && *r.Minimum >= 0 && *r.Minimum <= 100
	case "forbid_operation":
		return r.Operation == "delete" || r.Operation == "patch" || r.Operation == "update" || r.Operation == "create"
	case "allowed_mutation_scope":
		return r.Namespace != "" || r.ResourceKind != "" || r.ResourceName != "" || r.ResourceUID != ""
	case "require_approval":
		return r.ApprovalRequired && r.Namespace != "" && r.ResourceName != ""
	case "no_public_exposure":
		return true
	case "approved_image", "post_image":
		return r.Namespace != "" && r.ResourceName != "" && r.Value != "" && len(r.Value) <= 512
	case "ready_before_mutation":
		return r.Namespace != "" && r.ResourceName != "" && r.Minimum != nil && *r.Minimum >= 0
	case "service_ready_before_mutation":
		return r.Namespace != "" && r.ResourceName != "" && r.Maximum != nil && *r.Maximum >= 0 && *r.Maximum <= 100
	}
	return false
}
func (s *Service) SaveContract(ctx context.Context, p auth.Principal, w, e string, in ContractInput) (Contract, error) {
	var out Contract
	role, err := s.role(ctx, p, w)
	if err != nil {
		return out, err
	}
	if role == "viewer" || identity.ValidateUUID(e) != nil {
		return out, auth.ErrForbidden
	}
	if len(in.Rules) == 0 || len(in.Rules) > 50 || len(in.Service) > 120 {
		return out, identity.ErrValidation
	}
	if in.State == "" {
		if in.Active {
			in.State = "active"
		} else {
			in.State = "inactive"
		}
	}
	if in.State != "draft" && in.State != "active" && in.State != "inactive" {
		return out, identity.ErrValidation
	}
	if in.State != "draft" && role != "owner" && role != "admin" {
		return out, auth.ErrForbidden
	}
	in.Active = in.State == "active"
	for _, r := range in.Rules {
		if !validRule(r) {
			return out, identity.ErrValidation
		}
		if r.Type == "max_http_5xx_pct" || r.Type == "max_p95_latency_ms" || r.Type == "min_request_success_pct" || r.Type == "service_ready_before_mutation" {
			parts := strings.Split(in.Service, "/")
			if len(parts) != 2 || parts[0] == "" || parts[1] == "" {
				return out, identity.ErrValidation
			}
		}
	}
	if in.ContractID == "" {
		in.ContractID = uuid.NewString()
	} else if identity.ValidateUUID(in.ContractID) != nil {
		return out, identity.ErrValidation
	}
	now := s.now().UTC()
	if in.EffectiveAt.IsZero() {
		in.EffectiveAt = now
	}
	if in.EffectiveAt.Before(now) {
		return out, identity.ErrValidation
	}
	rulesJSON, err := json.Marshal(in.Rules)
	if err != nil {
		return out, err
	}
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return out, err
	}
	defer tx.Rollback(ctx)
	var exists bool
	err = tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM customer_kubernetes_environments WHERE workspace_id=$1 AND id=$2 AND revoked_at IS NULL)`, w, e).Scan(&exists)
	if err != nil {
		return out, err
	}
	if !exists {
		return out, auth.ErrForbidden
	}
	if _, err = tx.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtext($1))`, in.ContractID); err != nil {
		return out, err
	}
	var version int
	ownerID := p.UserID
	err = tx.QueryRow(ctx, `SELECT owner_user_id FROM operational_contract_versions WHERE workspace_id=$1 AND environment_id=$2 AND contract_id=$3 ORDER BY version LIMIT 1`, w, e, in.ContractID).Scan(&ownerID)
	if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		return out, err
	}
	err = tx.QueryRow(ctx, `SELECT COALESCE(MAX(version),0)+1 FROM operational_contract_versions WHERE workspace_id=$1 AND environment_id=$2 AND contract_id=$3`, w, e, in.ContractID).Scan(&version)
	if err != nil {
		return out, err
	}
	_, err = tx.Exec(ctx, `INSERT INTO operational_contract_versions(contract_id,workspace_id,environment_id,version,owner_user_id,service_name,active,effective_at,rules,governance_state) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9::jsonb,$10)`, in.ContractID, w, e, version, ownerID, in.Service, in.Active, in.EffectiveAt, string(rulesJSON), in.State)
	if err != nil {
		return out, err
	}
	if err = tx.Commit(ctx); err != nil {
		return out, err
	}
	return Contract{ID: in.ContractID, WorkspaceID: w, EnvironmentID: e, Version: version, Active: in.Active, State: in.State, EffectiveAt: in.EffectiveAt, OwnerUserID: ownerID, Service: in.Service, Rules: in.Rules}, nil
}
func (s *Service) ListContracts(ctx context.Context, p auth.Principal, w, e string) ([]Contract, error) {
	if _, err := s.role(ctx, p, w); err != nil {
		return nil, err
	}
	return s.listContractsInternal(ctx, w, e)
}
func (s *Service) listContractsInternal(ctx context.Context, w, e string) ([]Contract, error) {
	if identity.ValidateUUID(e) != nil {
		return nil, auth.ErrForbidden
	}
	rows, err := s.pool.Query(ctx, `SELECT contract_id,workspace_id,environment_id,version,owner_user_id,service_name,active,COALESCE(governance_state,CASE WHEN active THEN 'active' ELSE 'inactive' END),effective_at,rules FROM operational_contract_versions WHERE workspace_id=$1 AND environment_id=$2 ORDER BY contract_id,version`, w, e)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Contract{}
	for rows.Next() {
		var c Contract
		var data []byte
		if err = rows.Scan(&c.ID, &c.WorkspaceID, &c.EnvironmentID, &c.Version, &c.OwnerUserID, &c.Service, &c.Active, &c.State, &c.EffectiveAt, &data); err != nil {
			return nil, err
		}
		if err = json.Unmarshal(data, &c.Rules); err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	return out, rows.Err()
}
func activeContracts(history []Contract, at time.Time) []Contract {
	latest := map[string]Contract{}
	for _, c := range history {
		if c.State == "draft" {
			continue
		}
		if !c.EffectiveAt.After(at) && (latest[c.ID].Version < c.Version) {
			latest[c.ID] = c
		}
	}
	out := []Contract{}
	for _, c := range latest {
		if c.Active {
			out = append(out, c)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out
}
func (s *Service) AssessRun(ctx context.Context, p auth.Principal, w, e, sessionID string) (Report, error) {
	return s.assessRun(ctx, p, w, e, sessionID, false)
}
func (s *Service) AssessRunSystem(ctx context.Context, w, e, sessionID string) (Report, error) {
	return s.assessRun(ctx, auth.Principal{}, w, e, sessionID, true)
}
func (s *Service) assessRun(ctx context.Context, p auth.Principal, w, e, sessionID string, system bool) (Report, error) {
	var zero Report
	if identity.ValidateUUID(w) != nil || identity.ValidateUUID(e) != nil || identity.ValidateUUID(sessionID) != nil {
		return zero, auth.ErrForbidden
	}
	if !system {
		role, err := s.role(ctx, p, w)
		if err != nil {
			return zero, err
		}
		if role == "viewer" {
			return zero, auth.ErrForbidden
		}
	}
	var runID string
	err := s.pool.QueryRow(ctx, `SELECT run_id FROM customer_observation_sessions WHERE workspace_id=$1 AND environment_id=$2 AND id=$3`, w, e, sessionID).Scan(&runID)
	if errors.Is(err, pgx.ErrNoRows) {
		return zero, auth.ErrForbidden
	}
	if err != nil {
		return zero, err
	}
	var canonical verification.Result
	if system {
		canonical, err = s.canonical.GetInternal(ctx, w, runID)
	} else {
		canonical, err = s.canonical.Get(ctx, p, w, runID)
	}
	if err != nil {
		return zero, err
	}
	var history []Contract
	if system {
		history, err = s.listContractsInternal(ctx, w, e)
	} else {
		history, err = s.ListContracts(ctx, p, w, e)
	}
	if err != nil {
		return zero, err
	}
	claims := []string{}
	claimPayloads := []json.RawMessage{}
	rows, err := s.pool.Query(ctx, `SELECT claim_id::text,to_jsonb(c) FROM customer_agent_action_claims c WHERE workspace_id=$1 AND environment_id=$2 AND session_id=$3 ORDER BY id`, w, e, sessionID)
	if err != nil {
		return zero, err
	}
	for rows.Next() {
		var id string
		var payload []byte
		if err = rows.Scan(&id, &payload); err != nil {
			rows.Close()
			return zero, err
		}
		claims = append(claims, "claim:"+id)
		claimPayloads = append(claimPayloads, json.RawMessage(payload))
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return zero, err
	}
	actions := []Action{}
	rows, err = s.pool.Query(ctx, `SELECT source_event_id,operation,resource_namespace,resource_kind,resource_name,resource_uid,stage_at,audit_origin_status FROM customer_kubernetes_audit_records WHERE workspace_id=$1 AND environment_id=$2 AND session_id=$3 ORDER BY persisted_at,id`, w, e, sessionID)
	if err != nil {
		return zero, err
	}
	for rows.Next() {
		var a Action
		var origin string
		if err = rows.Scan(&a.ID, &a.Operation, &a.Namespace, &a.Kind, &a.Name, &a.UID, &a.OccurredAt, &origin); err != nil {
			rows.Close()
			return zero, err
		}
		a.Source = "collector_forwarded_audit_unverified"
		a.OriginAuthenticated = origin == "verified"
		actions = append(actions, a)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return zero, err
	}
	rows, err = s.pool.Query(ctx, `SELECT a.source_event_id,a.event_payload,a.event_at FROM customer_authenticated_audit_events a WHERE a.workspace_id=$1 AND a.environment_id=$2 AND a.session_id=$3 ORDER BY a.persisted_at,a.id`, w, e, sessionID)
	if err != nil {
		return zero, err
	}
	for rows.Next() {
		var id string
		var raw []byte
		var occurred time.Time
		if err = rows.Scan(&id, &raw, &occurred); err != nil {
			rows.Close()
			return zero, err
		}
		var event kubeaudit.Event
		if err = json.Unmarshal(raw, &event); err != nil {
			rows.Close()
			return zero, err
		}
		kind, namespace, supported := kubeaudit.CustomerActionTarget(event)
		if !supported {
			rows.Close()
			return zero, fmt.Errorf("stored direct audit event has unsupported target")
		}
		actions = append(actions, Action{ID: id, Source: "kubernetes-api", OriginAuthenticated: true, Operation: event.Verb, Kind: kind, Namespace: namespace, Name: event.ObjectRef.Name, UID: event.ObjectRef.UID, OccurredAt: occurred})
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return zero, err
	}
	customerDecisions := []customerattribution.Assessment{}
	rows, err = s.pool.Query(ctx, `SELECT d.status,d.reason_code,d.reason,d.evaluated_at,a.source_event_id,a.evidence_id,COALESCE(d.execution_receipt_id::text,'') FROM customer_attribution_decisions d JOIN customer_authenticated_audit_events a ON a.id=d.audit_event_id WHERE d.workspace_id=$1 AND d.environment_id=$2 AND d.session_id=$3 ORDER BY d.evaluated_at,d.id`, w, e, sessionID)
	if err != nil {
		return zero, err
	}
	for rows.Next() {
		var x customerattribution.Assessment
		var auditID, evidenceID, receiptID string
		x.ContractVersion = customerattribution.ContractVersion
		if err = rows.Scan(&x.Status, &x.ReasonCode, &x.Reason, &x.EvaluatedAt, &auditID, &evidenceID, &receiptID); err != nil {
			rows.Close()
			return zero, err
		}
		x.EvidenceRefs = []string{"audit:" + auditID, "evidence:" + evidenceID}
		if receiptID != "" {
			x.EvidenceRefs = append(x.EvidenceRefs, "receipt:"+receiptID)
		}
		customerDecisions = append(customerDecisions, x)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return zero, err
	}
	bindings := []ExecutionBinding{}
	rows, err = s.pool.Query(ctx, `SELECT r.id::text,r.action_id::text,COALESCE(r.payload->>'preflight_id',''),r.agent_version_id::text,
  COALESCE(a.id::text,''),COALESCE(a.source_event_id,''),COALESCE(d.id::text,''),COALESCE(d.status,''),COALESCE(d.reason,''),
  r.operation,COALESCE(r.payload->>'image',''),r.resource_namespace,r.resource_name
  FROM customer_execution_receipts r
  LEFT JOIN customer_authenticated_audit_events a ON a.workspace_id=r.workspace_id AND a.environment_id=r.environment_id AND a.session_id=r.session_id AND a.run_id=r.run_id AND a.source_event_id=r.source_event_id
  LEFT JOIN LATERAL (SELECT id,status,reason FROM customer_attribution_decisions WHERE workspace_id=r.workspace_id AND environment_id=r.environment_id AND session_id=r.session_id AND run_id=r.run_id AND audit_event_id=a.id AND execution_receipt_id=r.id ORDER BY evaluated_at DESC,id DESC LIMIT 1) d ON true
  WHERE r.workspace_id=$1 AND r.environment_id=$2 AND r.session_id=$3 AND r.run_id=$4 ORDER BY r.authenticated_at,r.id`, w, e, sessionID, runID)
	if err != nil {
		return zero, err
	}
	for rows.Next() {
		var b ExecutionBinding
		if err = rows.Scan(&b.ReceiptID, &b.ActionID, &b.PreflightID, &b.AgentVersionID, &b.AuditEventID, &b.AuditSourceID, &b.DecisionID, &b.DecisionStatus, &b.DecisionReason, &b.Operation, &b.Image, &b.Namespace, &b.ResourceName); err != nil {
			rows.Close()
			return zero, err
		}
		bindings = append(bindings, b)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return zero, err
	}
	metricSamples := []MetricSample{}
	rows, err = s.pool.Query(ctx, `SELECT id,namespace,service_name,sampled_at,acquired_at,error_rate_pct,request_success_rate_pct,p95_latency_ms,source_digest FROM customer_service_metric_samples WHERE workspace_id=$1 AND environment_id=$2 AND session_id=$3 ORDER BY sampled_at,id`, w, e, sessionID)
	if err != nil {
		return zero, err
	}
	for rows.Next() {
		var sample MetricSample
		if err = rows.Scan(&sample.ID, &sample.Namespace, &sample.Service, &sample.SampledAt, &sample.AcquiredAt, &sample.ErrorRatePct, &sample.RequestSuccessRatePct, &sample.P95LatencyMS, &sample.SourceDigest); err != nil {
			rows.Close()
			return zero, err
		}
		sample.ProvenanceStatus = "COLLECTOR_AUTHENTICATED_METRICS_UNVERIFIED"
		metricSamples = append(metricSamples, sample)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return zero, err
	}
	directObservations := []DirectObservation{}
	rows, err = s.pool.Query(ctx, `SELECT id,source_id,source_kind,session_id,source_digest,observed_at,acquired_at,payload,COALESCE(monitoring_cycle_id::text,'') FROM customer_direct_outcome_observations WHERE workspace_id=$1 AND environment_id=$2 AND session_id=$3 ORDER BY acquired_at,id`, w, e, sessionID)
	if err != nil {
		return zero, err
	}
	for rows.Next() {
		var item DirectObservation
		var digest []byte
		if err = rows.Scan(&item.ID, &item.SourceID, &item.Kind, &item.SessionID, &digest, &item.ObservedAt, &item.AcquiredAt, &item.Payload, &item.MonitoringCycleID); err != nil {
			rows.Close()
			return zero, err
		}
		item.SourceDigest = "sha256:" + hex.EncodeToString(digest)
		directObservations = append(directObservations, item)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return zero, err
	}
	monitorEvents := []MonitoringEvent{}
	rows, err = s.pool.Query(ctx, `SELECT id,cycle_id,cycle_number,attempt,source_id,status,COALESCE(observation_id::text,''),reason,recorded_at FROM assurance_monitoring_events WHERE workspace_id=$1 AND environment_id=$2 AND session_id=$3 ORDER BY recorded_at,id`, w, e, sessionID)
	if err != nil {
		return zero, err
	}
	for rows.Next() {
		var x MonitoringEvent
		if err = rows.Scan(&x.ID, &x.CycleID, &x.CycleNumber, &x.Attempt, &x.SourceID, &x.Status, &x.ObservationID, &x.Reason, &x.RecordedAt); err != nil {
			rows.Close()
			return zero, err
		}
		monitorEvents = append(monitorEvents, x)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return zero, err
	}
	approvals := []ApprovalRecord{}
	rows, err = s.pool.Query(ctx, `SELECT a.id,a.action_id,a.resource,a.decision,a.approved_by,a.recorded_at,a.valid_until,r.revoked_at FROM operational_approval_records a LEFT JOIN operational_approval_revocations r ON r.approval_id=a.id WHERE a.workspace_id=$1 AND a.environment_id=$2 AND a.session_id=$3 ORDER BY a.recorded_at,a.id`, w, e, sessionID)
	if err != nil {
		return zero, err
	}
	for rows.Next() {
		var a ApprovalRecord
		if err = rows.Scan(&a.ID, &a.ActionID, &a.Resource, &a.Decision, &a.ApprovedBy, &a.RecordedAt, &a.ValidUntil, &a.RevokedAt); err != nil {
			rows.Close()
			return zero, err
		}
		approvals = append(approvals, a)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return zero, err
	}
	preflights := []GatewayPreflight{}
	rows, err = s.pool.Query(ctx, `SELECT id,action_id,agent_version_id,operation,resource_namespace,resource_name,image,decision,reason_code,contract_references,approval_references,decided_at,expires_at FROM customer_gateway_preflight_decisions WHERE workspace_id=$1 AND environment_id=$2 AND session_id=$3 ORDER BY decided_at,id`, w, e, sessionID)
	if err != nil {
		return zero, err
	}
	for rows.Next() {
		var item GatewayPreflight
		var contracts, approvals []byte
		if err = rows.Scan(&item.ID, &item.ActionID, &item.AgentVersionID, &item.Operation, &item.Namespace, &item.ResourceName, &item.Image, &item.Decision, &item.ReasonCode, &contracts, &approvals, &item.DecidedAt, &item.ExpiresAt); err != nil {
			rows.Close()
			return zero, err
		}
		if err = json.Unmarshal(contracts, &item.ContractReferences); err != nil {
			rows.Close()
			return zero, err
		}
		if err = json.Unmarshal(approvals, &item.ApprovalReferences); err != nil {
			rows.Close()
			return zero, err
		}
		preflights = append(preflights, item)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return zero, err
	}
	reconciliations := []ReconciliationState{}
	rows, err = s.pool.Query(ctx, `SELECT id,claim_id,state,result,revision,last_reason,evaluated_at FROM customer_audit_reconciliation_cases WHERE workspace_id=$1 AND environment_id=$2 AND session_id=$3 ORDER BY claim_id`, w, e, sessionID)
	if err != nil {
		return zero, err
	}
	for rows.Next() {
		var item ReconciliationState
		if err = rows.Scan(&item.ID, &item.ClaimID, &item.State, &item.Result, &item.Revision, &item.Reason, &item.EvaluatedAt); err != nil {
			rows.Close()
			return zero, err
		}
		reconciliations = append(reconciliations, item)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return zero, err
	}
	observations := []verification.Snapshot{}
	rows, err = s.pool.Query(ctx, `SELECT batch_id::text,captured_at,payload FROM customer_observation_batches WHERE session_id=$1 AND environment_id=$2 ORDER BY sequence`, sessionID, e)
	if err != nil {
		return zero, err
	}
	for rows.Next() {
		var id string
		var at time.Time
		var payload []byte
		if err = rows.Scan(&id, &at, &payload); err != nil {
			rows.Close()
			return zero, err
		}
		var value struct {
			Observation agentgateway.Observation `json:"observation"`
		}
		if err = json.Unmarshal(payload, &value); err != nil {
			rows.Close()
			return zero, err
		}
		observations = append(observations, verification.Snapshot{Reference: "customer_observation_batch:" + id, CapturedAt: at, SourceProvider: "customer-kubernetes-collector:v1", Observation: value.Observation})
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return zero, err
	}
	now := s.now().UTC()
	contractAt := canonical.Final.CapturedAt
	if contractAt.IsZero() {
		contractAt = canonical.CreatedAt
	}
	in := Input{Canonical: canonical, Claims: claims, ClaimPayloads: claimPayloads, Actions: actions, MetricSamples: metricSamples, DirectObservations: directObservations, Approvals: approvals, GatewayPreflights: preflights, Reconciliations: reconciliations, Contracts: activeContracts(history, contractAt), Observations: observations, Window: 5 * time.Minute, Freshness: 2 * time.Minute, EvaluatedAt: now}
	report := Evaluate(in)
	report.MonitoringEvents = monitorEvents
	report.Findings = append(report.Findings, assessSourceHealth(monitorEvents)...)
	report.CustomerAttribution = customerDecisions
	report.EnvironmentID = e
	report.OperationExplanations = explainOperations(report, bindings)
	// The fingerprint excludes evaluation time, so retries with the same source
	// evidence and contract versions return the same immutable revision.
	fingerprintData, err := json.Marshal(struct {
		AssessmentVersion   string
		Canonical           string
		Claims              []string
		ClaimPayloads       []json.RawMessage
		Actions             []Action
		CustomerAttribution []customerattribution.Assessment
		Bindings            []ExecutionBinding
		MetricSamples       []MetricSample
		DirectObservations  []DirectObservation
		MonitoringEvents    []MonitoringEvent
		Approvals           []ApprovalRecord
		GatewayPreflights   []GatewayPreflight
		Reconciliations     []ReconciliationState
		Contracts           []Contract
		Observations        []verification.Snapshot
	}{ContractVersion + ":explanations:v1", canonical.ID, claims, claimPayloads, actions, customerDecisions, bindings, metricSamples, directObservations, monitorEvents, approvals, preflights, reconciliations, in.Contracts, observations})
	if err != nil {
		return zero, err
	}
	digest := sha256.Sum256(fingerprintData)
	fingerprint := "sha256:" + hex.EncodeToString(digest[:])
	report.SourceFingerprint = fingerprint
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return zero, err
	}
	defer tx.Rollback(ctx)
	if _, err = tx.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtext($1))`, runID); err != nil {
		return zero, err
	}
	var prior []byte
	err = tx.QueryRow(ctx, `SELECT report FROM operational_assessment_reports WHERE workspace_id=$1 AND run_id=$2 AND source_fingerprint=$3`, w, runID, fingerprint).Scan(&prior)
	if err == nil {
		if err = json.Unmarshal(prior, &report); err != nil {
			return zero, err
		}
		return report, tx.Commit(ctx)
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return zero, err
	}
	var priorReport Report
	var priorRaw []byte
	priorErr := tx.QueryRow(ctx, `SELECT report FROM operational_assessment_reports WHERE workspace_id=$1 AND run_id=$2 ORDER BY revision DESC LIMIT 1`, w, runID).Scan(&priorRaw)
	if priorErr == nil {
		if err = json.Unmarshal(priorRaw, &priorReport); err != nil {
			return zero, err
		}
		report.Findings = append(report.Findings, assuranceTransitions(priorReport, report)...)
	} else if !errors.Is(priorErr, pgx.ErrNoRows) {
		return zero, priorErr
	}
	err = tx.QueryRow(ctx, `SELECT COALESCE(MAX(revision),0)+1 FROM operational_assessment_reports WHERE workspace_id=$1 AND run_id=$2`, w, runID).Scan(&report.Revision)
	if err != nil {
		return zero, err
	}
	data, err := json.Marshal(report)
	if err != nil {
		return zero, err
	}
	_, err = tx.Exec(ctx, `INSERT INTO operational_assessment_reports(workspace_id,environment_id,session_id,run_id,revision,source_fingerprint,canonical_result_id,report,evaluated_at) VALUES($1,$2,$3,$4,$5,$6,$7,$8::jsonb,$9)`, w, e, sessionID, runID, report.Revision, fingerprint, canonical.ID, string(data), now)
	if err != nil {
		return zero, fmt.Errorf("persist operational assessment: %w", err)
	}
	if err = s.persistFindings(ctx, tx, w, e, sessionID, report); err != nil {
		return zero, fmt.Errorf("persist operational findings: %w", err)
	}
	monitorOnlyGap := len(priorRaw) > 0 && monitoringOnlyGap(priorReport, report)
	if !monitorOnlyGap {
		if err = s.persistEpisode(ctx, tx, w, e, sessionID, report); err != nil {
			return zero, fmt.Errorf("persist operational episode: %w", err)
		}
	}
	return report, tx.Commit(ctx)
}
func (s *Service) ListReports(ctx context.Context, p auth.Principal, w, e, sessionID string) ([]Report, error) {
	if _, err := s.role(ctx, p, w); err != nil {
		return nil, err
	}
	if identity.ValidateUUID(e) != nil || identity.ValidateUUID(sessionID) != nil {
		return nil, auth.ErrForbidden
	}
	var exists bool
	err := s.pool.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM customer_observation_sessions WHERE workspace_id=$1 AND environment_id=$2 AND id=$3)`, w, e, sessionID).Scan(&exists)
	if err != nil {
		return nil, err
	}
	if !exists {
		return nil, auth.ErrForbidden
	}
	rows, err := s.pool.Query(ctx, `SELECT report FROM operational_assessment_reports WHERE workspace_id=$1 AND environment_id=$2 AND session_id=$3 ORDER BY revision`, w, e, sessionID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Report{}
	for rows.Next() {
		var data []byte
		var r Report
		if err = rows.Scan(&data); err != nil {
			return nil, err
		}
		if err = json.Unmarshal(data, &r); err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}
