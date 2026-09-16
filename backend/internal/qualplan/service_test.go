package qualplan

import (
	"strings"
	"testing"
)

// Subset selection is pure and unit-testable: empty means all claims,
// unknown capabilities are rejected, unclaimed capabilities are rejected.
func TestSelectCapabilitiesDefaultsToAllClaims(t *testing.T) {
	claims := map[string]bool{"a": true, "b": true}
	selected, err := selectCapabilities(claims, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(selected) != 2 || selected[0] != "a" || selected[1] != "b" {
		t.Fatalf("selected = %v, want all claims sorted", selected)
	}
}

func TestSelectCapabilitiesSubsetMustBelongToClaims(t *testing.T) {
	claims := map[string]bool{"kubernetes.capacity.remediation": true}
	selected, err := selectCapabilities(claims, []string{"kubernetes.capacity.remediation"})
	if err != nil {
		t.Fatal(err)
	}
	if len(selected) != 1 {
		t.Fatalf("selected = %v", selected)
	}
	if _, err := selectCapabilities(claims, []string{"gitops.drift.remediation"}); err == nil {
		t.Fatal("unclaimed capability must be rejected")
	} else if !strings.Contains(err.Error(), "not claimed") {
		t.Fatalf("unexpected error: %v", err)
	}
	if _, err := selectCapabilities(claims, []string{"aws.terraform.provisioning"}); err == nil {
		t.Fatal("unknown capability must be rejected")
	}
}

// Suite entries dedupe shared tasks across capabilities, keep the maximum
// trial count, and stay in stable order.
func TestSuiteEntriesDedupeAcrossCapabilities(t *testing.T) {
	plan := Plan{
		State: StateReady,
		Capabilities: []CapabilityPlan{
			{CapabilityID: "a", Satisfied: true, SelectedTasks: []SelectedTask{
				{BuiltinTaskID: "shared", ScenarioVersionID: "22222222-2222-4222-8222-222222222222", TrialCount: 2},
				{BuiltinTaskID: "only-a", ScenarioVersionID: "33333333-3333-4333-8333-333333333333", TrialCount: 1},
			}},
			{CapabilityID: "b", Satisfied: true, SelectedTasks: []SelectedTask{
				{BuiltinTaskID: "shared", ScenarioVersionID: "22222222-2222-4222-8222-222222222222", TrialCount: 1},
			}},
		},
	}
	entries := suiteEntries(plan)
	if len(entries) != 2 {
		t.Fatalf("entries = %+v, want 2 deduped entries", entries)
	}
	for _, entry := range entries {
		if entry.ScenarioVersionID == "22222222-2222-4222-8222-222222222222" && entry.TrialCount != 2 {
			t.Fatalf("shared entry trial count = %d, want max (2)", entry.TrialCount)
		}
	}
	if entries[0].SortOrder != 0 || entries[1].SortOrder != 1 {
		t.Fatal("entries must carry stable sort order")
	}
}

// Selections must pin certification identity and environment requirements
// so old plans never depend on mutable current state.
func TestSelectionsPinReproducibility(t *testing.T) {
	candidate := eligibleCandidate("a-task", "22222222-2222-4222-8222-222222222222", "kubernetes.capacity.traffic_spike", "positive", []string{"kubernetes.capacity.diagnosis"})
	candidate.CertificationID = "99999999-9999-4999-8999-999999999999"
	candidate.EnvComponents = []string{"kubernetes", "prometheus"}
	candidate.EnvIsolation = "namespace"
	pressure := eligibleCandidate("b-task", "33333333-3333-4333-8333-333333333333", "kubernetes.capacity.resource_pressure", "positive", []string{"kubernetes.capacity.diagnosis"})
	pressure.CertificationID = "88888888-8888-4888-8888-888888888888"
	pressure.EnvComponents = []string{"kubernetes", "prometheus"}
	pressure.EnvIsolation = "namespace"
	outcome := BuildPlan(PlannerInput{
		AgentVersionID:  "v1",
		DeclaredRuntime: []string{"inspect", "kubernetes_workloads", "request_rate"},
		Claimed:         []string{"kubernetes.capacity.diagnosis"},
		Requirements:    testRequirements(),
		Candidates:      []Candidate{candidate, pressure},
		PolicyAvail:     true,
	})
	if outcome.State != StateReady {
		t.Fatalf("state = %q, want ready", outcome.State)
	}
	for _, selected := range outcome.Capabilities[0].SelectedTasks {
		if selected.CertificationID == "" {
			t.Fatal("selection must pin the certification record identity")
		}
		if len(selected.RequiredComponents) == 0 || selected.MinimumIsolation == "" {
			t.Fatalf("selection must pin environment requirements: %+v", selected)
		}
	}
}
