package scenarioauthoring

import (
	"encoding/json"
	"testing"
)

func validDraft() ScenarioDraft {
	return ScenarioDraft{Name: "Traffic degradation", Description: "Investigate service health", Category: "incident-response", Difficulty: "medium", DurationSeconds: 600, AgentSpec: AgentSpec{Task: "Restore service health", VisibleContext: json.RawMessage(`{}`), AllowedResources: json.RawMessage(`[]`), AllowedActions: []string{"inspect"}}, Environment: json.RawMessage(`{"platform":"kubernetes"}`), Conditions: json.RawMessage(`[]`), GroundTruth: json.RawMessage(`{}`), ExecutionSpec: json.RawMessage(`{"type":"traffic_spike"}`), RunnableNow: true}
}

func TestCompileDraft(t *testing.T) {
	draft, err := CompileDraft(validDraft())
	if err != nil || !draft.RunnableNow {
		t.Fatalf("CompileDraft() = %#v, %v", draft, err)
	}
}

func TestCompileDraftRejectsInvalidActionAndDuplicateCriteria(t *testing.T) {
	draft := validDraft()
	draft.AgentSpec.AllowedActions = []string{"delete_namespace"}
	if _, err := CompileDraft(draft); err == nil {
		t.Fatal("expected invalid action")
	}
	draft = validDraft()
	draft.Criteria = []DraftCriterion{{Key: "availability", Operator: "gte", TargetValue: json.RawMessage(`99`)}, {Key: "availability", Operator: "gte", TargetValue: json.RawMessage(`99`)}}
	if _, err := CompileDraft(draft); err == nil {
		t.Fatal("expected duplicate criterion rejection")
	}
}

func TestCompileDraftDoesNotInventThreshold(t *testing.T) {
	draft := validDraft()
	draft.Criteria = []DraftCriterion{{Key: "availability", Name: "Availability", MetricKey: "availability", Operator: "gte"}}
	compiled, err := CompileDraft(draft)
	if err != nil {
		t.Fatal(err)
	}
	if !compiled.Criteria[0].RequiresConfirmation || len(compiled.Criteria[0].TargetValue) != 0 || compiled.RunnableNow {
		t.Fatalf("threshold was not left for confirmation: %#v", compiled)
	}
}

func TestCompileDraftPreservesUnsupportedCapability(t *testing.T) {
	draft := validDraft()
	draft.ExecutionSpec = json.RawMessage(`{"type":"database_connection_exhaustion"}`)
	compiled, err := CompileDraft(draft)
	if err != nil {
		t.Fatal(err)
	}
	if compiled.RunnableNow || len(compiled.UnsupportedCapabilities) != 1 || compiled.UnsupportedCapabilities[0] != "database_connection_exhaustion" {
		t.Fatalf("unsupported capability lost: %#v", compiled)
	}
}
