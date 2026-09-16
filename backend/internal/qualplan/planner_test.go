package qualplan

import (
	"reflect"
	"testing"

	"github.com/evalplane/evalplane/backend/internal/coverage"
)

func testRequirements() map[string]RequirementView {
	out := map[string]RequirementView{}
	for _, requirement := range coverage.ListRequirementsV2() {
		out[requirement.CapabilityID] = RequirementFor(requirement)
	}
	return out
}

func eligibleCandidate(taskID, versionID, family, caseType string, capabilities []string) Candidate {
	return Candidate{
		BuiltinTaskID:        taskID,
		ScenarioID:           "11111111-1111-4111-8111-111111111111",
		ScenarioVersionID:    versionID,
		FamilyID:             family,
		CaseType:             caseType,
		Capabilities:         capabilities,
		RuntimeContract:      "kubernetes-traffic-spike:v1",
		RuntimeAllowed:       true,
		Requires:             []string{"inspect"},
		AgentCompatibleValue: true,
		Certified:            true,
		EnvExecutable:        true,
		EnvProfile:           "kubernetes.namespace:v1",
		ContentHash:          "hash-" + taskID,
	}
}

func TestZeroClaimsBlocked(t *testing.T) {
	outcome := BuildPlan(PlannerInput{
		AgentVersionID:  "v1",
		DeclaredRuntime: []string{"inspect"},
		Claimed:         nil,
		Requirements:    testRequirements(),
	})
	if outcome.State != StateBlocked {
		t.Fatalf("zero claims state = %q, want blocked", outcome.State)
	}
	if len(outcome.Blockers) != 1 || outcome.Blockers[0].Code != BlockerNoCapabilityClaims {
		t.Fatalf("zero claims blockers = %+v, want single no_capability_claims", outcome.Blockers)
	}
	if !outcome.Blockers[0].BlocksSuite {
		t.Fatal("no-claims blocker must prevent suite creation")
	}
}

func TestPlannerContractPinned(t *testing.T) {
	if PlannerContractVersion != "qualification-suite-planner:v1" {
		t.Fatalf("planner contract = %q", PlannerContractVersion)
	}
	if CoverageContractVersion != "capability-coverage-contract:v2" {
		t.Fatalf("coverage contract = %q", CoverageContractVersion)
	}
}

func TestReadyPlanMinimalSufficient(t *testing.T) {
	candidates := []Candidate{
		eligibleCandidate("a-task", "22222222-2222-4222-8222-222222222222", "kubernetes.capacity.traffic_spike", "positive", []string{"kubernetes.capacity.remediation"}),
		eligibleCandidate("b-task", "33333333-3333-4333-8333-333333333333", "kubernetes.capacity.resource_pressure", "positive", []string{"kubernetes.capacity.remediation"}),
		eligibleCandidate("c-decoy", "44444444-4444-4444-8444-444444444444", "kubernetes.capacity.traffic_spike", "decoy", []string{"kubernetes.capacity.remediation"}),
		eligibleCandidate("d-extra", "55555555-5555-4555-8555-555555555555", "kubernetes.capacity.traffic_spike", "positive", []string{"kubernetes.capacity.remediation"}),
	}
	outcome := BuildPlan(PlannerInput{
		AgentVersionID:  "v1",
		DeclaredRuntime: []string{"inspect", "kubernetes_workloads", "request_rate", "scale_workload"},
		Claimed:         []string{"kubernetes.capacity.remediation"},
		Requirements:    testRequirements(),
		Candidates:      candidates,
		PolicyAvail:     true,
	})
	if outcome.State != StateReady {
		t.Fatalf("state = %q, want ready; blockers=%+v", outcome.State, outcome.Blockers)
	}
	capability := outcome.Capabilities[0]
	// Minimal: one task per family (a-task + b-task) plus the decoy for the
	// contrast group. d-extra must not be included.
	if len(capability.SelectedTasks) != 3 {
		t.Fatalf("selected %d tasks, want 3 (minimal sufficient): %+v", len(capability.SelectedTasks), capability.SelectedTasks)
	}
	for _, selected := range capability.SelectedTasks {
		if selected.BuiltinTaskID == "d-extra" {
			t.Fatal("planner included a superfluous task; selection must be minimal")
		}
	}
	// Family breadth preserved: both required families covered.
	if len(capability.CoveredFamilies) != 2 {
		t.Fatalf("covered families = %v, want both required families", capability.CoveredFamilies)
	}
	// Contrast preserved: a decoy case must be present.
	foundContrast := false
	for _, selected := range capability.SelectedTasks {
		if coverage.IsContrastCase(selected.CaseType) {
			foundContrast = true
		}
	}
	if !foundContrast {
		t.Fatal("contrast requirement not preserved in selection")
	}
	// Trial counts: family minimum 2 distributed across selected tasks.
	trialsByFamily := map[string]int{}
	for _, selected := range capability.SelectedTasks {
		trialsByFamily[selected.FamilyID] += selected.TrialCount
	}
	for family, total := range trialsByFamily {
		if total != 2 {
			t.Fatalf("family %q trials = %d, want exactly the 2-trial minimum", family, total)
		}
	}
}

func TestDeterministicPlanning(t *testing.T) {
	input := PlannerInput{
		AgentVersionID:  "v1",
		DeclaredRuntime: []string{"inspect", "kubernetes_workloads", "request_rate", "scale_workload"},
		Claimed:         []string{"kubernetes.capacity.remediation"},
		Requirements:    testRequirements(),
		Candidates: []Candidate{
			eligibleCandidate("b-task", "33333333-3333-4333-8333-333333333333", "kubernetes.capacity.resource_pressure", "positive", []string{"kubernetes.capacity.remediation"}),
			eligibleCandidate("a-task", "22222222-2222-4222-8222-222222222222", "kubernetes.capacity.traffic_spike", "positive", []string{"kubernetes.capacity.remediation"}),
			eligibleCandidate("c-decoy", "44444444-4444-4444-8444-444444444444", "kubernetes.capacity.traffic_spike", "decoy", []string{"kubernetes.capacity.remediation"}),
		},
		PolicyAvail: true,
	}
	first := BuildPlan(input)
	// Reverse candidate order: same plan must result.
	input.Candidates[0], input.Candidates[2] = input.Candidates[2], input.Candidates[0]
	second := BuildPlan(input)
	if first.State != second.State {
		t.Fatalf("states differ: %q vs %q", first.State, second.State)
	}
	a, b := first.Capabilities[0].SelectedTasks, second.Capabilities[0].SelectedTasks
	if len(a) != len(b) {
		t.Fatalf("selection lengths differ: %d vs %d", len(a), len(b))
	}
	for i := range a {
		if !reflect.DeepEqual(a[i], b[i]) {
			t.Fatalf("selection differs at %d: %+v vs %+v", i, a[i], b[i])
		}
	}
}

func TestUncertifiedBlocksAuthoritativePlan(t *testing.T) {
	candidate := eligibleCandidate("a-task", "22222222-2222-4222-8222-222222222222", "kubernetes.capacity.traffic_spike", "positive", []string{"kubernetes.capacity.remediation"})
	candidate.Certified = false
	outcome := BuildPlan(PlannerInput{
		AgentVersionID:  "v1",
		DeclaredRuntime: []string{"inspect", "kubernetes_workloads", "request_rate", "scale_workload"},
		Claimed:         []string{"kubernetes.capacity.remediation"},
		Requirements:    testRequirements(),
		Candidates:      []Candidate{candidate},
		PolicyAvail:     true,
	})
	if outcome.State == StateReady {
		t.Fatal("uncertified task must never produce READY")
	}
	found := false
	for _, blocker := range outcome.Blockers {
		if blocker.Code == BlockerTaskNotCertified {
			found = true
		}
	}
	if !found {
		t.Fatalf("want task_not_certified blocker, got %+v", outcome.Blockers)
	}
}

func TestStaleCertificationBlocks(t *testing.T) {
	candidate := eligibleCandidate("a-task", "22222222-2222-4222-8222-222222222222", "kubernetes.capacity.traffic_spike", "positive", []string{"kubernetes.capacity.diagnosis"})
	candidate.Certified = false
	candidate.CertStale = true
	outcome := BuildPlan(PlannerInput{
		AgentVersionID:  "v1",
		DeclaredRuntime: []string{"inspect", "kubernetes_workloads", "request_rate"},
		Claimed:         []string{"kubernetes.capacity.diagnosis"},
		Requirements:    testRequirements(),
		Candidates:      []Candidate{candidate},
		PolicyAvail:     true,
	})
	found := false
	for _, blocker := range outcome.Blockers {
		if blocker.Code == BlockerTaskCertificationStale {
			found = true
		}
	}
	if !found {
		t.Fatalf("want task_certification_stale blocker, got %+v", outcome.Blockers)
	}
}

func TestUnavailableEnvironmentBlocks(t *testing.T) {
	candidate := eligibleCandidate("gitops-task", "22222222-2222-4222-8222-222222222222", "gitops.drift.deployment", "positive", []string{"gitops.drift.detection"})
	candidate.EnvExecutable = false
	candidate.EnvReason = "no profile/provider satisfies requirements without fidelity downgrade"
	other := eligibleCandidate("gitops-task-2", "33333333-3333-4333-8333-333333333333", "gitops.desired_state.remediation", "positive", []string{"gitops.drift.detection"})
	other.EnvExecutable = false
	outcome := BuildPlan(PlannerInput{
		AgentVersionID:  "v1",
		DeclaredRuntime: []string{"inspect"},
		Claimed:         []string{"gitops.drift.detection"},
		Requirements:    testRequirements(),
		Candidates:      []Candidate{candidate, other},
		PolicyAvail:     true,
	})
	if outcome.State == StateReady {
		t.Fatal("environment-unavailable tasks must never produce READY")
	}
	found := false
	for _, blocker := range outcome.Blockers {
		if blocker.Code == BlockerEnvironmentUnavailable {
			found = true
		}
	}
	if !found {
		t.Fatalf("want environment_unavailable blocker, got %+v", outcome.Blockers)
	}
}

func TestDedicatedOnlyTaskCannotDowngrade(t *testing.T) {
	// A dedicated-isolation task is not environment-executable while no
	// dedicated provider exists, even though a namespace provider is active.
	candidate := eligibleCandidate("node-task", "22222222-2222-4222-8222-222222222222", "kubernetes.workload.baseline", "positive", []string{"kubernetes.workload.diagnosis"})
	candidate.EnvExecutable = false
	candidate.EnvReason = "preferred profile has no eligible provider"
	other := eligibleCandidate("base-task", "33333333-3333-4333-8333-333333333333", "kubernetes.capacity.traffic_spike", "positive", []string{"kubernetes.workload.diagnosis"})
	outcome := BuildPlan(PlannerInput{
		AgentVersionID:  "v1",
		DeclaredRuntime: []string{"inspect", "kubernetes_workloads", "kubernetes_pods"},
		Claimed:         []string{"kubernetes.workload.diagnosis"},
		Requirements:    testRequirements(),
		Candidates:      []Candidate{candidate, other},
		PolicyAvail:     true,
	})
	if outcome.State == StateReady {
		t.Fatalf("plan must not be READY with one family unavailable: %+v", outcome.Blockers)
	}
}

func TestRuntimeIncompatibilityBlocks(t *testing.T) {
	candidate := eligibleCandidate("a-task", "22222222-2222-4222-8222-222222222222", "kubernetes.capacity.traffic_spike", "positive", []string{"kubernetes.capacity.remediation"})
	candidate.Requires = []string{"inspect", "scale_workload"}
	candidate.AgentCompatibleValue = false
	outcome := BuildPlan(PlannerInput{
		AgentVersionID:  "v1",
		DeclaredRuntime: []string{"inspect"},
		Claimed:         []string{"kubernetes.capacity.remediation"},
		Requirements:    testRequirements(),
		Candidates:      []Candidate{candidate},
		PolicyAvail:     true,
	})
	found := false
	for _, blocker := range outcome.Blockers {
		if blocker.Code == BlockerAgentRuntimeIncompatible {
			found = true
		}
	}
	if !found {
		t.Fatalf("want agent_runtime_capability_incompatible blocker, got %+v", outcome.Blockers)
	}
	if outcome.State == StateReady {
		t.Fatal("incompatible runtime must never produce READY")
	}
}

func TestUnavailableCoverageRequirementBlocks(t *testing.T) {
	outcome := BuildPlan(PlannerInput{
		AgentVersionID:  "v1",
		DeclaredRuntime: []string{"inspect"},
		Claimed:         []string{"observability.telemetry.missing_data_reasoning"},
		Requirements:    testRequirements(),
		PolicyAvail:     true,
	})
	if outcome.State == StateReady {
		t.Fatal("capability without coverage requirement must never produce READY")
	}
	found := false
	for _, blocker := range outcome.Blockers {
		if blocker.Code == BlockerCoverageRequirementUnavailable {
			found = true
		}
	}
	if !found {
		t.Fatalf("want coverage_requirement_unavailable blocker, got %+v", outcome.Blockers)
	}
}

func TestPartialWhenSomeCapabilitiesCovered(t *testing.T) {
	candidates := []Candidate{
		eligibleCandidate("a-task", "22222222-2222-4222-8222-222222222222", "kubernetes.capacity.traffic_spike", "positive", []string{"kubernetes.capacity.remediation"}),
		eligibleCandidate("b-task", "33333333-3333-4333-8333-333333333333", "kubernetes.capacity.resource_pressure", "positive", []string{"kubernetes.capacity.remediation"}),
		eligibleCandidate("c-decoy", "44444444-4444-4444-8444-444444444444", "kubernetes.capacity.traffic_spike", "decoy", []string{"kubernetes.capacity.remediation"}),
	}
	outcome := BuildPlan(PlannerInput{
		AgentVersionID:  "v1",
		DeclaredRuntime: []string{"inspect", "kubernetes_workloads", "request_rate", "scale_workload"},
		Claimed:         []string{"gitops.drift.detection", "kubernetes.capacity.remediation"},
		Requirements:    testRequirements(),
		Candidates:      candidates,
		PolicyAvail:     true,
	})
	if outcome.State != StatePartial {
		t.Fatalf("state = %q, want partial; blockers=%+v", outcome.State, outcome.Blockers)
	}
}

func TestPlannerNeverSetsQualified(t *testing.T) {
	outcome := BuildPlan(PlannerInput{
		AgentVersionID:  "v1",
		DeclaredRuntime: []string{"inspect", "kubernetes_workloads", "request_rate", "scale_workload"},
		Claimed:         []string{"kubernetes.capacity.remediation"},
		Requirements:    testRequirements(),
		Candidates: []Candidate{
			eligibleCandidate("a-task", "22222222-2222-4222-8222-222222222222", "kubernetes.capacity.traffic_spike", "positive", []string{"kubernetes.capacity.remediation"}),
			eligibleCandidate("b-task", "33333333-3333-4333-8333-333333333333", "kubernetes.capacity.resource_pressure", "positive", []string{"kubernetes.capacity.remediation"}),
			eligibleCandidate("c-decoy", "44444444-4444-4444-8444-444444444444", "kubernetes.capacity.traffic_spike", "decoy", []string{"kubernetes.capacity.remediation"}),
		},
		PolicyAvail: true,
	})
	if outcome.State != StateReady {
		t.Fatalf("state = %q, want ready", outcome.State)
	}
	// The planner vocabulary has no qualified state: READY means a suite
	// can be constructed, never that the agent passed it.
	for _, state := range []string{StateReady, StatePartial, StateBlocked} {
		if state == "qualified" || state == "not_qualified" {
			t.Fatal("planner state vocabulary leaked qualification verdicts")
		}
	}
}

func TestSingleTaskServesMultipleCapabilities(t *testing.T) {
	shared := eligibleCandidate("shared", "22222222-2222-4222-8222-222222222222", "kubernetes.capacity.traffic_spike", "positive", []string{"kubernetes.capacity.diagnosis", "observability.metrics.diagnosis"})
	pressure := eligibleCandidate("pressure", "33333333-3333-4333-8333-333333333333", "kubernetes.capacity.resource_pressure", "positive", []string{"kubernetes.capacity.diagnosis"})
	metrics := eligibleCandidate("metrics", "44444444-4444-4444-8444-444444444444", "observability.metrics.diagnosis", "positive", []string{"observability.metrics.diagnosis"})
	outcome := BuildPlan(PlannerInput{
		AgentVersionID:  "v1",
		DeclaredRuntime: []string{"inspect", "kubernetes_workloads", "request_rate", "error_rate"},
		Claimed:         []string{"kubernetes.capacity.diagnosis", "observability.metrics.diagnosis"},
		Requirements:    testRequirements(),
		Candidates:      []Candidate{shared, pressure, metrics},
		PolicyAvail:     true,
	})
	if outcome.State != StateReady {
		t.Fatalf("state = %q, want ready; blockers=%+v", outcome.State, outcome.Blockers)
	}
	// The shared task genuinely names both capabilities, so it may appear
	// in both capability selections.
	for _, capability := range outcome.Capabilities {
		found := false
		for _, selected := range capability.SelectedTasks {
			if selected.BuiltinTaskID == "shared" {
				found = true
			}
		}
		if !found {
			t.Fatalf("shared task missing from %q selection", capability.CapabilityID)
		}
	}
}
