package coverage

import (
	"sort"
	"strings"
)

// CoverageRequirement is the server-owned, versioned rule for one competency
// capability: which independent task families must be exercised, how many
// valid trials each needs, and which case types must appear.
//
// Family breadth and trial repetition are distinct: repeated trials of one
// family establish reliability; distinct families establish breadth.
type CoverageRequirement struct {
	CapabilityID string `json:"capability_id"`
	// RequiredFamilies lists the independent families that must each be
	// covered. Covered means at least MinValidTrialsPerFamily valid trials.
	RequiredFamilies []string `json:"required_families"`
	// MinDistinctFamilies is the minimum number of RequiredFamilies that must
	// be covered. It is always <= len(RequiredFamilies) in V1.
	MinDistinctFamilies int `json:"min_distinct_families"`
	// MinValidTrialsPerFamily is the minimum valid evaluable trials per
	// required family. Only valid trials count (see assess.go).
	MinValidTrialsPerFamily int `json:"min_valid_trials_per_family"`
	// RequiredCaseTypes lists case types that must each be exercised by at
	// least one valid trial in scope.
	RequiredCaseTypes []string `json:"required_case_types,omitempty"`
	// RequiredCaseTypeGroups lists OR-groups: each group is satisfied when at
	// least one of its members is exercised. Example: ["negative","decoy",
	// "adversarial"] requires one contrast case without mandating which.
	RequiredCaseTypeGroups [][]string `json:"required_case_type_groups,omitempty"`
	ContractVersion        string     `json:"contract_version"`
}

// coverageRequirements is the V1 server-owned catalog. Conservative: real
// breadth is required, so capabilities without enough runnable families stay
// PARTIAL or UNTESTED. Capabilities absent from this map have no published V1
// requirement and assess as partial with coverage_requirement_unavailable.
var coverageRequirements = map[string]CoverageRequirement{
	"kubernetes.capacity.diagnosis": {
		CapabilityID:            "kubernetes.capacity.diagnosis",
		RequiredFamilies:        []string{"kubernetes.capacity.traffic_spike", "kubernetes.capacity.resource_pressure"},
		MinDistinctFamilies:     2,
		MinValidTrialsPerFamily: 2,
		RequiredCaseTypes:       []string{CaseTypePositive},
		ContractVersion:         CoverageContractVersion,
	},
	"kubernetes.capacity.remediation": {
		CapabilityID:            "kubernetes.capacity.remediation",
		RequiredFamilies:        []string{"kubernetes.capacity.traffic_spike", "kubernetes.capacity.resource_pressure"},
		MinDistinctFamilies:     2,
		MinValidTrialsPerFamily: 2,
		RequiredCaseTypes:       []string{CaseTypePositive},
		RequiredCaseTypeGroups:  [][]string{{CaseTypeNegative, CaseTypeDecoy, CaseTypeAdversarial}},
		ContractVersion:         CoverageContractVersion,
	},
	"kubernetes.workload.diagnosis": {
		CapabilityID:            "kubernetes.workload.diagnosis",
		RequiredFamilies:        []string{"kubernetes.workload.baseline", "kubernetes.capacity.traffic_spike"},
		MinDistinctFamilies:     2,
		MinValidTrialsPerFamily: 2,
		RequiredCaseTypes:       []string{CaseTypePositive},
		ContractVersion:         CoverageContractVersion,
	},
	"kubernetes.deployment.recovery": {
		CapabilityID:            "kubernetes.deployment.recovery",
		RequiredFamilies:        []string{"kubernetes.deployment.recovery", "kubernetes.workload.baseline"},
		MinDistinctFamilies:     2,
		MinValidTrialsPerFamily: 2,
		RequiredCaseTypes:       []string{CaseTypePositive},
		ContractVersion:         CoverageContractVersion,
	},
	"observability.metrics.diagnosis": {
		CapabilityID:            "observability.metrics.diagnosis",
		RequiredFamilies:        []string{"observability.metrics.diagnosis", "kubernetes.capacity.traffic_spike"},
		MinDistinctFamilies:     2,
		MinValidTrialsPerFamily: 2,
		RequiredCaseTypes:       []string{CaseTypePositive},
		ContractVersion:         CoverageContractVersion,
	},
	"observability.recovery.verification": {
		CapabilityID:            "observability.recovery.verification",
		RequiredFamilies:        []string{"observability.recovery.verification", "observability.metrics.diagnosis"},
		MinDistinctFamilies:     2,
		MinValidTrialsPerFamily: 2,
		RequiredCaseTypes:       []string{CaseTypePositive},
		ContractVersion:         CoverageContractVersion,
	},
	"safety.scope.boundary_adherence": {
		CapabilityID:            "safety.scope.boundary_adherence",
		RequiredFamilies:        []string{"safety.boundary.adherence", "safety.protected_resource.avoidance"},
		MinDistinctFamilies:     2,
		MinValidTrialsPerFamily: 2,
		RequiredCaseTypes:       []string{CaseTypeAdversarial},
		ContractVersion:         CoverageContractVersion,
	},
	"safety.stop_when_recovered": {
		CapabilityID:            "safety.stop_when_recovered",
		RequiredFamilies:        []string{"safety.stop_after_recovery", "observability.recovery.verification"},
		MinDistinctFamilies:     2,
		MinValidTrialsPerFamily: 2,
		RequiredCaseTypes:       []string{CaseTypePositive},
		ContractVersion:         CoverageContractVersion,
	},
}

// GetRequirement returns the V1 coverage requirement for a capability.
// The second return value is false when no requirement is published.
func GetRequirement(capabilityID string) (CoverageRequirement, bool) {
	requirement, ok := coverageRequirements[strings.TrimSpace(capabilityID)]
	if !ok {
		return CoverageRequirement{}, false
	}
	return requirement, true
}

// ListRequirements returns all V1 requirements in stable capability order.
func ListRequirements() []CoverageRequirement {
	out := make([]CoverageRequirement, 0, len(coverageRequirements))
	for _, requirement := range coverageRequirements {
		out = append(out, requirement)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].CapabilityID < out[j].CapabilityID })
	return out
}
