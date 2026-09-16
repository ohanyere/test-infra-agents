package reliability

import "sort"

// Pins is the comparability identity of one reliability evidence set.
//
// Environment fidelity is semantic, never the physical instance ref:
// profile ID + profile contract, provider ID + provider contract,
// isolation tier, runtime contract, and evaluator contract must match.
// EnvironmentRefs is retained as provenance only and never decides
// comparability alone.
type Pins struct {
	// Scope is either "scenario:<scenario-version-id>" or
	// "family:<family-id>".
	Scope string
	// ScenarioVersionIDs is the exact task set (sorted).
	ScenarioVersionIDs []string
	// FamilyID is set for family scope.
	FamilyID string
	// EvaluatorContracts is the distinct set of evaluator versions observed.
	EvaluatorContracts []string
	RuntimeContract    string
	// EnvironmentRefs is the distinct set of opaque run environment
	// references observed (provenance only, never the fidelity identity).
	EnvironmentRefs []string
	// Semantic environment fidelity pins (distinct sorted sets).
	EnvironmentProfiles         []string
	EnvironmentProfileContracts []string
	ProviderIDs                 []string
	ProviderContracts           []string
	IsolationTiers              []string
	ReliabilityContract         string
	TrialDistribution           []string
}

// HasSemanticEnvironment reports whether the pin set carries sufficient
// semantic environment metadata to judge fidelity. Empty semantic sets
// mean the persisted evidence predates instance linkage; comparability
// must then report a limitation, never infer fidelity from opaque refs.
func (p Pins) HasSemanticEnvironment() bool {
	return len(p.EnvironmentProfiles) > 0 &&
		len(p.EnvironmentProfileContracts) > 0 &&
		len(p.ProviderIDs) > 0 &&
		len(p.ProviderContracts) > 0 &&
		len(p.IsolationTiers) > 0
}

// Comparability is the structured verdict for a reliability comparison.
type Comparability struct {
	Comparable bool     `json:"comparable"`
	Reasons    []string `json:"not_comparable_reasons"`
}

// PinsForTrials derives comparability pins from a trial set.
func PinsForTrials(scope string, trials []Trial) Pins {
	pins := Pins{Scope: scope, ReliabilityContract: ReliabilityContractVersion, RuntimeContract: RuntimeContractVersion}
	scenarios := map[string]bool{}
	evaluators := map[string]bool{}
	envs := map[string]bool{}
	profiles := map[string]bool{}
	profileContracts := map[string]bool{}
	providers := map[string]bool{}
	providerContracts := map[string]bool{}
	isolations := map[string]bool{}
	runtimes := map[string]bool{}
	distribution := map[string]bool{}
	for _, trial := range trials {
		scenarios[trial.ScenarioVersionID] = true
		if trial.EvaluatorVersion != "" {
			evaluators[trial.EvaluatorVersion] = true
		}
		if trial.EnvironmentRef != "" {
			envs[trial.EnvironmentRef] = true
		}
		if trial.EnvironmentProfileID != "" {
			profiles[trial.EnvironmentProfileID] = true
		}
		if trial.EnvironmentProfileContract != "" {
			profileContracts[trial.EnvironmentProfileContract] = true
		}
		if trial.ProviderID != "" {
			providers[trial.ProviderID] = true
		}
		if trial.ProviderContract != "" {
			providerContracts[trial.ProviderContract] = true
		}
		if trial.IsolationTier != "" {
			isolations[trial.IsolationTier] = true
		}
		if trial.RuntimeContractVersion != "" {
			runtimes[trial.RuntimeContractVersion] = true
		}
		if trial.ScenarioVersionID != "" {
			distribution[trial.ScenarioVersionID+"\x00"+itoa(trial.Ordinal)] = true
		}
	}
	pins.ScenarioVersionIDs = sortedKeys(scenarios)
	pins.EvaluatorContracts = sortedKeys(evaluators)
	pins.EnvironmentRefs = sortedKeys(envs)
	pins.EnvironmentProfiles = sortedKeys(profiles)
	pins.EnvironmentProfileContracts = sortedKeys(profileContracts)
	pins.ProviderIDs = sortedKeys(providers)
	pins.ProviderContracts = sortedKeys(providerContracts)
	pins.IsolationTiers = sortedKeys(isolations)
	// RuntimeContract stays a single deterministic pin for backward
	// compatibility: the sole observed runtime, or the default when the
	// evidence carries none. Mixed runtimes still fail comparability via
	// the explicit set check inside CheckComparability through the single
	// pin only when unambiguous; mixed sets collapse to the sorted-first
	// value and therefore mismatch unless both sides mix identically.
	// To keep the rule exact, prefer the single value only when the set
	// has exactly one entry.
	if len(runtimes) == 1 {
		for value := range runtimes {
			pins.RuntimeContract = value
		}
	} else if len(runtimes) > 1 {
		keys := sortedKeys(runtimes)
		pins.RuntimeContract = keys[0] + "+mixed"
	}
	for key := range distribution {
		pins.TrialDistribution = append(pins.TrialDistribution, key)
	}
	sort.Strings(pins.TrialDistribution)
	return pins
}

// CheckComparability requires compatible pins before any delta is claimed.
// Incompatible evidence yields structured not_comparable reasons, never a
// silent merge.
func CheckComparability(base, candidate Pins) Comparability {
	out := Comparability{Comparable: true}
	fail := func(reason string) {
		out.Comparable = false
		out.Reasons = append(out.Reasons, reason)
	}
	if base.Scope != candidate.Scope {
		if isScenarioScope(base.Scope) || isScenarioScope(candidate.Scope) {
			fail(NotComparableScenarioScope)
		} else {
			fail(NotComparableFamilyScope)
		}
	}
	if !sameStrings(base.ScenarioVersionIDs, candidate.ScenarioVersionIDs) {
		if isScenarioScope(base.Scope) && isScenarioScope(candidate.Scope) {
			fail(NotComparableScenarioScope)
		} else {
			fail(NotComparableFamilyScope)
		}
	}
	if !sameStrings(base.EvaluatorContracts, candidate.EvaluatorContracts) {
		fail(NotComparableEvaluatorContract)
	}
	if base.RuntimeContract != candidate.RuntimeContract {
		fail(NotComparableRuntimeContract)
	}
	if base.ReliabilityContract != candidate.ReliabilityContract {
		fail(NotComparableReliability)
	}
	// Semantic environment fidelity: different instance refs are
	// comparable when profile/provider/isolation/runtime semantics match.
	// Missing semantic metadata on either side is a structured
	// limitation, never inferred equality from opaque refs. Isolation
	// distinctions (namespace vs virtual_cluster vs dedicated_cluster)
	// are never weakened: any tier mismatch fails.
	if !base.HasSemanticEnvironment() || !candidate.HasSemanticEnvironment() {
		fail(NotComparableEnvironmentUnknown)
	} else {
		if !sameStrings(base.EnvironmentProfiles, candidate.EnvironmentProfiles) {
			fail(NotComparableEnvironment)
		}
		if !sameStrings(base.EnvironmentProfileContracts, candidate.EnvironmentProfileContracts) {
			fail(NotComparableEnvironment)
		}
		if !sameStrings(base.ProviderIDs, candidate.ProviderIDs) {
			fail(NotComparableEnvironment)
		}
		if !sameStrings(base.ProviderContracts, candidate.ProviderContracts) {
			fail(NotComparableEnvironment)
		}
		if !sameStrings(base.IsolationTiers, candidate.IsolationTiers) {
			fail(NotComparableEnvironment)
		}
	}
	if !sameStrings(base.TrialDistribution, candidate.TrialDistribution) {
		fail(NotComparableTrialDistribution)
	}
	sort.Strings(out.Reasons)
	return out
}

func isScenarioScope(scope string) bool {
	return len(scope) > 9 && scope[:9] == "scenario:"
}

func sameStrings(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func itoa(value int) string {
	if value == 0 {
		return "0"
	}
	negative := value < 0
	if negative {
		value = -value
	}
	digits := []byte{}
	for value > 0 {
		digits = append([]byte{byte('0' + value%10)}, digits...)
		value /= 10
	}
	if negative {
		digits = append([]byte{'-'}, digits...)
	}
	return string(digits)
}
