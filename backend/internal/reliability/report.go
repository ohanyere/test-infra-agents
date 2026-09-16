package reliability

// ScopedSummary is the shared reliability reading attached at every scope:
// empirical metrics, supported-k estimates, sufficiency, and the separate
// platform-invalid signal.
type ScopedSummary struct {
	Empirical   Empirical `json:"empirical"`
	KMetrics    []KMetric `json:"k_metrics"`
	Sufficiency string    `json:"sample_sufficiency"`
	// ConsistencyK3 is a convenience pointer to the k=3 pass^k estimate
	// (nil when unavailable). Infrastructure qualification treats
	// consistency as the primary reliability signal.
	ConsistencyK3 *float64 `json:"consistency_k3,omitempty"`
}

// SummarizeScope builds a ScopedSummary with explicit sufficiency
// thresholds (task-level callers pass the scenario thresholds, broader
// scopes pass the family thresholds).
func SummarizeScope(_ Policy, trials []Trial, minimum, target int) ScopedSummary {
	empirical := Summarize(trials)
	out := ScopedSummary{
		Empirical:   empirical,
		KMetrics:    KMetrics(trials),
		Sufficiency: AssessSufficiency(empirical.ValidTrials, minimum, target),
	}
	if metric := KLookup(out.KMetrics, 3); metric != nil && metric.Available {
		value := *metric.PassToTheK
		out.ConsistencyK3 = &value
	}
	return out
}

// ExecutionAgentEntry is one agent's reading inside an execution report.
type ExecutionAgentEntry struct {
	AgentVersionID string                  `json:"agent_version_id"`
	Summary        ScopedSummary           `json:"summary"`
	Tasks          []TaskReliability       `json:"tasks"`
	Families       []FamilyReliability     `json:"families"`
	Capabilities   []CapabilityReliability `json:"capabilities"`
	// CoverageContract pins the coverage contract view the capability
	// section aggregates under (current qualification coverage contract).
	CoverageContract string `json:"coverage_contract_version"`
}

// ExecutionReport is the SuiteExecution-scope reliability report. Overall is
// the execution aggregate; ByAgent carries per-agent task/family/capability
// readings. Capability readings use each agent version's claimed
// capabilities; agents without claims report an empty list (never inferred).
type ExecutionReport struct {
	SuiteExecutionID    string                `json:"suite_execution_id"`
	SuiteVersionID      string                `json:"suite_version_id"`
	ReliabilityContract string                `json:"reliability_contract_version"`
	ReliabilityPolicy   Policy                `json:"reliability_policy"`
	CoverageContract    string                `json:"coverage_contract_version"`
	Overall             ScopedSummary         `json:"overall"`
	ByAgent             []ExecutionAgentEntry `json:"by_agent"`
	Saturation          []Saturation          `json:"task_saturation"`
}

// AgentExecutionEntry summarizes one suite execution inside an agent report
// (valid vs invalid, pass rate, pass@k/pass^k, sufficiency, family breadth)
// without duplicating full task detail.
type AgentExecutionEntry struct {
	SuiteExecutionID      string        `json:"suite_execution_id"`
	Summary               ScopedSummary `json:"summary"`
	DistinctScenarios     int           `json:"distinct_scenarios"`
	DistinctFamilies      int           `json:"distinct_families"`
	DistinctVariants      int           `json:"distinct_variants"`
	PlatformInvalidTrials int           `json:"platform_invalid_trials"`
}

// AgentReport is the AgentVersion-scope reliability report.
type AgentReport struct {
	AgentID             string                  `json:"agent_id"`
	AgentVersionID      string                  `json:"agent_version_id"`
	ReliabilityContract string                  `json:"reliability_contract_version"`
	ReliabilityPolicy   Policy                  `json:"reliability_policy"`
	CoverageContract    string                  `json:"coverage_contract_version"`
	Summary             ScopedSummary           `json:"summary"`
	Tasks               []TaskReliability       `json:"tasks"`
	Families            []FamilyReliability     `json:"families"`
	Capabilities        []CapabilityReliability `json:"capabilities"`
	Executions          []AgentExecutionEntry   `json:"executions"`
}

// ScenarioReport is the ScenarioVersion-scope reliability report. Tasks
// carries one entry per agent version with evidence (plus the aggregate when
// an explicit agent filter is absent); Saturation is the catalog-maintenance
// signal for the task.
type ScenarioReport struct {
	ScenarioID          string            `json:"scenario_id"`
	ScenarioVersionID   string            `json:"scenario_version_id"`
	ReliabilityContract string            `json:"reliability_contract_version"`
	ReliabilityPolicy   Policy            `json:"reliability_policy"`
	CoverageContract    string            `json:"coverage_contract_version"`
	AgentVersionID      string            `json:"agent_version_id,omitempty"`
	Tasks               []TaskReliability `json:"tasks"`
	Saturation          Saturation        `json:"saturation"`
}

// ComparisonReport pairs two agent versions' reliability over the same
// exact compatible evidence.
type ComparisonReport struct {
	Scope                   string           `json:"scope"`
	BaselineAgentVersionID  string           `json:"baseline_agent_version_id"`
	CandidateAgentVersionID string           `json:"candidate_agent_version_id"`
	Delta                   ReliabilityDelta `json:"delta"`
	ReliabilityContract     string           `json:"reliability_contract_version"`
}
