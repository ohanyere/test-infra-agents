package coverage

import (
	"testing"
)

// V1 must remain exactly reproducible after V2 is introduced.
func TestV1RequirementsUnchangedByV2(t *testing.T) {
	v1 := ListRequirements()
	if len(v1) != 8 {
		t.Fatalf("V1 requirement count changed: got %d, want 8", len(v1))
	}
	for _, requirement := range v1 {
		if requirement.ContractVersion != CoverageContractVersion {
			t.Fatalf("V1 requirement %q contract mutated to %q", requirement.CapabilityID, requirement.ContractVersion)
		}
		if requirement.MinValidTrialsPerFamily != 2 || requirement.MinDistinctFamilies != 2 {
			t.Fatalf("V1 requirement %q weakened: %+v", requirement.CapabilityID, requirement)
		}
	}
	remediation, ok := GetRequirement("kubernetes.capacity.remediation")
	if !ok {
		t.Fatal("V1 kubernetes.capacity.remediation requirement missing")
	}
	if len(remediation.RequiredCaseTypeGroups) != 1 {
		t.Fatalf("V1 contrast requirement weakened: %+v", remediation)
	}
}

func TestV2PublishesGitOpsDeliveryRequirements(t *testing.T) {
	expected := []string{
		"gitops.drift.detection",
		"gitops.drift.remediation",
		"gitops.desired_state.diagnosis",
		"gitops.desired_state.remediation",
		"gitops.argocd.diagnosis",
		"gitops.argocd.recovery",
		"delivery.rollout.diagnosis",
		"delivery.rollout.recovery",
		"delivery.progressive_delivery.diagnosis",
		"delivery.progressive_delivery.recovery",
	}
	for _, id := range expected {
		requirement, ok := GetRequirementV2(id)
		if !ok {
			t.Fatalf("V2 requirement missing for %q", id)
		}
		if requirement.ContractVersion != CoverageContractV2 {
			t.Fatalf("V2 requirement %q carries contract %q", id, requirement.ContractVersion)
		}
		if len(requirement.RequiredFamilies) < 2 || requirement.MinDistinctFamilies < 2 {
			t.Fatalf("V2 requirement %q lacks family breadth: %+v", id, requirement)
		}
		if requirement.MinValidTrialsPerFamily < 2 {
			t.Fatalf("V2 requirement %q weakens trial counts: %+v", id, requirement)
		}
		if len(requirement.RequiredCaseTypes) == 0 {
			t.Fatalf("V2 requirement %q has no required case types", id)
		}
		for _, family := range requirement.RequiredFamilies {
			if _, ok := GetTaskFamily(family); !ok {
				t.Fatalf("V2 requirement %q references unknown family %q", id, family)
			}
		}
	}
	// Remediation-class capabilities must demand contrast evidence.
	for _, id := range []string{"gitops.drift.remediation", "gitops.desired_state.remediation", "gitops.argocd.recovery", "delivery.rollout.recovery", "delivery.progressive_delivery.recovery"} {
		requirement, _ := GetRequirementV2(id)
		if len(requirement.RequiredCaseTypeGroups) == 0 {
			t.Fatalf("V2 remediation requirement %q lacks contrast group", id)
		}
	}
}

func TestV2PreservesV1Entries(t *testing.T) {
	combined := ListRequirementsV2()
	if len(combined) != 18 {
		t.Fatalf("combined requirement count = %d, want 18 (8 V1 + 10 V2)", len(combined))
	}
	for _, id := range []string{"kubernetes.capacity.remediation", "safety.scope.boundary_adherence"} {
		v1, ok1 := GetRequirement(id)
		v2, ok2 := GetRequirementV2(id)
		if !ok1 || !ok2 {
			t.Fatalf("requirement %q missing in v1=%v v2=%v", id, ok1, ok2)
		}
		if v2.ContractVersion != CoverageContractVersion {
			t.Fatalf("V1 entry %q surfaced with contract %q through V2 lookup", id, v2.ContractVersion)
		}
		if len(v1.RequiredFamilies) != len(v2.RequiredFamilies) {
			t.Fatalf("V1 entry %q changed through V2 lookup", id)
		}
	}
	if _, ok := GetRequirementV2("aws.terraform.provisioning"); ok {
		t.Fatal("unknown capability must have no requirement")
	}
}
