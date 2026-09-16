package reliability

import "sort"

// Saturation measures whether repeated evaluation on the same task still
// adds evidence. It is evaluator/catalog-maintenance evidence: it never
// retires a task automatically, and it is never defined by pass rate alone.
// A hard task can be saturated (stable failures); an easy task can be
// developing (thin variant breadth).
type Saturation struct {
	ScenarioVersionID string `json:"scenario_version_id"`
	State             string `json:"state"`
	// Reasons carries short machine-readable causes for the state.
	Reasons    []string `json:"reasons"`
	Executions int      `json:"executions"`
	// DistinctAgentVersions counts agent versions evaluated on this task.
	DistinctAgentVersions int            `json:"distinct_agent_versions"`
	DistinctVariants      int            `json:"distinct_variants"`
	OutcomeDistribution   map[string]int `json:"outcome_distribution"`
	// RecentOutcomeStability is stable | unstable | insufficient over the
	// trailing window of valid trials in persisted order.
	RecentOutcomeStability string `json:"recent_outcome_stability"`
	// FamilyAlternatives counts other ScenarioVersions in the same Task
	// Family with persisted evidence: places to gather fresh evidence
	// instead of repeating this task.
	FamilyAlternatives  int    `json:"family_alternatives"`
	ReliabilityContract string `json:"reliability_contract_version"`
}

// Saturation thresholds (deterministic, versioned by the reliability
// contract, not the policy: saturation describes the catalog, not a
// qualification sample target).
const (
	saturationMinValid        = 10
	saturationMinVariants     = 2
	saturationMinExecutions   = 2
	saturationStabilityWindow = 5
	freshMaxValid             = 3
)

// BuildSaturation computes task saturation. trials are scoped to one
// ScenarioVersion across executions/agents; familyAlternatives counts other
// evidenced ScenarioVersions in the same family (0 when the family is
// unknown, which never blocks saturation but is reported).
func BuildSaturation(scenarioVersionID string, trials []Trial, familyAlternatives int) Saturation {
	valid := ValidTrials(trials)
	out := Saturation{
		ScenarioVersionID:   scenarioVersionID,
		FamilyAlternatives:  familyAlternatives,
		OutcomeDistribution: map[string]int{},
		ReliabilityContract: ReliabilityContractVersion,
	}
	executions := map[string]bool{}
	agents := map[string]bool{}
	variants := map[string]bool{}
	for _, trial := range trials {
		if trial.SuiteExecutionID != "" {
			executions[trial.SuiteExecutionID] = true
		}
		if trial.AgentVersionID != "" {
			agents[trial.AgentVersionID] = true
		}
	}
	for _, trial := range valid {
		out.OutcomeDistribution[trial.OutcomeClass()]++
		if trial.VariantID != "" {
			variants[trial.VariantID] = true
		}
	}
	out.Executions = len(executions)
	out.DistinctAgentVersions = len(agents)
	out.DistinctVariants = len(variants)
	out.RecentOutcomeStability = recentStability(valid)
	switch {
	case len(valid) < freshMaxValid:
		out.State = SaturationFresh
		out.Reasons = []string{"few_valid_trials"}
	case len(valid) >= saturationMinValid &&
		len(variants) >= saturationMinVariants &&
		len(executions) >= saturationMinExecutions &&
		out.RecentOutcomeStability == "stable":
		out.State = SaturationSaturated
		out.Reasons = []string{"ample_repetition", "variant_breadth", "multi_execution", "recent_outcomes_stable"}
	default:
		out.State = SaturationDeveloping
		out.Reasons = developingReasons(len(valid), len(variants), len(executions), out.RecentOutcomeStability)
	}
	return out
}

func developingReasons(valid, variants, executions int, stability string) []string {
	reasons := []string{}
	if valid < saturationMinValid {
		reasons = append(reasons, "needs_more_repetition")
	}
	if variants < saturationMinVariants {
		reasons = append(reasons, "needs_variant_breadth")
	}
	if executions < saturationMinExecutions {
		reasons = append(reasons, "needs_multi_execution")
	}
	if stability == "unstable" {
		reasons = append(reasons, "recent_outcomes_unstable")
	}
	if stability == "insufficient" {
		reasons = append(reasons, "recent_history_thin")
	}
	sort.Strings(reasons)
	return reasons
}

// recentStability compares outcome classes over the trailing window of valid
// trials in persisted (Ordinal, RunID) order. Stable means every trial in
// the window shares one outcome class; the window must be full
// (saturationStabilityWindow) for stable, otherwise insufficient when fewer
// than 3 valid trials exist, else unstable-vs-stable by unanimity.
func recentStability(valid []Trial) string {
	if len(valid) < 3 {
		return "insufficient"
	}
	ordered := append([]Trial(nil), valid...)
	sort.Slice(ordered, func(i, j int) bool {
		if ordered[i].Ordinal != ordered[j].Ordinal {
			return ordered[i].Ordinal < ordered[j].Ordinal
		}
		return ordered[i].RunID < ordered[j].RunID
	})
	window := saturationStabilityWindow
	if len(ordered) < window {
		window = len(ordered)
	}
	first := ordered[len(ordered)-window].OutcomeClass()
	for _, trial := range ordered[len(ordered)-window+1:] {
		if trial.OutcomeClass() != first {
			return "unstable"
		}
	}
	if len(ordered) < saturationStabilityWindow {
		return "insufficient"
	}
	return "stable"
}
