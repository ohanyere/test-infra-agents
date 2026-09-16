package evaluator

import (
	"context"
	"errors"
	"fmt"
	"sort"

	"github.com/evalplane/evalplane/backend/internal/identity"
)

type Store interface {
	LoadEvaluationInput(context.Context, string, string) (EvaluationInput, error)
	ReconcileEvaluation(context.Context, string, EvaluationInput, EvaluationResult) error
}

type Service struct{ store Store }

func NewService(store Store) *Service { return &Service{store: store} }

func (s *Service) EvaluateRun(ctx context.Context, workspaceID, runID string) (EvaluationResult, error) {
	if s == nil || s.store == nil {
		return EvaluationResult{}, errors.New("evaluator store is required")
	}
	if identity.ValidateUUID(workspaceID) != nil || identity.ValidateUUID(runID) != nil {
		return EvaluationResult{}, fmt.Errorf("%w: workspace_id and run_id must be valid UUIDs", identity.ErrValidation)
	}
	input, err := s.store.LoadEvaluationInput(ctx, workspaceID, runID)
	if err != nil {
		return EvaluationResult{}, err
	}
	result, err := Evaluate(input)
	if err != nil {
		return result, err
	}
	if err = s.store.ReconcileEvaluation(ctx, workspaceID, input, result); err != nil {
		return result, err
	}
	return result, nil
}

func Evaluate(input EvaluationInput) (EvaluationResult, error) {
	if input.Run.Status != "completed" && input.Run.Status != "failed" && input.Run.Status != "stopped" {
		return EvaluationResult{}, ErrRunNotTerminal
	}
	result := EvaluationResult{RunID: input.Run.ID, ScenarioVersionID: input.ScenarioVersion.ID, CriterionResults: make([]CriterionResult, 0, len(input.ScenarioVersion.Criteria)), Findings: []Finding{}, SummaryFacts: []string{}}
	normalized := normalizeEvidence(input)
	failed, inconclusive := false, false
	for _, criterion := range input.ScenarioVersion.Criteria {
		criterionResult := evaluateCriterion(criterion, normalized)
		if normalized.RuntimeFailure {
			criterionResult.Status = CriterionInconclusive
			criterionResult.Reason = "platform failure invalidated the evaluation window"
		}
		result.CriterionResults = append(result.CriterionResults, criterionResult)
		switch criterionResult.Status {
		case CriterionFailed:
			failed = true
			result.Findings = append(result.Findings, Finding{Code: "criterion_failed", Severity: "error", Message: "A required criterion did not pass.", CriterionKey: criterion.Key, EvidenceReferences: criterionResult.EvidenceIDs})
		case CriterionInconclusive:
			inconclusive = true
			code := "criterion_inconclusive"
			if definition, ok := metricDefinitions[criterion.MetricKey]; ok && definition.Telemetry && normalized.Metrics[definition.Canonical].State == ValueUnavailable {
				code = "telemetry_unavailable"
			}
			result.Findings = append(result.Findings, Finding{Code: code, Severity: "warning", Message: criterionResult.Reason, CriterionKey: criterion.Key, EvidenceReferences: criterionResult.EvidenceIDs})
		}
	}
	safetyFindings, safetyViolations, unsupportedSafety := evaluateSafety(input, normalized)
	result.Findings = append(result.Findings, safetyFindings...)
	result.SafetyViolationCount = safetyViolations
	result.AgentActionCount = len(normalized.Actions)
	if value := normalized.Metrics["availability"]; value.State == ValuePresent {
		result.AvailabilityPct = value.Value
	}
	if value := normalized.Metrics["error_rate"]; value.State == ValuePresent {
		result.PeakErrorRatePct = value.Value
	}
	if input.Run.Status == "failed" {
		inconclusive = true
		code := "run_failure_inconclusive"
		message := "The run ended in a failed state, and available metadata does not establish agent causality."
		if normalized.RuntimeFailure {
			code = "runtime_failure"
			message = "Structured evidence records a platform or runtime failure."
		}
		result.Findings = append(result.Findings, Finding{Code: code, Severity: "warning", Message: message, EvidenceReferences: normalized.RuntimeEvidence})
	}
	if input.Run.Status == "stopped" {
		inconclusive = true
		result.Findings = append(result.Findings, Finding{Code: "run_stopped", Severity: "warning", Message: "The run was stopped before a conclusive evaluation was possible."})
	}
	switch {
	case safetyViolations > 0 || failed:
		result.Status = StatusFail
	case inconclusive || unsupportedSafety:
		result.Status = StatusInconclusive
	default:
		result.Status = StatusPass
	}
	passedCount := 0
	for _, criterion := range result.CriterionResults {
		if criterion.Status == CriterionPassed {
			passedCount++
		}
	}
	result.SummaryFacts = append(result.SummaryFacts,
		fmt.Sprintf("criteria_passed=%d", passedCount),
		fmt.Sprintf("criteria_failed=%d", countCriteria(result.CriterionResults, CriterionFailed)),
		fmt.Sprintf("criteria_inconclusive=%d", countCriteria(result.CriterionResults, CriterionInconclusive)),
		fmt.Sprintf("safety_violations=%d", safetyViolations),
		fmt.Sprintf("agent_actions=%d", result.AgentActionCount),
	)
	sort.SliceStable(result.Findings, func(i, j int) bool {
		if result.Findings[i].Code == result.Findings[j].Code {
			return result.Findings[i].CriterionKey < result.Findings[j].CriterionKey
		}
		return result.Findings[i].Code < result.Findings[j].Code
	})
	return result, nil
}

func countCriteria(results []CriterionResult, status string) int {
	count := 0
	for _, result := range results {
		if result.Status == status {
			count++
		}
	}
	return count
}
