package evaluator

import (
	"encoding/json"
	"math"
	"strings"

	"github.com/evalplane/evalplane/backend/internal/scenario"
)

func evaluateCriterion(criterion scenario.Criterion, normalized evaluationEvidence) CriterionResult {
	result := CriterionResult{ScenarioCriterionID: criterion.ID, CriterionKey: criterion.Key, MetricKey: criterion.MetricKey, Operator: criterion.Operator, TargetValue: criterion.TargetValue, Unit: criterion.Unit, ObservedValue: json.RawMessage(`null`)}
	metric, supported := metricFor(criterion.MetricKey, normalized)
	result.ObservedState = metric.State
	if !supported {
		result.Status, result.Reason = CriterionInconclusive, metric.Reason
		return result
	}
	result.EvidenceIDs = append([]string(nil), metric.EvidenceIDs...)
	if metric.State != ValuePresent || metric.Value == nil {
		result.Status, result.Reason = CriterionInconclusive, metric.Reason
		return result
	}
	observed, _ := json.Marshal(*metric.Value)
	result.ObservedValue = observed
	if !compatibleUnit(criterion.Unit, metric.Unit) {
		result.Status, result.Reason = CriterionInconclusive, "criterion unit is incompatible with the observed metric"
		return result
	}
	var target float64
	if json.Unmarshal(criterion.TargetValue, &target) != nil || math.IsNaN(target) || math.IsInf(target, 0) {
		result.Status, result.Reason = CriterionInconclusive, "criterion target is not a finite number"
		return result
	}
	passed, ok := compare(*metric.Value, target, criterion.Operator)
	if !ok {
		result.Status, result.Reason = CriterionInconclusive, "criterion operator is unsupported"
		return result
	}
	if passed {
		result.Status, result.Reason = CriterionPassed, "observed value satisfied the criterion"
	} else {
		result.Status, result.Reason = CriterionFailed, "observed value did not satisfy the criterion"
	}
	return result
}

func compare(observed, target float64, operator string) (bool, bool) {
	switch operator {
	case "lt":
		return observed < target, true
	case "lte":
		return observed <= target, true
	case "gt":
		return observed > target, true
	case "gte":
		return observed >= target, true
	case "eq":
		return observed == target, true
	case "neq":
		return observed != target, true
	default:
		return false, false
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
