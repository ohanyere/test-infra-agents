package reliability

import (
	"sort"

	"github.com/evalplane/evalplane/backend/internal/coverage"
)

// Coverage contract selection for capability aggregation.
//
//	CoverageContractV1 is the historical contract
//	  (capability-coverage-contract:v1): 8 requirements, frozen. V1-only
//	  builders below reproduce historical semantics byte-for-byte.
//	CoverageContractV2 is the current qualification coverage contract
//	  (capability-coverage-contract:v2): V1 entries unchanged plus 10
//	  GitOps/progressive-delivery requirements.
//	CurrentCoverageContractVersion is the contract current Stage-33
//	  AgentVersion reliability aggregates under (V2 view). Reports pin
//	  this value; per-capability CoverageContract carries the exact
//	  requirement contract (V1 entries stay v1 inside the V2 view).
const (
	CoverageContractV1 = coverage.CoverageContractVersion
	CoverageContractV2 = coverage.CoverageContractV2
	// CurrentCoverageContractVersion is the coverage contract reliability
	// uses for current capability aggregation.
	CurrentCoverageContractVersion = coverage.CoverageContractV2
)

// RequirementUnavailableState is the structured state for capabilities
// with no published requirement under the selected contract. Callers must
// check RequirementAvailable first; no reliability is fabricated.
const RequirementUnavailableState = CapabilityInsufficient

// CapabilityReliability measures reliability per capability using the
// pinned coverage contract's capability-to-family mappings. It reports
// evidence only: required families, covered families, per-family
// reliability, valid trial counts, and contrast-case reliability. It never
// emits a QUALIFIED verdict; Stage-22 QualificationPolicy owns that.
type CapabilityReliability struct {
	CapabilityID string `json:"capability_id"`
	// RequiredFamilies comes from the server-owned coverage requirement.
	RequiredFamilies []string `json:"required_families"`
	// CoveredFamilies lists required families meeting the coverage minimum.
	// Breadth truth comes from coverage; reliability never redefines it.
	CoveredFamilies []string `json:"covered_families"`
	// Families carries per-family reliability for every required family
	// (plus any observed non-required family in scope, marked by absence
	// from RequiredFamilies).
	Families        []FamilyReliability `json:"families"`
	ValidTrialCount int                 `json:"valid_trial_count"`
	// Summary pools valid evidence from required families only: empirical
	// metrics, supported-k estimates, and sufficiency at family thresholds.
	// Consistency (pass^k) is the primary reliability signal; pass@k is
	// secondary and never labeled consistency.
	Summary ScopedSummary `json:"summary"`
	// ContrastTrials counts valid negative/contrast trials (negative, decoy,
	// adversarial) in scope.
	ContrastTrials int `json:"contrast_trials"`
	// ContrastPassRate is the pass rate over valid contrast trials, nil when
	// no contrast evidence exists. Contrast evidence is preserved, never
	// folded silently into the overall rate.
	ContrastPassRate *float64 `json:"contrast_pass_rate"`
	// ReliabilityState is insufficient_evidence | limited_evidence |
	// measured. Never qualified / not_qualified.
	ReliabilityState     string `json:"reliability_state"`
	ReliabilityContract  string `json:"reliability_contract_version"`
	CoverageContract     string `json:"coverage_contract_version"`
	TaskFamilyContract   string `json:"task_family_contract_version"`
	RequirementAvailable bool   `json:"requirement_available"`
}

// BuildCapabilityReliabilities assesses every claimed capability over the
// same trial scope under the current coverage contract (V2 view: V1
// entries with their original V1 contract plus V2 GitOps/delivery
// requirements). Families are shared across capabilities; requirements
// name the families each capability needs.
func BuildCapabilityReliabilities(policy Policy, capabilityIDs []string, trials []Trial) []CapabilityReliability {
	return buildCapabilitiesWith(policy, capabilityIDs, trials, CurrentCoverageContractVersion, coverage.GetRequirementV2)
}

// BuildCapabilityReliabilitiesV1 assesses every claimed capability under
// the frozen historical V1 contract (coverage.GetRequirement). It exists
// so historical V1 readings remain reproducible; new reliability reports
// must use BuildCapabilityReliabilities (current contract).
func BuildCapabilityReliabilitiesV1(policy Policy, capabilityIDs []string, trials []Trial) []CapabilityReliability {
	return buildCapabilitiesWith(policy, capabilityIDs, trials, CoverageContractV1, coverage.GetRequirement)
}

func buildCapabilitiesWith(policy Policy, capabilityIDs []string, trials []Trial, selectedContract string, lookup func(string) (coverage.CoverageRequirement, bool)) []CapabilityReliability {
	ids := append([]string(nil), capabilityIDs...)
	sort.Strings(ids)
	out := make([]CapabilityReliability, 0, len(ids))
	for _, id := range ids {
		out = append(out, buildCapabilityWith(policy, id, trials, selectedContract, lookup))
	}
	return out
}

func buildCapability(policy Policy, capabilityID string, trials []Trial) CapabilityReliability {
	return buildCapabilityWith(policy, capabilityID, trials, CurrentCoverageContractVersion, coverage.GetRequirementV2)
}

func buildCapabilityWith(policy Policy, capabilityID string, trials []Trial, selectedContract string, lookup func(string) (coverage.CoverageRequirement, bool)) CapabilityReliability {
	out := CapabilityReliability{
		CapabilityID:        capabilityID,
		Families:            []FamilyReliability{},
		ReliabilityContract: ReliabilityContractVersion,
		CoverageContract:    selectedContract,
		TaskFamilyContract:  coverage.TaskFamilyContractVersion,
	}
	requirement, ok := lookup(capabilityID)
	if !ok {
		out.ReliabilityState = RequirementUnavailableState
		return out
	}
	// Pin the exact requirement contract (V1 entries stay v1 inside the
	// V2 view); the report-level pin records the selected contract view.
	out.CoverageContract = requirement.ContractVersion
	out.RequirementAvailable = true
	out.RequiredFamilies = append([]string(nil), requirement.RequiredFamilies...)
	families := BuildFamilyReliabilities(policy, trials)
	byFamily := map[string]FamilyReliability{}
	for _, family := range families {
		byFamily[family.FamilyID] = family
	}
	requiredSet := map[string]bool{}
	for _, familyID := range requirement.RequiredFamilies {
		requiredSet[familyID] = true
	}
	requiredTrials := make([]Trial, 0, len(trials))
	for _, trial := range trials {
		if requiredSet[trial.FamilyID] {
			requiredTrials = append(requiredTrials, trial)
		}
	}
	out.Summary = SummarizeScope(policy, requiredTrials, policy.MinValidTrialsFamily, policy.TargetValidTrialsFamily)
	// Deterministic coverage check mirrors the coverage engine minimum so
	// CoveredFamilies stays consistent with TESTED breadth semantics.
	validByFamily := map[string]int{}
	for _, trial := range ValidTrials(trials) {
		if trial.FamilyID == "" {
			continue
		}
		validByFamily[trial.FamilyID]++
	}
	for _, familyID := range requirement.RequiredFamilies {
		if validByFamily[familyID] >= requirement.MinValidTrialsPerFamily {
			out.CoveredFamilies = append(out.CoveredFamilies, familyID)
		}
		family, found := byFamily[familyID]
		if !found {
			family = FamilyReliability{
				FamilyID:             familyID,
				ScenarioVersionIDs:   []string{},
				CaseTypeDistribution: map[string]int{},
				Consistency:          KMetrics(nil),
				Sufficiency:          SufficiencyInsufficient,
				ReliabilityContract:  ReliabilityContractVersion,
			}
		}
		out.Families = append(out.Families, family)
	}
	// Observed non-required families follow in stable order for transparency.
	extra := make([]string, 0)
	for _, family := range families {
		if !requiredSet[family.FamilyID] {
			extra = append(extra, family.FamilyID)
		}
	}
	sort.Strings(extra)
	for _, familyID := range extra {
		out.Families = append(out.Families, byFamily[familyID])
	}
	contrastPassed, contrastFailed := 0, 0
	for _, trial := range ValidTrials(trials) {
		out.ValidTrialCount++
		if coverage.IsContrastCase(trial.CaseType) {
			out.ContrastTrials++
			switch trial.Result {
			case ResultPass:
				contrastPassed++
			case ResultFail:
				contrastFailed++
			}
		}
	}
	if decisive := contrastPassed + contrastFailed; decisive > 0 {
		value := 100 * float64(contrastPassed) / float64(decisive)
		out.ContrastPassRate = &value
	}
	switch {
	case out.ValidTrialCount < policy.MinValidTrialsFamily || len(out.CoveredFamilies) == 0:
		out.ReliabilityState = CapabilityInsufficient
	case out.ValidTrialCount < policy.TargetValidTrialsFamily || len(out.CoveredFamilies) < len(out.RequiredFamilies):
		out.ReliabilityState = CapabilityLimited
	default:
		out.ReliabilityState = CapabilityMeasured
	}
	return out
}
