package coverage

import (
	"sort"
)

// Trial validity values accepted by the engine. Only ValidityValid counts
// toward TESTED coverage. The caller maps platform outcomes to these:
//
//	valid:            platform produced a valid evaluation (agent pass, agent
//	                   failure, inconclusive, or stopped are all valid trials;
//	                   the question is "did we validly test?", not "did it pass?").
//	invalid_platform: environment provisioning failure, missing required
//	                   telemetry caused by Evalplane, abandoned platform
//	                   execution, or any other platform-invalid outcome.
//	pending:          evidence not complete; never persisted as TESTED.
const (
	TrialValidityValid           = "valid"
	TrialValidityInvalidPlatform = "invalid_platform"
	TrialValidityPending         = "pending"
)

// TrialEvidence is one evaluable trial in scope. FamilyID and CaseType come
// from the explicit ScenarioVersion assignment; unknown families are rejected
// before assessment. VariantID exposes depth (distinct variants exercised)
// without leaking hidden evaluator-only variant values.
type TrialEvidence struct {
	RunID             string
	ScenarioVersionID string
	FamilyID          string
	CaseType          string
	Validity          string
	EvaluationResult  string
	VariantID         string
}

// FamilyCoverage is per-family evidence for one capability.
type FamilyCoverage struct {
	FamilyID           string   `json:"family_id"`
	Required           bool     `json:"required"`
	ValidTrials        int      `json:"valid_trials"`
	DistinctVariants   int      `json:"distinct_variant_count"`
	CaseTypesExercised []string `json:"case_types_exercised"`
	Covered            bool     `json:"covered"`
}

// MissingRequirement is a structured, machine-readable coverage gap.
type MissingRequirement struct {
	Code     string `json:"code"`
	FamilyID string `json:"family_id,omitempty"`
	Detail   string `json:"detail,omitempty"`
}

// Assessment is the deterministic coverage verdict for one capability claim
// in an exact evidence scope. It never contains qualified/not_qualified:
// those belong to qualification policy, never to coverage.
type Assessment struct {
	CapabilityID              string               `json:"capability_id"`
	ClaimStatus               string               `json:"claim_status"`
	TestStatus                string               `json:"test_status"`
	CoverageContractVersion   string               `json:"coverage_contract_version"`
	TaskFamilyContractVersion string               `json:"task_family_contract_version"`
	CapabilityCatalogVersion  string               `json:"capability_catalog_version"`
	RequiredFamilyCount       int                  `json:"required_family_count"`
	CoveredFamilyCount        int                  `json:"covered_family_count"`
	ValidTrialCount           int                  `json:"valid_trial_count"`
	DistinctVariantCount      int                  `json:"distinct_variant_count"`
	Families                  []FamilyCoverage     `json:"families"`
	MissingRequirements       []MissingRequirement `json:"missing_requirements"`
}

// Assess deterministically evaluates coverage for one capability claim.
// Pure function: no I/O, no LLM, stable sorted output.
func Assess(capabilityID string, trials []TrialEvidence) Assessment {
	out := Assessment{
		CapabilityID:              capabilityID,
		ClaimStatus:               ClaimStatusClaimed,
		CoverageContractVersion:   CoverageContractVersion,
		TaskFamilyContractVersion: TaskFamilyContractVersion,
		CapabilityCatalogVersion:  CapabilityCatalogVersion,
		Families:                  []FamilyCoverage{},
		MissingRequirements:       []MissingRequirement{},
	}

	requirement, hasRequirement := GetRequirement(capabilityID)
	if !hasRequirement {
		// Absence of requirements is never success.
		out.TestStatus = TestStatusPartial
		out.MissingRequirements = []MissingRequirement{{Code: CoverageRequirementUnavailable, Detail: "no published V1 coverage requirement for this capability"}}
		return out
	}
	out.RequiredFamilyCount = len(requirement.RequiredFamilies)

	valid := make([]TrialEvidence, 0, len(trials))
	seenPlatformInvalid := false
	for _, trial := range trials {
		switch trial.Validity {
		case TrialValidityValid:
			valid = append(valid, trial)
		case TrialValidityInvalidPlatform:
			seenPlatformInvalid = true
		}
	}

	// Group valid trials by family. Unknown families are rejected by callers;
	// defensively ignore blanks here so one bad row cannot poison assessment.
	byFamily := map[string][]TrialEvidence{}
	for _, trial := range valid {
		if trial.FamilyID == "" {
			continue
		}
		byFamily[trial.FamilyID] = append(byFamily[trial.FamilyID], trial)
	}

	variantSet := map[string]bool{}
	for _, trial := range valid {
		if trial.VariantID != "" {
			variantSet[trial.VariantID] = true
		}
	}
	out.ValidTrialCount = len(valid)
	out.DistinctVariantCount = len(variantSet)

	requiredSet := map[string]bool{}
	for _, familyID := range requirement.RequiredFamilies {
		requiredSet[familyID] = true
	}

	familyIDs := make([]string, 0, len(requirement.RequiredFamilies)+len(byFamily))
	seenFamily := map[string]bool{}
	for _, familyID := range requirement.RequiredFamilies {
		if !seenFamily[familyID] {
			seenFamily[familyID] = true
			familyIDs = append(familyIDs, familyID)
		}
	}
	for familyID := range byFamily {
		if !seenFamily[familyID] {
			seenFamily[familyID] = true
			familyIDs = append(familyIDs, familyID)
		}
	}
	sort.Strings(familyIDs)

	exercisedCaseTypes := map[string]bool{}
	for _, familyID := range familyIDs {
		familyTrials := byFamily[familyID]
		caseSet := map[string]bool{}
		variants := map[string]bool{}
		for _, trial := range familyTrials {
			if trial.CaseType != "" {
				caseSet[trial.CaseType] = true
				exercisedCaseTypes[trial.CaseType] = true
			}
			if trial.VariantID != "" {
				variants[trial.VariantID] = true
			}
		}
		covered := requiredSet[familyID] && len(familyTrials) >= requirement.MinValidTrialsPerFamily
		if covered {
			out.CoveredFamilyCount++
		}
		out.Families = append(out.Families, FamilyCoverage{
			FamilyID:           familyID,
			Required:           requiredSet[familyID],
			ValidTrials:        len(familyTrials),
			DistinctVariants:   len(variants),
			CaseTypesExercised: NormalizeCaseTypes(keys(caseSet)),
			Covered:            covered,
		})
	}

	if len(valid) == 0 {
		out.TestStatus = TestStatusUntested
		if seenPlatformInvalid {
			out.MissingRequirements = []MissingRequirement{
				{Code: PlatformInvalidOnly, Detail: "evidence exists but only platform-invalid trials; platform-invalid execution never satisfies coverage"},
				{Code: NoValidEvidence, Detail: "no valid qualifying evidence for the capability"},
			}
		} else {
			out.MissingRequirements = []MissingRequirement{
				{Code: NoValidEvidence, Detail: "no valid qualifying evidence for the capability"},
			}
		}
		return out
	}

	missing := []MissingRequirement{}
	for _, familyID := range requirement.RequiredFamilies {
		familyTrials := byFamily[familyID]
		if len(familyTrials) < requirement.MinValidTrialsPerFamily {
			if len(familyTrials) == 0 {
				missing = append(missing, MissingRequirement{Code: MissingRequiredFamily, FamilyID: familyID, Detail: "required task family has no valid trials"})
			} else {
				missing = append(missing, MissingRequirement{Code: InsufficientValidTrials, FamilyID: familyID, Detail: "required task family has fewer valid trials than required"})
			}
		}
	}
	// Distinct-family breadth: ten trials of one family never satisfy two.
	if out.CoveredFamilyCount < requirement.MinDistinctFamilies {
		// Only add a generic breadth gap when per-family gaps do not already
		// explain it (avoid duplicate noise when every family is missing).
		if len(missing) == 0 {
			missing = append(missing, MissingRequirement{Code: MissingRequiredFamily, Detail: "fewer distinct task families covered than required"})
		}
	}
	for _, required := range requirement.RequiredCaseTypes {
		if !exercisedCaseTypes[required] {
			missing = append(missing, MissingRequirement{Code: MissingRequiredCaseType, Detail: "required case type not exercised: " + required})
		}
	}
	for _, group := range requirement.RequiredCaseTypeGroups {
		satisfied := false
		for _, member := range group {
			if exercisedCaseTypes[member] {
				satisfied = true
				break
			}
		}
		if !satisfied {
			missing = append(missing, MissingRequirement{Code: MissingRequiredCaseType, Detail: "required case-type group not exercised: one of " + joinSorted(group)})
		}
	}

	if len(missing) == 0 {
		out.TestStatus = TestStatusTested
		out.MissingRequirements = []MissingRequirement{}
		return out
	}
	out.TestStatus = TestStatusPartial
	out.MissingRequirements = missing
	return out
}

// AssessMany assesses every claimed capability over the same trial set.
// Trials carry their own family, so one scope can feed many capabilities.
func AssessMany(capabilityIDs []string, trials []TrialEvidence) []Assessment {
	out := make([]Assessment, 0, len(capabilityIDs))
	for _, id := range capabilityIDs {
		out = append(out, Assess(id, trialsForCapability(id, trials)))
	}
	sort.Slice(out, func(i, j int) bool { return out[i].CapabilityID < out[j].CapabilityID })
	return out
}

// trialsForCapability selects trials relevant to a capability. V1 keeps this
// intentionally broad: any valid trial in scope contributes family/case
// evidence, because families are shared across capabilities and requirements
// name the families they need. Family-scoping mistakes surface as
// missing_required_family rather than silent exclusion.
func trialsForCapability(_ string, trials []TrialEvidence) []TrialEvidence {
	return trials
}

func keys(set map[string]bool) []string {
	out := make([]string, 0, len(set))
	for value := range set {
		out = append(out, value)
	}
	return out
}

func joinSorted(values []string) string {
	sorted := append([]string(nil), values...)
	sort.Strings(sorted)
	joined := ""
	for i, value := range sorted {
		if i > 0 {
			joined += "/"
		}
		joined += value
	}
	return joined
}
