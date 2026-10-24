package provenance

import (
	"fmt"
	"strings"
)

const (
	ContractVersion         = "verification-provenance:v1"
	AgentIdentityContractV1 = "agent-implementation-identity:v1"

	LevelDeclared = "declared"
	LevelLegacy   = "legacy"

	KindArtifactDigest     = "artifact_digest"
	KindSourceRevision     = "source_revision"
	KindDeploymentRevision = "deployment_revision"
	KindModelProvider      = "model_provider"
	KindDeclared           = "declared"
	KindLegacy             = "legacy"
)

// AgentImplementationIdentity identifies the implementation behind an agent
// version. A declared identity is immutable once registered, but is not a
// cryptographic attestation by Realverax.
type AgentImplementationIdentity struct {
	ContractVersion string `json:"contract_version"`
	Kind            string `json:"kind"`
	Identifier      string `json:"identifier"`
	ProvenanceLevel string `json:"provenance_level"`
}

type ContractIdentities struct {
	Verifier             string `json:"verifier"`
	Evaluator            string `json:"evaluator"`
	EvidenceRequirements string `json:"evidence_requirements"`
	Attribution          string `json:"attribution"`
	Snapshot             string `json:"snapshot"`
	TemporalPolicy       string `json:"temporal_policy"`
	StabilityPolicy      string `json:"stability_policy"`
}

type ProviderIdentity struct {
	Role            string `json:"role"`
	ProviderID      string `json:"provider_id"`
	ContractVersion string `json:"contract_version,omitempty"`
}

type Verification struct {
	ContractVersion     string                      `json:"contract_version"`
	AgentImplementation AgentImplementationIdentity `json:"agent_implementation"`
	Contracts           ContractIdentities          `json:"contracts"`
	Providers           []ProviderIdentity          `json:"providers"`
}

func LegacyAgentIdentity() AgentImplementationIdentity {
	return AgentImplementationIdentity{
		ContractVersion: AgentIdentityContractV1,
		Kind:            KindLegacy,
		Identifier:      "unavailable",
		ProvenanceLevel: LevelLegacy,
	}
}

func ValidateDeclaredAgentIdentity(value AgentImplementationIdentity) (AgentImplementationIdentity, error) {
	value.ContractVersion = strings.TrimSpace(value.ContractVersion)
	value.Kind = strings.TrimSpace(value.Kind)
	value.Identifier = strings.TrimSpace(value.Identifier)
	value.ProvenanceLevel = strings.TrimSpace(value.ProvenanceLevel)
	if value.ContractVersion != AgentIdentityContractV1 {
		return value, fmt.Errorf("contract_version must be %s", AgentIdentityContractV1)
	}
	switch value.Kind {
	case KindArtifactDigest, KindSourceRevision, KindDeploymentRevision, KindModelProvider, KindDeclared:
	default:
		return value, fmt.Errorf("kind must be artifact_digest, source_revision, deployment_revision, model_provider, or declared")
	}
	if value.Identifier == "" || len(value.Identifier) > 500 {
		return value, fmt.Errorf("identifier is required and must be at most 500 characters")
	}
	if value.ProvenanceLevel != LevelDeclared {
		return value, fmt.Errorf("provenance_level must be declared")
	}
	return value, nil
}
