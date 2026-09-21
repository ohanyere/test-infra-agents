package main

import (
	"testing"

	"github.com/evalplane/evalplane/backend/internal/agentgateway"
	"github.com/evalplane/evalplane/backend/internal/scenario"
)

func TestQualificationObservablesValidateAndExposeRequiredEvidence(t *testing.T) {
	version, err := scenario.ValidateVersion(scenario.VersionInput{
		Category: "gitops-remediation", Difficulty: "easy", DurationSeconds: 170,
		Observables: qualificationObservables(),
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(version.Observables) != len(qualificationObservables()) {
		t.Fatalf("qualification observables changed: %v", version.Observables)
	}
	observation := agentgateway.Observation{
		Workloads: []agentgateway.WorkloadObservation{{Name: "checkout", Image: "evalplane-reference:v18"}},
		Pods:      []agentgateway.PodObservation{{Name: "checkout-pod", Ready: true}},
		Metrics:   map[string]any{"telemetry_available": true, "request_rate_rps": 12.0, "error_rate_pct": 0.5, "p95_latency_ms": 100.0, "availability_pct": 99.5},
	}
	projected := agentgateway.ProjectObservation(observation, version.Observables)
	if len(projected.Workloads) != 1 || projected.Workloads[0].Image != "evalplane-reference:v18" || len(projected.Pods) != 1 {
		t.Fatalf("qualification lost image/workload/pod evidence: %+v", projected)
	}
	for _, key := range []string{"request_rate_rps", "error_rate_pct", "p95_latency_ms", "availability_pct"} {
		if _, ok := projected.Metrics[key]; !ok {
			t.Fatalf("qualification lost %q evidence: %+v", key, projected.Metrics)
		}
	}
}
