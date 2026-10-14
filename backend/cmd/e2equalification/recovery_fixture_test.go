package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestStage2InsufficientFixtureHasNoTrafficIndependentOfWorkerDuration(t *testing.T) {
	var spec struct {
		Type    string `json:"type"`
		Profile struct {
			StartVUs *int `json:"start_vus"`
			Stages   []struct {
				Target          int `json:"target"`
				DurationSeconds int `json:"duration_seconds"`
			} `json:"stages"`
		} `json:"profile"`
	}
	if err := json.Unmarshal([]byte(insufficientEvidenceExecutionSpec), &spec); err != nil {
		t.Fatal(err)
	}
	if spec.Type != "traffic_spike" || len(spec.Profile.Stages) != 1 {
		t.Fatalf("insufficient fixture execution profile = %+v", spec)
	}
	stage := spec.Profile.Stages[0]
	if spec.Profile.StartVUs == nil || *spec.Profile.StartVUs != 0 || stage.Target != 0 || stage.DurationSeconds <= 0 {
		t.Fatalf("insufficient fixture must explicitly run a bounded zero-VU interval so no request metrics can become eligible: start_vus=%v stage=%+v", spec.Profile.StartVUs, stage)
	}
}

func TestHostedVersionedImagesShareSufficientCapacity(t *testing.T) {
	root := filepath.Join("..", "..", "..")
	workflow, err := os.ReadFile(filepath.Join(root, ".github", "workflows", "verification-e2e.yml"))
	if err != nil {
		t.Fatal(err)
	}
	dockerfile, err := os.ReadFile(filepath.Join(root, "backend", "e2e", "Dockerfile"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(dockerfile), "ARG MAX_CONCURRENT_WORK=4") || !strings.Contains(string(dockerfile), "MAX_CONCURRENT_WORK=${MAX_CONCURRENT_WORK}") {
		t.Fatal("reference image capacity must be an explicit build-time fixture contract")
	}
	if !strings.Contains(string(workflow), "evalplane-reference:v17 --build-arg APP_ERROR_RATE=0 --build-arg MAX_CONCURRENT_WORK=32") {
		t.Fatal("v17 must represent recovery under the 12-user qualification peak")
	}
	if !strings.Contains(string(workflow), "evalplane-reference:v18 --build-arg APP_ERROR_RATE=100 --build-arg MAX_CONCURRENT_WORK=32") {
		t.Fatal("v18 must isolate injected failure behavior at the same sufficient capacity as v17")
	}
}
