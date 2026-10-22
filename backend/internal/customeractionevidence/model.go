// Package customeractionevidence defines the internal contract for independently
// sourced customer action evidence. It does not expose a customer mutation API.
package customeractionevidence

import "time"

const ContractVersion = "customer-independent-action-evidence:v1"

type Kind string

const (
	KindAgentDeclaredAction       Kind = "agent_declared_action"
	KindObservedState             Kind = "observed_state"
	KindAuthenticatedActionRecord Kind = "authenticated_action_record"
)

type TrustClassification string

const (
	TrustUnverifiedAgentClaim       TrustClassification = "UNVERIFIED_AGENT_CLAIM"
	TrustIndependentlyObservedState TrustClassification = "INDEPENDENTLY_OBSERVED_STATE"
	TrustAuthenticatedActionRecord  TrustClassification = "AUTHENTICATED_ACTION_RECORD"
	TrustInsufficientProvenance     TrustClassification = "INSUFFICIENT_PROVENANCE"
	TrustConflictingProvenance      TrustClassification = "CONFLICTING_PROVENANCE"
	TrustAttributionQualified       TrustClassification = "ATTRIBUTION_QUALIFIED"
)

type Scope struct {
	WorkspaceID   string `json:"workspace_id"`
	EnvironmentID string `json:"environment_id"`
	SessionID     string `json:"session_id"`
	RunID         string `json:"run_id"`
}

type Target struct {
	Kind      string `json:"kind,omitempty"`
	Namespace string `json:"namespace,omitempty"`
	Name      string `json:"name,omitempty"`
	UID       string `json:"uid,omitempty"`
}

type Candidate struct {
	ContractVersion string                   `json:"contract_version"`
	Kind            Kind                     `json:"evidence_kind"`
	SourceID        string                   `json:"source_id"`
	SourceIdentity  string                   `json:"source_identity,omitempty"`
	SourceEventID   string                   `json:"source_event_id,omitempty"`
	ActionType      string                   `json:"action_type,omitempty"`
	Operation       string                   `json:"operation,omitempty"`
	Target          Target                   `json:"target"`
	ActorAssertion  string                   `json:"actor_assertion,omitempty"`
	OccurredAt      time.Time                `json:"occurred_at,omitempty"`
	ObservedAt      time.Time                `json:"observed_at,omitempty"`
	CorrelationIDs  []string                 `json:"correlation_ids,omitempty"`
	EvidenceRefs    []string                 `json:"evidence_references,omitempty"`
	Uncertainty     []string                 `json:"uncertainty,omitempty"`
	MissingFields   []string                 `json:"missing_fields,omitempty"`
	KubernetesAudit *KubernetesAuditMetadata `json:"kubernetes_audit,omitempty"`
}

type KubernetesAuditMetadata struct {
	SourceEventDigest     string    `json:"source_event_digest"`
	Verb                  string    `json:"verb"`
	APIGroup              string    `json:"api_group"`
	APIVersion            string    `json:"api_version"`
	Resource              string    `json:"resource"`
	AuditStage            string    `json:"audit_stage"`
	ResponseCode          int32     `json:"response_code"`
	RequestReceivedAt     time.Time `json:"request_received_at"`
	StageAt               time.Time `json:"stage_at"`
	AcquiredAt            time.Time `json:"acquired_at"`
	ClusterIdentity       string    `json:"cluster_identity"`
	AuditPolicyDigest     string    `json:"audit_policy_digest"`
	SourceConfiguration   string    `json:"source_configuration"`
	AcquisitionLocation   string    `json:"acquisition_location"`
	AcquisitionMethod     string    `json:"acquisition_method"`
	VerificationState     string    `json:"verification_state"`
	ProvisioningIdentity  string    `json:"provisioning_identity"`
	KubernetesPrincipal   string    `json:"kubernetes_principal,omitempty"`
	ProvenanceLimitations []string  `json:"provenance_limitations"`
}

// Assessment is produced by trusted server-side provider verification, never
// decoded from a customer request. Authentication proves source identity, not
// causation; causality is a separate assessment.
type Assessment struct {
	AuthenticationStatus string   `json:"authentication_status"`
	AuthorizationStatus  string   `json:"authorization_status"`
	SourceIdentityStatus string   `json:"source_identity_status"`
	AuthenticationMethod string   `json:"authentication_method,omitempty"`
	CredentialScope      string   `json:"credential_scope,omitempty"`
	ProvenanceStatus     string   `json:"provenance_verification_status"`
	ActorIdentity        string   `json:"actor_identity,omitempty"`
	ActorAuthority       string   `json:"actor_authority,omitempty"`
	ActorStatus          string   `json:"actor_identity_status"`
	CausalityStatus      string   `json:"causality_status"`
	Conflicts            []string `json:"conflicts,omitempty"`
	Uncertainty          []string `json:"uncertainty,omitempty"`
}

type SourceProvenance struct {
	ID                   string `json:"id"`
	Identity             string `json:"identity,omitempty"`
	EventID              string `json:"event_id,omitempty"`
	AuthenticationMethod string `json:"authentication_method,omitempty"`
	CredentialScope      string `json:"credential_scope,omitempty"`
	AuthenticationStatus string `json:"authentication_status"`
	AuthorizationStatus  string `json:"authorization_status"`
	IdentityStatus       string `json:"identity_status"`
	VerificationStatus   string `json:"verification_status"`
}

type ActorIdentity struct {
	Identity  string `json:"identity,omitempty"`
	Authority string `json:"authority,omitempty"`
	Status    string `json:"status"`
}

type Integrity struct {
	Algorithm string `json:"algorithm"`
	Digest    string `json:"digest"`
}

type Evidence struct {
	ContractVersion     string                   `json:"contract_version"`
	Kind                Kind                     `json:"evidence_kind"`
	Scope               Scope                    `json:"scope"`
	Source              SourceProvenance         `json:"source"`
	ActionType          string                   `json:"action_type,omitempty"`
	Operation           string                   `json:"operation,omitempty"`
	Target              Target                   `json:"target"`
	Actor               ActorIdentity            `json:"actor"`
	OccurredAt          time.Time                `json:"occurred_at,omitempty"`
	ObservedAt          time.Time                `json:"observed_at,omitempty"`
	IngestedAt          time.Time                `json:"ingested_at,omitempty"`
	CorrelationIDs      []string                 `json:"correlation_ids"`
	EvidenceRefs        []string                 `json:"evidence_references"`
	ProvenanceStatus    string                   `json:"provenance_verification_status"`
	CausalityStatus     string                   `json:"causality_status"`
	ProvenanceConflicts []string                 `json:"provenance_conflicts"`
	Trust               TrustClassification      `json:"trust_classification"`
	Integrity           Integrity                `json:"integrity"`
	Uncertainty         []string                 `json:"uncertainty"`
	MissingFields       []string                 `json:"missing_fields"`
	KubernetesAudit     *KubernetesAuditMetadata `json:"kubernetes_audit,omitempty"`
	providerVerified    bool
	providerSeal        [32]byte
}

type PersistResult struct {
	ID         string
	Duplicate  bool
	IngestedAt time.Time
}
