package reliability

import "sort"

// KDelta is the paired pass@k / pass^k delta for one k. Values are present
// only when both sides expose the metric (n >= k on both); otherwise the
// entry records unavailability explicitly.
type KDelta struct {
	K               int      `json:"k"`
	Available       bool     `json:"available"`
	PassAtKDelta    *float64 `json:"pass_at_k_delta,omitempty"`
	PassToTheKDelta *float64 `json:"pass_to_the_k_delta,omitempty"`
}

// FamilyDelta pairs per-family reliability deltas.
type FamilyDelta struct {
	FamilyID               string   `json:"family_id"`
	Available              bool     `json:"available"`
	PassRateDelta          *float64 `json:"pass_rate_delta,omitempty"`
	ValidTrialsDelta       int      `json:"valid_trials_delta"`
	DistinctScenariosDelta int      `json:"distinct_scenarios_delta"`
	BaselineSufficiency    string   `json:"baseline_sufficiency,omitempty"`
	CandidateSufficiency   string   `json:"candidate_sufficiency,omitempty"`
}

// ReliabilityDelta extends agent-version comparison with reliability
// deltas over the same exact compatible evidence. Positive pass-rate /
// consistency deltas favor the candidate. No improvement is claimed when
// samples are incompatible: callers must check Comparability first.
type ReliabilityDelta struct {
	Comparability        Comparability `json:"comparability"`
	PassRateDelta        *float64      `json:"pass_rate_delta,omitempty"`
	ValidTrialsDelta     int           `json:"valid_trials_delta"`
	PlatformInvalidDelta int           `json:"platform_invalid_delta"`
	KMetrics             []KDelta      `json:"k_metrics"`
	Families             []FamilyDelta `json:"families"`
}

// Compare builds the delta between baseline and candidate evidence sets.
// The scope selects task scope ("scenario:<id>") or family scope
// ("family:<id>"). Deltas are computed only when CheckComparability passes;
// otherwise the delta fields stay empty and Comparability explains why.
func Compare(policy Policy, scope string, baseline, candidate []Trial) ReliabilityDelta {
	out := ReliabilityDelta{
		Comparability: CheckComparability(PinsForTrials(scope, baseline), PinsForTrials(scope, candidate)),
	}
	if !out.Comparability.Comparable {
		return out
	}
	baseEmpirical := Summarize(baseline)
	candidateEmpirical := Summarize(candidate)
	out.ValidTrialsDelta = candidateEmpirical.ValidTrials - baseEmpirical.ValidTrials
	out.PlatformInvalidDelta = candidateEmpirical.PlatformInvalidTrials - baseEmpirical.PlatformInvalidTrials
	if baseEmpirical.PassRate != nil && candidateEmpirical.PassRate != nil {
		delta := *candidateEmpirical.PassRate - *baseEmpirical.PassRate
		out.PassRateDelta = &delta
	}
	baseK := KMetrics(baseline)
	candidateK := KMetrics(candidate)
	for _, k := range SupportedK {
		entry := KDelta{K: k}
		base, candidate := KLookup(baseK, k), KLookup(candidateK, k)
		if base != nil && candidate != nil && base.Available && candidate.Available {
			entry.Available = true
			at := *candidate.PassAtK - *base.PassAtK
			to := *candidate.PassToTheK - *base.PassToTheK
			entry.PassAtKDelta = &at
			entry.PassToTheKDelta = &to
		}
		out.KMetrics = append(out.KMetrics, entry)
	}
	baseFamilies := BuildFamilyReliabilities(policy, baseline)
	candidateFamilies := BuildFamilyReliabilities(policy, candidate)
	byBase := map[string]FamilyReliability{}
	for _, family := range baseFamilies {
		byBase[family.FamilyID] = family
	}
	byCandidate := map[string]FamilyReliability{}
	for _, family := range candidateFamilies {
		byCandidate[family.FamilyID] = family
	}
	ids := map[string]bool{}
	for id := range byBase {
		ids[id] = true
	}
	for id := range byCandidate {
		ids[id] = true
	}
	names := make([]string, 0, len(ids))
	for id := range ids {
		names = append(names, id)
	}
	sort.Strings(names)
	for _, id := range names {
		base, hasBase := byBase[id]
		candidate, hasCandidate := byCandidate[id]
		entry := FamilyDelta{FamilyID: id, Available: hasBase && hasCandidate}
		if hasBase {
			entry.BaselineSufficiency = base.Sufficiency
		}
		if hasCandidate {
			entry.CandidateSufficiency = candidate.Sufficiency
		}
		if entry.Available {
			entry.ValidTrialsDelta = candidate.ValidTrials - base.ValidTrials
			entry.DistinctScenariosDelta = candidate.DistinctScenarios - base.DistinctScenarios
			if base.PassRate != nil && candidate.PassRate != nil {
				delta := *candidate.PassRate - *base.PassRate
				entry.PassRateDelta = &delta
			}
		}
		out.Families = append(out.Families, entry)
	}
	if out.Families == nil {
		out.Families = []FamilyDelta{}
	}
	return out
}
