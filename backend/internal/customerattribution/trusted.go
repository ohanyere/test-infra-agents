package customerattribution

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"strings"
	"time"

	"github.com/evalplane/evalplane/backend/internal/auth"
	"github.com/evalplane/evalplane/backend/internal/identity"
	"github.com/evalplane/evalplane/backend/internal/kubeaudit"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
)

// A receipt is an assertion by a separately operated, registered execution
// issuer. The issuer must authenticate the agent before executing the API call
// and sign the actual Audit-Id response header. Agent-submitted receipts and
// Kubernetes service accounts alone cannot establish attribution.
type ReceiptPayload struct {
	IssuerID               string    `json:"issuer_id"`
	WorkspaceID            string    `json:"workspace_id"`
	EnvironmentID          string    `json:"environment_id"`
	ClusterIdentity        string    `json:"cluster_identity"`
	SessionID              string    `json:"session_id"`
	RunID                  string    `json:"run_id"`
	AgentVersionID         string    `json:"agent_version_id"`
	AgentCertificateSHA256 string    `json:"agent_certificate_sha256"`
	ExecutionSessionID     string    `json:"execution_session_id"`
	ActionID               string    `json:"action_id"`
	PreflightID            string    `json:"preflight_id,omitempty"`
	GuidanceRequestID      string    `json:"guidance_request_id,omitempty"`
	Image                  string    `json:"image,omitempty"`
	StartedAt              time.Time `json:"started_at,omitempty"`
	AuditID                string    `json:"audit_id"`
	Principal              string    `json:"kubernetes_principal"`
	Operation              string    `json:"operation"`
	Namespace              string    `json:"namespace"`
	ResourceName           string    `json:"resource_name"`
	ResourceUID            string    `json:"resource_uid"`
	IssuedAt               time.Time `json:"issued_at"`
	ExpiresAt              time.Time `json:"expires_at"`
}

type SignedReceipt struct {
	Payload   ReceiptPayload `json:"payload"`
	Signature string         `json:"signature"` // unpadded base64url Ed25519 signature
}

type Issuer struct {
	ID                     string     `json:"id"`
	WorkspaceID            string     `json:"workspace_id"`
	EnvironmentID          string     `json:"environment_id"`
	AgentVersionID         string     `json:"agent_version_id"`
	AgentCertificateSHA256 string     `json:"agent_certificate_sha256"`
	PublicKey              string     `json:"public_key"`
	Credential             string     `json:"credential,omitempty"`
	CreatedAt              time.Time  `json:"created_at"`
	ExpiresAt              time.Time  `json:"expires_at"`
	RevokedAt              *time.Time `json:"revoked_at,omitempty"`
}

type StoredDecision struct {
	ID                 string     `json:"id"`
	AuditEventID       string     `json:"audit_event_id"`
	ExecutionReceiptID string     `json:"execution_receipt_id,omitempty"`
	Assessment         Assessment `json:"assessment"`
}

type Service struct {
	pool  *pgxpool.Pool
	roles interface {
		GetRole(context.Context, identity.DBTX, string, string) (string, error)
	}
	now func() time.Time
}

func NewService(pool *pgxpool.Pool, roles interface {
	GetRole(context.Context, identity.DBTX, string, string) (string, error)
}) *Service {
	return &Service{pool: pool, roles: roles, now: func() time.Time { return time.Now().UTC() }}
}

func (s *Service) role(ctx context.Context, p auth.Principal, workspace string) (string, error) {
	if identity.ValidateUUID(workspace) != nil {
		return "", auth.ErrForbidden
	}
	role, err := s.roles.GetRole(ctx, s.pool, workspace, p.UserID)
	if errors.Is(err, identity.ErrNotFound) {
		return "", auth.ErrForbidden
	}
	return role, err
}

func (s *Service) RegisterIssuer(ctx context.Context, principal auth.Principal, workspace, environment, agentVersionID, agentCertificateSHA256, encodedKey string) (Issuer, error) {
	role, err := s.role(ctx, principal, workspace)
	if err != nil {
		return Issuer{}, err
	}
	if role != "owner" && role != "admin" {
		return Issuer{}, auth.ErrForbidden
	}
	if identity.ValidateUUID(environment) != nil || identity.ValidateUUID(agentVersionID) != nil {
		return Issuer{}, identity.ErrValidation
	}
	key, err := base64.RawURLEncoding.DecodeString(encodedKey)
	if err != nil || len(key) != ed25519.PublicKeySize {
		return Issuer{}, identity.ErrValidation
	}
	certificateDigest, err := hex.DecodeString(agentCertificateSHA256)
	if err != nil || len(certificateDigest) != sha256.Size {
		return Issuer{}, identity.ErrValidation
	}
	secret := make([]byte, 32)
	if _, err = rand.Read(secret); err != nil {
		return Issuer{}, err
	}
	credential := "rvx_issuer_" + base64.RawURLEncoding.EncodeToString(secret)
	credentialDigest := sha256.Sum256([]byte(credential))
	var out Issuer
	err = s.pool.QueryRow(ctx, `INSERT INTO customer_execution_issuers(workspace_id,environment_id,agent_version_id,agent_certificate_sha256,public_key,credential_hash,created_by)
	 SELECT $1,e.id,av.id,$4,$5,$6,$7 FROM customer_kubernetes_environments e JOIN agent_versions av ON av.id=$3 JOIN agents a ON a.id=av.agent_id AND a.owner_workspace_id=e.workspace_id AND a.status='active' WHERE e.workspace_id=$1 AND e.id=$2 AND e.revoked_at IS NULL
	 RETURNING id,workspace_id,environment_id,agent_version_id,created_at,expires_at`, workspace, environment, agentVersionID, certificateDigest, key, credentialDigest[:], principal.UserID).Scan(&out.ID, &out.WorkspaceID, &out.EnvironmentID, &out.AgentVersionID, &out.CreatedAt, &out.ExpiresAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return Issuer{}, auth.ErrForbidden
	}
	if err != nil {
		return Issuer{}, trustError(err)
	}
	out.PublicKey = encodedKey
	out.AgentCertificateSHA256 = agentCertificateSHA256
	out.Credential = credential
	return out, nil
}

func (s *Service) ListIssuers(ctx context.Context, principal auth.Principal, workspace, environment string) ([]Issuer, error) {
	if _, err := s.role(ctx, principal, workspace); err != nil {
		return nil, err
	}
	if identity.ValidateUUID(environment) != nil {
		return nil, auth.ErrForbidden
	}
	rows, err := s.pool.Query(ctx, `SELECT id,workspace_id,environment_id,agent_version_id,agent_certificate_sha256,public_key,created_at,expires_at,revoked_at FROM customer_execution_issuers WHERE workspace_id=$1 AND environment_id=$2 ORDER BY created_at DESC`, workspace, environment)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Issuer{}
	for rows.Next() {
		var x Issuer
		var key []byte
		var certificateDigest []byte
		if err = rows.Scan(&x.ID, &x.WorkspaceID, &x.EnvironmentID, &x.AgentVersionID, &certificateDigest, &key, &x.CreatedAt, &x.ExpiresAt, &x.RevokedAt); err != nil {
			return nil, err
		}
		x.PublicKey = base64.RawURLEncoding.EncodeToString(key)
		x.AgentCertificateSHA256 = hex.EncodeToString(certificateDigest)
		out = append(out, x)
	}
	return out, rows.Err()
}

func (s *Service) RevokeIssuer(ctx context.Context, principal auth.Principal, workspace, environment, issuer string) error {
	role, err := s.role(ctx, principal, workspace)
	if err != nil {
		return err
	}
	if role != "owner" && role != "admin" {
		return auth.ErrForbidden
	}
	if identity.ValidateUUID(environment) != nil || identity.ValidateUUID(issuer) != nil {
		return auth.ErrForbidden
	}
	tag, err := s.pool.Exec(ctx, `UPDATE customer_execution_issuers SET revoked_at=now() WHERE workspace_id=$1 AND environment_id=$2 AND id=$3 AND revoked_at IS NULL`, workspace, environment, issuer)
	if err != nil {
		return err
	}
	if tag.RowsAffected() != 1 {
		return auth.ErrForbidden
	}
	return nil
}

func validateReceipt(p ReceiptPayload, now time.Time) bool {
	for _, id := range []string{p.IssuerID, p.WorkspaceID, p.EnvironmentID, p.SessionID, p.RunID, p.AgentVersionID, p.ExecutionSessionID, p.ActionID} {
		if identity.ValidateUUID(id) != nil {
			return false
		}
	}
	certificateDigest, err := hex.DecodeString(p.AgentCertificateSHA256)
	return err == nil && len(certificateDigest) == sha256.Size && p.ClusterIdentity != "" && p.AuditID != "" && p.Principal != "" && p.Namespace != "" && p.ResourceName != "" &&
		(p.Operation == "patch" || p.Operation == "update") && !p.IssuedAt.IsZero() &&
		!p.IssuedAt.After(now.Add(30*time.Second)) && p.ExpiresAt.After(now) &&
		p.ExpiresAt.Sub(p.IssuedAt) <= 5*time.Minute
}

// IngestReceipt verifies the signed gateway assertion and binds it to one
// immutable, directly authenticated audit event. A registered key is a trust
// root only if the customer keeps the private key in its independent gateway.
func (s *Service) IngestReceipt(ctx context.Context, principal auth.Principal, workspace, environment string, receipt SignedReceipt) (StoredDecision, error) {
	role, err := s.role(ctx, principal, workspace)
	if err != nil {
		return StoredDecision{}, err
	}
	if role != "owner" && role != "admin" {
		return StoredDecision{}, auth.ErrForbidden
	}
	return s.ingestReceipt(ctx, workspace, environment, receipt)
}

// IngestGatewayReceipt authenticates the separately operated issuer with a
// one-time-provisioned opaque credential. The Ed25519 signature is independently
// verified below; possession of this transport credential alone is insufficient.
func (s *Service) IngestGatewayReceipt(ctx context.Context, credential string, receipt SignedReceipt) (StoredDecision, error) {
	if !strings.HasPrefix(credential, "rvx_issuer_") || len(credential) > 96 {
		return StoredDecision{}, auth.ErrForbidden
	}
	digest := sha256.Sum256([]byte(credential))
	var issuer, workspace, environment string
	err := s.pool.QueryRow(ctx, `SELECT id,workspace_id,environment_id FROM customer_execution_issuers WHERE credential_hash=$1 AND revoked_at IS NULL AND expires_at>now()`, digest[:]).Scan(&issuer, &workspace, &environment)
	if err != nil || receipt.Payload.IssuerID != issuer {
		return StoredDecision{}, auth.ErrForbidden
	}
	return s.ingestReceipt(ctx, workspace, environment, receipt)
}

func (s *Service) ingestReceipt(ctx context.Context, workspace, environment string, receipt SignedReceipt) (StoredDecision, error) {
	p := receipt.Payload
	now := s.now().UTC()
	if p.WorkspaceID != workspace || p.EnvironmentID != environment || !validateReceipt(p, now) {
		return StoredDecision{}, identity.ErrValidation
	}
	sig, err := base64.RawURLEncoding.DecodeString(receipt.Signature)
	if err != nil || len(sig) != ed25519.SignatureSize {
		return StoredDecision{}, identity.ErrValidation
	}
	canonical, err := json.Marshal(p)
	if err != nil {
		return StoredDecision{}, err
	}
	digest := sha256.Sum256(canonical)
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return StoredDecision{}, err
	}
	defer tx.Rollback(ctx)
	var publicKey []byte
	var cluster string
	var issuerAgentVersion string
	var issuerAgentCertificate []byte
	var revoked *time.Time
	var issuerExpiry time.Time
	err = tx.QueryRow(ctx, `SELECT i.public_key,i.agent_version_id,i.agent_certificate_sha256,i.revoked_at,i.expires_at,e.cluster_identifier FROM customer_execution_issuers i JOIN customer_kubernetes_environments e ON e.workspace_id=i.workspace_id AND e.id=i.environment_id WHERE i.id=$1 AND i.workspace_id=$2 AND i.environment_id=$3 AND e.revoked_at IS NULL FOR UPDATE OF i`, p.IssuerID, workspace, environment).Scan(&publicKey, &issuerAgentVersion, &issuerAgentCertificate, &revoked, &issuerExpiry, &cluster)
	receiptAgentCertificate, _ := hex.DecodeString(p.AgentCertificateSHA256)
	if err != nil || revoked != nil || !now.Before(issuerExpiry) || issuerAgentVersion != p.AgentVersionID || subtle.ConstantTimeCompare(issuerAgentCertificate, receiptAgentCertificate) != 1 || cluster != p.ClusterIdentity || !ed25519.Verify(ed25519.PublicKey(publicKey), canonical, sig) {
		return StoredDecision{}, auth.ErrForbidden
	}
	var runAgentVersion, sessionStatus string
	var baselineAt, completedAt *time.Time
	var namespaces, kinds []string
	err = tx.QueryRow(ctx, `SELECT r.agent_version_id,s.status,s.completed_at,(SELECT b.captured_at FROM customer_observation_batches b WHERE b.session_id=s.id AND b.sequence=1 AND b.phase='baseline'),e.namespaces,e.resource_kinds FROM customer_observation_sessions s JOIN runs r ON r.id=s.run_id JOIN customer_kubernetes_environments e ON e.workspace_id=s.workspace_id AND e.id=s.environment_id WHERE s.workspace_id=$1 AND s.environment_id=$2 AND s.id=$3 AND s.run_id=$4 FOR UPDATE OF s`, workspace, environment, p.SessionID, p.RunID).Scan(&runAgentVersion, &sessionStatus, &completedAt, &baselineAt, &namespaces, &kinds)
	if err != nil || sessionStatus == "revoked" || runAgentVersion != p.AgentVersionID || baselineAt == nil || p.IssuedAt.Before(*baselineAt) || (completedAt != nil && p.IssuedAt.After(*completedAt)) || !contains(namespaces, p.Namespace) || !contains(kinds, "Deployment") {
		return StoredDecision{}, auth.ErrForbidden
	}
	if p.PreflightID != "" {
		if identity.ValidateUUID(p.PreflightID) != nil || p.Image == "" || p.StartedAt.IsZero() || p.StartedAt.After(p.IssuedAt) {
			return StoredDecision{}, identity.ErrValidation
		}
		var decision, image, namespace, resource, operation string
		var decidedAt, expiresAt time.Time
		err = tx.QueryRow(ctx, `SELECT decision,image,resource_namespace,resource_name,operation,decided_at,expires_at FROM customer_gateway_preflight_decisions WHERE id=$1 AND workspace_id=$2 AND environment_id=$3 AND session_id=$4 AND run_id=$5 AND issuer_id=$6 AND agent_version_id=$7 AND action_id=$8`, p.PreflightID, workspace, environment, p.SessionID, p.RunID, p.IssuerID, p.AgentVersionID, p.ActionID).Scan(&decision, &image, &namespace, &resource, &operation, &decidedAt, &expiresAt)
		if err != nil || decision != "allowed" || image != p.Image || namespace != p.Namespace || resource != p.ResourceName || operation != p.Operation || p.StartedAt.Before(decidedAt) || !p.StartedAt.Before(expiresAt) {
			return StoredDecision{}, auth.ErrForbidden
		}
	}
	var eventID, eventSession string
	err = tx.QueryRow(ctx, `SELECT id,session_id FROM customer_authenticated_audit_events WHERE workspace_id=$1 AND environment_id=$2 AND source_event_id=$3`, workspace, environment, p.AuditID).Scan(&eventID, &eventSession)
	if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		return StoredDecision{}, err
	}
	if eventID != "" && eventSession != p.SessionID {
		return StoredDecision{}, auth.ErrForbidden
	}
	var priorID string
	var priorDigest []byte
	err = tx.QueryRow(ctx, `SELECT id,payload_digest FROM customer_execution_receipts WHERE issuer_id=$1 AND (action_id=$2 OR source_event_id=$3)`, p.IssuerID, p.ActionID, p.AuditID).Scan(&priorID, &priorDigest)
	if err == nil && string(priorDigest) != string(digest[:]) {
		return StoredDecision{}, identity.ErrConflict
	}
	if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		return StoredDecision{}, err
	}
	receiptID := priorID
	if receiptID == "" {
		err = tx.QueryRow(ctx, `INSERT INTO customer_execution_receipts(workspace_id,environment_id,session_id,run_id,issuer_id,agent_version_id,agent_certificate_sha256,action_id,execution_session_id,source_event_id,kubernetes_principal,operation,resource_namespace,resource_name,resource_uid,issued_at,expires_at,payload_digest,payload,signature)
	 VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15,$16,$17,$18,$19::jsonb,$20) RETURNING id`, workspace, environment, p.SessionID, p.RunID, p.IssuerID, p.AgentVersionID, receiptAgentCertificate, p.ActionID, p.ExecutionSessionID, p.AuditID, p.Principal, p.Operation, p.Namespace, p.ResourceName, p.ResourceUID, p.IssuedAt, p.ExpiresAt, digest[:], string(canonical), sig).Scan(&receiptID)
		if err != nil {
			return StoredDecision{}, trustError(err)
		}
	}
	if p.GuidanceRequestID != "" {
		if identity.ValidateUUID(p.GuidanceRequestID) != nil || p.StartedAt.IsZero() {
			return StoredDecision{}, identity.ErrValidation
		}
		var matched bool
		err = tx.QueryRow(ctx, `SELECT EXISTS(
            SELECT 1 FROM operational_guidance_requests g
            JOIN operational_guidance_credentials c ON c.id=g.credential_id AND c.workspace_id=g.workspace_id AND c.environment_id=g.environment_id
            WHERE g.id=$1 AND g.workspace_id=$2 AND g.environment_id=$3 AND c.agent_version_id=$4
              AND g.request->>'namespace'=$5 AND g.request->>'resource_kind'='Deployment'
              AND g.request->>'resource_name'=$6
              AND (g.request->>'operation'=$7 OR g.request->>'operation'='set_image' AND $7='patch')
              AND (COALESCE(g.request->>'run_id','')='' OR g.request->>'run_id'=$8)
              AND (COALESCE(g.request->>'session_id','')='' OR g.request->>'session_id'=$9)
              AND g.created_at<=$10 AND g.created_at>$10-interval '24 hours'
        )`, p.GuidanceRequestID, workspace, environment, p.AgentVersionID, p.Namespace, p.ResourceName, p.Operation, p.RunID, p.SessionID, p.StartedAt).Scan(&matched)
		if err != nil || !matched {
			return StoredDecision{}, auth.ErrForbidden
		}
		_, err = tx.Exec(ctx, `INSERT INTO operational_guidance_operation_links(workspace_id,environment_id,session_id,run_id,guidance_request_id,execution_receipt_id,agent_version_id)
            VALUES($1,$2,$3,$4,$5,$6,$7) ON CONFLICT (execution_receipt_id) DO NOTHING`, workspace, environment, p.SessionID, p.RunID, p.GuidanceRequestID, receiptID, p.AgentVersionID)
		if err != nil {
			return StoredDecision{}, err
		}
	}
	out := StoredDecision{ID: receiptID, ExecutionReceiptID: receiptID, Assessment: Assessment{ContractVersion: ContractVersion, Status: StatusInconclusive, ReasonCode: "MISSING_AUDIT_EVIDENCE", Reason: "Signed execution receipt retained pending independent audit evidence.", EvidenceRefs: []string{"receipt:" + receiptID}, EvaluatedAt: now}}
	if eventID != "" {
		decisions, reconcileErr := ReconcileAuthenticatedAuditTx(ctx, tx, eventID, now)
		if reconcileErr != nil {
			return StoredDecision{}, reconcileErr
		}
		for _, decision := range decisions {
			if decision.ExecutionReceiptID == receiptID {
				out = decision
			}
		}
	}
	return out, tx.Commit(ctx)
}

func contains(values []string, value string) bool {
	for _, item := range values {
		if item == value {
			return true
		}
	}
	return false
}

// ReconcileAuthenticatedAuditTx is called in the same transaction that stores
// either side of the correlation. The session row lock serializes receipt and
// audit arrival, so process restarts cannot strand a matching pending receipt.
func ReconcileAuthenticatedAuditTx(ctx context.Context, tx pgx.Tx, eventID string, evaluatedAt time.Time) ([]StoredDecision, error) {
	var workspace, environment, session, run, auditID, evidenceID string
	var eventAt time.Time
	var raw []byte
	err := tx.QueryRow(ctx, `SELECT workspace_id,environment_id,session_id,run_id,source_event_id,evidence_id,event_at,event_raw FROM customer_authenticated_audit_events WHERE id=$1`, eventID).Scan(&workspace, &environment, &session, &run, &auditID, &evidenceID, &eventAt, &raw)
	if err != nil {
		return nil, err
	}
	var audit kubeaudit.Event
	if err = json.Unmarshal(raw, &audit); err != nil {
		return nil, err
	}
	rows, err := tx.Query(ctx, `SELECT id,payload FROM customer_execution_receipts WHERE workspace_id=$1 AND environment_id=$2 AND session_id=$3 AND run_id=$4 AND source_event_id=$5 ORDER BY authenticated_at,id`, workspace, environment, session, run, auditID)
	if err != nil {
		return nil, err
	}
	type candidate struct {
		id      string
		p       ReceiptPayload
		matches bool
	}
	candidates := []candidate{}
	for rows.Next() {
		var item candidate
		var encoded []byte
		if err = rows.Scan(&item.id, &encoded); err != nil {
			rows.Close()
			return nil, err
		}
		if err = json.Unmarshal(encoded, &item.p); err != nil {
			rows.Close()
			return nil, err
		}
		p := item.p
		item.matches = p.Principal == audit.User.Username && p.Operation == audit.Verb && p.Namespace == audit.ObjectRef.Namespace && p.ResourceName == audit.ObjectRef.Name && p.ResourceUID == audit.ObjectRef.UID && p.AuditID == audit.AuditID && audit.Response.Code >= 200 && audit.Response.Code < 300 && !eventAt.After(p.IssuedAt.Add(30*time.Second)) && !p.IssuedAt.After(eventAt.Add(2*time.Minute))
		candidates = append(candidates, item)
	}
	if err = rows.Err(); err != nil {
		rows.Close()
		return nil, err
	}
	rows.Close()
	matching := 0
	for _, item := range candidates {
		if item.matches {
			matching++
		}
	}
	out := []StoredDecision{}
	for _, item := range candidates {
		a := Assessment{ContractVersion: ContractVersion, Status: StatusNotAttributed, ReasonCode: "CONTRADICTORY_EXECUTION_BINDING", Reason: "The signed execution receipt conflicts with the independently authenticated audit event.", EvidenceRefs: []string{"audit:" + auditID, "evidence:" + evidenceID, "receipt:" + item.id}, EvaluatedAt: evaluatedAt}
		if item.matches {
			a.Status = StatusAttributed
			a.ReasonCode = "AUTHENTICATED_EXECUTION_AND_AUDIT_MATCH"
			a.Reason = "A registered independent execution issuer signed this exact operation and matching authenticated audit event."
		}
		if item.matches && matching > 1 {
			a.Status = StatusInconclusive
			a.ReasonCode = "CONFLICTING_EXECUTION_BINDINGS"
			a.Reason = "Multiple independently signed execution receipts claim the same Kubernetes audit event."
		}
		decisionBytes, _ := json.Marshal(struct {
			Audit   string
			Receipt string
			Status  string
			Reason  string
		}{eventID, item.id, a.Status, a.ReasonCode})
		digest := sha256.Sum256(decisionBytes)
		_, err = tx.Exec(ctx, `INSERT INTO customer_attribution_decisions(workspace_id,environment_id,session_id,run_id,audit_event_id,execution_receipt_id,status,reason_code,reason,decision_digest,evaluated_at) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11) ON CONFLICT(audit_event_id,decision_digest) DO NOTHING`, workspace, environment, session, run, eventID, item.id, a.Status, a.ReasonCode, a.Reason, digest[:], evaluatedAt)
		if err != nil {
			return nil, err
		}
		var id string
		if err = tx.QueryRow(ctx, `SELECT id FROM customer_attribution_decisions WHERE audit_event_id=$1 AND decision_digest=$2`, eventID, digest[:]).Scan(&id); err != nil {
			return nil, err
		}
		out = append(out, StoredDecision{ID: id, AuditEventID: eventID, ExecutionReceiptID: item.id, Assessment: a})
	}
	return out, nil
}

func trustError(err error) error {
	var pg *pgconn.PgError
	if errors.As(err, &pg) && pg.Code == "23505" {
		return identity.ErrConflict
	}
	return err
}

func (s *Service) ListDecisions(ctx context.Context, principal auth.Principal, workspace, environment, session string) ([]StoredDecision, error) {
	if _, err := s.role(ctx, principal, workspace); err != nil {
		return nil, err
	}
	if identity.ValidateUUID(environment) != nil || identity.ValidateUUID(session) != nil {
		return nil, auth.ErrForbidden
	}
	rows, err := s.pool.Query(ctx, `SELECT d.id,d.audit_event_id,COALESCE(d.execution_receipt_id::text,''),d.status,d.reason_code,d.reason,d.evaluated_at,a.source_event_id,a.evidence_id FROM customer_attribution_decisions d JOIN customer_authenticated_audit_events a ON a.id=d.audit_event_id WHERE d.workspace_id=$1 AND d.environment_id=$2 AND d.session_id=$3 ORDER BY d.evaluated_at,d.id`, workspace, environment, session)
	if err != nil {
		return nil, err
	}
	out := []StoredDecision{}
	for rows.Next() {
		var x StoredDecision
		var auditID, evidenceID string
		x.Assessment.ContractVersion = ContractVersion
		if err = rows.Scan(&x.ID, &x.AuditEventID, &x.ExecutionReceiptID, &x.Assessment.Status, &x.Assessment.ReasonCode, &x.Assessment.Reason, &x.Assessment.EvaluatedAt, &auditID, &evidenceID); err != nil {
			return nil, err
		}
		x.Assessment.EvidenceRefs = []string{"audit:" + auditID, "evidence:" + evidenceID}
		if x.ExecutionReceiptID != "" {
			x.Assessment.EvidenceRefs = append(x.Assessment.EvidenceRefs, "receipt:"+x.ExecutionReceiptID)
		}
		out = append(out, x)
	}
	if err = rows.Err(); err != nil {
		rows.Close()
		return nil, err
	}
	rows.Close()
	pending, err := s.pool.Query(ctx, `SELECT r.id,r.source_event_id,r.authenticated_at FROM customer_execution_receipts r WHERE r.workspace_id=$1 AND r.environment_id=$2 AND r.session_id=$3 AND NOT EXISTS(SELECT 1 FROM customer_attribution_decisions d WHERE d.execution_receipt_id=r.id) ORDER BY r.authenticated_at,r.id`, workspace, environment, session)
	if err != nil {
		return nil, err
	}
	defer pending.Close()
	for pending.Next() {
		var id, auditID string
		var at time.Time
		if err = pending.Scan(&id, &auditID, &at); err != nil {
			return nil, err
		}
		out = append(out, StoredDecision{ID: id, ExecutionReceiptID: id, Assessment: Assessment{ContractVersion: ContractVersion, Status: StatusInconclusive, ReasonCode: "MISSING_AUDIT_EVIDENCE", Reason: "Signed execution receipt retained pending a matching authenticated audit event in this observation session.", EvidenceRefs: []string{"receipt:" + id, "claimed_audit:" + auditID}, EvaluatedAt: at}})
	}
	return out, pending.Err()
}

func FingerprintReceipt(p ReceiptPayload) string {
	b, _ := json.Marshal(p)
	d := sha256.Sum256(b)
	return "sha256:" + hex.EncodeToString(d[:])
}
