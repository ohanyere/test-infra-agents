package worldspec

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/evalplane/evalplane/backend/internal/environment"
	"github.com/evalplane/evalplane/backend/internal/scenario"
)

func validSpec() Spec {
	return Spec{ContractVersion: ContractV1, CompilerID: "test:v1", Components: []Component{{ID: "metrics", Type: "prometheus"}, {ID: "cluster", Type: "kubernetes", Version: "1.34", RequiredCapabilities: []string{"observe", "mutate"}}}, Edges: []Edge{{From: "cluster", To: "metrics", Relationship: "observed_by"}}, Observations: []Binding{{ID: "availability", ComponentID: "metrics", Contract: "metrics:v1"}}, Tools: []Binding{{ID: "scale", ComponentID: "cluster", Contract: "kubernetes-tools:v1"}}, BaselineAssertions: []Assertion{{ID: "ready", ComponentID: "cluster", Contract: "ready:v1"}}, MinimumFidelity: FidelityLocal, MinimumIsolation: "namespace", CleanupStrategy: CleanupDestroy}
}

func TestValidationTopologyAndHash(t *testing.T) {
	s := validSpec()
	a, err := Hash(s)
	if err != nil {
		t.Fatal(err)
	}
	s.Components[1].RequiredCapabilities = []string{"mutate", "observe"}
	b, err := Hash(s)
	if err != nil || a != b {
		t.Fatalf("semantic hash unstable: %s %s %v", a, b, err)
	}
	s.Components[1].Version = "1.35"
	c, _ := Hash(s)
	if c == a {
		t.Fatal("component version did not change hash")
	}
	s = validSpec()
	s.Edges = append(s.Edges, Edge{From: "metrics", To: "cluster", Relationship: "feeds"})
	if _, err := Normalize(s); err == nil || !strings.Contains(err.Error(), "cycle") {
		t.Fatalf("wanted cycle rejection, got %v", err)
	}
	s = validSpec()
	s.Edges[0].To = "missing"
	if _, err := Normalize(s); err == nil {
		t.Fatal("unknown endpoint accepted")
	}
	s = validSpec()
	s.Components = append(s.Components, s.Components[0])
	if _, err := Normalize(s); err == nil {
		t.Fatal("duplicate component accepted")
	}
}

func TestStableTopologicalOrder(t *testing.T) {
	components := []Component{{ID: "z", Type: "x"}, {ID: "a", Type: "x"}, {ID: "m", Type: "x"}}
	order, err := TopologicalOrder(components, nil)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Join(order, ",") != "a,m,z" {
		t.Fatalf("order=%v", order)
	}
}

func TestLegacyCompilerDeterministicAndUnsupportedFails(t *testing.T) {
	v := scenario.Version{ExecutionSpec: json.RawMessage(`{"type":"traffic_spike","profile":{"target":9}}`), Observables: []string{"p95_latency", "availability"}, AllowedActions: []string{"scale_workload", "inspect"}}
	req := environment.LegacyDefaultRequirements()
	a, err := CompileLegacy(v, req)
	if err != nil {
		t.Fatal(err)
	}
	b, err := CompileLegacy(v, req)
	if err != nil {
		t.Fatal(err)
	}
	ha, _ := Hash(a)
	hb, _ := Hash(b)
	if ha != hb || a.CompilerID != LegacyCompilerV1 || a.MinimumFidelity != FidelityLegacy {
		t.Fatalf("legacy output not pinned: %#v", a)
	}
	v.ExecutionSpec = json.RawMessage(`{"type":"gitops"}`)
	if _, err := CompileLegacy(v, req); err == nil {
		t.Fatal("unsupported runtime mapped")
	}
}

func TestAgentProjectionAndSafeRefsDoNotLeak(t *testing.T) {
	s := validSpec()
	s.ScenarioAssertions = []Assertion{{ID: "secret-oracle", ComponentID: "cluster", Contract: "verifier:v1", AgentVisible: false}}
	raw, _ := json.Marshal(s.AgentView())
	for _, forbidden := range []string{"secret-oracle", "verifier:v1", "baseline_assertions", "scenario_assertions"} {
		if strings.Contains(string(raw), forbidden) {
			t.Fatalf("agent view leaked %q: %s", forbidden, raw)
		}
	}
	for _, ref := range []string{"https://user:password@example.test", "k8s://namespace?token=abc", "Bearer abc", "kubeconfig=/tmp/x"} {
		if ValidateSafeRef(ref) == nil {
			t.Fatalf("unsafe ref accepted: %s", ref)
		}
	}
	if err := ValidateSafeRef("k8s://namespace/eval-run"); err != nil {
		t.Fatal(err)
	}
}

func TestInvalidContractsAndLifecycleFailClosed(t *testing.T) {
	s := validSpec()
	s.ContractVersion = "world-spec:v2"
	if _, err := Normalize(s); err == nil {
		t.Fatal("unknown contract accepted")
	}
	s = validSpec()
	s.CleanupStrategy = "reuse-maybe"
	if _, err := Normalize(s); err == nil {
		t.Fatal("cleanup accepted")
	}
	s = validSpec()
	s.MinimumFidelity = "premium"
	if _, err := Normalize(s); err == nil {
		t.Fatal("fidelity accepted")
	}
	s = validSpec()
	s.Initializers = []Binding{{ID: "init", ComponentID: "missing", Contract: "init:v1"}}
	if _, err := Normalize(s); err == nil {
		t.Fatal("unknown lifecycle component accepted")
	}
}
