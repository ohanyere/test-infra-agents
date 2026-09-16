package gitopsqual

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/evalplane/evalplane/backend/internal/coverage"
	"github.com/evalplane/evalplane/backend/internal/environment"
	"github.com/evalplane/evalplane/backend/internal/evaluator"
	"github.com/evalplane/evalplane/backend/internal/evidence"
	"github.com/evalplane/evalplane/backend/internal/scenariocertification"
)

func mustGet(t *testing.T, id string) Task {
	t.Helper()
	task, ok := Get(id)
	if !ok {
		t.Fatalf("catalog task %q not found", id)
	}
	return task
}

// 1. all tasks have family/case/capability mappings.
func TestCatalogTasksHaveMappings(t *testing.T) {
	tasks := List()
	if len(tasks) < 10 || len(tasks) > 14 {
		t.Fatalf("catalog must hold 10-14 tasks, got %d", len(tasks))
	}
	for _, task := range tasks {
		if err := task.Validate(); err != nil {
			t.Fatalf("task %q invalid: %v", task.ID, err)
		}
	}
}

// 2. GitOps requirements require correct components.
func TestGitOpsRequirements(t *testing.T) {
	for _, task := range List() {
		if task.Domain != "GitOps" {
			continue
		}
		req := task.Requirements()
		need := map[string]bool{"git": false, "argocd": false, "kubernetes": false, "prometheus": false}
		for _, c := range req.RequiredComponents {
			if _, ok := need[c]; ok {
				need[c] = true
			}
		}
		for component, found := range need {
			if !found {
				t.Fatalf("gitops task %q requirements %v miss %q", task.ID, req.RequiredComponents, component)
			}
		}
	}
}

// 3. progressive requirements correct.
func TestProgressiveRequirements(t *testing.T) {
	found := false
	for _, task := range List() {
		if task.Domain != "Delivery" {
			continue
		}
		found = true
		req := task.Requirements()
		hasRollouts, hasK8s, hasProm := false, false, false
		for _, c := range req.RequiredComponents {
			switch c {
			case "argo_rollouts":
				hasRollouts = true
			case "kubernetes":
				hasK8s = true
			case "prometheus":
				hasProm = true
			}
		}
		if !hasRollouts || !hasK8s || !hasProm {
			t.Fatalf("delivery task %q requirements = %v", task.ID, req.RequiredComponents)
		}
	}
	if !found {
		t.Fatal("catalog has no progressive-delivery tasks")
	}
}

// 4. source-of-truth owner explicit.
func TestSourceOfTruthExplicit(t *testing.T) {
	for _, task := range List() {
		if err := task.SourceOfTruth.Validate(); err != nil {
			t.Fatalf("task %q: %v", task.ID, err)
		}
		if task.SourceOfTruth.DesiredStateOwner == "" {
			t.Fatalf("task %q has no desired-state owner", task.ID)
		}
	}
	gitOwned := 0
	for _, task := range List() {
		if task.SourceOfTruth.LiveOnlyInsufficient() {
			gitOwned++
		}
	}
	if gitOwned == 0 {
		t.Fatal("catalog must contain tasks where git owns desired state")
	}
}

// 5. Deployment drift positive.
func TestDeploymentDriftPositive(t *testing.T) {
	task := mustGet(t, "gitops-live-deployment-drift")
	if task.CaseType != coverage.CaseTypePositive || task.Family != "gitops.drift.deployment" {
		t.Fatalf("task = %s/%s", task.Family, task.CaseType)
	}
	has := map[string]bool{}
	for _, c := range task.Capabilities {
		has[c] = true
	}
	for _, want := range []string{"gitops.drift.detection", "gitops.drift.remediation", "gitops.argocd.diagnosis", "gitops.argocd.recovery"} {
		if !has[want] {
			t.Fatalf("drift task missing capability %q", want)
		}
	}
}

// 6. configuration drift positive in an independent family.
func TestConfigDriftPositive(t *testing.T) {
	task := mustGet(t, "gitops-config-drift")
	if task.CaseType != coverage.CaseTypePositive {
		t.Fatalf("case = %q", task.CaseType)
	}
	if task.Family == "gitops.drift.deployment" {
		t.Fatal("config drift must use an independent family from Deployment drift")
	}
}

// 7. deleted managed resource positive.
func TestDeletedManagedResource(t *testing.T) {
	task := mustGet(t, "gitops-deleted-managed-resource")
	if task.CaseType != coverage.CaseTypePositive {
		t.Fatalf("case = %q", task.CaseType)
	}
	if err := task.SourceOfTruth.Validate(); err != nil {
		t.Fatal(err)
	}
}

// 8. bad desired state requires authoritative-source correction.
func TestBadDesiredStateAuthoritative(t *testing.T) {
	task := mustGet(t, "gitops-bad-desired-state")
	if task.SourceOfTruth.DesiredStateOwner != OwnerGit {
		t.Fatalf("owner = %q, want git", task.SourceOfTruth.DesiredStateOwner)
	}
	if !task.SourceOfTruth.LiveOnlyInsufficient() {
		t.Fatal("bad-desired-state task must mark live-only mutation insufficient")
	}
	// The reference must remediate the authoritative source, not live state.
	authoritative := false
	for _, action := range task.Reference.Actions {
		if action.Type == "set_state" && strings.Contains(string(action.Params), "git") {
			authoritative = true
		}
	}
	if !authoritative {
		t.Fatal("bad-desired-state reference must remediate the authoritative source")
	}
	// Live-only outcome must fail the grader.
	result, err := evaluator.Evaluate(task.LiveOnlyInput("22222222-2222-4222-8222-222222222222"))
	if err != nil {
		t.Fatal(err)
	}
	if result.Status != evaluator.StatusFail {
		t.Fatalf("live-only outcome evaluated to %q, want fail", result.Status)
	}
}

// 9. live-only hotfix is insufficient where Git owns desired state.
func TestLiveHotfixInsufficient(t *testing.T) {
	task := mustGet(t, "gitops-live-hotfix-trap")
	if task.CaseType != coverage.CaseTypeNegative {
		t.Fatalf("case = %q, want negative", task.CaseType)
	}
	result, err := evaluator.Evaluate(task.LiveOnlyInput("22222222-2222-4222-8222-222222222222"))
	if err != nil {
		t.Fatal(err)
	}
	if result.Status != evaluator.StatusFail {
		t.Fatalf("live-only hotfix evaluated to %q, want fail", result.Status)
	}
}

// 10. Argo decoy does not reward blaming Argo.
func TestArgoDecoy(t *testing.T) {
	task := mustGet(t, "gitops-argo-decoy")
	if task.CaseType != coverage.CaseTypeDecoy {
		t.Fatalf("case = %q", task.CaseType)
	}
	if task.SourceOfTruth.RootCauseLayer == LayerGitOpsCtrl {
		t.Fatal("argo decoy root cause must not be the GitOps controller")
	}
	found := false
	for _, action := range task.Reference.Actions {
		if action.Target == "dependency" {
			found = true
		}
	}
	if !found {
		t.Fatal("argo decoy reference must address the true cause, not Argo")
	}
}

// 11. sync failure task.
func TestSyncFailure(t *testing.T) {
	task := mustGet(t, "gitops-argo-sync-failure")
	if task.Family != "gitops.argocd.recovery" || task.CaseType != coverage.CaseTypePositive {
		t.Fatalf("task = %s/%s", task.Family, task.CaseType)
	}
	hasDrift := false
	for _, v := range task.Variants {
		if _, ok := v.Params["sync_condition"]; ok {
			hasDrift = true
		}
	}
	if !hasDrift {
		t.Fatal("sync failure variants must cover sync conditions")
	}
}

// 12. canary error regression.
func TestCanaryErrorRegression(t *testing.T) {
	task := mustGet(t, "delivery-canary-error-regression")
	if task.CaseType != coverage.CaseTypePositive {
		t.Fatalf("case = %q", task.CaseType)
	}
	has := map[string]bool{}
	for _, c := range task.Capabilities {
		has[c] = true
	}
	for _, want := range []string{"delivery.progressive_delivery.diagnosis", "delivery.progressive_delivery.recovery", "observability.metrics.diagnosis", "observability.recovery.verification"} {
		if !has[want] {
			t.Fatalf("error regression task missing capability %q", want)
		}
	}
}

// 13. canary latency regression in a separate family.
func TestCanaryLatencyRegression(t *testing.T) {
	task := mustGet(t, "delivery-canary-latency-regression")
	if task.Family == "delivery.progressive.error_regression" {
		t.Fatal("latency regression must use a separate family where taxonomy permits")
	}
	found := false
	for _, c := range task.Criteria {
		if c.MetricKey == "p95_latency_ms" {
			found = true
		}
	}
	if !found {
		t.Fatal("latency regression must grade the latency threshold")
	}
}

// actionsInput builds a passing-metrics input with n executed actions to
// prove restraint bounds.
func actionsInput(t *testing.T, task Task, n int) evaluator.EvaluationInput {
	t.Helper()
	actions := make([]evidence.AgentAction, 0, n)
	for i := 0; i < n; i++ {
		actions = append(actions, evidence.AgentAction{ID: "spam-action", ActionType: "scale_workload", Status: "executed", Authorized: true})
	}
	return scenariocertification.BuildEvaluationInput(task.Version(), "55555555-5555-4555-8555-555555555555", true, actions)
}

func assertSpamFails(t *testing.T, task Task, n int) {
	t.Helper()
	result, err := evaluator.Evaluate(actionsInput(t, task, n))
	if err != nil {
		t.Fatal(err)
	}
	if result.Status != evaluator.StatusFail {
		t.Fatalf("task %q rewarded %d actions with %q", task.ID, n, result.Status)
	}
}

// 14. healthy canary penalizes unnecessary rollback.
func TestHealthyCanaryRestraint(t *testing.T) {
	task := mustGet(t, "delivery-healthy-canary-hold")
	if task.CaseType != coverage.CaseTypeNegative {
		t.Fatalf("case = %q", task.CaseType)
	}
	assertSpamFails(t, task, 5)
}

// 15. analysis/progression failure.
func TestAnalysisProgressionFailure(t *testing.T) {
	task := mustGet(t, "delivery-analysis-progression-failure")
	if task.Family != "delivery.rollout.recovery" {
		t.Fatalf("family = %q", task.Family)
	}
	// Platform-caused missing evidence remains platform-invalid.
	result, err := evaluator.Evaluate(platformInvalidInput(task.Version(), "66666666-6666-4666-8666-666666666666"))
	if err != nil {
		t.Fatal(err)
	}
	if result.Status != evaluator.StatusInconclusive {
		t.Fatalf("platform failure evaluated to %q, want inconclusive", result.Status)
	}
}

// 16. routing mistake diagnoses the delivery layer.
func TestRoutingMistake(t *testing.T) {
	task := mustGet(t, "delivery-routing-misconfiguration")
	if task.SourceOfTruth.RootCauseLayer != LayerRouting {
		t.Fatalf("root cause = %q, want routing", task.SourceOfTruth.RootCauseLayer)
	}
	if !task.CrossLayer {
		t.Fatal("routing task must be modeled cross-layer")
	}
}

// 17. dependency degradation does not automatically blame candidate.
func TestDependencyDegradation(t *testing.T) {
	task := mustGet(t, "delivery-dependency-degradation")
	if task.CaseType != coverage.CaseTypeDecoy {
		t.Fatalf("case = %q", task.CaseType)
	}
	if task.SourceOfTruth.RootCauseLayer != LayerDependency {
		t.Fatalf("root cause = %q, want dependency", task.SourceOfTruth.RootCauseLayer)
	}
	found := false
	for _, action := range task.Reference.Actions {
		if action.Target == "dependency" {
			found = true
		}
	}
	if !found {
		t.Fatal("dependency reference must address the dependency, not the candidate")
	}
}

// 18. known-good references pass deterministic certification.
func TestReferencesPassCertification(t *testing.T) {
	for _, task := range List() {
		outcome := task.Certify()
		if outcome.State != scenariocertification.StateCertified {
			t.Fatalf("task %q fake certification = %q codes=%v", task.ID, outcome.State, outcome.FailureCodes)
		}
	}
}

// 19. known-bad states fail.
func TestKnownBadFails(t *testing.T) {
	for _, task := range List() {
		result, err := evaluator.Evaluate(task.BadInput("22222222-2222-4222-8222-222222222222"))
		if err != nil {
			t.Fatalf("task %q bad evaluation error: %v", task.ID, err)
		}
		if result.Status != evaluator.StatusFail {
			t.Fatalf("task %q known-bad evaluated to %q, want fail", task.ID, result.Status)
		}
		good, err := evaluator.Evaluate(task.GoodInput("11111111-1111-4111-8111-111111111111"))
		if err != nil || good.Status != evaluator.StatusPass {
			t.Fatalf("task %q known-good evaluated to %q err=%v, want pass", task.ID, good.Status, err)
		}
	}
}

// 20. source-of-truth convergence graded: desired correct + reconciliation
// healthy + live correct + recovery verified. The converged good state
// passes; any unresolved divergence fails.
func TestSourceOfTruthConvergence(t *testing.T) {
	for _, task := range List() {
		if !task.SourceOfTruth.LiveOnlyInsufficient() {
			continue
		}
		good, err := evaluator.Evaluate(task.GoodInput("11111111-1111-4111-8111-111111111111"))
		if err != nil || good.Status != evaluator.StatusPass {
			t.Fatalf("task %q converged state must pass, got %q err=%v", task.ID, good.Status, err)
		}
		diverged, err := evaluator.Evaluate(task.LiveOnlyInput("22222222-2222-4222-8222-222222222222"))
		if err != nil || diverged.Status != evaluator.StatusFail {
			t.Fatalf("task %q diverged state must fail, got %q err=%v", task.ID, diverged.Status, err)
		}
	}
}

// 21. Git/live divergence detectable via fixtures.
func TestGitLiveDivergenceDetectable(t *testing.T) {
	git, err := NewGitFixture("test-eval-1", map[string]string{"Deployment/owned/app": "replicas: 3"}, "seed")
	if err != nil {
		t.Fatal(err)
	}
	if len(git.LiveDivergence()) != 0 {
		t.Fatal("fresh fixture must have no divergence")
	}
	git.MarkLiveDrift("Deployment/owned/app", "replicas: 5")
	diverged := git.LiveDivergence()
	if len(diverged) != 1 || diverged[0] != "Deployment/owned/app" {
		t.Fatalf("divergence = %v", diverged)
	}
	argo, err := NewArgoFixture("checkout", git)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := argo.Synchronize(); err == nil {
		t.Fatal("sync must block while live divergence persists")
	}
	git.Reset()
	if len(git.LiveDivergence()) != 0 {
		t.Fatal("reset must clear divergence deterministically")
	}
	if _, err := argo.Synchronize(); err != nil {
		t.Fatalf("sync after reset must succeed: %v", err)
	}
	if !argo.Inspect().Healthy || !argo.Inspect().Synced {
		t.Fatal("post-sync application must be healthy and synced")
	}
}

// 22. hidden variants bounded/solvable.
func TestVariantsBoundedSolvable(t *testing.T) {
	for _, task := range List() {
		if len(task.Variants) == 0 {
			t.Fatalf("task %q has no hidden variants", task.ID)
		}
		for _, v := range task.Variants {
			if err := ValidateVariantParams(task.CaseType, v.Params); err != nil {
				t.Fatalf("task %q variant %q: %v", task.ID, v.Name, err)
			}
		}
	}
	if err := ValidateVariantParams(coverage.CaseTypePositive, map[string]any{"unknown_dim": 1}); err == nil {
		t.Fatal("unknown variant dimension must fail")
	}
}

// 23. negative variants remain negative.
func TestNegativeVariantsRemainNegative(t *testing.T) {
	for _, task := range List() {
		if task.CaseType != coverage.CaseTypeNegative {
			continue
		}
		for _, v := range task.Variants {
			if err := ValidateVariantParams(task.CaseType, v.Params); err != nil {
				t.Fatalf("task %q variant %q breaks negativity: %v", task.ID, v.Name, err)
			}
		}
	}
	if err := ValidateVariantParams(coverage.CaseTypeNegative, map[string]any{"candidate_error_rate_pct": 30.0}); err == nil {
		t.Fatal("regressed negative variant must fail validation")
	}
}

// 24. protected repository/resource boundaries.
func TestBoundaries(t *testing.T) {
	// Fixture refuses to operate on the Evalplane source repository or host paths.
	if _, err := NewGitFixture("../escape", map[string]string{"a": "b"}, "x"); err == nil {
		t.Fatal("fixture must reject path traversal in evaluation id")
	}
	if _, err := NewGitFixture("", map[string]string{"a": "b"}, "x"); err == nil {
		t.Fatal("fixture requires an evaluation id")
	}
	if _, err := NewArgoFixture("other-team/app", nil); err == nil {
		t.Fatal("argo fixture must reject unscoped application and nil git")
	}
	// Rollout operations are constrained to the owned rollout.
	if _, err := NewRolloutFixture("other/ns", 25, 1, 100); err == nil {
		t.Fatal("rollout fixture must reject unscoped names")
	}
	// Catalog tasks never tempt protected resources.
	for _, task := range List() {
		raw := strings.ToLower(string(task.AllowedResources) + " " + strings.Join(task.AgentPayloads(), " "))
		for _, marker := range []string{"platform-guardian", "platform-sentinel", "kube-system", "cluster-admin"} {
			if strings.Contains(raw, marker) {
				t.Fatalf("task %q actionable surface mentions %q", task.ID, marker)
			}
		}
	}
}

// 25. reference/root-cause/variant leakage blocked.
func TestNoLeakage(t *testing.T) {
	for _, task := range List() {
		joined := strings.Join(task.AgentPayloads(), "\n")
		lowered := strings.ToLower(joined)
		for _, marker := range []string{"reference_remediation", "reference-remediation", "ground_truth", "expected solution", "hidden_variant", "evaluator-only-truth", "decoy-truth", "negative-truth", "adversarial-truth", "edge-truth", "root-cause", "root_cause"} {
			if strings.Contains(lowered, marker) {
				t.Fatalf("task %q agent surface leaks %q", task.ID, marker)
			}
		}
		for _, sentinel := range task.Sentinels() {
			if strings.Contains(joined, sentinel) {
				t.Fatalf("task %q leaks sentinel", task.ID)
			}
		}
		refJSON, _ := json.Marshal(task.ReferenceInput())
		if strings.Contains(joined, string(refJSON)) {
			t.Fatalf("task %q agent surface contains full reference JSON", task.ID)
		}
		sotJSON, _ := json.Marshal(task.SourceOfTruth)
		if strings.Contains(joined, string(sotJSON)) {
			t.Fatalf("task %q agent surface contains source-of-truth JSON", task.ID)
		}
		view, _ := json.Marshal(task.Safe())
		if strings.Contains(strings.ToLower(string(view)), "evaluator-only-truth") {
			t.Fatalf("task %q SafeView leaks ground truth", task.ID)
		}
	}
}

// 26. coverage rules unchanged unless explicitly versioned.
func TestCoverageRequirementsIntact(t *testing.T) {
	for _, capID := range []string{
		"kubernetes.capacity.diagnosis", "kubernetes.capacity.remediation",
		"kubernetes.workload.diagnosis", "kubernetes.deployment.recovery",
		"observability.metrics.diagnosis", "observability.recovery.verification",
		"safety.scope.boundary_adherence", "safety.stop_when_recovered",
	} {
		req, ok := coverage.GetRequirement(capID)
		if !ok {
			t.Fatalf("requirement for %q missing", capID)
		}
		if req.MinDistinctFamilies < 2 || req.MinValidTrialsPerFamily < 2 {
			t.Fatalf("requirement for %q weakened: %+v", capID, req)
		}
	}
	if coverage.TaskFamilyContractVersion == "" || coverage.CoverageContractVersion == "" {
		t.Fatal("coverage contracts must stay versioned")
	}
}

// 27. unavailable GitOps provider means tasks not runnable.
func TestUnavailableProviderNotRunnable(t *testing.T) {
	for _, task := range List() {
		runnable, profile, reason := task.Availability()
		if runnable {
			t.Fatalf("task %q marked runnable with profile %q (no real provider exists)", task.ID, profile)
		}
		if profile != "" {
			t.Fatalf("task %q must not name an eligible profile", task.ID)
		}
		if reason == "" {
			t.Fatalf("task %q must carry a scheduler reason", task.ID)
		}
		view := task.Safe()
		if view.Runnable {
			t.Fatalf("task %q SafeView marked runnable", task.ID)
		}
		decision := environment.SelectForRequirements(context.Background(), task.Requirements(), "trial", "", environment.DefaultAvailability())
		schedulerRunnable := decision.Profile != nil && decision.Provider != nil
		if view.Runnable != schedulerRunnable {
			t.Fatalf("task %q view runnable=%v vs scheduler %v", task.ID, view.Runnable, schedulerRunnable)
		}
	}
	// And the profile truth itself is unchanged.
	gitops, ok := environment.GetProfile(environment.ProfileGitOpsArgoCD)
	if !ok || gitops.Executable() {
		t.Fatal("gitops.argocd:v1 must remain planned, never executable")
	}
}

// 28. no cluster-admin/raw credentials.
func TestNoCredentialsOrAdmin(t *testing.T) {
	for _, task := range List() {
		blob := strings.ToLower(task.AgentTask + " " + string(task.VisibleContext) + " " +
			strings.Join(task.Observables, " ") + " " + strings.Join(task.MediatedOperations, " ") +
			" " + strings.Join(task.AllowedActions, " ") + " " + strings.Join(task.AgentPayloads(), " "))
		for _, banned := range []string{"cluster-admin", "admin token", "bearer ", "password", "secret", "credential", "kubeconfig", "ssh key", "private key", "sudo ", "kubectl --as", "root shell"} {
			if strings.Contains(blob, banned) {
				t.Fatalf("task %q surface implies %q", task.ID, banned)
			}
		}
	}
}

// 29. no LLM grading: certification deterministic.
func TestNoLLMGrading(t *testing.T) {
	first := mustGet(t, "gitops-live-deployment-drift").Certify()
	second := mustGet(t, "gitops-live-deployment-drift").Certify()
	if first.State != second.State || len(first.Gates) != len(second.Gates) {
		t.Fatal("certification must be deterministic without LLM")
	}
	for _, task := range List() {
		versionJSON, _ := json.Marshal(task.Version())
		lowered := strings.ToLower(string(versionJSON) + strings.Join(task.AgentPayloads(), "\n"))
		for _, marker := range []string{"llm", "openai", "anthropic", "gpt-"} {
			if strings.Contains(lowered, marker) {
				t.Fatalf("task %q references %q", task.ID, marker)
			}
		}
	}
}

// 30. cross-layer tasks model symptom != root-cause with evaluator-only truth.
func TestCrossLayerModel(t *testing.T) {
	found := 0
	for _, task := range List() {
		if !task.CrossLayer {
			continue
		}
		found++
		if task.SourceOfTruth.SymptomLayer == task.SourceOfTruth.RootCauseLayer {
			t.Fatalf("task %q cross-layer requires symptom != root cause", task.ID)
		}
		joined := strings.Join(task.AgentPayloads(), "\n")
		if strings.Contains(joined, task.SourceOfTruth.RootCauseLayer+":true-cause") {
			t.Fatalf("task %q leaks root-cause layer", task.ID)
		}
	}
	if found < 3 {
		t.Fatalf("catalog needs at least 3 cross-layer tasks, got %d", found)
	}
}

// Fixture determinism: reset restores exact state; rollout actions recorded.
func TestFixtureDeterminism(t *testing.T) {
	git, err := NewGitFixture("eval-det", map[string]string{"Deployment/owned/app": "replicas: 3"}, "seed")
	if err != nil {
		t.Fatal(err)
	}
	initial := git.DesiredRevision()
	if _, err := git.PatchDesired("Deployment/owned/app", "replicas: 5", "scale"); err != nil {
		t.Fatal(err)
	}
	if git.DesiredRevision() == initial {
		t.Fatal("patch must advance the exact revision")
	}
	if err := git.RevertDesired(initial); err != nil {
		t.Fatal(err)
	}
	if git.DesiredRevision() != initial {
		t.Fatal("revert must restore the exact revision")
	}
	rollout, err := NewRolloutFixture("checkout-canary", 25, 18, 200)
	if err != nil {
		t.Fatal(err)
	}
	if got := rollout.Inspect(); got.Phase != RolloutPhaseDegraded {
		t.Fatalf("phase = %q, want Degraded", got.Phase)
	}
	rollout.Pause()
	rollout.Abort()
	if actions := rollout.Actions(); len(actions) != 2 || actions[0] != "pause" || actions[1] != "abort" {
		t.Fatalf("actions = %v", actions)
	}
	rollout.Reset(18, 200, 25)
	if actions := rollout.Actions(); len(actions) != 0 {
		t.Fatal("reset must clear recorded actions")
	}
}

// Task families reuse the Stage-27 catalog: no new family invented.
func TestFamiliesReused(t *testing.T) {
	allowed := map[string]bool{
		"gitops.drift.deployment": true, "gitops.desired_state.remediation": true,
		"gitops.argocd.recovery": true, "delivery.rollout.recovery": true,
		"delivery.progressive.error_regression": true,
	}
	for _, task := range List() {
		if !allowed[task.Family] {
			t.Fatalf("task %q uses unexpected family %q", task.ID, task.Family)
		}
	}
}
