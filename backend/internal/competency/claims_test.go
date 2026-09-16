package competency

import (
	"strings"
	"testing"
)

func TestUnknownClaimRejected(t *testing.T) {
	if _, err := ValidateClaimIDs([]string{"does.not.exist"}); err == nil {
		t.Fatal("expected unknown claim to be rejected")
	}
	if _, err := ValidateClaimIDs([]string{"kubernetes.workload.diagnosis", "bogus.capability"}); err == nil {
		t.Fatal("expected mixed unknown claim to be rejected")
	}
	// Arbitrary customer-created IDs are never accepted in V1.
	if _, err := ValidateClaimIDs([]string{"my-custom-capability"}); err == nil {
		t.Fatal("expected arbitrary customer capability to be rejected")
	}
}

func TestInactiveClaimRejected(t *testing.T) {
	// All V1 entries are active; any non-active entry must be rejected.
	// This asserts the gate itself: unknown IDs fail IsActive and validation.
	if IsActive("does.not.exist") {
		t.Fatal("unknown capability must not be active")
	}
	counts := map[string]int{}
	for _, item := range List() {
		counts[item.Status]++
		if item.Status != StatusActive {
			if _, err := ValidateClaimIDs([]string{item.ID}); err == nil {
				t.Fatalf("expected non-active %q to be rejected", item.ID)
			}
		}
	}
	if counts[StatusActive] != len(List()) {
		t.Fatalf("V1 catalog must be all active, got %v", counts)
	}
}

func TestDuplicateClaimRejected(t *testing.T) {
	if _, err := ValidateClaimIDs([]string{"kubernetes.workload.diagnosis", "kubernetes.workload.diagnosis"}); err == nil {
		t.Fatal("expected duplicate claim to be rejected")
	}
}

func TestNewVersionCanHaveDifferentClaims(t *testing.T) {
	first, err := ValidateClaimIDs([]string{"kubernetes.workload.diagnosis"})
	if err != nil {
		t.Fatal(err)
	}
	second, err := ValidateClaimIDs([]string{"gitops.drift.remediation", "delivery.rollout.recovery"})
	if err != nil {
		t.Fatal(err)
	}
	if len(first) == len(second) && first[0] == second[0] {
		t.Fatal("expected distinct claim sets to validate independently")
	}
	history := map[string][]string{"v1": first}
	history["v2"] = second
	if len(history["v1"]) != 1 || history["v1"][0] != "kubernetes.workload.diagnosis" {
		t.Fatalf("historical claims must not be rewritten: %v", history)
	}
}

func TestLowLevelRequirementMismatchRejected(t *testing.T) {
	declared := []string{"inspect", "kubernetes_workloads"}
	claimed := []string{"kubernetes.capacity.remediation"}
	if err := ValidateCompatibility(declared, claimed); err == nil {
		t.Fatal("expected incompatibility when scale_workload/request_rate are missing")
	} else if !strings.Contains(err.Error(), "scale_workload") {
		t.Fatalf("incompatibility result must name missing runtime capabilities, got %v", err)
	}
	compatible := []string{"inspect", "kubernetes_workloads", "request_rate", "scale_workload"}
	if err := ValidateCompatibility(compatible, claimed); err != nil {
		t.Fatalf("expected compatible registration, got %v", err)
	}
	// Missing permissions are never auto-added: validation only reports.
	missing := MissingRequirements(declared, claimed)
	if len(missing) != 1 || missing[0].CapabilityID != "kubernetes.capacity.remediation" || len(missing[0].Missing) == 0 {
		t.Fatalf("structured incompatibility missing=%v", missing)
	}
}

func TestClaimDoesNotProduceQualified(t *testing.T) {
	if ClaimStatusClaimed == "qualified" || ClaimStatusClaimed == "tested" {
		t.Fatalf("claim status must stay claimed, got %q", ClaimStatusClaimed)
	}
	if _, err := ValidateClaimIDs([]string{"safety.stop_when_recovered"}); err != nil {
		t.Fatal(err)
	}
	// Competency validation never mentions qualification outcomes.
	if err := ValidateCompatibility([]string{"inspect", "no_op"}, []string{"safety.stop_when_recovered"}); err != nil {
		t.Fatal(err)
	}
}

func TestEmptyClaimsAllowedForOldVersions(t *testing.T) {
	got, err := ValidateClaimIDs(nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 0 {
		t.Fatalf("empty claims must stay empty, got %v", got)
	}
	if err := ValidateCompatibility([]string{"inspect"}, nil); err != nil {
		t.Fatal(err)
	}
}
