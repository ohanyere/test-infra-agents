package qualplan

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/evalplane/evalplane/backend/internal/environment"
	"github.com/evalplane/evalplane/backend/internal/gitopsqual"
	"github.com/evalplane/evalplane/backend/internal/scenario"
	"github.com/evalplane/evalplane/backend/internal/scenariocertification"
	"github.com/evalplane/evalplane/backend/internal/srequal"
)

// Stage-32 consistency truth: 12 runnable Stage-30 SRE tasks + 13 Stage-31
// GitOps/progressive-delivery tasks = 25 unified bridge tasks. Planned
// dedicated-cluster concepts are never runnable qualification candidates.
var expectedStage31IDs = []string{
	"delivery-analysis-progression-failure",
	"delivery-canary-error-regression",
	"delivery-canary-latency-regression",
	"delivery-dependency-degradation",
	"delivery-healthy-canary-hold",
	"delivery-routing-misconfiguration",
	"gitops-argo-decoy",
	"gitops-argo-sync-failure",
	"gitops-bad-desired-state",
	"gitops-config-drift",
	"gitops-deleted-managed-resource",
	"gitops-live-deployment-drift",
	"gitops-live-hotfix-trap",
}

func TestBridgeConsistencyCounts(t *testing.T) {
	if got := len(srequal.List()); got != 12 {
		t.Fatalf("Stage-30 runnable built-ins = %d, want 12", got)
	}
	if got := len(gitopsqual.List()); got != 13 {
		t.Fatalf("Stage-31 built-ins = %d, want 13", got)
	}
	if got := len(ListBuiltinTasks()); got != 25 {
		t.Fatalf("unified bridge count = %d, want 25", got)
	}
	// Planned dedicated-cluster concepts must stay out of the runnable bridge.
	for _, planned := range srequal.PlannedConcepts() {
		if _, ok := GetBuiltinTask(planned.ID); ok {
			t.Fatalf("planned concept %q must not appear in the runnable bridge", planned.ID)
		}
	}
	// IDs are unique and stably ordered.
	tasks := ListBuiltinTasks()
	seen := map[string]bool{}
	for _, task := range tasks {
		if task.ID == "" {
			t.Fatal("bridge task has empty ID")
		}
		if seen[task.ID] {
			t.Fatalf("duplicate bridge task %q", task.ID)
		}
		seen[task.ID] = true
	}
	for i := 1; i < len(tasks); i++ {
		if tasks[i-1].ID >= tasks[i].ID {
			t.Fatal("bridge task list is not in stable ID order")
		}
	}
}

func TestBridgeConsistencyStage31IDsExact(t *testing.T) {
	tasks := ListBuiltinTasks()
	counts := map[string]int{}
	for _, task := range tasks {
		counts[task.ID]++
	}
	for _, id := range expectedStage31IDs {
		if counts[id] != 1 {
			t.Fatalf("expected Stage-31 task %q appears %d times, want exactly once", id, counts[id])
		}
		if _, ok := gitopsqual.Get(id); !ok {
			t.Fatalf("expected Stage-31 task %q missing from gitopsqual catalog", id)
		}
	}
	// No extra gitopsqual task may hide outside the expected set.
	for _, task := range gitopsqual.List() {
		found := false
		for _, id := range expectedStage31IDs {
			if task.ID == id {
				found = true
				break
			}
		}
		if !found {
			t.Fatalf("gitopsqual task %q is not in the expected Stage-31 ID set", task.ID)
		}
	}
}

func TestBridgeConsistencyAllMaterialize(t *testing.T) {
	if err := ValidateBuiltinMappings(); err != nil {
		t.Fatal(err)
	}
	for _, task := range ListBuiltinTasks() {
		hash := ContentHash(task)
		if hash == "" {
			t.Fatalf("task %q has empty content hash", task.ID)
		}
		if _, err := scenario.ValidateVersion(ToVersionInput(task, hash)); err != nil {
			t.Fatalf("task %q version invalid: %v", task.ID, err)
		}
		if _, err := scenario.ValidateCreate(ToCreateInput(task, hash)); err != nil {
			t.Fatalf("task %q create invalid: %v", task.ID, err)
		}
	}
}

func TestBridgeConsistencyIdempotentMaterialization(t *testing.T) {
	for _, task := range ListBuiltinTasks() {
		firstHash := ContentHash(task)
		secondHash := ContentHash(task)
		if firstHash == "" || firstHash != secondHash {
			t.Fatalf("task %q content hash must be deterministic and non-empty", task.ID)
		}
		firstInput, _ := json.Marshal(ToVersionInput(task, firstHash))
		secondInput, _ := json.Marshal(ToVersionInput(task, secondHash))
		if string(firstInput) != string(secondInput) {
			t.Fatalf("task %q materialization input must be stable across calls", task.ID)
		}
		if BridgeSlug(task.ID) != "builtin-"+task.ID {
			t.Fatalf("task %q slug changed", task.ID)
		}
		version := scenario.Version{SourceMetadata: ToVersionInput(task, firstHash).SourceMetadata}
		if ContentHashOfVersion(version) != firstHash {
			t.Fatalf("task %q materialized version must round-trip its content hash", task.ID)
		}
	}
}

func isGitOpsBridgeTask(task BuiltinTask) bool {
	return strings.HasPrefix(task.ID, "gitops-") || strings.HasPrefix(task.ID, "delivery-")
}

func TestBridgeConsistencyGitOpsRemainUnavailable(t *testing.T) {
	gitopsCount := 0
	for _, task := range ListBuiltinTasks() {
		if !isGitOpsBridgeTask(task) {
			continue
		}
		gitopsCount++
		// Environment requirements must still name the unavailable provider:
		// unavailable does NOT mean omitted.
		components := strings.Join(task.Requirements.RequiredComponents, ",")
		hasUnavailable := strings.Contains(components, "argocd") ||
			strings.Contains(components, "argo_rollouts") ||
			strings.Contains(components, "git")
		if !hasUnavailable {
			t.Fatalf("GitOps task %q requirements %q must name the unavailable provider", task.ID, components)
		}
		// Scheduler truth (same path as Availability): no eligible environment.
		decision := environment.SelectForRequirements(
			context.Background(), task.Requirements, "trial", "", environment.DefaultAvailability())
		if decision.Profile != nil && decision.Provider != nil {
			t.Fatalf("GitOps task %q unexpectedly resolves to %s; must stay unavailable",
				task.ID, decision.Profile.ID)
		}
		// Catalog-level availability agrees.
		catalogTask, ok := gitopsqual.Get(task.ID)
		if !ok {
			t.Fatalf("bridge GitOps task %q missing from gitopsqual catalog", task.ID)
		}
		if runnable, _, _ := catalogTask.Availability(); runnable {
			t.Fatalf("GitOps task %q must remain runnable=false after materialization", task.ID)
		}
		// Materialization must preserve family/case/capabilities so the planner
		// still sees the task as a candidate (unavailable, not omitted).
		if task.Family == "" || task.CaseType == "" || len(task.Capabilities) == 0 {
			t.Fatalf("GitOps task %q lost family/case/capability mapping in the bridge", task.ID)
		}
	}
	if gitopsCount != 13 {
		t.Fatalf("bridge GitOps task count = %d, want 13", gitopsCount)
	}
}

func bridgeCandidatesUncertifiedUnavailable() []Candidate {
	out := []Candidate{}
	for _, task := range ListBuiltinTasks() {
		out = append(out, Candidate{
			BuiltinTaskID:        task.ID,
			ScenarioID:           "11111111-1111-4111-8111-111111111111",
			ScenarioVersionID:    "22222222-2222-4222-8222-222222222222",
			FamilyID:             task.Family,
			CaseType:             task.CaseType,
			Capabilities:         append([]string(nil), task.Capabilities...),
			RuntimeContract:      task.Requirements.RuntimeContract,
			RuntimeAllowed:       true,
			Requires:             []string{"inspect"},
			AgentCompatibleValue: true,
			Certified:            false,
			EnvExecutable:        false,
			EnvReason:            "no_eligible_environment",
			ContentHash:          ContentHash(task),
		})
	}
	return out
}

// The planner must surface every bridge task as a candidate where its
// family/capability applies, yet never manufacture READY from
// unavailable or uncertified tasks. Focused fixture certification passing
// must not count as authoritative eligibility.
func TestBridgeConsistencyPlannerCannotManufactureReady(t *testing.T) {
	// Focused fake certification passes for representative tasks, proving the
	// negative: fixture success is not authoritative eligibility.
	sreTask, ok := srequal.Get("traffic-capacity-saturation")
	if !ok {
		t.Fatal("expected SRE task missing")
	}
	if outcome := sreTask.Certify(); outcome.State != scenariocertification.StateCertified {
		t.Fatalf("expected focused fixture pass for SRE task, got %q", outcome.State)
	}
	gitopsTask, ok := gitopsqual.Get("gitops-live-deployment-drift")
	if !ok {
		t.Fatal("expected GitOps task missing")
	}
	if outcome := gitopsTask.Certify(); outcome.State != scenariocertification.StateCertified {
		t.Fatalf("expected focused fixture pass for GitOps task, got %q", outcome.State)
	}

	requirements := testRequirements()
	candidates := bridgeCandidatesUncertifiedUnavailable()

	// Every bridge family/capability is represented in the candidate pool.
	families := map[string]bool{}
	for _, candidate := range candidates {
		families[candidate.FamilyID] = true
	}
	for _, task := range ListBuiltinTasks() {
		if !families[task.Family] {
			t.Fatalf("bridge task %q family %q has no planner candidate", task.ID, task.Family)
		}
	}

	// GitOps claim over truthfully unavailable + uncertified candidates.
	gitopsOutcome := BuildPlan(PlannerInput{
		AgentVersionID:  "v1",
		DeclaredRuntime: []string{"inspect"},
		Claimed:         []string{"gitops.drift.detection"},
		Requirements:    requirements,
		Candidates:      candidates,
		PolicyAvail:     true,
	})
	if gitopsOutcome.State == StateReady {
		t.Fatal("planner manufactured READY from unavailable/uncertified GitOps tasks")
	}
	assertBlocker := func(outcome PlanOutcome, code string) {
		for _, blocker := range outcome.Blockers {
			if blocker.Code == code {
				return
			}
		}
		t.Fatalf("want %s blocker, got %+v", code, outcome.Blockers)
	}
	assertBlocker(gitopsOutcome, BlockerTaskNotCertified)
	// Environment truth is covered by the fake-certified case below:
	// ineligibility reports the first failing gate per candidate
	// (certification before environment), so a fully uncertified pool
	// surfaces task_not_certified while the certified-but-unavailable pool
	// surfaces environment_unavailable.

	// SRE claim over uncertified candidates is equally blocked.
	sreOutcome := BuildPlan(PlannerInput{
		AgentVersionID:  "v1",
		DeclaredRuntime: []string{"inspect", "kubernetes_workloads", "request_rate", "scale_workload"},
		Claimed:         []string{"kubernetes.capacity.remediation"},
		Requirements:    requirements,
		Candidates:      candidates,
		PolicyAvail:     true,
	})
	if sreOutcome.State == StateReady {
		t.Fatal("planner manufactured READY from uncertified SRE tasks")
	}
	assertBlocker(sreOutcome, BlockerTaskNotCertified)

	// Fake-certified (Certified=true) but environment-unavailable must still
	// never yield READY for a GitOps claim.
	fakeCertified := bridgeCandidatesUncertifiedUnavailable()
	for i := range fakeCertified {
		fakeCertified[i].Certified = true
	}
	fakeOutcome := BuildPlan(PlannerInput{
		AgentVersionID:  "v1",
		DeclaredRuntime: []string{"inspect"},
		Claimed:         []string{"gitops.drift.detection"},
		Requirements:    requirements,
		Candidates:      fakeCertified,
		PolicyAvail:     true,
	})
	if fakeOutcome.State == StateReady {
		t.Fatal("planner manufactured READY from environment-unavailable tasks even when fake-certified")
	}
	assertBlocker(fakeOutcome, BlockerEnvironmentUnavailable)
}

func sourceReferenceJSON(taskID string) string {
	if task, ok := srequal.Get(taskID); ok {
		raw, _ := json.Marshal(task.ReferenceInput())
		return string(raw)
	}
	if task, ok := gitopsqual.Get(taskID); ok {
		raw, _ := json.Marshal(task.ReferenceInput())
		return string(raw)
	}
	return ""
}

func sourceGroundTruth(taskID string) string {
	if task, ok := srequal.Get(taskID); ok {
		return string(task.GroundTruth)
	}
	if task, ok := gitopsqual.Get(taskID); ok {
		return string(task.GroundTruth)
	}
	return ""
}

// Evaluator-only reference remediation stays in the Stage-28 reference table
// and never enters ScenarioVersion agent-visible fields. Ground truth
// persists on the row but is excluded from AgentView by construction.
func TestBridgeConsistencyNoReferenceLeakage(t *testing.T) {
	for _, task := range ListBuiltinTasks() {
		hash := ContentHash(task)
		input := ToVersionInput(task, hash)
		version := scenario.Version{
			AgentTask:           input.AgentTask,
			AgentVisibleContext: input.AgentVisibleContext,
			Observables:         append([]string(nil), input.Observables...),
			AllowedActions:      append([]string(nil), input.AllowedActions...),
			AllowedResources:    input.AllowedResources,
			GroundTruth:         input.GroundTruth,
		}
		viewJSON, _ := json.Marshal(version.AgentView())
		agentSurface := string(viewJSON) + "\x00" + input.AgentTask +
			"\x00" + string(input.AgentVisibleContext) +
			"\x00" + string(input.AllowedResources) +
			"\x00" + strings.Join(input.Observables, ",") +
			"\x00" + strings.Join(input.AllowedActions, ",") +
			"\x00" + string(input.SourceMetadata)

		// Ground truth persists on the row (never agent-visible).
		if len(input.GroundTruth) == 0 || string(input.GroundTruth) == "{}" {
			t.Fatalf("task %q must persist ground truth on the version row", task.ID)
		}
		if groundTruth := sourceGroundTruth(task.ID); groundTruth != "" {
			if strings.Contains(string(viewJSON), "evaluator-only-truth") {
				t.Fatalf("task %q ground-truth marker leaked into AgentView", task.ID)
			}
		}

		// Reference remediation markers must never appear agent-side.
		for _, marker := range []string{"reference-remediation", "VerificationNotes", "verification_notes", "cleanupExpected", "cleanup_expected", "referenceActions", "reference_actions"} {
			if strings.Contains(strings.ToLower(agentSurface), strings.ToLower(marker)) {
				t.Fatalf("task %q evaluator-only reference marker %q in agent surface", task.ID, marker)
			}
		}
		if ref := sourceReferenceJSON(task.ID); ref != "" && ref != "null" && len(ref) > 10 {
			// Reference preconditions/verification notes are distinctive;
			// none of that payload may be embedded agent-side.
			var decoded map[string]any
			if json.Unmarshal([]byte(ref), &decoded) == nil {
				if notes, _ := decoded["verificationNotes"].(string); notes != "" {
					if strings.Contains(agentSurface, notes) {
						t.Fatalf("task %q reference verification notes leaked agent-side", task.ID)
					}
				}
			}
		}

		// Source metadata pins origin without evaluator-only data.
		var meta map[string]any
		if err := json.Unmarshal(input.SourceMetadata, &meta); err != nil {
			t.Fatalf("task %q source metadata invalid: %v", task.ID, err)
		}
		if meta["origin"] != "builtin-catalog-bridge" || meta["builtin_task_id"] != task.ID {
			t.Fatalf("task %q source metadata must pin bridge origin, got %s", task.ID, string(input.SourceMetadata))
		}
		if meta["content_hash"] != hash {
			t.Fatalf("task %q source metadata must pin its content hash", task.ID)
		}
		lowered := strings.ToLower(string(input.SourceMetadata))
		for _, banned := range []string{"reference", "ground_truth", "groundtruth", "evaluator-only"} {
			if strings.Contains(lowered, banned) {
				t.Fatalf("task %q source metadata leaked evaluator-only field %q", task.ID, banned)
			}
		}
	}
}
