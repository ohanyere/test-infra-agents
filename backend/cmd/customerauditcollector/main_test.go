package main

import "testing"

func TestCandidateFiltersToSuccessfulDeploymentMutations(t *testing.T) {
	base := []byte(`{"apiVersion":"audit.k8s.io/v1","kind":"Event","auditID":"id","stage":"ResponseComplete","verb":"patch","requestReceivedTimestamp":"2026-10-19T10:00:00Z","stageTimestamp":"2026-10-19T10:00:01Z","user":{"username":"serviceaccount"},"objectRef":{"apiGroup":"apps","apiVersion":"v1","resource":"deployments","namespace":"checkout","name":"web","uid":"uid"},"responseStatus":{"code":200}}`)
	if !candidate(base) {
		t.Fatal("successful mutation omitted")
	}
	if candidate([]byte(`{"stage":"ResponseComplete"}`)) {
		t.Fatal("incomplete event accepted")
	}
}
