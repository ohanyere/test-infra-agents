package composition

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/evalplane/evalplane/backend/internal/worldspec"
)

func fixture() (worldspec.Spec, []Adapter) {
	s := worldspec.Spec{ContractVersion: worldspec.ContractV1, CompilerID: "test:v1", Components: []worldspec.Component{{ID: "cluster", Type: "kubernetes", Version: "1.34", RequiredCapabilities: []string{"mutate"}}, {ID: "metrics", Type: "prometheus", Version: "3"}}, Edges: []worldspec.Edge{{From: "cluster", To: "metrics", Relationship: "observed_by"}}, Observations: []worldspec.Binding{{ID: "availability", ComponentID: "metrics", Contract: "metrics:v1"}}, Tools: []worldspec.Binding{{ID: "scale", ComponentID: "cluster", Contract: "tools:v1"}}, ScenarioAssertions: []worldspec.Assertion{{ID: "private-truth", ComponentID: "cluster", Contract: "oracle:v1"}}, MinimumFidelity: worldspec.FidelityLocal, MinimumIsolation: "namespace", CleanupStrategy: worldspec.CleanupDestroy}
	a := []Adapter{{ID: "z-low", ContractVersion: "adapter:v1", ProviderID: "p", ComponentTypes: []string{"kubernetes"}, Capabilities: []string{"mutate"}, MaximumFidelity: worldspec.FidelityLocal, ProfileID: "k8s", IsolationTier: "namespace", Priority: 10}, {ID: "metrics", ContractVersion: "adapter:v1", ProviderID: "p", ComponentTypes: []string{"prometheus"}, MaximumFidelity: worldspec.FidelityReplica, ProfileID: "k8s", IsolationTier: "namespace", Priority: 10}, {ID: "a-high", ContractVersion: "adapter:v1", ProviderID: "p2", ComponentTypes: []string{"kubernetes"}, Capabilities: []string{"mutate"}, MaximumFidelity: worldspec.FidelityReal, ProfileID: "dedicated", IsolationTier: "dedicated_cluster", Priority: 20}}
	return s, a
}

func TestCompileDeterministicCapabilityDrivenAndNoDowngrade(t *testing.T) {
	s, a := fixture()
	first, err := Compile("scenario-v1", s, a)
	if err != nil {
		t.Fatal(err)
	}
	second, err := Compile("scenario-v1", s, append([]Adapter(nil), a...))
	if err != nil {
		t.Fatal(err)
	}
	if first.PlanHash != second.PlanHash {
		t.Fatalf("hash unstable %s %s", first.PlanHash, second.PlanHash)
	}
	if first.Placements[0].AdapterID != "z-low" || first.SelectedFidelity != worldspec.FidelityLocal {
		t.Fatalf("unexpected deterministic placement %#v", first)
	}
	s.MinimumFidelity = worldspec.FidelityReal
	plan, err := Compile("scenario-v1", s, a)
	if err == nil || plan.PlanHash != "" {
		t.Fatal("planner silently downgraded a component")
	}
}

func TestMissingCapabilityUnknownFidelityAndSemanticHash(t *testing.T) {
	s, a := fixture()
	s.Components[0].RequiredCapabilities = []string{"exec-shell"}
	if _, err := Compile("scenario", s, a); err == nil {
		t.Fatal("missing capability accepted")
	}
	s, a = fixture()
	a[0].MaximumFidelity = "mystery"
	a[2].Priority = 5
	if _, err := Compile("scenario", s, a); err == nil {
		t.Fatal("unknown adapter fidelity accepted")
	}
	s, a = fixture()
	p, _ := Compile("scenario", s, a)
	s.Tools[0].Contract = "tools:v2"
	changed, _ := Compile("scenario", s, a)
	if p.PlanHash == changed.PlanHash {
		t.Fatal("tool contract did not change plan hash")
	}
}

func TestAgentProjectionExcludesVerifierTruth(t *testing.T) {
	s, a := fixture()
	p, err := Compile("scenario", s, a)
	if err != nil {
		t.Fatal(err)
	}
	raw, _ := json.Marshal(p.AgentView())
	for _, bad := range []string{"private-truth", "oracle:v1", "verifiers", "lifecycle_order"} {
		if strings.Contains(string(raw), bad) {
			t.Fatalf("projection leaked %q: %s", bad, raw)
		}
	}
}
