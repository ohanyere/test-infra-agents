// Package reliability owns deterministic reliability and evaluation-science
// primitives for AgentVersion qualification evidence.
//
// It distinguishes:
//
//	"the agent passed once" (pass@k: at least one of k valid trials succeeds)
//	from "the agent reliably succeeds" (pass^k: all k valid trials succeed).
//
// For infrastructure qualification, pass^k / consistency is the stronger
// signal. pass@k is never called "consistency."
//
// Design rules (reliability-metrics-contract:v1):
//
//   - Valid trials only: platform-invalid and pending trials never enter the
//     agent denominator. Platform-invalid counts stay visible separately and
//     are never converted into agent failures.
//   - Valid agent failures reuse the existing deterministic taxonomy: a valid
//     evaluated outcome that is not a pass (wrong remediation, safety
//     violation, unrestored state, forbidden action, failed restraint case,
//     valid-environment timeout) counts against the agent.
//   - No LLM is used anywhere in this package. All functions are pure
//     functions of persisted trial evidence. No grading is altered.
//   - Integer combinatorics first: pass@k / pass^k use finite-sample
//     hypergeometric estimators computed with rational-safe intermediates;
//     decimals are produced only at the API/display edge with central
//     rounding (see RoundPercent).
//   - Breadth and repetition stay separate: retries of one ScenarioVersion
//     never masquerade as Task Family breadth.
//   - This package never emits a QUALIFIED verdict. Stage-22
//     QualificationPolicy owns qualification; reliability is evidence for it.
package reliability

import (
	"math"
)

// ReliabilityContractVersion pins the metric definitions used by every
// report in this package. Historical reports remain reproducible because the
// contract version travels with the report; formula changes require a new
// contract version, never silent mutation.
const ReliabilityContractVersion = "reliability-metrics-contract:v1"

// ReliabilityPolicyVersion pins the default sample-sufficiency thresholds.
const ReliabilityPolicyVersion = "reliability-policy:v1"

// EvaluatorContractVersion mirrors the deterministic evaluator contract.
// Trial rows carry their own evaluator version; reports pin the distinct set
// observed so cross-contract comparisons are rejected as not_comparable.
const EvaluatorContractVersion = "deterministic-evaluator:v1"

// RuntimeContractVersion mirrors the runnable traffic-spike runtime contract.
const RuntimeContractVersion = "kubernetes-traffic-spike:v1"

// SupportedK is the bounded server-owned set of k values for pass@k/pass^k.
// A metric is emitted only when n >= k; otherwise it is explicitly
// unavailable (never a fabricated zero).
var SupportedK = []int{1, 2, 3, 5}

// Trial validity values. Only ValidityValid enters reliability denominators.
const (
	ValidityValid           = "valid"
	ValidityInvalidPlatform = "invalid_platform"
	ValidityPending         = "pending"
)

// Deterministic evaluation outcomes (see evaluator package).
const (
	ResultPass         = "pass"
	ResultFail         = "fail"
	ResultInconclusive = "inconclusive"
	ResultStopped      = "stopped"
)

// Sample sufficiency states. Deterministic thresholds live in Policy.
const (
	SufficiencyInsufficient = "insufficient"
	SufficiencyProvisional  = "provisional"
	SufficiencySufficient   = "sufficient"
)

// Saturation states for catalog maintenance. Saturation measures diminishing
// new evidence from repetition, never task difficulty or pass rate.
const (
	SaturationFresh      = "fresh"
	SaturationDeveloping = "developing"
	SaturationSaturated  = "saturated"
)

// Capability reliability states. Deliberately distinct from qualification
// verdicts: this package never emits qualified / not_qualified.
const (
	CapabilityInsufficient = "insufficient_evidence"
	CapabilityLimited      = "limited_evidence"
	CapabilityMeasured     = "measured"
)

// Comparability reason codes (stable, machine-readable).
const (
	NotComparableScenarioScope     = "scenario_scope_mismatch"
	NotComparableFamilyScope       = "family_scope_mismatch"
	NotComparableEvaluatorContract = "evaluator_contract_mismatch"
	NotComparableRuntimeContract   = "runtime_contract_mismatch"
	NotComparableEnvironment       = "environment_fidelity_mismatch"
	// NotComparableEnvironmentUnknown reports that persisted evidence
	// lacks sufficient semantic environment metadata to judge fidelity:
	// comparability is withheld rather than inferred from opaque refs.
	NotComparableEnvironmentUnknown = "environment_semantic_unavailable"
	NotComparableReliability        = "reliability_contract_mismatch"
	NotComparableTrialDistribution  = "trial_distribution_mismatch"
)

// RoundPercent rounds a 0-100 percentage to one decimal place for display.
// Internal computation keeps full float64 precision; this helper is the
// single display-edge rounding point.
func RoundPercent(value float64) float64 {
	return math.Round(value*10) / 10
}
