package evaluator

import (
	"encoding/json"
	"errors"

	"github.com/evalplane/evalplane/backend/internal/evaluationbatch"
	"github.com/evalplane/evalplane/backend/internal/evidence"
	"github.com/evalplane/evalplane/backend/internal/scenario"
)

const (
	ContractVersion = "deterministic-evaluator:v1"

	StatusPass         = "pass"
	StatusFail         = "fail"
	StatusInconclusive = "inconclusive"

	CriterionPassed       = "passed"
	CriterionFailed       = "failed"
	CriterionInconclusive = "inconclusive"

	ValuePresent     = "present"
	ValueUnavailable = "unavailable"
	ValueInvalid     = "invalid"
)

var ErrRunNotTerminal = errors.New("run is not terminal")

type EvaluationInput struct {
	Run             evaluationbatch.Run
	ScenarioVersion scenario.Version
	Events          []evidence.RunEvent
	Actions         []evidence.AgentAction
}

type MetricValue struct {
	State       string   `json:"state"`
	Value       *float64 `json:"value,omitempty"`
	Unit        string   `json:"unit,omitempty"`
	EvidenceIDs []string `json:"evidence_ids,omitempty"`
	Reason      string   `json:"reason,omitempty"`
}

type CriterionResult struct {
	ScenarioCriterionID string          `json:"scenario_criterion_id"`
	CriterionKey        string          `json:"criterion_key"`
	MetricKey           string          `json:"metric_key"`
	Operator            string          `json:"operator"`
	TargetValue         json.RawMessage `json:"target_value"`
	Unit                *string         `json:"unit,omitempty"`
	ObservedValue       json.RawMessage `json:"observed_value"`
	ObservedState       string          `json:"observed_state"`
	Status              string          `json:"status"`
	Reason              string          `json:"reason"`
	EvidenceIDs         []string        `json:"evidence_ids,omitempty"`
}

type Finding struct {
	Code               string   `json:"code"`
	Severity           string   `json:"severity"`
	Message            string   `json:"message"`
	CriterionKey       string   `json:"criterion_key,omitempty"`
	EvidenceReferences []string `json:"evidence_references,omitempty"`
}

type EvaluationResult struct {
	RunID                  string            `json:"run_id"`
	ScenarioVersionID      string            `json:"scenario_version_id"`
	Status                 string            `json:"status"`
	CriterionResults       []CriterionResult `json:"criterion_results"`
	Findings               []Finding         `json:"findings"`
	SummaryFacts           []string          `json:"summary_facts"`
	AvailabilityPct        *float64          `json:"availability_pct,omitempty"`
	PeakErrorRatePct       *float64          `json:"peak_error_rate_pct,omitempty"`
	SafetyViolationCount   int               `json:"safety_violation_count"`
	SecondaryIncidentCount int               `json:"secondary_incident_count"`
	AgentActionCount       int               `json:"agent_action_count"`
}

type evaluationEvidence struct {
	Metrics         map[string]MetricValue
	Actions         []evidence.AgentAction
	Events          []evidence.RunEvent
	RuntimeFailure  bool
	RuntimeEvidence []string
}
