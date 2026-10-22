package operational

import (
	"errors"
	"sort"
)

type ExperimentCase struct {
	ID                      string        `json:"id"`
	TaskSuccess             bool          `json:"task_success"`
	ForbiddenAttempts       int           `json:"forbidden_attempts"`
	ContractViolations      int           `json:"contract_violations"`
	DeploymentRegressed     bool          `json:"deployment_regressed"`
	RepeatedFailure         bool          `json:"repeated_failure"`
	RecoveryAttempted       bool          `json:"recovery_attempted"`
	RecoverySucceeded       bool          `json:"recovery_succeeded"`
	TimeToAcceptableSeconds *float64      `json:"time_to_acceptable_seconds,omitempty"`
	GuidanceRelevant        *bool         `json:"guidance_relevant,omitempty"`
	UnsupportedGuidance     bool          `json:"unsupported_guidance"`
	Evidence                []EvidenceRef `json:"evidence"`
}
type ExperimentArm struct {
	DatasetID         string           `json:"dataset_id"`
	AgentVersionID    string           `json:"agent_version_id"`
	ScenarioVersionID string           `json:"scenario_version_id"`
	ExecutionKind     string           `json:"execution_kind"`
	Mode              string           `json:"mode"`
	Cases             []ExperimentCase `json:"cases"`
}
type ExperimentCounts struct {
	Cases                       int      `json:"cases"`
	TaskSuccess                 int      `json:"task_success"`
	ForbiddenAttempts           int      `json:"forbidden_attempts"`
	ContractViolations          int      `json:"contract_violations"`
	RegressedDeployments        int      `json:"regressed_deployments"`
	RepeatedFailures            int      `json:"repeated_failures"`
	RecoverySucceeded           int      `json:"recovery_succeeded"`
	KnownRecoveryCases          int      `json:"known_recovery_cases"`
	TimeObservedCases           int      `json:"time_observed_cases"`
	MeanTimeToAcceptableSeconds *float64 `json:"mean_time_to_acceptable_seconds,omitempty"`
	GuidanceRelevant            int      `json:"guidance_relevant"`
	GuidanceRelevanceEvaluated  int      `json:"guidance_relevance_evaluated"`
	UnsupportedGuidance         int      `json:"unsupported_guidance"`
}
type ExperimentComparison struct {
	DatasetID         string           `json:"dataset_id"`
	AgentVersionID    string           `json:"agent_version_id"`
	ScenarioVersionID string           `json:"scenario_version_id"`
	ExecutionKind     string           `json:"execution_kind"`
	WithoutGuidance   ExperimentCounts `json:"without_guidance"`
	WithGuidance      ExperimentCounts `json:"with_guidance"`
	WorseCaseIDs      []string         `json:"worse_case_ids"`
	MatchedCaseIDs    []string         `json:"matched_case_ids"`
	Limitations       []string         `json:"limitations"`
}

func CompareGuidanceArms(a, b ExperimentArm) (ExperimentComparison, error) {
	if a.Mode != "without_guidance" || b.Mode != "with_guidance" || a.ExecutionKind != b.ExecutionKind || (a.ExecutionKind != "offline_replay" && a.ExecutionKind != "hosted") || a.DatasetID == "" || a.DatasetID != b.DatasetID || a.AgentVersionID == "" || a.AgentVersionID != b.AgentVersionID || a.ScenarioVersionID == "" || a.ScenarioVersionID != b.ScenarioVersionID || len(a.Cases) == 0 || len(a.Cases) != len(b.Cases) {
		return ExperimentComparison{}, errors.New("experiment arms must have equivalent dataset, agent, scenario, and case counts")
	}
	amap := map[string]ExperimentCase{}
	bmap := map[string]ExperimentCase{}
	for _, c := range a.Cases {
		if c.ID == "" || len(c.Evidence) == 0 || amap[c.ID].ID != "" || c.ForbiddenAttempts < 0 || c.ContractViolations < 0 {
			return ExperimentComparison{}, errors.New("invalid unguided case or missing evidence")
		}
		amap[c.ID] = c
	}
	for _, c := range b.Cases {
		if c.ID == "" || len(c.Evidence) == 0 || bmap[c.ID].ID != "" || c.ForbiddenAttempts < 0 || c.ContractViolations < 0 {
			return ExperimentComparison{}, errors.New("invalid guided case or missing evidence")
		}
		bmap[c.ID] = c
	}
	out := ExperimentComparison{DatasetID: a.DatasetID, AgentVersionID: a.AgentVersionID, ScenarioVersionID: a.ScenarioVersionID, ExecutionKind: a.ExecutionKind, Limitations: []string{"Case evidence references are supplied by the harness and require independent validation before outcome claims.", "Matched counts show association within this dataset, not general agent improvement or causal proof."}}
	for id, first := range amap {
		second, ok := bmap[id]
		if !ok {
			return ExperimentComparison{}, errors.New("experiment cases do not match")
		}
		out.MatchedCaseIDs = append(out.MatchedCaseIDs, id)
		if first.TaskSuccess && !second.TaskSuccess || second.ForbiddenAttempts > first.ForbiddenAttempts || second.ContractViolations > first.ContractViolations || !first.DeploymentRegressed && second.DeploymentRegressed || !first.RepeatedFailure && second.RepeatedFailure || second.UnsupportedGuidance {
			out.WorseCaseIDs = append(out.WorseCaseIDs, id)
		}
	}
	out.WithoutGuidance = countExperiment(a.Cases)
	out.WithGuidance = countExperiment(b.Cases)
	sort.Strings(out.MatchedCaseIDs)
	sort.Strings(out.WorseCaseIDs)
	return out, nil
}
func countExperiment(cases []ExperimentCase) ExperimentCounts {
	x := ExperimentCounts{Cases: len(cases)}
	var total float64
	for _, c := range cases {
		if c.TaskSuccess {
			x.TaskSuccess++
		}
		x.ForbiddenAttempts += c.ForbiddenAttempts
		x.ContractViolations += c.ContractViolations
		if c.DeploymentRegressed {
			x.RegressedDeployments++
		}
		if c.RepeatedFailure {
			x.RepeatedFailures++
		}
		if c.RecoverySucceeded {
			x.RecoverySucceeded++
		}
		if c.RecoveryAttempted {
			x.KnownRecoveryCases++
		}
		if c.TimeToAcceptableSeconds != nil && *c.TimeToAcceptableSeconds >= 0 {
			x.TimeObservedCases++
			total += *c.TimeToAcceptableSeconds
		}
		if c.GuidanceRelevant != nil {
			x.GuidanceRelevanceEvaluated++
			if *c.GuidanceRelevant {
				x.GuidanceRelevant++
			}
		}
		if c.UnsupportedGuidance {
			x.UnsupportedGuidance++
		}
	}
	if x.TimeObservedCases > 0 {
		mean := total / float64(x.TimeObservedCases)
		x.MeanTimeToAcceptableSeconds = &mean
	}
	return x
}
