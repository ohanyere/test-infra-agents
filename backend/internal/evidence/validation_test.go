package evidence

import (
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/evalplane/evalplane/backend/internal/identity"
)

const testRunID = "11111111-1111-4111-8111-111111111111"

func TestEvidenceValidation(t *testing.T) {
	event := AppendRunEventInput{RunID: testRunID, EventType: "observation", OccurredAt: time.Now(), Summary: "Observed", Payload: json.RawMessage(`{}`)}
	action := RecordAgentActionInput{RunID: testRunID, ActionType: "restart", Status: "executed", RequestPayload: json.RawMessage(`{}`), ResultPayload: json.RawMessage(`{}`)}
	evaluation := CreateEvaluationInput{RunID: testRunID, Result: "inconclusive"}

	tests := map[string]func() error{
		"invalid event type": func() error {
			value := event
			value.EventType = "invalid"
			_, err := ValidateRunEvent(value)
			return err
		},
		"empty event summary": func() error {
			value := event
			value.Summary = " "
			_, err := ValidateRunEvent(value)
			return err
		},
		"invalid action status": func() error {
			value := action
			value.Status = "complete"
			_, err := ValidateAgentAction(value)
			return err
		},
		"negative evaluation count": func() error {
			value := evaluation
			value.AgentActionCount = -1
			_, err := ValidateEvaluation(value)
			return err
		},
		"invalid score": func() error {
			value := evaluation
			score := 101.0
			value.Score = &score
			_, err := ValidateEvaluation(value)
			return err
		},
		"invalid finding kind": func() error {
			value := evaluation
			value.Findings = []FindingInput{{Kind: "note", Summary: "Finding"}}
			_, err := ValidateEvaluation(value)
			return err
		},
		"duplicate criterion IDs": func() error {
			value := evaluation
			criterion := CriterionResultInput{ScenarioCriterionID: "22222222-2222-4222-8222-222222222222", ObservedValue: json.RawMessage(`1`)}
			value.Criteria = []CriterionResultInput{criterion, criterion}
			_, err := ValidateEvaluation(value)
			return err
		},
	}

	for name, run := range tests {
		t.Run(name, func(t *testing.T) {
			if err := run(); !errors.Is(err, identity.ErrValidation) {
				t.Fatalf("error=%v", err)
			}
		})
	}
}
