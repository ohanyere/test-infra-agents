// Package customerattribution evaluates customer-agent claims without treating
// collector transport or a Kubernetes principal as an execution identity.
package customerattribution

import "time"

const ContractVersion = "customer-agent-attribution:v1"
const (
	StatusAttributed    = "ATTRIBUTED"
	StatusNotAttributed = "NOT_ATTRIBUTED"
	StatusInconclusive  = "INCONCLUSIVE"
)

type IdentityBinding struct {
	WorkspaceID             string `json:"workspace_id"`
	EnvironmentID           string `json:"environment_id"`
	ClusterIdentity         string `json:"cluster_identity"`
	AgentVersionID          string `json:"agent_version_id"`
	AgentExecutionSessionID string `json:"agent_execution_session_id"`
	ActionID                string `json:"action_id"`
	CredentialID            string `json:"credential_id"`
	KubernetesPrincipal     string `json:"kubernetes_principal"`
}

type Claim struct {
	Identity    IdentityBinding
	ResourceUID string
	ActionType  string
}

type AuditObservation struct {
	ClusterIdentity               string
	ResourceUID                   string
	Operation                     string
	ResponseCode                  int32
	KubernetesPrincipal           string
	OriginStatus                  string // source-authenticated or unverified
	CollectorAuthenticationStatus string
	EventID                       string
	EventOccurredAt               time.Time
	AcquiredAt                    time.Time
}

type ExecutionRecord struct {
	Identity          IdentityBinding
	EventID           string
	Signature         []byte
	CredentialRevoked bool
	// No production verifier exists for customer execution records yet. These
	// fields are assertions until a trusted execution gateway verifies them.
}

type Assessment struct {
	ContractVersion string    `json:"contract_version"`
	Status          string    `json:"status"`
	ReasonCode      string    `json:"reason_code"`
	Reason          string    `json:"reason"`
	EvidenceRefs    []string  `json:"evidence_references"`
	EvaluatedAt     time.Time `json:"evaluated_at"`
}

// Assess uses the only current customer evidence path. It can reject a
// contradiction, but cannot return ATTRIBUTED: source authentication and a
// trusted execution record are not available in that path.
func Assess(claim Claim, audit *AuditObservation, record *ExecutionRecord, evaluatedAt time.Time) Assessment {
	out := Assessment{ContractVersion: ContractVersion, Status: StatusInconclusive, ReasonCode: "MISSING_AUDIT_EVIDENCE", Reason: "No independent audit evidence arrived for the claimed action.", EvidenceRefs: []string{}, EvaluatedAt: evaluatedAt.UTC()}
	if audit == nil {
		return out
	}
	if audit.EventID != "" {
		out.EvidenceRefs = append(out.EvidenceRefs, "audit:"+audit.EventID)
	}
	if audit.OriginStatus != "source-authenticated" {
		out.ReasonCode = "UNVERIFIED_AUDIT_ORIGIN"
		out.Reason = "The collector was authenticated, but the audit record's API-server origin was not."
		return out
	}
	if audit.ClusterIdentity != claim.Identity.ClusterIdentity || (claim.ResourceUID != "" && audit.ResourceUID != "" && audit.ResourceUID != claim.ResourceUID) {
		out.Status = StatusNotAttributed
		out.ReasonCode = "CONTRADICTORY_RESOURCE_BINDING"
		out.Reason = "The observed cluster or resource does not match the claimed operation."
		return out
	}
	if audit.ResponseCode < 200 || audit.ResponseCode >= 300 {
		out.Status = StatusNotAttributed
		out.ReasonCode = "MUTATION_FAILED"
		out.Reason = "The Kubernetes API did not complete the claimed mutation successfully."
		return out
	}
	if record == nil {
		out.ReasonCode = "MISSING_EXECUTION_BINDING"
		out.Reason = "No independently authenticated execution record binds this agent to the mutation."
		return out
	}
	if record.CredentialRevoked {
		out.ReasonCode = "EXECUTION_RECORD_UNVERIFIED"
		out.Reason = "A supplied revocation assertion cannot establish credential state without a trusted execution verifier."
		return out
	}
	if record.Identity.WorkspaceID != claim.Identity.WorkspaceID || record.Identity.EnvironmentID != claim.Identity.EnvironmentID || record.Identity.ClusterIdentity != claim.Identity.ClusterIdentity || record.Identity.AgentVersionID != claim.Identity.AgentVersionID || record.Identity.ActionID != claim.Identity.ActionID || record.Identity.AgentExecutionSessionID != claim.Identity.AgentExecutionSessionID || record.Identity.CredentialID != claim.Identity.CredentialID || record.Identity.KubernetesPrincipal != audit.KubernetesPrincipal || record.EventID != audit.EventID {
		out.ReasonCode = "EXECUTION_RECORD_UNVERIFIED"
		out.Reason = "The supplied execution record conflicts with the claim or audit record and has no trusted verifier."
		return out
	}
	out.ReasonCode = "EXECUTION_RECORD_UNVERIFIED"
	out.Reason = "An execution record was supplied, but no trusted customer execution verifier authenticated it."
	return out
}
