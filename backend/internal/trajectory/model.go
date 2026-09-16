package trajectory

import (
	"encoding/json"
	"time"
)

// TrajectoryEvent is the canonical safe projection of one observable
// operational behavior. It never carries chain-of-thought, hidden reasoning,
// model scratchpads, evaluator reference remediation, hidden variant values,
// evaluator-only root-cause metadata, or secrets.
type TrajectoryEvent struct {
	EventID      string          `json:"event_id"`
	Sequence     int64           `json:"sequence"`
	OccurredAt   time.Time       `json:"occurred_at"`
	RelativeMS   *int64          `json:"relative_ms,omitempty"`
	EventType    string          `json:"event_type"`
	Phase        string          `json:"phase"`
	Actor        string          `json:"actor"`
	Summary      string          `json:"summary"`
	Status       string          `json:"status"`
	SourceType   string          `json:"source_type"`
	SourceID     string          `json:"source_id"`
	EvidenceRefs []string        `json:"evidence_refs"`
	SafeMetadata json.RawMessage `json:"safe_metadata"`
	View         string          `json:"view"`
}

// OutcomeSummary is the deterministic summary object (no qualification verdict).
type OutcomeSummary struct {
	TrialStatus           string   `json:"trial_status"`
	EvaluationStatus      string   `json:"evaluation_status"`
	Validity              string   `json:"validity"`
	PassFailOutcome       string   `json:"pass_fail_outcome"`
	FailureClasses        []string `json:"failure_classes"`
	FindingCount          int      `json:"finding_count"`
	SafetyViolationCount  int      `json:"safety_violation_count"`
	ActionsRequested      int      `json:"actions_requested"`
	ActionsExecuted       int      `json:"actions_executed"`
	ActionsDenied         int      `json:"actions_denied"`
	ObservationsCount     int      `json:"observations_count"`
	TimeToRecoveryMS      *int64   `json:"time_to_recovery_ms"`
	RecoveryVerified      bool     `json:"recovery_verified"`
	ActionsAfterRecovery  int      `json:"actions_after_recovery"`
	ActionsBeforeRecovery int      `json:"actions_before_recovery"`
}

// DurationMetrics are computed only where evidence supports them; missing
// values are explicit nils, never fabricated.
type DurationMetrics struct {
	TrialDurationMS            *int64 `json:"trial_duration_ms"`
	EnvironmentSetupDurationMS *int64 `json:"environment_setup_duration_ms"`
	AgentActiveDurationMS      *int64 `json:"agent_active_duration_ms"`
	TimeToFirstActionMS        *int64 `json:"time_to_first_action_ms"`
	TimeToRecoveryMS           *int64 `json:"time_to_recovery_ms"`
	VerificationDurationMS     *int64 `json:"verification_duration_ms"`
}

// ReproPins carries safe reproducibility metadata. Hidden variant identity is
// an opaque safe ID only; hidden variant contents are never exposed.
type ReproPins struct {
	AgentVersionID           string  `json:"agent_version_id"`
	ScenarioVersionID        string  `json:"scenario_version_id"`
	SuiteExecutionID         *string `json:"suite_execution_id,omitempty"`
	SuiteVersionID           *string `json:"suite_version_id,omitempty"`
	HiddenVariantID          *string `json:"hidden_variant_id,omitempty"`
	RuntimeContract          string  `json:"runtime_contract"`
	EvaluatorContract        string  `json:"evaluator_contract"`
	TrajectoryContract       string  `json:"trajectory_contract"`
	EnvironmentProfileID     *string `json:"environment_profile_id,omitempty"`
	EnvironmentProviderID    *string `json:"environment_provider_id,omitempty"`
	EnvironmentIsolationTier *string `json:"environment_isolation_tier,omitempty"`
	TrialOrdinal             int     `json:"trial_ordinal"`
}

// Trajectory is the full read-model response for one trial.
type Trajectory struct {
	Contract           string            `json:"contract"`
	RunID              string            `json:"run_id"`
	WorkspaceID        string            `json:"workspace_id"`
	OrderingRule       string            `json:"ordering_rule"`
	Events             []TrajectoryEvent `json:"events"`
	Summary            OutcomeSummary    `json:"summary"`
	Durations          DurationMetrics   `json:"durations"`
	Pins               ReproPins         `json:"pins"`
	RecoveryDetectedAt *time.Time        `json:"recovery_detected_at,omitempty"`
	RecoveryVerifiedAt *time.Time        `json:"recovery_verified_at,omitempty"`
	TotalEvents        int               `json:"total_events"`
	Truncated          bool              `json:"truncated"`
}

// EvidenceDetail is the safe bounded detail for one evidence item.
type EvidenceDetail struct {
	EvidenceID  string          `json:"evidence_id"`
	SourceType  string          `json:"source_type"`
	SourceID    string          `json:"source_id"`
	OccurredAt  *time.Time      `json:"occurred_at,omitempty"`
	Summary     string          `json:"summary"`
	View        string          `json:"view"`
	SafeDetail  json.RawMessage `json:"safe_detail"`
	Preview     string          `json:"preview,omitempty"`
	Truncated   bool            `json:"truncated"`
	ByteSize    int             `json:"byte_size"`
	ContentHash string          `json:"content_hash"`
	Provenance  []string        `json:"provenance"`
}

// SuiteTrialRow is one trial in a suite-execution summary.
type SuiteTrialRow struct {
	RunID             string `json:"run_id"`
	TrialOrdinal      int    `json:"trial_ordinal"`
	AgentVersionID    string `json:"agent_version_id"`
	ScenarioVersionID string `json:"scenario_version_id"`
	Outcome           string `json:"outcome"`
	Validity          string `json:"validity"`
	ActionsRequested  int    `json:"actions_requested"`
	ActionsExecuted   int    `json:"actions_executed"`
	ActionsDenied     int    `json:"actions_denied"`
	SafetyViolations  int    `json:"safety_violations"`
	TimeToRecoveryMS  *int64 `json:"time_to_recovery_ms"`
	RecoveryVerified  bool   `json:"recovery_verified"`
}

// SuiteIssue is one safe integrity note for a suite summary. It carries a
// closed code plus a workspace-scoped run reference where authorized and safe.
// It never exposes cross-workspace identifiers or internal error detail.
type SuiteIssue struct {
	Code   string  `json:"code"`
	RunID  *string `json:"run_id,omitempty"`
	Detail string  `json:"detail,omitempty"`
}

// SuiteTrajectorySummary lists trials with concise evidence summaries.
// Integrity fields guarantee a partial summary is never indistinguishable
// from a complete one: complete=false with explicit expected/loaded counts
// and missing trial references whenever a referenced trial cannot be loaded.
type SuiteTrajectorySummary struct {
	Contract         string          `json:"contract"`
	SuiteExecutionID string          `json:"suite_execution_id"`
	WorkspaceID      string          `json:"workspace_id"`
	Trials           []SuiteTrialRow `json:"trials"`
	TotalTrials      int             `json:"total_trials"`
	Complete         bool            `json:"complete"`
	ExpectedTrials   int             `json:"expected_trials"`
	LoadedTrials     int             `json:"loaded_trials"`
	MissingTrials    []string        `json:"missing_trials,omitempty"`
	Issues           []SuiteIssue    `json:"issues,omitempty"`
}

// ComparisonSummary is the deterministic summary comparison of two trials.
type ComparisonSummary struct {
	Contract                string   `json:"contract"`
	BaselineRunID           string   `json:"baseline_run_id"`
	CandidateRunID          string   `json:"candidate_run_id"`
	BaselineOutcome         string   `json:"baseline_outcome"`
	CandidateOutcome        string   `json:"candidate_outcome"`
	BaselineActions         int      `json:"baseline_actions"`
	CandidateActions        int      `json:"candidate_actions"`
	BaselineDenied          int      `json:"baseline_denied"`
	CandidateDenied         int      `json:"candidate_denied"`
	BaselineRecoveryMS      *int64   `json:"baseline_recovery_ms"`
	CandidateRecoveryMS     *int64   `json:"candidate_recovery_ms"`
	BaselineVerified        bool     `json:"baseline_verified"`
	CandidateVerified       bool     `json:"candidate_verified"`
	BaselineFindings        int      `json:"baseline_findings"`
	CandidateFindings       int      `json:"candidate_findings"`
	BaselineFailureClasses  []string `json:"baseline_failure_classes"`
	CandidateFailureClasses []string `json:"candidate_failure_classes"`
}

// Filters carry safe server-side filters plus deterministic pagination.
type Filters struct {
	Limit     int
	Offset    int
	Phase     string
	EventType string
	Actor     string
	Status    string
}
