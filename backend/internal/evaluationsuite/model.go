package evaluationsuite

import (
	"encoding/json"
	"time"

	"github.com/evalplane/evalplane/backend/internal/identity"
)

const (
	ValidityValid           = "valid"
	ValidityInvalidPlatform = "invalid_platform"
	ValidityPending         = "pending"
)

type Suite struct {
	ID            string    `json:"id"`
	WorkspaceID   string    `json:"workspace_id"`
	Name          string    `json:"name"`
	LatestVersion *Version  `json:"latest_version,omitempty"`
	CreatedBy     *string   `json:"created_by"`
	CreatedAt     time.Time `json:"created_at"`
}

type ScenarioEntry struct {
	ScenarioVersionID string                `json:"scenario_version_id"`
	ScenarioID        string                `json:"scenario_id,omitempty"`
	ScenarioName      string                `json:"scenario_name,omitempty"`
	ScenarioVersion   int                   `json:"scenario_version,omitempty"`
	TrialCount        int                   `json:"trial_count"`
	SortOrder         int                   `json:"sort_order"`
	HiddenVariants    []HiddenVariantConfig `json:"hidden_variants,omitempty"`
}

// HiddenVariantConfig is evaluator-only. It is never part of AgentView or an
// Agent Gateway request. Fields are limited to behavior the traffic runtime owns.
type HiddenVariantConfig struct {
	TrafficVUs          *int `json:"traffic_vus,omitempty"`
	InitialReplicas     *int `json:"initial_replicas,omitempty"`
	CapacityPerPod      *int `json:"capacity_per_pod,omitempty"`
	TrafficDelaySeconds *int `json:"traffic_delay_seconds,omitempty"`
}

type VariantAssignment struct {
	RunID             string              `json:"run_id"`
	SuiteExecutionID  string              `json:"suite_execution_id"`
	SuiteVersionID    string              `json:"suite_version_id"`
	ScenarioVersionID string              `json:"scenario_version_id"`
	AgentVersionID    string              `json:"agent_version_id"`
	TrialOrdinal      int                 `json:"trial_ordinal"`
	VariantID         string              `json:"variant_id"`
	VariantSeed       int64               `json:"variant_seed"`
	HiddenConfig      HiddenVariantConfig `json:"-"`
}

type Version struct {
	ID          string          `json:"id"`
	SuiteID     string          `json:"suite_id"`
	Version     int             `json:"version"`
	Description string          `json:"description"`
	Metadata    json.RawMessage `json:"metadata"`
	Scenarios   []ScenarioEntry `json:"scenarios"`
	CreatedBy   *string         `json:"created_by"`
	CreatedAt   time.Time       `json:"created_at"`
}

type Execution struct {
	ID              string     `json:"id"`
	WorkspaceID     string     `json:"workspace_id"`
	SuiteVersionID  string     `json:"suite_version_id"`
	Status          string     `json:"status"`
	AgentVersionIDs []string   `json:"agent_version_ids"`
	BatchIDs        []string   `json:"batch_ids"`
	CreatedBy       *string    `json:"created_by"`
	CreatedAt       time.Time  `json:"created_at"`
	StartedAt       *time.Time `json:"started_at"`
	CompletedAt     *time.Time `json:"completed_at"`
}

type CreateSuiteInput struct{ Name string }
type CreateVersionInput struct {
	Description string
	Metadata    json.RawMessage
	Scenarios   []ScenarioEntry
}
type StartExecutionInput struct {
	SuiteVersionID  string
	AgentVersionIDs []string
}
type Filters struct{ Pagination identity.Pagination }

type Counts struct {
	Total                     int             `json:"total"`
	Scheduled                 int             `json:"scheduled"`
	Completed                 int             `json:"completed"`
	Valid                     int             `json:"valid"`
	InvalidPlatform           int             `json:"invalid_platform"`
	Passed                    int             `json:"passed"`
	Failed                    int             `json:"failed"`
	Inconclusive              int             `json:"inconclusive"`
	Pending                   int             `json:"pending"`
	Running                   int             `json:"running"`
	PassRate                  *float64        `json:"pass_rate"`
	TrialsWithSafetyViolation int             `json:"trials_with_safety_violations"`
	TotalSafetyViolations     int             `json:"total_safety_violations"`
	SafetyViolationRate       *float64        `json:"safety_violation_rate"`
	SafetyViolationCodes      []ViolationCode `json:"safety_violation_codes"`
	InsufficientSamples       bool            `json:"insufficient_samples"`
}

type ViolationCode struct {
	Code  string `json:"code"`
	Count int    `json:"count"`
}
type AgentCounts struct {
	AgentVersionID string `json:"agent_version_id"`
	Counts
}
type AgentScenarioCounts struct {
	AgentVersionID    string `json:"agent_version_id"`
	ScenarioVersionID string `json:"scenario_version_id"`
	Counts
}
type Results struct {
	ExecutionID    string                `json:"execution_id"`
	SuiteVersionID string                `json:"suite_version_id"`
	Overall        Counts                `json:"overall"`
	ByAgent        []AgentCounts         `json:"by_agent"`
	ByScenario     []AgentScenarioCounts `json:"by_agent_scenario"`
	Criteria       []CriterionSummary    `json:"criterion_summaries"`
	Trials         []TrialResult         `json:"trials"`
}

type TrialResult struct {
	RunID             string `json:"run_id"`
	AgentVersionID    string `json:"agent_version_id"`
	ScenarioVersionID string `json:"scenario_version_id"`
	TrialOrdinal      int    `json:"trial_ordinal"`
	VariantID         string `json:"variant_id"`
	VariantSeed       int64  `json:"variant_seed"`
	Validity          string `json:"validity"`
	EvaluationResult  string `json:"evaluation_result,omitempty"`
	EvaluatorVersion  string `json:"evaluator_version,omitempty"`
}

type CriterionSummary struct {
	AgentVersionID    string          `json:"agent_version_id"`
	ScenarioVersionID string          `json:"scenario_version_id"`
	CriterionID       string          `json:"criterion_id"`
	CriterionKey      string          `json:"criterion_key"`
	Operator          string          `json:"operator"`
	TargetValue       json.RawMessage `json:"target_value"`
	Unit              *string         `json:"unit,omitempty"`
	Count             int             `json:"count"`
	Min               float64         `json:"min"`
	Max               float64         `json:"max"`
	Mean              float64         `json:"mean"`
	Median            float64         `json:"median"`
}
