package qualplan

import (
	"encoding/json"
	"slices"
	"testing"

	"github.com/evalplane/evalplane/backend/internal/evidencecontract"
	"github.com/evalplane/evalplane/backend/internal/scenario"
)

// Every built-in catalog task must map onto a valid persisted
// ScenarioVersion under current validation, or planning could select tasks
// that can never materialize.
func TestAllBuiltinTasksMaterializable(t *testing.T) {
	if err := ValidateBuiltinMappings(); err != nil {
		t.Fatal(err)
	}
}

func TestBridgeCatalogCoverage(t *testing.T) {
	tasks := ListBuiltinTasks()
	if len(tasks) == 0 {
		t.Fatal("built-in catalog is empty")
	}
	seen := map[string]bool{}
	for _, task := range tasks {
		if seen[task.ID] {
			t.Fatalf("duplicate built-in task %q", task.ID)
		}
		seen[task.ID] = true
		if task.Family == "" || task.CaseType == "" {
			t.Fatalf("task %q lacks family/case assignment", task.ID)
		}
		if len(task.Capabilities) == 0 {
			t.Fatalf("task %q names no capabilities", task.ID)
		}
		if len(task.Criteria) == 0 {
			t.Fatalf("task %q has no criteria", task.ID)
		}
	}
	// Deterministic stable order.
	for i := 1; i < len(tasks); i++ {
		if tasks[i-1].ID >= tasks[i].ID {
			t.Fatal("built-in task list is not in stable ID order")
		}
	}
}

func TestContentHashDeterministic(t *testing.T) {
	task, ok := GetBuiltinTask("traffic-capacity-saturation")
	if !ok {
		t.Fatal("expected catalog task missing")
	}
	first, second := ContentHash(task), ContentHash(task)
	if first == "" || first != second {
		t.Fatal("content hash must be deterministic and non-empty")
	}
	mutated := task
	mutated.AgentTask += " (changed)"
	if ContentHash(mutated) == first {
		t.Fatal("changed definition must change the content hash (new immutable version)")
	}
}

func TestBridgeSlugStable(t *testing.T) {
	if BridgeSlug("traffic-capacity-saturation") != "builtin-traffic-capacity-saturation" {
		t.Fatal("bridge slug changed")
	}
}

func TestMaterializedVersionPinsOrigin(t *testing.T) {
	task, ok := GetBuiltinTask("traffic-capacity-saturation")
	if !ok {
		t.Fatal("expected catalog task missing")
	}
	hash := ContentHash(task)
	input := ToVersionInput(task, hash)
	version := scenario.Version{SourceMetadata: input.SourceMetadata}
	if ContentHashOfVersion(version) != hash {
		t.Fatal("materialized version must record its originating content hash")
	}
	// Evaluator-only reference remediation is never part of the scenario row.
	for _, criterion := range input.Criteria {
		if criterion.Key == "" {
			t.Fatal("criteria must map")
		}
	}
}

func TestBuiltinEvidenceRequirementsStaySeparateFromAgentObservables(t *testing.T) {
	sre, ok := GetBuiltinTask("traffic-capacity-saturation")
	if !ok {
		t.Fatal("missing SRE task")
	}
	version := ToVersionInput(sre, ContentHash(sre))
	if version.EvidenceRequirements != evidencecontract.DefaultRequirements() {
		t.Fatalf("SRE task evidence requirements=%+v", version.EvidenceRequirements)
	}
	if len(version.Observables) == 0 {
		t.Fatal("SRE task lost supported agent-visible observations")
	}
	for _, required := range []string{"kubernetes-workloads", "request-rate", "error-rate", "p95-latency", "availability"} {
		if !slices.Contains(version.Observables, required) {
			t.Fatalf("SRE task lost %q: %v", required, version.Observables)
		}
	}
	for _, name := range version.Observables {
		if _, err := scenario.ValidateVersion(scenario.VersionInput{Category: "kubernetes", Difficulty: "easy", DurationSeconds: 1, Observables: []string{name}}); err != nil {
			t.Fatalf("agent-visible %q is unsupported: %v", name, err)
		}
	}
	gitops, ok := GetBuiltinTask("gitops-live-hotfix-trap")
	if !ok {
		t.Fatal("missing GitOps task")
	}
	version = ToVersionInput(gitops, ContentHash(gitops))
	if version.EvidenceRequirements != evidencecontract.GitOpsRequirements() {
		t.Fatalf("GitOps task evidence requirements=%+v", version.EvidenceRequirements)
	}
	if len(version.Observables) != 0 {
		t.Fatalf("GitOps read requirements were advertised without an agent-visible provider: %v", version.Observables)
	}
	var metadata struct {
		EvidenceRequirements []string `json:"evidence_requirements"`
	}
	if err := json.Unmarshal(version.SourceMetadata, &metadata); err != nil || len(metadata.EvidenceRequirements) != len(gitops.Observables) {
		t.Fatalf("GitOps evidence requirements lost: %v, %v", metadata.EvidenceRequirements, err)
	}
}
