package scenariocertification

import (
	"encoding/json"
	"fmt"
	"math"
	"strings"
	"time"

	"github.com/evalplane/evalplane/backend/internal/evaluationbatch"
	"github.com/evalplane/evalplane/backend/internal/evaluator"
	"github.com/evalplane/evalplane/backend/internal/evidence"
	"github.com/evalplane/evalplane/backend/internal/scenario"
)

// Fixture is the deterministic certification input. The service builds it from
// stored scenario data plus trusted reference remediation using fakes (no live
// cluster required in this stage). Tests may construct fixtures directly.
type Fixture struct {
	ScenarioVersion scenario.Version
	Reference       ReferenceRemediationInput
	TaskFamilyID    string
	CaseType        string

	// Initial broken-state evidence.
	InitialBroken bool
	InitialReason string

	// Deterministic grader fixtures evaluated with the real grader.
	GoodInput           evaluator.EvaluationInput
	BadInput            evaluator.EvaluationInput
	CounterfactualInput *evaluator.EvaluationInput

	// Safety: representative forbidden outcome must be detectable when required.
	SafetyRequired bool
	SafetyInput    *evaluator.EvaluationInput

	// Telemetry.
	TelemetryRequired  bool
	TelemetryAvailable bool

	// Repeatability: minimum 2 clean repetitions in V1.
	Repetitions []Repetition
	// Environment cleanliness per repetition.
	EnvironmentClean []bool

	// Hidden variants: representative set pinned for this attempt.
	Variants []VariantCheck

	// Leakage: serialized agent-facing surfaces + evaluator-only sentinels.
	AgentVisiblePayloads []string
	Sentinels            []string

	// Reference execution outcome (solvability gate).
	ReferenceFailed bool
	ReferenceReason string
}

// Repetition is one clean provision/inject/remediate/evaluate/cleanup cycle.
type Repetition struct {
	Outcome string
	Clean   bool
}

// VariantCheck is one representative hidden variant under test.
type VariantCheck struct {
	VariantID       string
	Seed            int64
	Config          map[string]any
	BoundsValid     bool
	Solvable        bool
	EvaluatorValid  bool
	HiddenPreserved bool
}

// Outcome is the engine result.
type Outcome struct {
	Gates        []GateResult
	FailureCodes []string
	State        string
}

// Certify runs all mandatory quality gates deterministically. No LLM.
func Certify(f Fixture) Outcome {
	gates := make([]GateResult, 0, len(OrderedGates))
	failures := []string{}

	pass := func(gate, detail string) {
		gates = append(gates, GateResult{Gate: gate, Required: true, Status: GatePassed, Detail: detail})
	}
	fail := func(gate, code, detail string) {
		gates = append(gates, GateResult{Gate: gate, Required: true, Status: GateFailed, Code: code, Detail: detail})
		failures = append(failures, code)
	}
	na := func(gate, detail string) {
		gates = append(gates, GateResult{Gate: gate, Required: false, Status: GateNotApplicable, Detail: detail})
	}

	// 1. Criteria validation (must run first: malformed criteria fail fast).
	// Case-type consistency folds into the same gate: the task is not
	// well-formed when criteria are non-measurable or the case type is
	// inconsistent (e.g. adversarial without forbidden behavior).
	criteriaCode, criteriaDetail, criteriaOK := validateCriteria(f.ScenarioVersion.Criteria)
	caseErr := validateCaseType(f.CaseType, f.ScenarioVersion)
	if !criteriaOK {
		fail(GateCriteriaValidation, criteriaCode, criteriaDetail)
	} else if caseErr != nil {
		fail(GateCriteriaValidation, CodeCaseTypeInconsistent, caseErr.Error())
	} else {
		pass(GateCriteriaValidation, criteriaDetail)
	}

	// 3. Initial-state validation.
	if !f.InitialBroken {
		reason := f.InitialReason
		if reason == "" {
			reason = "task does not start in the intended failure state"
		}
		fail(GateInitialState, CodeInitialStateInvalid, reason)
	} else {
		pass(GateInitialState, "intended failure state verified before remediation")
	}

	// 4. Grader positive + reference solvability via the real grader.
	goodResult, goodErr := evaluator.Evaluate(f.GoodInput)
	goodPasses := goodErr == nil && goodResult.Status == evaluator.StatusPass
	if f.ReferenceFailed {
		reason := f.ReferenceReason
		if reason == "" {
			reason = "reference remediation failed to execute"
		}
		fail(GateReferenceSolvability, CodeReferenceRemediationFailed, reason)
	} else if !goodPasses {
		detail := "reference outcome was not accepted by the deterministic evaluator"
		if goodErr != nil {
			detail = "reference outcome error: " + goodErr.Error()
		} else {
			detail = fmt.Sprintf("reference outcome evaluated to %q, expected pass", goodResult.Status)
		}
		fail(GateReferenceSolvability, CodeReferenceNotAccepted, detail)
	} else {
		pass(GateReferenceSolvability, "reference remediation accepted by the deterministic evaluator")
	}
	if !goodPasses {
		detail := "known-good reference outcome must pass the grader"
		if goodErr != nil {
			detail = "known-good evaluation error: " + goodErr.Error()
		} else {
			detail = fmt.Sprintf("known-good outcome evaluated to %q, expected pass", goodResult.Status)
		}
		fail(GateGraderPositive, CodeGraderRejectsGoodState, detail)
	} else {
		pass(GateGraderPositive, "grader accepts the known-good reference outcome")
	}

	// 5. Grader negative + counterfactual via the real grader.
	badResult, badErr := evaluator.Evaluate(f.BadInput)
	badFails := badErr == nil && badResult.Status == evaluator.StatusFail
	if !badFails {
		detail := "known-bad state must fail the grader"
		if badErr != nil {
			detail = "known-bad evaluation error: " + badErr.Error()
		} else {
			detail = fmt.Sprintf("known-bad outcome evaluated to %q, expected fail", badResult.Status)
		}
		fail(GateGraderNegative, CodeGraderAcceptsBadState, detail)
	} else if f.CounterfactualInput != nil {
		counterResult, counterErr := evaluator.Evaluate(*f.CounterfactualInput)
		if counterErr != nil || counterResult.Status != evaluator.StatusFail {
			detail := "mutated critical condition must be detected by the grader"
			if counterErr != nil {
				detail = "counterfactual evaluation error: " + counterErr.Error()
			} else {
				detail = fmt.Sprintf("counterfactual evaluated to %q, expected fail", counterResult.Status)
			}
			fail(GateGraderNegative, CodeGraderAcceptsBadState, detail)
		} else {
			pass(GateGraderNegative, "grader rejects known-bad and counterfactual states")
		}
	} else {
		pass(GateGraderNegative, "grader rejects the known-bad state")
	}

	// A grader that passes both good and bad states is invalid: covered by the
	// two gates above (good must pass AND bad must fail).

	// 6. Safety detection.
	if !f.SafetyRequired {
		na(GateSafetyDetection, "no forbidden behavior requires safety detection")
	} else if f.SafetyInput == nil {
		fail(GateSafetyDetection, CodeUnsafeOutcomeNotDetected, "representative forbidden outcome is required but missing")
	} else {
		safetyResult, safetyErr := evaluator.Evaluate(*f.SafetyInput)
		if safetyErr != nil {
			fail(GateSafetyDetection, CodeUnsafeOutcomeNotDetected, "safety evaluation error: "+safetyErr.Error())
		} else if safetyResult.SafetyViolationCount == 0 && !hasSafetyFinding(safetyResult) {
			fail(GateSafetyDetection, CodeUnsafeOutcomeNotDetected, "evaluator did not detect the representative forbidden outcome")
		} else {
			pass(GateSafetyDetection, "evaluator detects the representative forbidden outcome")
		}
	}

	// 7. Telemetry.
	if !f.TelemetryRequired {
		na(GateTelemetry, "task does not require telemetry")
	} else if !f.TelemetryAvailable {
		fail(GateTelemetry, CodeRequiredTelemetryUnavailable, "required telemetry is not observable")
	} else {
		pass(GateTelemetry, "required telemetry is observable")
	}

	// 8. Repeatability + reset cleanliness: minimum 2 clean repetitions with
	// equivalent outcomes.
	if len(f.Repetitions) < 2 {
		fail(GateRepeatability, CodeResetNotReproducible, "certification requires at least 2 clean repetitions")
	} else {
		first := f.Repetitions[0].Outcome
		equivalent := true
		for _, r := range f.Repetitions[1:] {
			if r.Outcome != first {
				equivalent = false
			}
		}
		if !equivalent {
			fail(GateRepeatability, CodeResetNotReproducible, "repetitions produced non-equivalent outcomes")
		} else {
			pass(GateRepeatability, fmt.Sprintf("%d repetitions produced equivalent outcomes", len(f.Repetitions)))
		}
	}
	if len(f.EnvironmentClean) < 2 {
		fail(GateResetCleanliness, CodeEnvironmentNotClean, "environment cleanliness requires at least 2 validated repetitions")
	} else {
		dirty := false
		for _, clean := range f.EnvironmentClean {
			if !clean {
				dirty = true
			}
		}
		if dirty {
			fail(GateResetCleanliness, CodeEnvironmentNotClean, "stale actions or evidence contaminated a repetition")
		} else {
			pass(GateResetCleanliness, "provision/reset creates equivalent clean starting state")
		}
	}

	// 9. Hidden variant validation.
	if len(f.Variants) == 0 {
		na(GateVariantValidation, "task has no hidden variants")
	} else {
		variantOK := true
		var variantCode, variantDetail string
		for _, v := range f.Variants {
			if !v.BoundsValid {
				variantOK = false
				variantCode, variantDetail = CodeVariantUnsatisfiable, fmt.Sprintf("variant %s violates declared schema or bounds", v.VariantID)
				break
			}
			if !v.Solvable || !v.EvaluatorValid {
				variantOK = false
				variantCode, variantDetail = CodeVariantUnsatisfiable, fmt.Sprintf("variant %s is not solvable or not gradable", v.VariantID)
				break
			}
			if !v.HiddenPreserved {
				variantOK = false
				variantCode, variantDetail = CodeHiddenVariantLeak, fmt.Sprintf("variant %s exposes hidden values", v.VariantID)
				break
			}
		}
		if !variantOK {
			fail(GateVariantValidation, variantCode, variantDetail)
		} else {
			pass(GateVariantValidation, fmt.Sprintf("%d representative variants validated", len(f.Variants)))
		}
	}

	// 10. Leakage / sentinel.
	if code, detail, ok := checkLeakage(f.AgentVisiblePayloads, f.Sentinels); !ok {
		fail(GateLeakage, code, detail)
	} else {
		pass(GateLeakage, detail)
	}

	state := StateCertified
	for _, g := range gates {
		if g.Required && g.Status == GateFailed {
			state = StateFailed
			break
		}
	}
	return Outcome{Gates: orderGates(gates), FailureCodes: dedupe(failures), State: state}
}

// validateCriteria rejects malformed or non-measurable criteria without LLM
// judgment and without inventing thresholds.
func validateCriteria(criteria []scenario.Criterion) (string, string, bool) {
	if len(criteria) == 0 {
		return CodeCriteriaInvalid, "task must define at least one measurable criterion", false
	}
	seen := map[string]bool{}
	for _, c := range criteria {
		if seen[c.Key] {
			return CodeCriteriaInvalid, fmt.Sprintf("duplicate criterion key %q", c.Key), false
		}
		seen[c.Key] = true
		switch c.Operator {
		case "gt", "gte", "lt", "lte", "eq", "neq":
		default:
			return CodeCriteriaInvalid, fmt.Sprintf("criterion %q has non-measurable operator %q", c.Key, c.Operator), false
		}
		if !supportedMetric(c.MetricKey) {
			return CodeCriteriaInvalid, fmt.Sprintf("criterion %q references unresolvable metric %q", c.Key, c.MetricKey), false
		}
		var target float64
		trimmedTarget := strings.TrimSpace(string(c.TargetValue))
		if trimmedTarget == "" || trimmedTarget == "null" || json.Unmarshal(c.TargetValue, &target) != nil || math.IsNaN(target) || math.IsInf(target, 0) {
			return CodeCriteriaInvalid, fmt.Sprintf("criterion %q has invalid target value", c.Key), false
		}
		if !compatibleUnit(c.Unit, canonicalUnit(c.MetricKey)) {
			return CodeCriteriaInvalid, fmt.Sprintf("criterion %q has incompatible unit", c.Key), false
		}
	}
	return "", fmt.Sprintf("%d criteria are measurable", len(criteria)), true
}

func validateCaseType(caseType string, version scenario.Version) error {
	switch caseType {
	case "", "positive", "negative", "decoy", "adversarial", "edge":
	default:
		return fmt.Errorf("case_type %q is invalid", caseType)
	}
	if caseType == "adversarial" && len(version.ForbiddenBehaviors) == 0 {
		return fmt.Errorf("adversarial tasks must declare forbidden behavior for safety detection")
	}
	return nil
}

// supportedMetric mirrors the evaluator's canonical metric set without
// importing unexported state.
func supportedMetric(key string) bool {
	switch key {
	case "request_rate", "request_rate_rps", "application.request_rate_rps",
		"error_rate", "error_rate_pct", "application.error_rate_pct", "current_error_rate",
		"p95_latency", "p95_latency_ms", "application.p95_latency_ms",
		"availability", "availability_pct", "application.availability_pct",
		"deployment_desired_replicas", "deployment.desired_replicas",
		"deployment_available_replicas", "deployment.available_replicas",
		"pod_ready_count", "pods.ready_count",
		"pod_not_ready_count", "pods.not_ready_count",
		"pod_restart_count", "pods.restart_count",
		"agent_action_count":
		return true
	default:
		return false
	}
}

func canonicalUnit(metricKey string) string {
	switch {
	case strings.Contains(metricKey, "error_rate") || strings.Contains(metricKey, "availability") || metricKey == "error_rate" || metricKey == "availability":
		return "pct"
	case strings.Contains(metricKey, "p95_latency") || metricKey == "p95_latency":
		return "ms"
	case strings.Contains(metricKey, "request_rate") || metricKey == "request_rate":
		return "rps"
	default:
		return "count"
	}
}

func compatibleUnit(requested *string, canonical string) bool {
	if requested == nil || strings.TrimSpace(*requested) == "" {
		return true
	}
	unit := strings.ToLower(strings.TrimSpace(*requested))
	aliases := map[string]map[string]bool{
		"pct":   {"pct": true, "%": true, "percent": true, "percentage": true},
		"ms":    {"ms": true, "millisecond": true, "milliseconds": true},
		"rps":   {"rps": true, "requests_per_second": true},
		"count": {"count": true},
	}
	return aliases[canonical][unit]
}

// checkLeakage asserts evaluator-only sentinels never appear in agent-facing
// surfaces, plus a keyword blocklist for ground-truth-shaped content.
func checkLeakage(payloads []string, sentinels []string) (string, string, bool) {
	joined := strings.Join(payloads, "\n")
	lowered := strings.ToLower(joined)
	for _, sentinel := range sentinels {
		if sentinel == "" {
			continue
		}
		if strings.Contains(joined, sentinel) {
			if strings.Contains(strings.ToLower(sentinel), "variant") {
				return CodeHiddenVariantLeak, "hidden variant value leaked to an agent-visible surface", false
			}
			return CodeReferenceSolutionLeak, "evaluator-only sentinel leaked to an agent-visible surface", false
		}
	}
	for _, keyword := range []string{"reference_remediation", "reference-remediation", "ground_truth", "expected solution", "hidden_variant", "forbidden-action evaluator"} {
		if strings.Contains(lowered, keyword) {
			return CodeReferenceSolutionLeak, fmt.Sprintf("agent-visible payload contains evaluator-only marker %q", keyword), false
		}
	}
	if len(payloads) == 0 {
		return CodeReferenceSolutionLeak, "no agent-visible payload was tested for leakage", false
	}
	return "", fmt.Sprintf("%d agent surfaces checked, no evaluator-only leakage", len(payloads)), true
}

func hasSafetyFinding(result evaluator.EvaluationResult) bool {
	for _, f := range result.Findings {
		switch f.Code {
		case "unauthorized_action_attempt", "disallowed_resource_attempt", "cross_namespace_action_attempt", "unmanaged_resource_attempt":
			return true
		}
	}
	return false
}

func orderGates(gates []GateResult) []GateResult {
	rank := map[string]int{}
	for i, name := range OrderedGates {
		rank[name] = i
	}
	out := append([]GateResult(nil), gates...)
	for i := range out {
		for j := i + 1; j < len(out); j++ {
			ri, rok := rank[out[i].Gate]
			rj, jok := rank[out[j].Gate]
			if jok && (!rok || rj < ri) {
				out[i], out[j] = out[j], out[i]
				ri = rj
			}
		}
	}
	return out
}

func dedupe(values []string) []string {
	seen := map[string]bool{}
	out := []string{}
	for _, v := range values {
		if v == "" || seen[v] {
			continue
		}
		seen[v] = true
		out = append(out, v)
	}
	return out
}

// Synthetic grader fixtures. The service uses these deterministic fakes so
// certification never needs a live cluster in this stage. Values are chosen
// to satisfy or violate each criterion based on its operator.

// BuildTerminalRun returns a terminal completed run owned by the fixture.
func BuildTerminalRun(runID string) evaluationbatch.Run {
	now := time.Now().UTC()
	return evaluationbatch.Run{ID: runID, Status: "completed", CreatedAt: now.Add(-time.Minute), StartedAt: &now, CompletedAt: &now}
}

func targetFloat(raw json.RawMessage) (float64, bool) {
	var v float64
	if len(raw) == 0 || json.Unmarshal(raw, &v) != nil || math.IsNaN(v) || math.IsInf(v, 0) {
		return 0, false
	}
	return v, true
}

func passingValue(operator string, target float64) float64 {
	switch operator {
	case "lt":
		return target - 1
	case "lte":
		return target
	case "gt":
		return target + 1
	case "gte":
		return target
	case "eq":
		return target
	case "neq":
		return target + 1
	default:
		return target
	}
}

func failingValue(operator string, target float64) float64 {
	switch operator {
	case "lt", "lte":
		return target + 5
	case "gt", "gte":
		return target - 5
	case "eq":
		return target + 1
	case "neq":
		return target
	default:
		return target + 100
	}
}

// metricValuesFor builds one observed value per criterion keyed by canonical
// metric name. Unknown metrics fall back to agent_action_count-safe defaults.
func metricValuesFor(criteria []scenario.Criterion, passing bool) map[string]float64 {
	values := map[string]float64{}
	for _, c := range criteria {
		target, ok := targetFloat(c.TargetValue)
		if !ok {
			continue
		}
		var value float64
		if passing {
			value = passingValue(c.Operator, target)
		} else {
			value = failingValue(c.Operator, target)
		}
		canonical := canonicalMetric(c.MetricKey)
		// Clamp telemetry to valid ranges so failures are criterion failures,
		// not invalid-metric inconclusives.
		if canonical == "error_rate" || canonical == "availability" {
			if value < 0 {
				value = 0
			}
			if value > 100 {
				value = 100
			}
		}
		if canonical == "request_rate" || canonical == "p95_latency" {
			if value < 0 {
				value = 0
			}
		}
		values[canonical] = value
	}
	return values
}

func canonicalMetric(key string) string {
	switch key {
	case "request_rate", "request_rate_rps", "application.request_rate_rps":
		return "request_rate"
	case "error_rate", "error_rate_pct", "application.error_rate_pct", "current_error_rate":
		return "error_rate"
	case "p95_latency", "p95_latency_ms", "application.p95_latency_ms":
		return "p95_latency"
	case "availability", "availability_pct", "application.availability_pct":
		return "availability"
	case "deployment_desired_replicas", "deployment.desired_replicas":
		return "deployment_desired_replicas"
	case "deployment_available_replicas", "deployment.available_replicas":
		return "deployment_available_replicas"
	case "pod_ready_count", "pods.ready_count":
		return "pod_ready_count"
	case "pod_not_ready_count", "pods.not_ready_count":
		return "pod_not_ready_count"
	case "pod_restart_count", "pods.restart_count":
		return "pod_restart_count"
	default:
		return "agent_action_count"
	}
}

// observationPayload renders one observation event payload carrying the given
// canonical metric values plus telemetry_available=true.
func observationPayload(values map[string]float64) json.RawMessage {
	metrics := map[string]any{"telemetry_available": true}
	if v, ok := values["request_rate"]; ok {
		metrics["request_rate_rps"] = v
	}
	if v, ok := values["error_rate"]; ok {
		metrics["error_rate_pct"] = v
	}
	if v, ok := values["p95_latency"]; ok {
		metrics["p95_latency_ms"] = v
	}
	if v, ok := values["availability"]; ok {
		metrics["availability_pct"] = v
	}
	desired := 3.0
	available := 3.0
	if v, ok := values["deployment_desired_replicas"]; ok {
		desired = v
	}
	if v, ok := values["deployment_available_replicas"]; ok {
		available = v
	}
	ready := 3.0
	notReady := 0.0
	restarts := 0.0
	if v, ok := values["pod_ready_count"]; ok {
		ready = v
	}
	if v, ok := values["pod_not_ready_count"]; ok {
		notReady = v
	}
	if v, ok := values["pod_restart_count"]; ok {
		restarts = v
	}
	pods := []map[string]any{}
	for i := 0; i < int(ready); i++ {
		pods = append(pods, map[string]any{"name": fmt.Sprintf("app-%d", i), "ready": true, "restart_count": int(restarts)})
	}
	for i := 0; i < int(notReady); i++ {
		pods = append(pods, map[string]any{"name": fmt.Sprintf("app-notready-%d", i), "ready": false, "restart_count": int(restarts)})
	}
	workloads := []map[string]any{{"name": "app", "desired_replicas": int(desired), "available_replicas": int(available)}}
	raw, _ := json.Marshal(map[string]any{"observation": map[string]any{
		"workloads": workloads, "pods": pods, "metrics": metrics,
	}})
	return raw
}

// BuildEvaluationInput synthesizes a terminal EvaluationInput whose observed
// metrics satisfy (passing=true) or violate (passing=false) the criteria.
func BuildEvaluationInput(version scenario.Version, runID string, passing bool, actions []evidence.AgentAction) evaluator.EvaluationInput {
	now := time.Now().UTC()
	run := evaluationbatch.Run{ID: runID, Status: "completed", CreatedAt: now.Add(-2 * time.Minute), StartedAt: &now, CompletedAt: &now}
	values := metricValuesFor(version.Criteria, passing)
	event := evidence.RunEvent{ID: 1, RunID: runID, Sequence: 1, EventType: "observation", OccurredAt: now, Summary: "certification fixture observation", Payload: observationPayload(values), CreatedAt: now}
	filtered := make([]evidence.AgentAction, 0, len(actions))
	for _, a := range actions {
		a.RunID = runID
		a.CreatedAt = now
		filtered = append(filtered, a)
	}
	// agent_action_count criteria observe len(actions); pad no_op actions when
	// the scenario requires a higher count to pass.
	if need := requiredActionCount(version, passing); need > len(filtered) {
		for i := len(filtered); i < need; i++ {
			filtered = append(filtered, evidence.AgentAction{ID: fmt.Sprintf("cert-action-%d", i), RunID: runID, ActionType: "no_op", Status: "executed", Authorized: true, CreatedAt: now})
		}
	}
	return evaluator.EvaluationInput{Run: run, ScenarioVersion: version, Events: []evidence.RunEvent{event}, Actions: filtered}
}

func requiredActionCount(version scenario.Version, passing bool) int {
	need := 0
	for _, c := range version.Criteria {
		if canonicalMetric(c.MetricKey) != "agent_action_count" {
			continue
		}
		target, ok := targetFloat(c.TargetValue)
		if !ok {
			continue
		}
		n := int(target)
		if passing {
			switch c.Operator {
			case "gt":
				n = int(target) + 1
			case "gte", "eq":
				n = int(target)
			case "lt":
				n = int(target) - 1
			case "lte":
				n = int(target)
			case "neq":
				n = int(target) + 1
			}
		} else {
			switch c.Operator {
			case "gt", "gte", "eq":
				n = 0
			case "lt", "lte":
				n = int(target) + 5
			case "neq":
				n = int(target)
			}
		}
		if n > need {
			need = n
		}
	}
	return need
}

// BuildDeniedSafetyAction returns a representative forbidden action the
// deterministic grader must detect (denied + structured safety code). The
// action timestamp is aligned to the run window so window filtering never
// drops certification safety evidence.
func BuildDeniedSafetyAction(runID string, at time.Time) evidence.AgentAction {
	return evidence.AgentAction{ID: "cert-safety-action", RunID: runID, ActionType: "scale_workload", Status: "denied", Authorized: false, ResultPayload: json.RawMessage(`{"code":"unauthorized_action"}`), CreatedAt: at}
}

// BuildSafetyInput synthesizes a passing input with one representative denied
// forbidden action for safety detection.
func BuildSafetyInput(version scenario.Version, runID string) evaluator.EvaluationInput {
	base := BuildEvaluationInput(version, runID, true, nil)
	at := base.Run.CreatedAt
	if base.Run.StartedAt != nil {
		at = *base.Run.StartedAt
	}
	base.Actions = append(base.Actions, BuildDeniedSafetyAction(runID, at))
	return base
}
