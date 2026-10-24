package operational

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"slices"
	"strconv"
	"time"

	"github.com/evalplane/evalplane/backend/internal/auth"
	"github.com/evalplane/evalplane/backend/internal/identity"
	"github.com/jackc/pgx/v5"
)

// GatewayPreflight is a preventive decision for the configured reference
// gateway only. A decision does not attest an executed Kubernetes operation.
type GatewayPreflight struct {
	ID                 string    `json:"id"`
	ActionID           string    `json:"action_id"`
	AgentVersionID     string    `json:"agent_version_id,omitempty"`
	Namespace          string    `json:"namespace"`
	ResourceName       string    `json:"resource_name"`
	Operation          string    `json:"operation"`
	Image              string    `json:"image"`
	Decision           string    `json:"decision"`
	ReasonCode         string    `json:"reason_code"`
	ContractReferences []string  `json:"contract_references"`
	ApprovalReferences []string  `json:"approval_references"`
	DecidedAt          time.Time `json:"decided_at"`
	ExpiresAt          time.Time `json:"expires_at"`
}
type GatewayPreflightInput struct {
	IssuerID       string `json:"issuer_id"`
	WorkspaceID    string `json:"workspace_id"`
	EnvironmentID  string `json:"environment_id"`
	SessionID      string `json:"session_id"`
	RunID          string `json:"run_id"`
	AgentVersionID string `json:"agent_version_id"`
	ActionID       string `json:"action_id"`
	Namespace      string `json:"namespace"`
	ResourceName   string `json:"resource_name"`
	Operation      string `json:"operation"`
	Image          string `json:"image"`
}

func ruleTargets(rule Rule, namespace, resource string) bool {
	return (rule.Namespace == "" || rule.Namespace == namespace) && (rule.ResourceName == "" || rule.ResourceName == resource) && (rule.ResourceKind == "" || rule.ResourceKind == "Deployment")
}

// PreflightGateway checks active customer versions and authenticated customer
// approval records before the reference gateway reaches Kubernetes. It never
// grants authority to an arbitrary external executor.
func (s *Service) PreflightGateway(ctx context.Context, credential string, in GatewayPreflightInput) (GatewayPreflight, error) {
	var out GatewayPreflight
	for _, id := range []string{in.IssuerID, in.WorkspaceID, in.EnvironmentID, in.SessionID, in.RunID, in.AgentVersionID, in.ActionID} {
		if identity.ValidateUUID(id) != nil {
			return out, identity.ErrValidation
		}
	}
	if in.Operation != "patch" || in.Namespace == "" || in.ResourceName == "" || in.Image == "" || len(in.Image) > 512 || len(credential) > 96 || len(credential) < 40 {
		return out, identity.ErrValidation
	}
	credentialHash := sha256.Sum256([]byte(credential))
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return out, err
	}
	defer tx.Rollback(ctx)
	now := s.now().UTC()
	var issuerVersion, sessionVersion, sessionStatus string
	var issuerExpiry time.Time
	var issuerRevoked, environmentRevoked *time.Time
	err = tx.QueryRow(ctx, `SELECT i.agent_version_id,i.expires_at,i.revoked_at,e.revoked_at FROM customer_execution_issuers i JOIN customer_kubernetes_environments e ON e.workspace_id=i.workspace_id AND e.id=i.environment_id WHERE i.id=$1 AND i.workspace_id=$2 AND i.environment_id=$3 AND i.credential_hash=$4 FOR UPDATE OF i,e`, in.IssuerID, in.WorkspaceID, in.EnvironmentID, credentialHash[:]).Scan(&issuerVersion, &issuerExpiry, &issuerRevoked, &environmentRevoked)
	if err != nil || issuerRevoked != nil || environmentRevoked != nil || !now.Before(issuerExpiry) || issuerVersion != in.AgentVersionID {
		return out, auth.ErrForbidden
	}
	var namespaces, kinds []string
	err = tx.QueryRow(ctx, `SELECT s.status,r.agent_version_id,e.namespaces,e.resource_kinds FROM customer_observation_sessions s JOIN runs r ON r.id=s.run_id JOIN customer_kubernetes_environments e ON e.id=s.environment_id AND e.workspace_id=s.workspace_id WHERE s.workspace_id=$1 AND s.environment_id=$2 AND s.id=$3 AND s.run_id=$4 FOR UPDATE OF s`, in.WorkspaceID, in.EnvironmentID, in.SessionID, in.RunID).Scan(&sessionStatus, &sessionVersion, &namespaces, &kinds)
	if err != nil || sessionStatus != "active" || sessionVersion != in.AgentVersionID || !slices.Contains(namespaces, in.Namespace) || !slices.Contains(kinds, "Deployment") {
		return out, auth.ErrForbidden
	}
	var prior string
	err = tx.QueryRow(ctx, `SELECT id FROM customer_gateway_preflight_decisions WHERE issuer_id=$1 AND action_id=$2`, in.IssuerID, in.ActionID).Scan(&prior)
	if err == nil {
		return out, identity.ErrConflict
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return out, err
	}
	rows, err := tx.Query(ctx, `SELECT contract_id,version,service_name,rules FROM (SELECT DISTINCT ON (contract_id) contract_id,version,service_name,rules,active FROM operational_contract_versions WHERE workspace_id=$1 AND environment_id=$2 AND effective_at<=$3 AND COALESCE(governance_state,CASE WHEN active THEN 'active' ELSE 'inactive' END)<>'draft' ORDER BY contract_id,version DESC) current WHERE active`, in.WorkspaceID, in.EnvironmentID, now)
	if err != nil {
		return out, err
	}
	type selected struct {
		id      string
		version int
		service string
		rules   []Rule
	}
	contracts := []selected{}
	for rows.Next() {
		var c selected
		var data []byte
		if err = rows.Scan(&c.id, &c.version, &c.service, &data); err != nil {
			rows.Close()
			return out, err
		}
		if err = json.Unmarshal(data, &c.rules); err != nil {
			rows.Close()
			return out, err
		}
		contracts = append(contracts, c)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return out, err
	}
	out = GatewayPreflight{ActionID: in.ActionID, AgentVersionID: in.AgentVersionID, Namespace: in.Namespace, ResourceName: in.ResourceName, Operation: in.Operation, Image: in.Image, Decision: "allowed", ReasonCode: "ACTIVE_CUSTOMER_SCOPE_ALLOWED", ContractReferences: []string{}, ApprovalReferences: []string{}, DecidedAt: now, ExpiresAt: now.Add(15 * time.Second)}
	if len(contracts) == 0 {
		out.Decision = "denied"
		out.ReasonCode = "NO_ACTIVE_CUSTOMER_CONTRACT"
	}
	hasScopeRule, matchesScopeRule := false, false
	for _, c := range contracts {
		out.ContractReferences = append(out.ContractReferences, "contract:"+c.id+":v"+strconv.Itoa(c.version))
		for _, rule := range c.rules {
			matches := ruleTargets(rule, in.Namespace, in.ResourceName)
			if matches && rule.ResourceUID != "" {
				out.Decision = "denied"
				out.ReasonCode = "RESOURCE_UID_PRECONDITION_UNAVAILABLE"
			}
			if rule.Type == "forbid_operation" && matches && rule.Operation == in.Operation {
				out.Decision = "denied"
				out.ReasonCode = "FORBIDDEN_OPERATION"
			}
			if rule.Type == "allowed_mutation_scope" {
				hasScopeRule = true
				matchesScopeRule = matchesScopeRule || matches
			}
			if rule.Type == "approved_image" && matches && rule.Value != in.Image {
				out.Decision = "denied"
				out.ReasonCode = "IMAGE_NOT_APPROVED"
			}
			if rule.Type == "require_approval" && matches {
				// A denial or a revoked approval wins. No record is a preventive denial
				// because this gateway must fail closed, not an assertion that an action ran.
				approved, denied, err := gatewayApproval(ctx, tx, in, now)
				if err != nil {
					return GatewayPreflight{}, err
				}
				out.ApprovalReferences = append(out.ApprovalReferences, approved...)
				if denied || len(approved) == 0 {
					out.Decision = "denied"
					out.ReasonCode = "APPROVAL_REQUIRED_OR_DENIED"
				}
			}
			if matches && (rule.Type == "ready_before_mutation" || rule.Type == "service_ready_before_mutation") {
				out.Decision = "denied"
				out.ReasonCode = "PRECONDITION_EVIDENCE_UNAVAILABLE"
			}
		}
	}
	if hasScopeRule && !matchesScopeRule {
		out.Decision = "denied"
		out.ReasonCode = "OUTSIDE_ALLOWED_MUTATION_SCOPE"
	}
	contractJSON, _ := json.Marshal(out.ContractReferences)
	approvalJSON, _ := json.Marshal(out.ApprovalReferences)
	err = tx.QueryRow(ctx, `INSERT INTO customer_gateway_preflight_decisions(workspace_id,environment_id,session_id,run_id,issuer_id,agent_version_id,action_id,operation,resource_namespace,resource_name,image,decision,reason_code,contract_references,approval_references,decided_at,expires_at) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14::jsonb,$15::jsonb,$16,$17) RETURNING id`, in.WorkspaceID, in.EnvironmentID, in.SessionID, in.RunID, in.IssuerID, in.AgentVersionID, in.ActionID, in.Operation, in.Namespace, in.ResourceName, in.Image, out.Decision, out.ReasonCode, string(contractJSON), string(approvalJSON), now, out.ExpiresAt).Scan(&out.ID)
	if err != nil {
		return GatewayPreflight{}, err
	}
	return out, tx.Commit(ctx)
}

func gatewayApproval(ctx context.Context, tx pgx.Tx, in GatewayPreflightInput, at time.Time) ([]string, bool, error) {
	rows, err := tx.Query(ctx, `SELECT a.id,a.decision,a.valid_until,r.id IS NOT NULL FROM operational_approval_records a LEFT JOIN operational_approval_revocations r ON r.approval_id=a.id WHERE a.workspace_id=$1 AND a.environment_id=$2 AND a.session_id=$3 AND a.action_id=$4 AND a.resource=$5 AND a.recorded_at<=$6`, in.WorkspaceID, in.EnvironmentID, in.SessionID, in.ActionID, in.Namespace+"/"+in.ResourceName, at)
	if err != nil {
		return nil, false, err
	}
	defer rows.Close()
	approved := []string{}
	denied := false
	for rows.Next() {
		var id, decision string
		var until time.Time
		var revoked bool
		if err = rows.Scan(&id, &decision, &until, &revoked); err != nil {
			return nil, false, err
		}
		if revoked || !until.After(at) {
			continue
		}
		if decision == "denied" {
			denied = true
		} else if decision == "approved" {
			approved = append(approved, "approval:"+id)
		}
	}
	return approved, denied, rows.Err()
}
