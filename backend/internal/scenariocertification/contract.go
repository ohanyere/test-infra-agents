// Package scenariocertification owns the scenario certification quality gate.
//
// Certification answers: "Is this Evaluation Task a trustworthy measurement
// instrument?" It is NOT agent qualification.
//
// An Evaluation Task / ScenarioVersion becomes authoritative qualification
// evidence only when it is certified under the pinned contract
// scenario-certification-contract:v1. Historical certifications pin the exact
// contract so future rule changes never rewrite history.
//
// No LLM is used anywhere in this package. All gates are deterministic.
package scenariocertification

const (
	// CertificationContractVersion pins the certification rule set.
	// Historical certifications pin this value.
	CertificationContractVersion = "scenario-certification-contract:v1"

	// ReferenceRemediationContractVersion pins the reference solution format.
	// Reference remediation is EVALUATOR-ONLY and never reaches the agent.
	ReferenceRemediationContractVersion = "reference-remediation:v1"

	// HiddenVariantContractVersion pins variant handling for certification.
	HiddenVariantContractVersion = "hidden-variant-contract:v1"

	// EvaluatorVersion pins the deterministic grader used for certification.
	EvaluatorVersion = "deterministic-evaluator:v1"

	// DefaultRuntimeContractVersion is the runtime contract certification runs
	// against in V1. Callers may pin a different runtime contract explicitly.
	DefaultRuntimeContractVersion = "kubernetes-traffic-spike:v1"

	// DefaultEnvironmentProfile is the default environment identity used when
	// the caller does not pin a specific profile.
	DefaultEnvironmentProfile = "default"
)

// Certification states. Machine-readable only; the frontend renders labels.
// uncertified and stale are derived (no stored row, or pinned contracts no
// longer match) and never persisted as row state.
const (
	StateUncertified = "uncertified"
	StateCertifying  = "certifying"
	StateCertified   = "certified"
	StateFailed      = "failed"
	StateStale       = "stale"
)

// Certification gates. Suggested set from the task spec. Not every task
// requires every optional gate; optional gates report not_applicable.
const (
	GateInitialState         = "initial_state"
	GateReferenceSolvability = "reference_solvability"
	GateGraderPositive       = "grader_positive"
	GateGraderNegative       = "grader_negative"
	GateSafetyDetection      = "safety_detection"
	GateTelemetry            = "telemetry"
	GateRepeatability        = "repeatability"
	GateResetCleanliness     = "reset_cleanliness"
	GateVariantValidation    = "variant_validation"
	GateLeakage              = "leakage"
	GateCriteriaValidation   = "criteria_validation"
)

// GateResult statuses.
const (
	GatePassed        = "passed"
	GateFailed        = "failed"
	GateNotApplicable = "not_applicable"
)

// Stable machine-readable failure codes.
const (
	CodeInitialStateInvalid          = "initial_state_invalid"
	CodeReferenceRemediationFailed   = "reference_remediation_failed"
	CodeReferenceNotAccepted         = "reference_not_accepted_by_evaluator"
	CodeGraderAcceptsBadState        = "grader_accepts_bad_state"
	CodeGraderRejectsGoodState       = "grader_rejects_good_state"
	CodeRequiredTelemetryUnavailable = "required_telemetry_unavailable"
	CodeEnvironmentNotClean          = "environment_not_clean"
	CodeResetNotReproducible         = "reset_not_reproducible"
	CodeHiddenVariantLeak            = "hidden_variant_leak"
	CodeReferenceSolutionLeak        = "reference_solution_leak"
	CodeCriteriaInvalid              = "criteria_invalid"
	CodeUnsafeOutcomeNotDetected     = "unsafe_outcome_not_detected"
	CodeVariantUnsatisfiable         = "variant_unsatisfiable"
	CodeCaseTypeInconsistent         = "case_type_inconsistent"
)

// OrderedGates is the canonical gate order for persistence and display.
var OrderedGates = []string{
	GateCriteriaValidation,
	GateInitialState,
	GateReferenceSolvability,
	GateGraderPositive,
	GateGraderNegative,
	GateSafetyDetection,
	GateTelemetry,
	GateRepeatability,
	GateResetCleanliness,
	GateVariantValidation,
	GateLeakage,
}

// IsCertificationState reports whether value is a stored row state.
func IsStoredState(value string) bool {
	switch value {
	case StateCertifying, StateCertified, StateFailed:
		return true
	default:
		return false
	}
}
