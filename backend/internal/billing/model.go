package billing

import "time"

// BillingAccount is the workspace billing domain record. One row per
// workspace. Clients can never write plan or status directly; provider
// reconciliation or internal operations own state transitions.
type BillingAccount struct {
	WorkspaceID     string    `json:"workspace_id"`
	PlanID          string    `json:"plan_id"`
	Status          string    `json:"status"`
	PeriodStart     time.Time `json:"period_start"`
	PeriodEnd       time.Time `json:"period_end"`
	CatalogVersion  string    `json:"catalog_version"`
	Provider        string    `json:"provider,omitempty"`
	CustomerRef     string    `json:"customer_ref,omitempty"`
	SubscriptionRef string    `json:"subscription_ref,omitempty"`
	CreatedAt       time.Time `json:"created_at"`
	UpdatedAt       time.Time `json:"updated_at"`
}

// EntitlementDecision is a reproducible entitlement snapshot fragment.
type EntitlementDecision struct {
	PlanID              string `json:"plan_id"`
	PlanDisplayName     string `json:"plan_display_name,omitempty"`
	BillingStatus       string `json:"billing_status"`
	EntitlementContract string `json:"entitlement_contract"`
	CatalogContract     string `json:"catalog_contract"`
	Key                 string `json:"key"`
	Allowed             bool   `json:"allowed"`
	Limit               *int64 `json:"limit,omitempty"`
	LimitLabel          string `json:"limit_label,omitempty"`
	Value               string `json:"value,omitempty"`
	Reason              string `json:"reason"`
	Code                string `json:"code,omitempty"`
}

// Usage kinds. Append-only; rows are never updated or deleted.
const (
	UsageGrant       = "grant"
	UsageReservation = "reservation"
	UsageCommit      = "commit"
	UsageRelease     = "release"
	UsageRefund      = "refund"
	UsageAdjustment  = "adjustment"
)

// UsageEntry is one immutable ledger row. Quantity is in integer meter units
// (evaluation execution usage units). Grants, reservations, commits,
// releases, and refunds carry positive quantities; adjustments are signed.
type UsageEntry struct {
	ID                 string    `json:"id"`
	WorkspaceID        string    `json:"workspace_id"`
	Kind               string    `json:"kind"`
	Quantity           int64     `json:"quantity"`
	MeterContract      string    `json:"meter_contract"`
	CatalogContract    string    `json:"catalog_contract"`
	IdempotencyKey     string    `json:"idempotency_key"`
	SuiteExecutionID   *string   `json:"suite_execution_id,omitempty"`
	RunID              *string   `json:"run_id,omitempty"`
	BatchID            *string   `json:"batch_id,omitempty"`
	ScenarioVersionID  *string   `json:"scenario_version_id,omitempty"`
	EnvironmentProfile *string   `json:"environment_profile,omitempty"`
	Provider           *string   `json:"provider,omitempty"`
	IsolationTier      *string   `json:"isolation_tier,omitempty"`
	Reason             string    `json:"reason,omitempty"`
	CreatedBy          *string   `json:"created_by,omitempty"`
	CreatedAt          time.Time `json:"created_at"`
}

// UsageSummary exposes safe audit data. No provider secrets.
type UsageSummary struct {
	WorkspaceID    string       `json:"workspace_id"`
	PeriodStart    time.Time    `json:"period_start"`
	PeriodEnd      time.Time    `json:"period_end"`
	Allowance      int64        `json:"allowance"`
	AllowanceLabel string       `json:"allowance_label"`
	Granted        int64        `json:"granted"`
	Reserved       int64        `json:"reserved"`
	Committed      int64        `json:"committed"`
	Available      int64        `json:"available"`
	Unlimited      bool         `json:"unlimited"`
	MeterContract  string       `json:"meter_contract"`
	Entries        []UsageEntry `json:"recent_entries"`
}

// ProviderEvent is the idempotent provider-event inbox row. Payload is a
// safe subset; full secret-bearing headers are never persisted.
type ProviderEvent struct {
	ID              string     `json:"id"`
	Provider        string     `json:"provider"`
	ProviderEventID string     `json:"provider_event_id"`
	EventType       string     `json:"event_type"`
	WorkspaceID     *string    `json:"workspace_id,omitempty"`
	Status          string     `json:"status"`
	FailureReason   *string    `json:"failure_reason,omitempty"`
	ReceivedAt      time.Time  `json:"received_at"`
	ProcessedAt     *time.Time `json:"processed_at,omitempty"`
}

// Provider event statuses.
const (
	ProviderEventReceived  = "received"
	ProviderEventApplied   = "applied"
	ProviderEventDuplicate = "duplicate"
	ProviderEventFailed    = "failed"
	ProviderEventStale     = "stale"
)

// ReserveInput pins raw usage facts separately from price conversion.
type ReserveInput struct {
	WorkspaceID        string
	Quantity           int64
	IdempotencyKey     string
	SuiteExecutionID   *string
	RunID              *string
	BatchID            *string
	ScenarioVersionID  *string
	EnvironmentProfile *string
	Provider           *string
	IsolationTier      *string
	Reason             string
	CreatedBy          *string
}

// SettleInput carries terminal trial truth for settlement.
type SettleInput struct {
	WorkspaceID       string
	RunID             string
	SuiteExecutionID  *string
	BatchID           *string
	ScenarioVersionID *string
	// RunStatus: completed|failed|stopped
	RunStatus string
	// Validity: valid|invalid_platform|pending
	Validity string
	// EvaluationStatus: pass|fail|inconclusive|""
	EvaluationStatus string
	// FailureClass: agent|platform|control|""
	FailureClass string
	// FailureCode: e.g. run_timeout, environment_provisioning_failed, worker_lease_expired
	FailureCode        string
	EnvironmentProfile *string
	Provider           *string
	IsolationTier      *string
}
