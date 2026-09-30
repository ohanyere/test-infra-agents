package verification

import (
	"encoding/json"
	"time"

	"github.com/evalplane/evalplane/backend/internal/agentgateway"
	"github.com/evalplane/evalplane/backend/internal/evidencecontract"
	"github.com/evalplane/evalplane/backend/internal/gitops"
	"github.com/evalplane/evalplane/backend/internal/provenance"
)

const (
	ContractVersion = "infrastructure-verifier:v3"
	SnapshotVersion = "infrastructure-snapshot:v2"

	StatusVerified     = "VERIFIED"
	StatusFailed       = "FAILED"
	StatusWarning      = "WARNING"
	StatusInconclusive = "INCONCLUSIVE"

	ValidityValid           = "valid"
	ValidityInvalidPlatform = "invalid_platform"
	ValidityInvalidTask     = "invalid_task"
)

type Snapshot struct {
	ContractVersion string                   `json:"contract_version"`
	Reference       string                   `json:"reference"`
	CapturedAt      time.Time                `json:"captured_at"`
	Observation     agentgateway.Observation `json:"observation"`
	GitOps          *gitops.Evidence         `json:"gitops,omitempty"`
}

type IntentAssertion struct {
	Key      string          `json:"key"`
	Metric   string          `json:"metric"`
	Operator string          `json:"operator"`
	Target   json.RawMessage `json:"target"`
}
type IntentResult struct {
	Objective  string            `json:"objective"`
	Assertions []IntentAssertion `json:"assertions"`
}
type InspectedAction struct {
	ID         string          `json:"id"`
	ActionType string          `json:"action_type"`
	Status     string          `json:"status"`
	TargetType string          `json:"target_type,omitempty"`
	TargetRef  string          `json:"target_ref,omitempty"`
	Authorized bool            `json:"authorized"`
	Requested  json.RawMessage `json:"requested"`
	Result     json.RawMessage `json:"result"`
	OccurredAt time.Time       `json:"occurred_at"`
}
type ActionInspection struct {
	Requested   int                   `json:"requested_count"`
	Authorized  int                   `json:"authorized_count"`
	Denied      int                   `json:"denied_count"`
	Executed    int                   `json:"executed_count"`
	Failed      int                   `json:"failed_count"`
	Actions     []InspectedAction     `json:"actions"`
	Attribution AttributionAssessment `json:"attribution"`
}
type AttributedResourceState struct {
	UID             string    `json:"uid,omitempty"`
	ResourceVersion string    `json:"resource_version,omitempty"`
	Generation      int64     `json:"generation,omitempty"`
	Value           string    `json:"value,omitempty"`
	ObservedAt      time.Time `json:"observed_at,omitempty"`
}
type ActionAttribution struct {
	ActionID       string                   `json:"action_id"`
	Status         string                   `json:"status"`
	Reason         string                   `json:"reason"`
	Resource       string                   `json:"resource,omitempty"`
	Field          string                   `json:"field,omitempty"`
	RequestedValue string                   `json:"requested_value,omitempty"`
	PreMutation    *AttributedResourceState `json:"pre_mutation,omitempty"`
	PostMutation   *AttributedResourceState `json:"post_mutation,omitempty"`
	LaterObserved  *AttributedResourceState `json:"later_observed,omitempty"`
}
type AttributionAssessment struct {
	ContractVersion string              `json:"contract_version"`
	Required        bool                `json:"required"`
	Status          string              `json:"status"`
	Reason          string              `json:"reason"`
	Actions         []ActionAttribution `json:"actions"`
}
type FieldChange struct {
	Field  string `json:"field"`
	Before any    `json:"before,omitempty"`
	After  any    `json:"after,omitempty"`
}
type InfrastructureChange struct {
	Resource          string        `json:"resource"`
	Classification    string        `json:"classification"`
	Attribution       string        `json:"attribution"`
	AttributionReason string        `json:"attribution_reason"`
	Fields            []FieldChange `json:"fields"`
}
type OutcomeResult struct {
	Status     string   `json:"status"`
	Reason     string   `json:"reason"`
	Assertions []string `json:"assertions"`
}
type Finding struct {
	Type     string   `json:"type"`
	Severity string   `json:"severity"`
	Summary  string   `json:"summary"`
	Blocking bool     `json:"blocking"`
	Evidence []string `json:"evidence"`
}
type SideEffectResult struct {
	Status   string    `json:"status"`
	Findings []Finding `json:"findings"`
}
type StabilityResult struct {
	Status       string    `json:"status"`
	Reason       string    `json:"reason"`
	Observations int       `json:"observations"`
	StartedAt    time.Time `json:"started_at,omitempty"`
	CompletedAt  time.Time `json:"completed_at,omitempty"`
}
type EvidenceDimensionAssessment struct {
	EvidenceType       string   `json:"evidence_type"`
	Applicability      string   `json:"applicability"`
	RequirementStatus  string   `json:"requirement_status"`
	Availability       string   `json:"availability"`
	SourceProvider     string   `json:"source_provider"`
	Reason             string   `json:"reason"`
	EvidenceReferences []string `json:"evidence_references"`
}
type EvidenceAssessment struct {
	ContractVersion string                        `json:"contract_version"`
	Requirements    evidencecontract.Requirements `json:"requirements"`
	Dimensions      []EvidenceDimensionAssessment `json:"dimensions"`
}
type InvariantResult struct {
	ID                      string    `json:"invariant_id"`
	Type                    string    `json:"invariant_type"`
	VerifierContractVersion string    `json:"verifier_contract_version"`
	Status                  string    `json:"status"`
	Scope                   []string  `json:"scope"`
	ExpectedCondition       string    `json:"expected_condition"`
	ObservedEvidence        []string  `json:"observed_evidence"`
	Reason                  string    `json:"reason"`
	EvidenceReferences      []string  `json:"evidence_references"`
	EvidenceStartedAt       time.Time `json:"evidence_started_at,omitempty"`
	EvidenceCompletedAt     time.Time `json:"evidence_completed_at,omitempty"`
}
type RiskFinding struct {
	AnalyzerID        string             `json:"analyzer_id"`
	AnalyzerVersion   string             `json:"analyzer_version"`
	Type              string             `json:"type"`
	Severity          string             `json:"severity"`
	Blocking          bool               `json:"blocking"`
	AffectedResources []string           `json:"affected_resources"`
	Evidence          []string           `json:"evidence"`
	Measurements      map[string]float64 `json:"measurements,omitempty"`
	Reason            string             `json:"reason"`
	Trigger           string             `json:"trigger"`
	EvidenceStrength  string             `json:"evidence_strength"`
	CreatedAt         time.Time          `json:"created_at"`
}
type Result struct {
	ID                      string                   `json:"id"`
	WorkspaceID             string                   `json:"workspace_id"`
	RunID                   string                   `json:"run_id"`
	AgentVersionID          string                   `json:"agent_version_id"`
	ScenarioVersionID       string                   `json:"scenario_version_id"`
	EnvironmentInstanceID   string                   `json:"environment_instance_id,omitempty"`
	CompositionPlanID       string                   `json:"composition_plan_id,omitempty"`
	CompositionPlanHash     string                   `json:"composition_plan_hash,omitempty"`
	VerifierContractVersion string                   `json:"verifier_contract_version"`
	SnapshotContractVersion string                   `json:"snapshot_contract_version"`
	Baseline                Snapshot                 `json:"baseline_snapshot"`
	Final                   Snapshot                 `json:"final_snapshot"`
	Intent                  IntentResult             `json:"intent"`
	ActionInspection        ActionInspection         `json:"action_inspection"`
	InfrastructureDiff      []InfrastructureChange   `json:"infrastructure_diff"`
	Outcome                 OutcomeResult            `json:"outcome"`
	Invariants              []InvariantResult        `json:"invariants"`
	SideEffects             SideEffectResult         `json:"side_effects"`
	Stability               StabilityResult          `json:"stability"`
	ForwardRisks            []RiskFinding            `json:"forward_risks"`
	EvidenceAssessment      EvidenceAssessment       `json:"evidence_assessment"`
	Provenance              *provenance.Verification `json:"provenance,omitempty"`
	Validity                string                   `json:"validity"`
	OverallStatus           string                   `json:"overall_status"`
	Reason                  string                   `json:"reason"`
	SourceMetadata          json.RawMessage          `json:"source_metadata"`
	EvidenceStartedAt       *time.Time               `json:"evidence_started_at,omitempty"`
	EvidenceCompletedAt     *time.Time               `json:"evidence_completed_at,omitempty"`
	CreatedAt               time.Time                `json:"created_at"`
}
