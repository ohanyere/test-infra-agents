package scenario

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestAgentViewDoesNotLeakEvaluatorFields(t *testing.T) {
	v := Version{AgentTask: "Checkout service health is degrading. Investigate and restore service health.", AgentVisibleContext: json.RawMessage(`{"symptom":"latency"}`), AllowedActions: []string{"inspect", "scale_workload"}, AllowedResources: json.RawMessage(`[{"kind":"Deployment","name":"eval-reference-app"}]`), GroundTruth: json.RawMessage(`{"cause":"traffic spike is saturating the application"}`), ExecutionSpec: json.RawMessage(`{"profile":"traffic spike execution profile"}`), ExpectedBehaviors: []string{"restore SLO"}, ForbiddenBehaviors: []string{"do not modify inventory"}, Criteria: []Criterion{{TargetValue: json.RawMessage(`99.9`)}}}
	b, e := json.Marshal(v.AgentView())
	if e != nil {
		t.Fatal(e)
	}
	text := string(b)
	for _, hidden := range []string{"traffic spike is saturating", "99.9", "criteria", "execution profile", "restore SLO", "do not modify inventory"} {
		if strings.Contains(text, hidden) {
			t.Fatalf("AgentView leaked %q: %s", hidden, text)
		}
	}
	for _, visible := range []string{"Checkout service health", "scale_workload", "eval-reference-app", "latency"} {
		if !strings.Contains(text, visible) {
			t.Fatalf("AgentView missing %q", visible)
		}
	}
}

func TestAgentViewResolvesHistoricalObservableAliases(t *testing.T) {
	version := Version{Observables: []string{"kubernetes_workloads", "request_rate", "cpu"}}
	got := version.AgentView().AllowedObservations
	if len(got) != 2 || got[0] != "kubernetes-workloads" || got[1] != "request-rate" {
		t.Fatalf("historical aliases did not resolve to supported agent observations: %v", got)
	}
}
