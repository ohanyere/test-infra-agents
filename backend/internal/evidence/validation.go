package evidence

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/evalplane/evalplane/backend/internal/identity"
)

const (
	maxEventSummaryLength      = 4000
	maxEvaluationSummaryLength = 10000
	maxFindingSummaryLength    = 4000
)

func ValidateRunEvent(in AppendRunEventInput) (AppendRunEventInput, error) {
	if identity.ValidateUUID(in.RunID) != nil {
		return in, invalid("run_id must be a valid UUID")
	}
	if !validEventType(in.EventType) {
		return in, invalid("event_type is invalid")
	}
	in.Summary = strings.TrimSpace(in.Summary)
	if in.Summary == "" || len(in.Summary) > maxEventSummaryLength {
		return in, invalid("summary is required and must be at most 4000 characters")
	}
	if in.OccurredAt.IsZero() {
		return in, invalid("occurred_at is required")
	}
	var ok bool
	if in.Payload, ok = validateObject(in.Payload); !ok {
		return in, invalid("payload must be a JSON object")
	}
	return in, nil
}

func ValidateAgentAction(in RecordAgentActionInput) (RecordAgentActionInput, error) {
	if identity.ValidateUUID(in.RunID) != nil {
		return in, invalid("run_id must be a valid UUID")
	}
	in.ActionType = strings.TrimSpace(in.ActionType)
	if in.ActionType == "" || len(in.ActionType) > 200 {
		return in, invalid("action_type is required and must be at most 200 characters")
	}
	if !validActionStatus(in.Status) {
		return in, invalid("status is invalid")
	}
	var ok bool
	if in.RequestPayload, ok = validateObject(in.RequestPayload); !ok {
		return in, invalid("request_payload must be a JSON object")
	}
	if in.ResultPayload, ok = validateObject(in.ResultPayload); !ok {
		return in, invalid("result_payload must be a JSON object")
	}
	return in, nil
}

func ValidateEvaluation(in CreateEvaluationInput) (CreateEvaluationInput, error) {
	if identity.ValidateUUID(in.RunID) != nil {
		return in, invalid("run_id must be a valid UUID")
	}
	switch in.Result {
	case "pass", "fail", "inconclusive", "stopped":
	default:
		return in, invalid("result is invalid")
	}
	if in.Score != nil && (*in.Score < 0 || *in.Score > 100) {
		return in, invalid("score must be between 0 and 100")
	}
	if in.RecoveryDurationMS != nil && *in.RecoveryDurationMS < 0 {
		return in, invalid("recovery_duration_ms must be non-negative")
	}
	if in.AvailabilityPct != nil && (*in.AvailabilityPct < 0 || *in.AvailabilityPct > 100) {
		return in, invalid("availability_pct must be between 0 and 100")
	}
	if in.PeakErrorRatePct != nil && (*in.PeakErrorRatePct < 0 || *in.PeakErrorRatePct > 100) {
		return in, invalid("peak_error_rate_pct must be between 0 and 100")
	}
	if in.SafetyViolationCount < 0 || in.SecondaryIncidentCount < 0 || in.AgentActionCount < 0 {
		return in, invalid("evaluation counts must be non-negative")
	}
	if len(in.Summary) > maxEvaluationSummaryLength {
		return in, invalid("summary must be at most 10000 characters")
	}
	seen := make(map[string]struct{}, len(in.Criteria))
	for i := range in.Criteria {
		criterion := &in.Criteria[i]
		if identity.ValidateUUID(criterion.ScenarioCriterionID) != nil {
			return in, invalid("scenario_criterion_id must be a valid UUID")
		}
		if _, exists := seen[criterion.ScenarioCriterionID]; exists {
			return in, invalid("criterion results must not contain duplicates")
		}
		seen[criterion.ScenarioCriterionID] = struct{}{}
		if !validJSON(criterion.ObservedValue) {
			return in, invalid("observed_value must be valid JSON")
		}
		var ok bool
		if criterion.Evidence, ok = validateObject(criterion.Evidence); !ok {
			return in, invalid("criterion evidence must be a JSON object")
		}
	}
	for i := range in.Findings {
		finding := &in.Findings[i]
		switch finding.Kind {
		case "strength", "limitation", "safety", "qualification":
		default:
			return in, invalid("finding kind is invalid")
		}
		finding.Summary = strings.TrimSpace(finding.Summary)
		if finding.Summary == "" || len(finding.Summary) > maxFindingSummaryLength {
			return in, invalid("finding summary is required and must be at most 4000 characters")
		}
		var ok bool
		if finding.Evidence, ok = validateObject(finding.Evidence); !ok {
			return in, invalid("finding evidence must be a JSON object")
		}
	}
	return in, nil
}

func ValidateEventFilters(in EventFilters) (EventFilters, error) {
	if in.EventType == "" {
		in.EventType = "all"
	}
	if in.EventType != "all" && !validEventType(in.EventType) {
		return in, invalid("event_type filter is invalid")
	}
	return in, nil
}

func ValidateActionFilters(in ActionFilters) (ActionFilters, error) {
	if in.Status == "" {
		in.Status = "all"
	}
	if in.Status != "all" && !validActionStatus(in.Status) {
		return in, invalid("status filter is invalid")
	}
	return in, nil
}

func validEventType(value string) bool {
	switch value {
	case "scenario", "observation", "action", "decision", "recovery", "evaluation", "safety", "system":
		return true
	}
	return false
}

func validActionStatus(value string) bool {
	switch value {
	case "proposed", "allowed", "denied", "executed", "failed":
		return true
	}
	return false
}

func validateObject(raw json.RawMessage) (json.RawMessage, bool) {
	if len(raw) == 0 {
		return json.RawMessage(`{}`), true
	}
	var object map[string]any
	if json.Unmarshal(raw, &object) != nil || object == nil {
		return nil, false
	}
	return raw, true
}

func validJSON(raw json.RawMessage) bool {
	return len(raw) > 0 && json.Valid(raw)
}

func invalid(message string) error {
	return fmt.Errorf("%w: %s", identity.ErrValidation, message)
}
