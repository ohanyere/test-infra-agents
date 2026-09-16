package scenariocertification

import (
	"encoding/json"
	"time"
)

// ReferenceAction is one ordered reference step. It reuses the existing typed
// action vocabulary (inspect, scale_workload, restart_pod, no_op) where
// practical. Arbitrary executable shell scripts are NOT the representation.
type ReferenceAction struct {
	Type     string          `json:"type"`
	Target   string          `json:"target,omitempty"`
	Replicas *int            `json:"replicas,omitempty"`
	Params   json.RawMessage `json:"params,omitempty"`
}

// ReferenceRemediation is the evaluator-only known-good remediation. It proves
// the task is solvable. It is NOT the one required agent trajectory: an agent
// may solve the task differently if infrastructure state and safety satisfy
// the deterministic grader. Never expose through AgentView / gateway / MCP.
type ReferenceRemediation struct {
	ScenarioVersionID string            `json:"scenario_version_id"`
	ContractVersion   string            `json:"contract_version"`
	Preconditions     json.RawMessage   `json:"preconditions"`
	ReferenceActions  []ReferenceAction `json:"reference_actions"`
	ExpectedState     json.RawMessage   `json:"expected_resulting_state"`
	VerificationNotes string            `json:"verification_notes"`
	CleanupExpected   string            `json:"cleanup_expectations"`
	ContentHash       string            `json:"content_hash"`
	CreatedAt         time.Time         `json:"created_at"`
	UpdatedAt         time.Time         `json:"updated_at"`
}

// ReferenceRemediationInput is the trusted authoring payload for associating
// evaluator-only reference remediation with a ScenarioVersion. Write-capable
// roles only. Never returned through agent-facing reads.
type ReferenceRemediationInput struct {
	Preconditions     json.RawMessage   `json:"preconditions"`
	ReferenceActions  []ReferenceAction `json:"reference_actions"`
	ExpectedState     json.RawMessage   `json:"expected_resulting_state"`
	VerificationNotes string            `json:"verification_notes"`
	CleanupExpected   string            `json:"cleanup_expectations"`
}

// GateResult is one structured gate outcome. Detail is safe (no secrets,
// no reference solution contents).
type GateResult struct {
	Gate     string `json:"gate"`
	Required bool   `json:"required"`
	Status   string `json:"status"`
	Code     string `json:"code,omitempty"`
	Detail   string `json:"detail,omitempty"`
}

// Certification is one persisted certification attempt/result. Historical rows
// are never mutated; a new attempt creates new evidence.
type Certification struct {
	ID                                  string       `json:"id"`
	WorkspaceID                         string       `json:"workspace_id"`
	ScenarioID                          string       `json:"scenario_id"`
	ScenarioVersionID                   string       `json:"scenario_version_id"`
	State                               string       `json:"state"`
	CertificationContractVersion        string       `json:"certification_contract_version"`
	ReferenceRemediationContractVersion string       `json:"reference_remediation_contract_version"`
	ReferenceContentHash                string       `json:"reference_content_hash"`
	EvaluatorVersion                    string       `json:"evaluator_version"`
	RuntimeContractVersion              string       `json:"runtime_contract_version"`
	EnvironmentProfile                  string       `json:"environment_profile"`
	ProviderID                          string       `json:"provider_id,omitempty"`
	ProviderContractVersion             string       `json:"provider_contract_version,omitempty"`
	IsolationTier                       string       `json:"isolation_tier,omitempty"`
	EnvironmentProfileContractVersion   string       `json:"environment_profile_contract_version,omitempty"`
	EnvironmentComponentContractVersion string       `json:"environment_component_contract_version,omitempty"`
	CriteriaVersion                     string       `json:"criteria_version"`
	HiddenVariantContractVersion        string       `json:"hidden_variant_contract_version"`
	TaskFamilyID                        string       `json:"task_family_id,omitempty"`
	CaseType                            string       `json:"case_type,omitempty"`
	GateResults                         []GateResult `json:"gate_results"`
	FailureCodes                        []string     `json:"failure_codes"`
	ReferenceEvaluationID               *string      `json:"reference_evaluation_id,omitempty"`
	VariantIDs                          []string     `json:"variant_ids"`
	VariantSeeds                        []int64      `json:"variant_seeds"`
	IdempotencyKey                      *string      `json:"idempotency_key,omitempty"`
	StartedAt                           time.Time    `json:"started_at"`
	CompletedAt                         *time.Time   `json:"completed_at,omitempty"`
}

// CertificationStatus is the derived read model: latest attempt plus the
// effective state (uncertified when no attempt, stale when pinned contracts no
// longer match) and authoritative eligibility.
type CertificationStatus struct {
	EffectiveState        string          `json:"effective_state"`
	Eligible              bool            `json:"eligible_authoritative_evidence"`
	Stale                 bool            `json:"stale"`
	StaleReasons          []string        `json:"stale_reasons"`
	Latest                *Certification  `json:"latest"`
	History               []Certification `json:"history"`
	CertificationContract string          `json:"certification_contract_version"`
	EvaluatorVersion      string          `json:"evaluator_version"`
}

// Eligible reports whether this ScenarioVersion may contribute authoritative
// qualification evidence. Only a fresh certified attempt qualifies; legacy
// versions without certification remain runnable but NOT authoritative.
func (s CertificationStatus) EligibleAuthoritative() bool {
	return s.Eligible
}

// VariantPin records which representative certification variants were used.
type VariantPin struct {
	VariantID string         `json:"variant_id"`
	Seed      int64          `json:"seed"`
	Config    map[string]any `json:"config"`
}

// CertifyRequest triggers one certification attempt. Public trigger shape is
// minimal and server-derived: only idempotency_key, environment_profile, and
// runtime_contract_version. Callers must not control the deterministic
// failure-injection model, the authoritative hidden certification variants,
// or the evaluator-only reference remediation. Unknown fields (including
// state, certified, simulate, variant_configs, reference_remediation,
// gate_results, failure_codes, evaluator_version) are rejected at the HTTP
// layer via DisallowUnknownFields.
type CertifyRequest struct {
	IdempotencyKey         *string `json:"idempotency_key,omitempty"`
	RuntimeContractVersion string  `json:"runtime_contract_version,omitempty"`
	EnvironmentProfile     string  `json:"environment_profile,omitempty"`
}

// Simulation forces deterministic failure modes for testing without live
// infrastructure. All fields default to false/empty (happy path). Never
// persisted as task state; only used to drive the deterministic fakes.
//
// Internal-only: used by deterministic engine/service helpers and focused
// unit tests. It is NOT part of the production HTTP request DTO and must
// never be accepted through the public certification trigger.
type Simulation struct {
	ReferenceFails          bool `json:"reference_fails,omitempty"`
	GoodRejected            bool `json:"good_rejected,omitempty"`
	BadPasses               bool `json:"bad_passes,omitempty"`
	CounterfactualPasses    bool `json:"counterfactual_passes,omitempty"`
	MissingTelemetry        bool `json:"missing_telemetry,omitempty"`
	DirtyEnvironment        bool `json:"dirty_environment,omitempty"`
	SecondRepetitionTainted bool `json:"second_repetition_tainted,omitempty"`
	LeakReference           bool `json:"leak_reference,omitempty"`
	LeakVariant             bool `json:"leak_variant,omitempty"`
	UnsafeUndetected        bool `json:"unsafe_undetected,omitempty"`
	InitialAlreadyFixed     bool `json:"initial_already_fixed,omitempty"`
	BadVariantBounds        bool `json:"bad_variant_bounds,omitempty"`
	UnsatisfiableVariant    bool `json:"unsatisfiable_variant,omitempty"`
}
