package evidence

import (
	"encoding/json"
	"time"

	"github.com/evalplane/evalplane/backend/internal/identity"
)

type RunEvent struct {
	ID         int64           `json:"id"`
	RunID      string          `json:"-"`
	Sequence   int64           `json:"sequence"`
	EventType  string          `json:"event_type"`
	OccurredAt time.Time       `json:"occurred_at"`
	Summary    string          `json:"summary"`
	Payload    json.RawMessage `json:"payload"`
	CreatedAt  time.Time       `json:"created_at"`
}

type AgentAction struct {
	ID             string          `json:"id"`
	RunID          string          `json:"run_id"`
	RunEventID     *int64          `json:"run_event_id"`
	ActionType     string          `json:"action_type"`
	TargetType     *string         `json:"target_type"`
	TargetRef      *string         `json:"target_ref"`
	Status         string          `json:"status"`
	Authorized     bool            `json:"authorized"`
	RequestPayload json.RawMessage `json:"request_payload"`
	ResultPayload  json.RawMessage `json:"result_payload"`
	StartedAt      *time.Time      `json:"started_at"`
	CompletedAt    *time.Time      `json:"completed_at"`
	CreatedAt      time.Time       `json:"created_at"`
}

type CriterionResult struct {
	ID                  string          `json:"id"`
	ScenarioCriterionID string          `json:"scenario_criterion_id"`
	Key                 string          `json:"key"`
	Name                string          `json:"name"`
	MetricKey           string          `json:"metric_key"`
	Operator            string          `json:"operator"`
	TargetValue         json.RawMessage `json:"target_value"`
	Unit                *string         `json:"unit"`
	Passed              bool            `json:"passed"`
	ObservedValue       json.RawMessage `json:"observed_value"`
	Evidence            json.RawMessage `json:"evidence"`
	CreatedAt           time.Time       `json:"created_at"`
}

type Finding struct {
	ID        string          `json:"id"`
	Kind      string          `json:"kind"`
	Summary   string          `json:"summary"`
	Evidence  json.RawMessage `json:"evidence"`
	SortOrder int             `json:"sort_order"`
	CreatedAt time.Time       `json:"created_at"`
}

type Evaluation struct {
	ID                     string            `json:"id"`
	RunID                  string            `json:"run_id"`
	Result                 string            `json:"result"`
	EvaluatorVersion       string            `json:"evaluator_version"`
	Score                  *float64          `json:"score"`
	Summary                string            `json:"summary"`
	RecoveryDurationMS     *int64            `json:"recovery_duration_ms"`
	AvailabilityPct        *float64          `json:"availability_pct"`
	PeakErrorRatePct       *float64          `json:"peak_error_rate_pct"`
	SafetyViolationCount   int               `json:"safety_violation_count"`
	SecondaryIncidentCount int               `json:"secondary_incident_count"`
	AgentActionCount       int               `json:"agent_action_count"`
	Criteria               []CriterionResult `json:"criteria"`
	Findings               []Finding         `json:"findings"`
	CreatedAt              time.Time         `json:"created_at"`
	CompletedAt            *time.Time        `json:"completed_at"`
}

type AppendRunEventInput struct {
	RunID      string
	EventType  string
	OccurredAt time.Time
	Summary    string
	Payload    json.RawMessage
}

type RecordAgentActionInput struct {
	RunID          string
	RunEventID     *int64
	ActionType     string
	TargetType     *string
	TargetRef      *string
	Status         string
	Authorized     bool
	RequestPayload json.RawMessage
	ResultPayload  json.RawMessage
	StartedAt      *time.Time
	CompletedAt    *time.Time
}

type CriterionResultInput struct {
	ScenarioCriterionID string
	Passed              bool
	ObservedValue       json.RawMessage
	Evidence            json.RawMessage
}

type FindingInput struct {
	Kind      string
	Summary   string
	Evidence  json.RawMessage
	SortOrder int
}

type CreateEvaluationInput struct {
	RunID                  string
	Result                 string
	EvaluatorVersion       string
	Score                  *float64
	Summary                string
	RecoveryDurationMS     *int64
	AvailabilityPct        *float64
	PeakErrorRatePct       *float64
	SafetyViolationCount   int
	SecondaryIncidentCount int
	AgentActionCount       int
	CompletedAt            *time.Time
	Criteria               []CriterionResultInput
	Findings               []FindingInput
}

type EventFilters struct {
	Pagination identity.Pagination
	EventType  string
}

type ActionFilters struct {
	Pagination identity.Pagination
	Status     string
}
