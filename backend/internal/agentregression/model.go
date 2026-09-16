package agentregression

import (
	"encoding/json"
	"time"
)

const MinimumComparablePairs = 3

const (
	ClassificationRegressed            = "regressed"
	ClassificationImproved             = "improved"
	ClassificationMixed                = "mixed"
	ClassificationNoDetectedRegression = "no_detected_regression"
	ClassificationInsufficientEvidence = "insufficient_evidence"
)

type Comparison struct {
	ID                      string                `json:"id"`
	WorkspaceID             string                `json:"workspace_id"`
	SuiteExecutionID        string                `json:"suite_execution_id"`
	SuiteVersionID          string                `json:"suite_version_id"`
	BaselineAgentVersionID  string                `json:"baseline_agent_version_id"`
	CandidateAgentVersionID string                `json:"candidate_agent_version_id"`
	Baseline                AgentVersionReference `json:"baseline"`
	Candidate               AgentVersionReference `json:"candidate"`
	CreatedBy               *string               `json:"created_by"`
	CreatedAt               time.Time             `json:"created_at"`
}

type AgentVersionReference struct {
	AgentID        string `json:"agent_id"`
	AgentName      string `json:"agent_name"`
	AgentVersionID string `json:"agent_version_id"`
	VersionLabel   string `json:"version_label"`
	ToolAccessMode string `json:"tool_access_mode"`
}

type CreateInput struct {
	SuiteVersionID          string `json:"suite_version_id"`
	BaselineAgentVersionID  string `json:"baseline_agent_version_id"`
	CandidateAgentVersionID string `json:"candidate_agent_version_id"`
}

type TrialRow struct {
	ScenarioVersionID string
	AgentVersionID    string
	RunID             string
	EvaluationID      string
	TrialOrdinal      int
	VariantID         string
	VariantSeed       int64
	RunStatus         string
	EvaluationResult  string
	EvaluatorVersion  string
	PlatformInvalid   bool
	SafetyCodes       []string
}

type CriterionRow struct {
	ScenarioVersionID string
	AgentVersionID    string
	TrialOrdinal      int
	VariantID         string
	CriterionKey      string
	Operator          string
	TargetValue       json.RawMessage
	Unit              *string
	Status            string
	ObservedState     string
	ObservedValue     json.RawMessage
}

type TrialSide struct {
	AgentVersionID   string   `json:"agent_version_id"`
	RunID            string   `json:"run_id,omitempty"`
	EvaluationID     string   `json:"evaluation_id,omitempty"`
	Validity         string   `json:"validity"`
	Result           string   `json:"result,omitempty"`
	EvaluatorVersion string   `json:"evaluator_version,omitempty"`
	SafetyCodes      []string `json:"safety_violation_codes"`
}

type TrialPair struct {
	ScenarioVersionID string    `json:"scenario_version_id"`
	TrialOrdinal      int       `json:"trial_ordinal"`
	VariantID         string    `json:"variant_id"`
	VariantSeed       int64     `json:"variant_seed"`
	Comparable        bool      `json:"comparable"`
	Incompatibility   string    `json:"incompatibility,omitempty"`
	Transition        string    `json:"transition,omitempty"`
	Baseline          TrialSide `json:"baseline"`
	Candidate         TrialSide `json:"candidate"`
}

type EvidenceQuality struct {
	ScheduledPairs        int `json:"scheduled_pairs"`
	ComparablePairs       int `json:"comparable_pairs"`
	InvalidBaselinePairs  int `json:"invalid_baseline_pairs"`
	InvalidCandidatePairs int `json:"invalid_candidate_pairs"`
	BothInvalidPairs      int `json:"both_invalid_pairs"`
	PendingPairs          int `json:"pending_pairs"`
	IncompatiblePairs     int `json:"incompatible_pairs"`
}

type OutcomeCounts struct {
	Passed       int      `json:"passed"`
	Failed       int      `json:"failed"`
	Inconclusive int      `json:"inconclusive"`
	PassRate     *float64 `json:"pass_rate"`
}

type TransitionCounts struct {
	Regressions             int `json:"regressions"`
	Improvements            int `json:"improvements"`
	Unchanged               int `json:"unchanged"`
	CandidateInconclusive   int `json:"candidate_inconclusive"`
	ResolvedInconclusive    int `json:"resolved_inconclusive"`
	InconclusiveTransitions int `json:"other_inconclusive_transitions"`
}

type SafetyCodeCount struct {
	Code  string `json:"code"`
	Count int    `json:"count"`
}

type SafetyComparison struct {
	New        []SafetyCodeCount `json:"new_violations"`
	Resolved   []SafetyCodeCount `json:"resolved_violations"`
	Persistent []SafetyCodeCount `json:"persistent_violations"`
}

type CriterionComparison struct {
	ScenarioVersionID string          `json:"scenario_version_id"`
	CriterionKey      string          `json:"criterion_key"`
	Operator          string          `json:"operator"`
	TargetValue       json.RawMessage `json:"target_value"`
	Unit              *string         `json:"unit,omitempty"`
	ComparedPairs     int             `json:"compared_pairs"`
	Regressions       int             `json:"regressions"`
	Improvements      int             `json:"improvements"`
	Unchanged         int             `json:"unchanged"`
	Inconclusive      int             `json:"inconclusive_transitions"`
	MetricDeltas      []MetricDelta   `json:"metric_deltas,omitempty"`
}

type MetricDelta struct {
	TrialOrdinal int     `json:"trial_ordinal"`
	Baseline     float64 `json:"baseline"`
	Candidate    float64 `json:"candidate"`
	Delta        float64 `json:"delta"`
	Movement     string  `json:"movement"`
}

type ScenarioBreakdown struct {
	ScenarioVersionID string           `json:"scenario_version_id"`
	EvidenceQuality   EvidenceQuality  `json:"evidence_quality"`
	Baseline          OutcomeCounts    `json:"baseline"`
	Candidate         OutcomeCounts    `json:"candidate"`
	Transitions       TransitionCounts `json:"transitions"`
}

type ReproducibilityManifest struct {
	PolicyVersionID             string `json:"policy_version_id,omitempty"`
	SuiteVersionID              string `json:"suite_version_id"`
	ScenarioVersionID           string `json:"scenario_version_id"`
	BaselineAgentVersionID      string `json:"baseline_agent_version_id"`
	CandidateAgentVersionID     string `json:"candidate_agent_version_id"`
	AgentVersionID              string `json:"agent_version_id,omitempty"`
	TrialOrdinal                int    `json:"trial_ordinal"`
	HiddenVariantID             string `json:"hidden_variant_id"`
	HiddenVariantSeed           int64  `json:"hidden_variant_seed"`
	HiddenVariantConfigIdentity string `json:"hidden_variant_config_identity"`
	EvaluatorVersion            string `json:"evaluator_version,omitempty"`
	CapabilityCatalogVersion    string `json:"capability_catalog_version"`
	// InfrastructureCapabilityCatalogVersion pins the semantic competency
	// catalog contract for capability claims (CLAIMED only).
	InfrastructureCapabilityCatalogVersion string `json:"infrastructure_capability_catalog_version,omitempty"`
	RuntimeContractVersion                 string `json:"runtime_contract_version"`
	MCPProtocolVersion                     string `json:"mcp_protocol_version,omitempty"`
	MCPToolContract                        string `json:"mcp_tool_contract,omitempty"`
}

type Results struct {
	Comparison           Comparison                `json:"comparison"`
	Classification       string                    `json:"classification"`
	InsufficientEvidence bool                      `json:"insufficient_evidence"`
	EvidenceQuality      EvidenceQuality           `json:"evidence_quality"`
	Baseline             OutcomeCounts             `json:"baseline"`
	Candidate            OutcomeCounts             `json:"candidate"`
	DeltaPassRate        *float64                  `json:"delta_pass_rate"`
	Transitions          TransitionCounts          `json:"transitions"`
	Safety               SafetyComparison          `json:"safety"`
	Scenarios            []ScenarioBreakdown       `json:"scenarios"`
	Criteria             []CriterionComparison     `json:"criteria"`
	Pairs                []TrialPair               `json:"pairs"`
	Reproducibility      []ReproducibilityManifest `json:"reproducibility_manifests"`
}
