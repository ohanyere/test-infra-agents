package executionworker

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/evalplane/evalplane/backend/internal/execution"
	"github.com/evalplane/evalplane/backend/internal/scenariorunner"
)

func TestHiddenVariantRuntimeValidationAndTrafficScaling(t *testing.T) {
	hidden := execution.HiddenVariant{ID: "hidden", Seed: 12345, Config: json.RawMessage(`{"traffic_vus":75,"initial_replicas":2,"capacity_per_pod":20,"traffic_delay_seconds":1}`)}
	parsed, err := parseRuntimeVariant(hidden)
	if err != nil || parsed.TrafficVUs == nil || *parsed.TrafficVUs != 75 || parsed.InitialReplicas == nil || *parsed.InitialReplicas != 2 {
		t.Fatalf("parsed=%+v error=%v", parsed, err)
	}
	profile := scenariorunner.TrafficSpikeProfile{Stages: []scenariorunner.TrafficStage{{Target: 10, Duration: time.Second}, {Target: 50, Duration: time.Second}}}
	scaled := scaleTrafficProfile(profile, 75)
	if scaled.Stages[0].Target != 15 || scaled.Stages[1].Target != 75 || scaled.Stages[0].Duration != time.Second {
		t.Fatalf("scaled=%+v", scaled)
	}
	if _, err = parseRuntimeVariant(execution.HiddenVariant{Config: json.RawMessage(`{"traffic_vus":1001}`)}); err == nil {
		t.Fatal("accepted an unbounded traffic variant")
	}
	if _, err = parseRuntimeVariant(execution.HiddenVariant{Config: json.RawMessage(`{"arbitrary_command":"bad"}`)}); err == nil {
		t.Fatal("accepted an unsupported variant dimension")
	}
}

func TestExecutionRunJSONCannotExposeHiddenVariant(t *testing.T) {
	raw, err := json.Marshal(execution.ExecutionRun{RunID: runID, HiddenVariant: execution.HiddenVariant{ID: "variant-secret", Seed: 12345, Config: json.RawMessage(`{"traffic_vus":75}`)}})
	if err != nil {
		t.Fatal(err)
	}
	for _, secret := range []string{"variant-secret", "12345", "traffic_vus"} {
		if strings.Contains(string(raw), secret) {
			t.Fatalf("hidden variant leaked from trusted run model: %s", raw)
		}
	}
}
