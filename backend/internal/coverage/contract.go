// Package coverage owns the deterministic capability-coverage engine.
//
// Separate concerns:
//
//	Capability Claim (competency): what an AgentVersion declares (CLAIMED only).
//	Task Family (this package): independent class of infrastructure situation.
//	Coverage assessment (this package): UNTESTED | PARTIAL | TESTED derived
//	  from valid evaluation evidence. TESTED means sufficient required
//	  evidence was exercised, NOT that the agent succeeded.
//	Qualification (qualification package): per-capability QUALIFIED /
//	  NOT QUALIFIED policy outcome. Never inferred from TESTED here.
//
// Contracts:
//
//	task-family-contract:v1
//	capability-coverage-contract:v1
//	infrastructure-capability-catalog:v1 (owned by competency)
//
// No LLM is used anywhere in this package. Assessment is a pure function of
// explicit family assignments, explicit case types, and valid trials.
package coverage

import (
	"github.com/evalplane/evalplane/backend/internal/competency"
	"github.com/evalplane/evalplane/backend/internal/runtimecontract"
)

const (
	// TaskFamilyContractVersion identifies the task-family catalog contract.
	// Historical assignments and assessments pin this value.
	TaskFamilyContractVersion = "task-family-contract:v1"

	// CoverageContractVersion identifies the coverage requirement + assessment
	// contract. Historical assessments pin this value.
	CoverageContractVersion = "capability-coverage-contract:v1"

	// CapabilityCatalogVersion is the competency catalog this coverage V1
	// was built against.
	CapabilityCatalogVersion = runtimecontract.InfrastructureCapabilityCatalogVersion
)

// Test states. Machine-readable only; the frontend renders display labels.
const (
	TestStatusUntested = "untested"
	TestStatusPartial  = "partial"
	TestStatusTested   = "tested"
)

// Missing requirement codes. Stable, machine-readable; never UI prose.
const (
	MissingRequiredFamily          = "missing_required_family"
	InsufficientValidTrials        = "insufficient_valid_trials"
	MissingRequiredCaseType        = "missing_required_case_type"
	NoValidEvidence                = "no_valid_evidence"
	PlatformInvalidOnly            = "platform_invalid_only"
	CoverageRequirementUnavailable = "coverage_requirement_unavailable"
)

// ClaimStatus mirrors the only V1 claim state.
const ClaimStatusClaimed = competency.ClaimStatusClaimed
