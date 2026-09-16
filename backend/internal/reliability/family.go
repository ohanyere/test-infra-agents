package reliability

import "sort"

// FamilyReliability aggregates only compatible valid evidence from
// ScenarioVersions belonging to the same Task Family. Breadth (distinct
// ScenarioVersions) and repetition (valid trials) remain separate fields so
// 20 retries of one scenario can never masquerade as broad family
// reliability.
type FamilyReliability struct {
	FamilyID string `json:"family_id"`
	// ScenarioVersionIDs lists the distinct exact tasks contributing valid
	// evidence, in stable order.
	ScenarioVersionIDs    []string `json:"scenario_version_ids"`
	DistinctScenarios     int      `json:"distinct_scenario_versions"`
	DistinctVariants      int      `json:"distinct_variants"`
	ValidTrials           int      `json:"valid_trials"`
	PassedTrials          int      `json:"passed_trials"`
	FailedTrials          int      `json:"failed_trials"`
	DecisiveTrials        int      `json:"decisive_trials"`
	InconclusiveTrials    int      `json:"inconclusive_trials"`
	StoppedTrials         int      `json:"stopped_trials"`
	PlatformInvalidTrials int      `json:"platform_invalid_trials"`
	// PassRate is the historical decisive-only rate (passed/decisive),
	// frozen under reliability-metrics-contract:v1. DecisivePassRate
	// exposes the same value explicitly; ValidSuccessRate is
	// passed/valid (every valid non-pass is non-success).
	PassRate         *float64 `json:"pass_rate"`
	DecisivePassRate *float64 `json:"decisive_pass_rate"`
	ValidSuccessRate *float64 `json:"valid_success_rate"`
	// Consistency carries the same supported-k pass^k/pass@k estimates at
	// family scope. Consumers must display pass^k (consistency) more
	// prominently than pass@k.
	Consistency []KMetric `json:"consistency"`
	// CaseTypeDistribution counts valid trials per explicit case type.
	CaseTypeDistribution map[string]int `json:"case_type_distribution"`
	Sufficiency          string         `json:"sample_sufficiency"`
	// BreadthNote is a machine-readable hint when repetition dominates:
	// "single_scenario" when all valid evidence comes from one
	// ScenarioVersion, "" otherwise.
	BreadthNote         string `json:"breadth_note,omitempty"`
	ReliabilityContract string `json:"reliability_contract_version"`
}

// BuildFamilyReliabilities groups compatible valid evidence by Task Family.
// Trials without an explicit family assignment never contribute (matching
// coverage semantics: unassigned versions are invisible here).
func BuildFamilyReliabilities(policy Policy, trials []Trial) []FamilyReliability {
	byFamily := map[string][]Trial{}
	for _, trial := range trials {
		if trial.FamilyID == "" {
			continue
		}
		byFamily[trial.FamilyID] = append(byFamily[trial.FamilyID], trial)
	}
	ids := make([]string, 0, len(byFamily))
	for id := range byFamily {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	out := make([]FamilyReliability, 0, len(ids))
	for _, id := range ids {
		familyTrials := byFamily[id]
		empirical := Summarize(familyTrials)
		scenarios := map[string]bool{}
		variants := map[string]bool{}
		distribution := map[string]int{}
		for _, trial := range ValidTrials(familyTrials) {
			scenarios[trial.ScenarioVersionID] = true
			if trial.VariantID != "" {
				variants[trial.VariantID] = true
			}
			if trial.CaseType != "" {
				distribution[trial.CaseType]++
			}
		}
		item := FamilyReliability{
			FamilyID:              id,
			ScenarioVersionIDs:    sortedKeys(scenarios),
			DistinctScenarios:     len(scenarios),
			DistinctVariants:      len(variants),
			ValidTrials:           empirical.ValidTrials,
			PassedTrials:          empirical.PassedTrials,
			FailedTrials:          empirical.FailedTrials,
			DecisiveTrials:        empirical.DecisiveTrials,
			InconclusiveTrials:    empirical.InconclusiveTrials,
			StoppedTrials:         empirical.StoppedTrials,
			PlatformInvalidTrials: empirical.PlatformInvalidTrials,
			PassRate:              empirical.PassRate,
			DecisivePassRate:      empirical.DecisivePassRate,
			ValidSuccessRate:      empirical.ValidSuccessRate,
			Consistency:           KMetrics(familyTrials),
			CaseTypeDistribution:  distribution,
			Sufficiency:           AssessSufficiency(empirical.ValidTrials, policy.MinValidTrialsFamily, policy.TargetValidTrialsFamily),
			ReliabilityContract:   ReliabilityContractVersion,
		}
		if item.DistinctScenarios <= 1 && empirical.ValidTrials > 0 {
			item.BreadthNote = "single_scenario"
		}
		out = append(out, item)
	}
	return out
}
