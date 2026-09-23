package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

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
