package coverage

import (
	"sort"
	"strings"
)

// CoverageContractV2 extends the V1 coverage contract with conservative
// requirements for GitOps and progressive-delivery competencies introduced
// alongside the Stage-31 task corpus.
//
// V1 semantics are never mutated: every V1 requirement keeps its exact
// families, trial counts, case-type rules, and contract version
// (capability-coverage-contract:v1). Historical V1 assessments remain
// reproducible through GetRequirement/ListRequirements, which are untouched.
//
// V2 requirements are published only where the current task corpus genuinely
// has independent families/cases:
//
//   - gitops.drift.deployment + gitops.desired_state.remediation are
//     independent families with positive and negative cases;
//   - gitops.argocd.recovery has positive and decoy cases and pairs with an
//     independent second family;
//   - delivery.rollout.recovery + delivery.progressive.error_regression are
//     independent families with positive, negative/decoy, and edge cases.
//
// Every V2 requirement still demands independent family breadth, minimum
// valid trials, and contrast/negative/decoy cases where appropriate.
// Capabilities without sufficient corpus remain without a requirement.
const CoverageContractV2 = "capability-coverage-contract:v2"

// v2Requirements holds only the NEW requirements published by V2. V1
// capabilities are served from coverageRequirements (requirements.go) with
// their original V1 contract version.
var v2Requirements = map[string]CoverageRequirement{
	"gitops.drift.detection": {
		CapabilityID:            "gitops.drift.detection",
		RequiredFamilies:        []string{"gitops.drift.deployment", "gitops.desired_state.remediation"},
		MinDistinctFamilies:     2,
		MinValidTrialsPerFamily: 2,
		RequiredCaseTypes:       []string{CaseTypePositive},
		ContractVersion:         CoverageContractV2,
	},
	"gitops.drift.remediation": {
		CapabilityID:            "gitops.drift.remediation",
		RequiredFamilies:        []string{"gitops.drift.deployment", "gitops.desired_state.remediation"},
		MinDistinctFamilies:     2,
		MinValidTrialsPerFamily: 2,
		RequiredCaseTypes:       []string{CaseTypePositive},
		RequiredCaseTypeGroups:  [][]string{{CaseTypeNegative, CaseTypeDecoy, CaseTypeAdversarial}},
		ContractVersion:         CoverageContractV2,
	},
	"gitops.desired_state.diagnosis": {
		CapabilityID:            "gitops.desired_state.diagnosis",
		RequiredFamilies:        []string{"gitops.desired_state.remediation", "gitops.drift.deployment"},
		MinDistinctFamilies:     2,
		MinValidTrialsPerFamily: 2,
		RequiredCaseTypes:       []string{CaseTypePositive},
		ContractVersion:         CoverageContractV2,
	},
	"gitops.desired_state.remediation": {
		CapabilityID:            "gitops.desired_state.remediation",
		RequiredFamilies:        []string{"gitops.desired_state.remediation", "gitops.drift.deployment"},
		MinDistinctFamilies:     2,
		MinValidTrialsPerFamily: 2,
		RequiredCaseTypes:       []string{CaseTypePositive},
		RequiredCaseTypeGroups:  [][]string{{CaseTypeNegative, CaseTypeDecoy, CaseTypeAdversarial}},
		ContractVersion:         CoverageContractV2,
	},
	"gitops.argocd.diagnosis": {
		CapabilityID:            "gitops.argocd.diagnosis",
		RequiredFamilies:        []string{"gitops.argocd.recovery", "gitops.drift.deployment"},
		MinDistinctFamilies:     2,
		MinValidTrialsPerFamily: 2,
		RequiredCaseTypes:       []string{CaseTypePositive},
		ContractVersion:         CoverageContractV2,
	},
	"gitops.argocd.recovery": {
		CapabilityID:            "gitops.argocd.recovery",
		RequiredFamilies:        []string{"gitops.argocd.recovery", "gitops.desired_state.remediation"},
		MinDistinctFamilies:     2,
		MinValidTrialsPerFamily: 2,
		RequiredCaseTypes:       []string{CaseTypePositive},
		RequiredCaseTypeGroups:  [][]string{{CaseTypeNegative, CaseTypeDecoy, CaseTypeAdversarial}},
		ContractVersion:         CoverageContractV2,
	},
	"delivery.rollout.diagnosis": {
		CapabilityID:            "delivery.rollout.diagnosis",
		RequiredFamilies:        []string{"delivery.rollout.recovery", "delivery.progressive.error_regression"},
		MinDistinctFamilies:     2,
		MinValidTrialsPerFamily: 2,
		RequiredCaseTypes:       []string{CaseTypePositive},
		ContractVersion:         CoverageContractV2,
	},
	"delivery.rollout.recovery": {
		CapabilityID:            "delivery.rollout.recovery",
		RequiredFamilies:        []string{"delivery.rollout.recovery", "delivery.progressive.error_regression"},
		MinDistinctFamilies:     2,
		MinValidTrialsPerFamily: 2,
		RequiredCaseTypes:       []string{CaseTypePositive},
		RequiredCaseTypeGroups:  [][]string{{CaseTypeNegative, CaseTypeDecoy, CaseTypeAdversarial}},
		ContractVersion:         CoverageContractV2,
	},
	"delivery.progressive_delivery.diagnosis": {
		CapabilityID:            "delivery.progressive_delivery.diagnosis",
		RequiredFamilies:        []string{"delivery.progressive.error_regression", "delivery.rollout.recovery"},
		MinDistinctFamilies:     2,
		MinValidTrialsPerFamily: 2,
		RequiredCaseTypes:       []string{CaseTypePositive},
		ContractVersion:         CoverageContractV2,
	},
	"delivery.progressive_delivery.recovery": {
		CapabilityID:            "delivery.progressive_delivery.recovery",
		RequiredFamilies:        []string{"delivery.progressive.error_regression", "delivery.rollout.recovery"},
		MinDistinctFamilies:     2,
		MinValidTrialsPerFamily: 2,
		RequiredCaseTypes:       []string{CaseTypePositive},
		RequiredCaseTypeGroups:  [][]string{{CaseTypeNegative, CaseTypeDecoy, CaseTypeAdversarial}},
		ContractVersion:         CoverageContractV2,
	},
}

// GetRequirementV2 returns the coverage requirement for a capability under
// the V2 contract view: V2 additions first, then the unchanged V1
// requirements with their original V1 contract version. Capabilities with no
// published requirement in either contract report false.
func GetRequirementV2(capabilityID string) (CoverageRequirement, bool) {
	trimmed := strings.TrimSpace(capabilityID)
	if requirement, ok := v2Requirements[trimmed]; ok {
		return requirement, true
	}
	return GetRequirement(trimmed)
}

// ListRequirementsV2 returns the combined V1 + V2 requirement catalog in
// stable capability order. V1 entries are identical to ListRequirements.
func ListRequirementsV2() []CoverageRequirement {
	seen := map[string]bool{}
	out := make([]CoverageRequirement, 0, len(coverageRequirements)+len(v2Requirements))
	for _, requirement := range ListRequirements() {
		out = append(out, requirement)
		seen[requirement.CapabilityID] = true
	}
	for _, requirement := range v2Requirements {
		if !seen[requirement.CapabilityID] {
			out = append(out, requirement)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].CapabilityID < out[j].CapabilityID })
	return out
}

// IsV2Requirement reports whether capabilityID was introduced by the V2
// contract (as opposed to carried over unchanged from V1).
func IsV2Requirement(capabilityID string) bool {
	_, ok := v2Requirements[strings.TrimSpace(capabilityID)]
	return ok
}
