package verification

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/evalplane/evalplane/backend/internal/agentgateway"
)

func withFinalAutoscaler(t *testing.T, in BuildInput, maximum int32) BuildInput {
	t.Helper()
	var payload struct {
		Observation agentgateway.Observation `json:"observation"`
	}
	if err := json.Unmarshal(in.Events[2].Payload, &payload); err != nil {
		t.Fatal(err)
	}
	payload.Observation.Autoscalers = []agentgateway.AutoscalerObservation{{
		Name:        "checkout-capacity",
		Namespace:   "run-ns",
		TargetKind:  "Deployment",
		TargetName:  "checkout",
		MinReplicas: 2,
		MaxReplicas: maximum,
	}}
	in.Events[2] = event(3, "final_observation_collected", in.Events[2].OccurredAt, payload.Observation)
	return in
}

func TestHPACapacityHeadroomThresholds(t *testing.T) {
	tests := []struct {
		name         string
		maximum      int32
		wantStatus   string
		wantFindings int
		wantSeverity string
		wantBlocking bool
		wantHeadroom float64
	}{
		{name: "acceptable two replica headroom", maximum: 8, wantStatus: StatusVerified},
		{name: "one replica warning", maximum: 7, wantStatus: StatusWarning, wantFindings: 1, wantSeverity: "MEDIUM", wantHeadroom: 1},
		{name: "zero replica blocking threshold", maximum: 6, wantStatus: StatusFailed, wantFindings: 1, wantSeverity: "HIGH", wantBlocking: true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := Derive(withFinalAutoscaler(t, validInput(), tc.maximum), HPACapacityHeadroomAnalyzer{})
			if got.OverallStatus != tc.wantStatus || len(got.ForwardRisks) != tc.wantFindings {
				t.Fatalf("status=%s risks=%+v", got.OverallStatus, got.ForwardRisks)
			}
			if tc.wantFindings == 0 {
				return
			}
			finding := got.ForwardRisks[0]
			if finding.AnalyzerID != "kubernetes-capacity-headroom" || finding.Severity != tc.wantSeverity || finding.Blocking != tc.wantBlocking {
				t.Fatalf("finding=%+v", finding)
			}
			if finding.Measurements["observed_headroom_replicas"] != tc.wantHeadroom || finding.Measurements["acceptable_minimum_headroom_replicas"] != 2 || finding.Measurements["blocking_headroom_replicas"] != 0 {
				t.Fatalf("measurements=%+v", finding.Measurements)
			}
			if len(finding.Evidence) != 1 || finding.Evidence[0] == "" || !strings.Contains(finding.Reason, "policy requires at least 2") {
				t.Fatalf("finding lacks evidence or policy reason: %+v", finding)
			}
		})
	}
}

func TestCapacityWarningRequiresMatchingObservedHPA(t *testing.T) {
	in := withFinalAutoscaler(t, validInput(), 7)
	var payload struct {
		Observation agentgateway.Observation `json:"observation"`
	}
	if err := json.Unmarshal(in.Events[2].Payload, &payload); err != nil {
		t.Fatal(err)
	}
	payload.Observation.Autoscalers[0].TargetName = "other-workload"
	in.Events[2] = event(3, "final_observation_collected", in.Events[2].OccurredAt, payload.Observation)
	got := Derive(in, HPACapacityHeadroomAnalyzer{})
	if got.OverallStatus != StatusVerified || len(got.ForwardRisks) != 0 {
		t.Fatalf("unmatched autoscaler created risk: %+v", got.ForwardRisks)
	}
}

func TestCapacityHeadroomAnalyzerIsRegistered(t *testing.T) {
	service := NewService(nil, nil, nil)
	found := false
	for _, analyzer := range service.analyzers {
		if _, ok := analyzer.(HPACapacityHeadroomAnalyzer); ok {
			found = true
		}
	}
	if !found {
		t.Fatal("capacity headroom analyzer is not part of normal verification derivation")
	}
}
