package qualification

import (
	"encoding/json"
	"time"

	"github.com/evalplane/evalplane/backend/internal/agentregression"
	"github.com/evalplane/evalplane/backend/internal/evaluationsuite"
	"github.com/evalplane/evalplane/backend/internal/failuretaxonomy"
)

const (
	StatusQualified    = "qualified"
	StatusNotQualified = "not_qualified"
	StatusInconclusive = "inconclusive"
	RulePassed         = "passed"
	RuleFailed         = "failed"
	RuleInconclusive   = "inconclusive"
)

type Policy struct {
	ID            string         `json:"id"`
	WorkspaceID   string         `json:"workspace_id"`
	Name          string         `json:"name"`
	LatestVersion *PolicyVersion `json:"latest_version,omitempty"`
	CreatedBy     *string        `json:"created_by"`
	CreatedAt     time.Time      `json:"created_at"`
}

type Definition struct {
	MinimumValidTrials          int      `json:"minimum_valid_trials"`
	MinimumOverallPassRate      float64  `json:"minimum_overall_pass_rate"`
	MinimumPerScenarioPassRate  *float64 `json:"minimum_per_scenario_pass_rate,omitempty"`
	MaximumInconclusiveRate     float64  `json:"maximum_inconclusive_rate"`
	MaximumInvalidPlatformRate  *float64 `json:"maximum_invalid_platform_rate,omitempty"`
	MaximumSafetyViolations     int      `json:"maximum_safety_violations"`
	ForbiddenSafetyCodes        []string `json:"forbidden_safety_codes"`
	RequireNoDetectedRegression bool     `json:"require_no_detected_regression"`
}

type PolicyVersion struct {
	ID          string     `json:"id"`
	PolicyID    string     `json:"policy_id"`
	Version     int        `json:"version"`
	Description string     `json:"description"`
	Definition  Definition `json:"definition"`
	CreatedBy   *string    `json:"created_by"`
	CreatedAt   time.Time  `json:"created_at"`
}

type CreatePolicyInput struct {
	Name string `json:"name"`
}
type CreateVersionInput struct {
	Description string     `json:"description"`
	Definition  Definition `json:"definition"`
}
type CreateAssessmentInput struct {
	PolicyVersionID        string  `json:"policy_version_id"`
	SuiteExecutionID       string  `json:"suite_execution_id"`
	AgentVersionID         string  `json:"agent_version_id"`
	RegressionComparisonID *string `json:"regression_comparison_id,omitempty"`
}

type RuleResult struct {
	RuleType             string          `json:"rule_type"`
	Status               string          `json:"status"`
	Expected             json.RawMessage `json:"expected"`
	Observed             json.RawMessage `json:"observed"`
	Reason               string          `json:"reason"`
	SupportingReferences []string        `json:"supporting_references"`
}

type Assessment struct {
	ID                     string                                    `json:"id"`
	WorkspaceID            string                                    `json:"workspace_id"`
	PolicyVersionID        string                                    `json:"policy_version_id"`
	SuiteExecutionID       string                                    `json:"suite_execution_id"`
	SuiteVersionID         string                                    `json:"suite_version_id"`
	AgentVersionID         string                                    `json:"agent_version_id"`
	RegressionComparisonID *string                                   `json:"regression_comparison_id,omitempty"`
	Status                 string                                    `json:"status"`
	Rules                  []RuleResult                              `json:"rules"`
	Reproducibility        []agentregression.ReproducibilityManifest `json:"reproducibility_manifests"`
	CreatedBy              *string                                   `json:"created_by"`
	CreatedAt              time.Time                                 `json:"created_at"`
}

type AssessmentFacts struct {
	SuiteVersionID string
	Counts         evaluationsuite.Counts
	Scenarios      []evaluationsuite.AgentScenarioCounts
	Trials         []evaluationsuite.TrialResult
	Regression     *agentregression.Results
}

type AgentComparison struct {
	AgentVersionID string                                `json:"agent_version_id"`
	Qualification  *Assessment                           `json:"qualification,omitempty"`
	Counts         evaluationsuite.Counts                `json:"counts"`
	Scenarios      []evaluationsuite.AgentScenarioCounts `json:"scenarios"`
	Criteria       []evaluationsuite.CriterionSummary    `json:"criterion_summaries"`
	Failures       []failuretaxonomy.FailureGroup        `json:"failures"`
}

type ComparativeReport struct {
	ExecutionID     string            `json:"execution_id"`
	SuiteVersionID  string            `json:"suite_version_id"`
	PolicyVersionID string            `json:"policy_version_id,omitempty"`
	Comparability   string            `json:"comparability"`
	Reasons         []string          `json:"not_comparable_reasons"`
	Agents          []AgentComparison `json:"agents"`
}
