package competency

import (
	"strings"
	"testing"

	"github.com/evalplane/evalplane/backend/internal/agentcapability"
	"github.com/evalplane/evalplane/backend/internal/runtimecontract"
)

func TestContractVersion(t *testing.T) {
	if ContractVersion != "infrastructure-capability-catalog:v1" {
		t.Fatalf("contract=%q", ContractVersion)
	}
	if ContractVersion != runtimecontract.InfrastructureCapabilityCatalogVersion {
		t.Fatalf("contract diverges from runtimecontract: %q vs %q", ContractVersion, runtimecontract.InfrastructureCapabilityCatalogVersion)
	}
}

func TestDomainFamilyHierarchy(t *testing.T) {
	items := List()
	if len(items) == 0 {
		t.Fatal("catalog is empty")
	}
	byID := map[string]Capability{}
	for _, item := range items {
		if item.Domain == "" || item.Family == "" || item.DisplayName == "" || item.Description == "" {
			t.Fatalf("missing metadata for %q", item.ID)
		}
		if item.CatalogVersion != ContractVersion {
			t.Fatalf("catalog version mismatch for %q: %q", item.ID, item.CatalogVersion)
		}
		switch item.Status {
		case StatusActive, StatusExperimental, StatusPlanned:
		default:
			t.Fatalf("invalid status %q for %q", item.Status, item.ID)
		}
		byID[item.ID] = item
	}
	// Spot-check hierarchy examples from the spec.
	for id, want := range map[string][2]string{
		"kubernetes.workload.diagnosis":          {"Kubernetes", "Workload Operations"},
		"kubernetes.capacity.remediation":        {"Kubernetes", "Capacity"},
		"gitops.drift.remediation":               {"GitOps", "Drift"},
		"delivery.progressive_delivery.recovery": {"Delivery", "Progressive Delivery"},
		"observability.metrics.diagnosis":        {"Observability", "Incident Diagnosis"},
		"safety.protected_resource_avoidance":    {"Safety", "Operational Boundaries"},
	} {
		item, ok := byID[id]
		if !ok {
			t.Fatalf("missing capability %q", id)
		}
		if item.Domain != want[0] || item.Family != want[1] {
			t.Fatalf("%q hierarchy=%q/%q want %q/%q", id, item.Domain, item.Family, want[0], want[1])
		}
	}
	groups := Domains()
	if len(groups) < 5 {
		t.Fatalf("expected at least 5 domains, got %v", groups)
	}
}

func TestCapabilityIDsUniqueAndExpected(t *testing.T) {
	items := List()
	seen := map[string]bool{}
	for _, item := range items {
		if seen[item.ID] {
			t.Fatalf("duplicate capability %q", item.ID)
		}
		seen[item.ID] = true
	}
	expected := []string{
		"kubernetes.workload.diagnosis", "kubernetes.workload.remediation",
		"kubernetes.capacity.diagnosis", "kubernetes.capacity.remediation",
		"kubernetes.deployment.diagnosis", "kubernetes.deployment.recovery",
		"kubernetes.networking.diagnosis", "kubernetes.dependency.diagnosis",
		"observability.metrics.diagnosis", "observability.telemetry.missing_data_reasoning",
		"observability.recovery.verification",
		"gitops.drift.detection", "gitops.drift.remediation",
		"gitops.desired_state.diagnosis", "gitops.desired_state.remediation",
		"gitops.argocd.diagnosis", "gitops.argocd.recovery",
		"delivery.rollout.diagnosis", "delivery.rollout.recovery",
		"delivery.progressive_delivery.diagnosis", "delivery.progressive_delivery.recovery",
		"safety.scope.boundary_adherence", "safety.protected_resource_avoidance",
		"safety.stop_when_recovered",
	}
	if len(items) != len(expected) {
		t.Fatalf("catalog size=%d want %d", len(items), len(expected))
	}
	for _, id := range expected {
		if !seen[id] {
			t.Fatalf("missing expected capability %q", id)
		}
		if !IsActive(id) {
			t.Fatalf("expected %q to be active/claimable", id)
		}
	}
}

func TestRequiresReferenceKnownRuntimeCapabilities(t *testing.T) {
	for _, item := range List() {
		if len(item.Requires) == 0 {
			t.Fatalf("%q must declare minimum runtime requirements", item.ID)
		}
		for _, req := range item.Requires {
			if !agentcapability.Known(req) {
				t.Fatalf("%q requires unknown runtime capability %q (reuse agentcapability vocabulary)", item.ID, req)
			}
		}
	}
	// Spec example: capacity remediation needs observe/scale-shaped tooling.
	got, ok := Get("kubernetes.capacity.remediation")
	if !ok {
		t.Fatal("missing kubernetes.capacity.remediation")
	}
	for _, want := range []string{"kubernetes_workloads", "request_rate", "scale_workload"} {
		found := false
		for _, req := range got.Requires {
			if req == want {
				found = true
			}
		}
		if !found {
			t.Fatalf("capacity remediation requires=%v, want %q", got.Requires, want)
		}
	}
}

func TestNoCloudProviderCapabilities(t *testing.T) {
	for _, item := range List() {
		lower := strings.ToLower(item.ID + " " + item.Domain + " " + item.DisplayName + " " + item.Description)
		for _, forbidden := range []string{"aws", "terraform", "gcp", "google cloud", "amazon", "azure"} {
			if strings.Contains(lower, forbidden) {
				t.Fatalf("catalog must not include cloud capability %q in %q", forbidden, item.ID)
			}
		}
	}
}

func TestNoLLMGrading(t *testing.T) {
	for _, item := range List() {
		lower := strings.ToLower(item.ID + " " + item.DisplayName + " " + item.Description)
		for _, forbidden := range []string{"llm grade", "llm grading", "llm judge", "model graded"} {
			if strings.Contains(lower, forbidden) {
				t.Fatalf("catalog must not imply LLM grading in %q", item.ID)
			}
		}
	}
}
