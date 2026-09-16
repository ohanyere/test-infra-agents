package reliability

// Policy is the versioned reliability sample policy. Thresholds are modest,
// transparent, and deterministic. Reliability requirements are separate from
// breadth (coverage-contract) requirements: this policy governs repetition
// depth, never family breadth.
type Policy struct {
	// ContractVersion pins reliability-policy:v1 on every report.
	ContractVersion string `json:"contract_version"`
	// MinValidTrialsScenario is the minimum valid-trial sample for one exact
	// ScenarioVersion before any k>1 metric or sufficiency beyond
	// insufficient is meaningful.
	MinValidTrialsScenario int `json:"min_valid_trials_per_scenario_version"`
	// TargetValidTrialsScenario is the stronger consistency sample target
	// for one ScenarioVersion (sufficient at or above).
	TargetValidTrialsScenario int `json:"target_valid_trials_per_scenario_version"`
	// MinValidTrialsFamily is the minimum valid-trial sample for one Task
	// Family before family reliability leaves insufficient.
	MinValidTrialsFamily int `json:"min_valid_trials_per_family"`
	// TargetValidTrialsFamily is the stronger sample target for one family.
	TargetValidTrialsFamily int `json:"target_valid_trials_per_family"`
	// MinDistinctVariants is the minimum distinct hidden variants exercised
	// before task evidence counts as diverse.
	MinDistinctVariants int `json:"min_distinct_variants"`
	// MinDistinctScenariosPerFamily is the minimum distinct ScenarioVersions
	// before family evidence counts as broad (retries of one scenario never
	// satisfy this).
	MinDistinctScenariosPerFamily int `json:"min_distinct_scenarios_per_family"`
	// MinContrastTrials is the minimum valid negative/contrast trials
	// reported alongside capability reliability where contrast evidence is
	// required. The reliability package reports the count; policy gating
	// stays with QualificationPolicy.
	MinContrastTrials int `json:"min_contrast_trials"`
}

// DefaultPolicy returns the conservative V1 defaults.
func DefaultPolicy() Policy {
	return Policy{
		ContractVersion:               ReliabilityPolicyVersion,
		MinValidTrialsScenario:        3,
		TargetValidTrialsScenario:     5,
		MinValidTrialsFamily:          6,
		TargetValidTrialsFamily:       10,
		MinDistinctVariants:           2,
		MinDistinctScenariosPerFamily: 2,
		MinContrastTrials:             1,
	}
}

// AssessSufficiency maps a valid-trial count to insufficient / provisional /
// sufficient against an explicit minimum and target. No statistical
// significance is claimed; these are deterministic evidence-depth states.
func AssessSufficiency(validTrials, minimum, target int) string {
	if validTrials < minimum {
		return SufficiencyInsufficient
	}
	if validTrials < target {
		return SufficiencyProvisional
	}
	return SufficiencySufficient
}
