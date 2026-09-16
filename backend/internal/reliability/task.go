package reliability

import "sort"

// TaskPins pins the exact execution contracts one task reliability reading
// was computed under. Trials from materially incompatible contracts must
// never be merged; comparability checks enforce this.
type TaskPins struct {
	ScenarioVersionID       string   `json:"scenario_version_id"`
	AgentVersionID          string   `json:"agent_version_id"`
	FamilyID                string   `json:"family_id,omitempty"`
	CaseType                string   `json:"case_type,omitempty"`
	ReliabilityContract     string   `json:"reliability_contract_version"`
	ReliabilityPolicy       string   `json:"reliability_policy_version"`
	EvaluatorContracts      []string `json:"evaluator_contract_versions"`
	RuntimeContract         string   `json:"runtime_contract_version"`
	EnvironmentRefs         []string `json:"environment_refs,omitempty"`
	EnvironmentProfiles     []string `json:"environment_profiles,omitempty"`
	ProviderIDs             []string `json:"provider_ids,omitempty"`
	IsolationTiers          []string `json:"isolation_tiers,omitempty"`
	DistinctSuiteExecutions int      `json:"distinct_suite_executions"`
}

// TaskReliability is the task-level reliability reading for one exact
// ScenarioVersion (and one AgentVersion, or one entry of a per-agent list).
type TaskReliability struct {
	Pins                    TaskPins       `json:"pins"`
	Empirical               Empirical      `json:"empirical"`
	KMetrics                []KMetric      `json:"k_metrics"`
	MaxConsecutivePasses    int            `json:"max_consecutive_passes"`
	LatestConsecutivePasses int            `json:"latest_consecutive_passes"`
	Diversity               Diversity      `json:"variant_diversity"`
	Sufficiency             string         `json:"sample_sufficiency"`
	OutcomeDistribution     map[string]int `json:"outcome_distribution"`
	FailureBreakdown        map[string]int `json:"failure_breakdown"`
}

// BuildTaskReliability computes the task-level reading over trials already
// scoped to one ScenarioVersion (optionally one agent). Family/case pins
// come from the explicit ScenarioVersion assignment. eligibleVariants may be
// nil when no legitimate variant denominator exists.
func BuildTaskReliability(policy Policy, scenarioVersionID, agentVersionID string, trials []Trial, eligibleVariants *int) TaskReliability {
	empirical := Summarize(trials)
	metrics := KMetrics(trials)
	maxCons, latestCons := ConsecutiveSuccess(trials)
	out := TaskReliability{
		Empirical:               empirical,
		KMetrics:                metrics,
		MaxConsecutivePasses:    maxCons,
		LatestConsecutivePasses: latestCons,
		Diversity:               DescribeDiversity(trials, eligibleVariants),
		Sufficiency:             AssessSufficiency(empirical.ValidTrials, policy.MinValidTrialsScenario, policy.TargetValidTrialsScenario),
		OutcomeDistribution:     map[string]int{},
		FailureBreakdown:        map[string]int{},
	}
	pins := TaskPins{
		ScenarioVersionID:   scenarioVersionID,
		AgentVersionID:      agentVersionID,
		ReliabilityContract: ReliabilityContractVersion,
		ReliabilityPolicy:   policy.ContractVersion,
		RuntimeContract:     RuntimeContractVersion,
	}
	evaluators := map[string]bool{}
	envs := map[string]bool{}
	profiles := map[string]bool{}
	providers := map[string]bool{}
	isolations := map[string]bool{}
	executions := map[string]bool{}
	for _, trial := range trials {
		if pins.FamilyID == "" && trial.FamilyID != "" {
			pins.FamilyID = trial.FamilyID
		}
		if pins.CaseType == "" && trial.CaseType != "" {
			pins.CaseType = trial.CaseType
		}
		if trial.EvaluatorVersion != "" {
			evaluators[trial.EvaluatorVersion] = true
		}
		if trial.EnvironmentRef != "" {
			envs[trial.EnvironmentRef] = true
		}
		if trial.EnvironmentProfileID != "" {
			profiles[trial.EnvironmentProfileID] = true
		}
		if trial.ProviderID != "" {
			providers[trial.ProviderID] = true
		}
		if trial.IsolationTier != "" {
			isolations[trial.IsolationTier] = true
		}
		if trial.SuiteExecutionID != "" {
			executions[trial.SuiteExecutionID] = true
		}
		class := trial.OutcomeClass()
		out.OutcomeDistribution[class]++
		// Failure breakdown reuses the deterministic outcome vocabulary:
		// valid non-pass outcomes by class, never a competing taxonomy.
		if trial.IsCountedValid() && trial.Result != ResultPass {
			out.FailureBreakdown[trial.Result]++
		}
		if trial.SafetyViolations > 0 {
			out.FailureBreakdown["trials_with_safety_violations"]++
		}
	}
	pins.EvaluatorContracts = sortedKeys(evaluators)
	pins.EnvironmentRefs = sortedKeys(envs)
	pins.EnvironmentProfiles = sortedKeys(profiles)
	pins.ProviderIDs = sortedKeys(providers)
	pins.IsolationTiers = sortedKeys(isolations)
	pins.DistinctSuiteExecutions = len(executions)
	out.Pins = pins
	if out.OutcomeDistribution == nil {
		out.OutcomeDistribution = map[string]int{}
	}
	if out.FailureBreakdown == nil {
		out.FailureBreakdown = map[string]int{}
	}
	return out
}

func sortedKeys(set map[string]bool) []string {
	out := make([]string, 0, len(set))
	for value := range set {
		out = append(out, value)
	}
	sort.Strings(out)
	return out
}
