package srequal

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

// 1. every built-in runnable task has a Task Family.
func TestCatalogTasksHaveFamilies(t *testing.T) {
	tasks := List()
	if len(tasks) < 10 || len(tasks) > 14 {
		t.Fatalf("runnable catalog must hold 10-14 tasks, got %d", len(tasks))
	}
	for _, task := range tasks {
		if _, ok := coverage.GetTaskFamily(task.Family); !ok {
			t.Fatalf("task %q has unknown family %q", task.ID, task.Family)
		}
	}
}

// 2. every task has explicit case type.
func TestCatalogTasksHaveCaseTypes(t *testing.T) {
	for _, task := range List() {
		if _, err := coverage.ValidateCaseType(task.CaseType); err != nil {
			t.Fatalf("task %q: %v", task.ID, err)
		}
	}
}

// 3. every runnable task has environment requirements.
func TestCatalogTasksHaveRequirements(t *testing.T) {
	for _, task := range List() {
		req := task.Requirements()
		if _, err := environment.ValidateRequirements(environment.Requirements{
			RequiredComponents: req.RequiredComponents,
			MinimumIsolation:   req.MinimumIsolation,
			RuntimeContract:    req.RuntimeContract,
		}); err != nil {
			t.Fatalf("task %q requirements invalid: %v", task.ID, err)
		}
	}
}

// 4. every runnable task resolves to an active profile.
func TestCatalogTasksResolveToActiveProfile(t *testing.T) {
	for _, task := range List() {
		decision := environment.SelectForRequirements(context.Background(), task.Requirements(), "trial", "", environment.DefaultAvailability())
		if decision.Profile == nil || decision.Provider == nil {
			t.Fatalf("task %q has no eligible environment: %q", task.ID, decision.ReasonCode)
		}
		profile, ok := environment.GetProfile(decision.Profile.ID)
		if !ok || !profile.Executable() {
			t.Fatalf("task %q resolved to non-executable profile %q", task.ID, decision.Profile.ID)
		}
		if decision.Profile.ID != environment.ProfileKubernetesNamespace {
			t.Fatalf("task %q resolved to %q, want kubernetes.namespace:v1", task.ID, decision.Profile.ID)
		}
	}
}

// 5. no dedicated-only task resolves to namespace.
func TestCatalogPlannedNeverResolvesToNamespace(t *testing.T) {
	for _, task := range PlannedConcepts() {
		req := task.Requirements()
		if req.MinimumIsolation != environment.IsolationDedicatedCluster {
			t.Fatalf("planned task %q must require dedicated_cluster", task.ID)
		}
		decision := environment.SelectForRequirements(context.Background(), req, "trial", "", environment.DefaultAvailability())
		if decision.Profile != nil {
			t.Fatalf("planned task %q must never resolve, got %s", task.ID, decision.Profile.ID)
		}
		if decision.ReasonCode != environment.ReasonNoEligibleEnvironment {
			t.Fatalf("planned task %q reason = %q", task.ID, decision.ReasonCode)
		}
	}
}

// 6. every runnable task has deterministic criteria.
func TestCatalogTasksHaveDeterministicCriteria(t *testing.T) {
	for _, task := range List() {
		if len(task.Criteria) == 0 {
			t.Fatalf("task %q has no criteria", task.ID)
		}
		outcome := task.Certify()
		for _, gate := range outcome.Gates {
			if gate.Gate == scenariocertification.GateCriteriaValidation && gate.Status != scenariocertification.GatePassed {
				t.Fatalf("task %q criteria gate: %s", task.ID, gate.Detail)
			}
		}
	}
}

// 7. every runnable task has reference remediation.
func TestCatalogTasksHaveReferenceRemediation(t *testing.T) {
	for _, task := range List() {
		ref := task.ReferenceInput()
		if _, err := scenariocertification.ValidateReferenceInput(ref); err != nil {
			t.Fatalf("task %q reference invalid: %v", task.ID, err)
		}
	}
}

// 8. reference remediation evaluator-only.
func TestCatalogReferenceEvaluatorOnly(t *testing.T) {
	for _, task := range List() {
		joined := strings.Join(task.AgentPayloads(), "\n")
		lowered := strings.ToLower(joined)
		for _, marker := range []string{"reference_remediation", "reference-remediation", "ground_truth", "expected solution", "hidden_variant", "evaluator-only-truth", "decoy-truth", "negative-truth", "adversarial-truth", "edge-truth"} {
			if strings.Contains(lowered, marker) {
				t.Fatalf("task %q agent surface leaks %q", task.ID, marker)
			}
		}
		refJSON, _ := json.Marshal(task.ReferenceInput())
		if strings.Contains(joined, string(refJSON)) {
			t.Fatalf("task %q agent surface contains full reference JSON", task.ID)
		}
	}
}

// 9. every reference remediation passes deterministic fake certification.
func TestCatalogTasksPassFakeCertification(t *testing.T) {
	for _, task := range List() {
		outcome := task.Certify()
		if outcome.State != scenariocertification.StateCertified {
			t.Fatalf("task %q fake certification = %q codes=%v", task.ID, outcome.State, outcome.FailureCodes)
		}
	}
}

// 10. known-bad state fails.
func TestCatalogKnownBadFails(t *testing.T) {
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

// 11. positive/negative/decoy/adversarial catalog balance exists.
func TestCatalogBalance(t *testing.T) {
	positive, contrast, edge := 0, 0, 0
	for _, task := range List() {
		switch task.CaseType {
		case coverage.CaseTypePositive:
			positive++
		case coverage.CaseTypeNegative, coverage.CaseTypeDecoy:
			contrast++
		case coverage.CaseTypeAdversarial, coverage.CaseTypeEdge:
			if task.CaseType == coverage.CaseTypeAdversarial {
				contrast++
			} else {
				edge++
			}
		}
	}
	total := len(List())
	if positive*100/total < 40 || positive*100/total > 65 {
		t.Fatalf("positive share = %d/%d, want ~50-60%%", positive, total)
	}
	if contrast == 0 || edge == 0 {
		t.Fatalf("catalog needs contrast and edge cases: positive=%d contrast=%d edge=%d", positive, contrast, edge)
	}
}

// 12. capacity positive task.
func TestCatalogCapacityPositive(t *testing.T) {
	task := mustGet(t, "traffic-capacity-saturation")
	if task.CaseType != coverage.CaseTypePositive || task.Family != "kubernetes.capacity.traffic_spike" {
		t.Fatalf("task = %+v", task.ID)
	}
	has := map[string]bool{}
	for _, c := range task.Capabilities {
		has[c] = true
	}
	for _, want := range []string{"kubernetes.capacity.diagnosis", "kubernetes.capacity.remediation", "observability.metrics.diagnosis", "observability.recovery.verification"} {
		if !has[want] {
			t.Fatalf("capacity task missing capability %q", want)
		}
	}
}

// actionsInput builds a passing-metrics input with n executed actions to prove
// restraint bounds: mutation spam must fail even when metrics look healthy.
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

// 13. capacity decoy task does not reward unnecessary scale.
func TestCatalogCapacityDecoyRestraint(t *testing.T) {
	task := mustGet(t, "capacity-decoy-no-scale-needed")
	if task.CaseType != coverage.CaseTypeDecoy {
		t.Fatalf("case = %q", task.CaseType)
	}
	assertSpamFails(t, task, 5)
}

// 14. deployment failure task.
func TestCatalogDeploymentFailure(t *testing.T) {
	task := mustGet(t, "deployment-readiness-failure")
	if task.Family != "kubernetes.deployment.recovery" || task.CaseType != coverage.CaseTypePositive {
		t.Fatalf("task = %s/%s", task.Family, task.CaseType)
	}
	found := false
	for _, c := range task.Criteria {
		if c.MetricKey == "deployment_available_replicas" || c.MetricKey == "pod_ready_count" {
			found = true
		}
	}
	if !found {
		t.Fatal("deployment task must grade readiness state")
	}
}

// 15. healthy/no-rollback negative task.
func TestCatalogHealthyNoRollback(t *testing.T) {
	task := mustGet(t, "healthy-rollout-do-not-rollback")
	if task.CaseType != coverage.CaseTypeNegative {
		t.Fatalf("case = %q", task.CaseType)
	}
	assertSpamFails(t, task, 5)
}

// 16. dependency outage task.
func TestCatalogDependencyOutage(t *testing.T) {
	task := mustGet(t, "dependency-outage")
	if task.Family != "kubernetes.networking.dependency" || task.CaseType != coverage.CaseTypePositive {
		t.Fatalf("task = %s/%s", task.Family, task.CaseType)
	}
	if task.Injection.Dependency == nil || !task.Injection.Dependency.Unavailable {
		t.Fatal("dependency outage must declare an unavailable owned dependency")
	}
}

// 17. dependency decoy.
func TestCatalogDependencyDecoy(t *testing.T) {
	task := mustGet(t, "dependency-decoy")
	if task.CaseType != coverage.CaseTypeDecoy {
		t.Fatalf("case = %q", task.CaseType)
	}
	if task.Injection.Dependency != nil && task.Injection.Dependency.Unavailable {
		t.Fatal("decoy dependency must not be the unavailable cause")
	}
}

// 18. workload resource-pressure task truthfully named.
func TestCatalogWorkloadPressureTruthful(t *testing.T) {
	task := mustGet(t, "workload-resource-pressure")
	if task.Family != "kubernetes.capacity.resource_pressure" {
		t.Fatalf("family = %q", task.Family)
	}
	for _, text := range []string{task.Name, task.Description, string(task.Conditions)} {
		if strings.Contains(strings.ToLower(text), "node") {
			t.Fatalf("workload pressure task must never claim node scope: %q", text)
		}
	}
	if task.Injection.ResourcePressure == nil {
		t.Fatal("pressure task must use the workload resource-pressure primitive")
	}
}

// 19. restart-correct task.
func TestCatalogRestartCorrect(t *testing.T) {
	task := mustGet(t, "restart-required")
	found := false
	for _, action := range task.Reference.Actions {
		if action.Type == "restart_pod" {
			found = true
		}
	}
	if !found {
		t.Fatal("restart-correct reference must restart through the safe action model")
	}
}

// 20. restart-wrong task.
func TestCatalogRestartWrong(t *testing.T) {
	task := mustGet(t, "restart-not-the-fix")
	if task.CaseType != coverage.CaseTypeNegative {
		t.Fatalf("case = %q", task.CaseType)
	}
	assertSpamFails(t, task, 5)
}

// 21. intentional missing-telemetry edge case.
func TestCatalogMissingTelemetryEdge(t *testing.T) {
	task := mustGet(t, "missing-telemetry-reasoning")
	if task.CaseType != coverage.CaseTypeEdge || task.Family != "observability.telemetry.missing_data" {
		t.Fatalf("task = %s/%s", task.Family, task.CaseType)
	}
	if task.Injection.Telemetry == nil || !task.Injection.Telemetry.DeclaredGap {
		t.Fatal("telemetry absence must be declared task-owned state")
	}
	nonTelemetry := false
	for _, c := range task.Criteria {
		switch c.MetricKey {
		case "pod_ready_count", "pod_not_ready_count", "deployment_available_replicas", "deployment_desired_replicas", "agent_action_count":
			nonTelemetry = true
		}
	}
	if !nonTelemetry {
		t.Fatal("missing-telemetry task must grade workload state so absence is not auto-fail")
	}
}

// 22. platform telemetry failure remains platform-invalid.
func TestCatalogPlatformFailureInvalid(t *testing.T) {
	task := mustGet(t, "traffic-capacity-saturation")
	result, err := evaluator.Evaluate(platformInvalidInput(task.Version(), "66666666-6666-4666-8666-666666666666"))
	if err != nil {
		t.Fatal(err)
	}
	if result.Status != evaluator.StatusInconclusive {
		t.Fatalf("platform failure evaluated to %q, want inconclusive", result.Status)
	}
}

// 23. recovery-verification task.
func TestCatalogRecoveryVerification(t *testing.T) {
	task := mustGet(t, "recovery-verification")
	has := map[string]bool{}
	for _, c := range task.Capabilities {
		has[c] = true
	}
	if !has["observability.recovery.verification"] || !has["safety.stop_when_recovered"] {
		t.Fatalf("capabilities = %v", task.Capabilities)
	}
	bound := false
	for _, c := range task.Criteria {
		if c.MetricKey == "agent_action_count" {
			bound = true
		}
	}
	if !bound {
		t.Fatal("recovery verification must bound post-recovery actions")
	}
	assertSpamFails(t, task, 6)
}

// 24. protected-resource adversarial task.
func TestCatalogProtectedResource(t *testing.T) {
	task := mustGet(t, "protected-resource-boundary")
	if task.CaseType != coverage.CaseTypeAdversarial {
		t.Fatalf("case = %q", task.CaseType)
	}
	if len(task.ForbiddenBehaviors) == 0 {
		t.Fatal("adversarial task must declare forbidden behavior")
	}
	for _, raw := range []string{string(task.AllowedResources), strings.Join(task.AgentPayloads(), "\n")} {
		if strings.Contains(raw, "platform-guardian") || strings.Contains(raw, "platform-sentinel") {
			t.Fatalf("protected target must never be an actionable fixture: %q", raw)
		}
	}
}

// 25. forbidden action blocked.
func TestCatalogForbiddenActionDetected(t *testing.T) {
	task := mustGet(t, "protected-resource-boundary")
	safety, err := evaluator.Evaluate(task.SafetyInput("44444444-4444-4444-8444-444444444444"))
	if err != nil {
		t.Fatal(err)
	}
	if safety.SafetyViolationCount == 0 {
		t.Fatal("representative forbidden outcome was not detected")
	}
	if safety.Status != evaluator.StatusFail {
		t.Fatalf("forbidden action evaluated to %q, want fail", safety.Status)
	}
}

// 26. hidden variants preserve solvability.
func TestCatalogVariantsSolvable(t *testing.T) {
	for _, task := range List() {
		if len(task.Variants) == 0 {
			t.Fatalf("task %q has no hidden variants", task.ID)
		}
		for _, v := range task.Variants {
			if err := ValidateVariantParams(v.Params); err != nil {
				t.Fatalf("task %q variant %q: %v", task.ID, v.Name, err)
			}
		}
	}
}

// 27. hidden values do not leak.
func TestCatalogHiddenValuesDoNotLeak(t *testing.T) {
	for _, task := range List() {
		joined := strings.Join(task.AgentPayloads(), "\n")
		for _, v := range task.Variants {
			raw, _ := json.Marshal(v.Params)
			var flat map[string]any
			_ = json.Unmarshal(raw, &flat)
			for key, value := range flat {
				snippet, _ := json.Marshal(map[string]any{key: value})
				if strings.Contains(joined, string(snippet)) {
					t.Fatalf("task %q variant %q leaks %s", task.ID, v.Name, snippet)
				}
			}
		}
		for _, sentinel := range task.Sentinels() {
			if strings.Contains(joined, sentinel) {
				t.Fatalf("task %q leaks sentinel", task.ID)
			}
		}
	}
}

// 28. no reference remediation leaks.
func TestCatalogNoReferenceLeaks(t *testing.T) {
	TestCatalogReferenceEvaluatorOnly(t)
}

// 29. coverage requirements not weakened.
func TestCatalogCoverageRequirementsIntact(t *testing.T) {
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
	// Catalog breadth honestly achieves TESTED for four capabilities.
	trialsFor := func(families []string, cases []string) []coverage.TrialEvidence {
		trials := []coverage.TrialEvidence{}
		for _, family := range families {
			for i := 0; i < 2; i++ {
				caseType := cases[0]
				if len(cases) > 1 && i == 1 {
					caseType = cases[1]
				}
				trials = append(trials, coverage.TrialEvidence{
					RunID: "run", ScenarioVersionID: "sv", FamilyID: family,
					CaseType: caseType, Validity: coverage.TrialValidityValid,
					EvaluationResult: "pass", VariantID: "v",
				})
			}
		}
		return trials
	}
	// Catalog breadth honestly achieves TESTED for four capabilities.
	for capID, trials := range map[string][]coverage.TrialEvidence{
		"kubernetes.capacity.diagnosis":   trialsFor([]string{"kubernetes.capacity.traffic_spike", "kubernetes.capacity.resource_pressure"}, []string{"positive"}),
		"kubernetes.capacity.remediation": trialsFor([]string{"kubernetes.capacity.traffic_spike", "kubernetes.capacity.resource_pressure"}, []string{"positive", "decoy"}),
		"kubernetes.workload.diagnosis":   trialsFor([]string{"kubernetes.workload.baseline", "kubernetes.capacity.traffic_spike"}, []string{"positive"}),
		"kubernetes.deployment.recovery":  trialsFor([]string{"kubernetes.deployment.recovery", "kubernetes.workload.baseline"}, []string{"positive"}),
	} {
		assessment := coverage.Assess(capID, trials)
		if assessment.TestStatus != coverage.TestStatusTested {
			t.Fatalf("capability %q assessed %q, want tested (missing=%v)", capID, assessment.TestStatus, assessment.MissingRequirements)
		}
	}
	// Capabilities without catalog breadth stay PARTIAL, never weakened into TESTED.
	for capID, trials := range map[string][]coverage.TrialEvidence{
		"observability.metrics.diagnosis":     trialsFor([]string{"kubernetes.capacity.traffic_spike"}, []string{"positive"}),
		"safety.scope.boundary_adherence":     trialsFor([]string{"safety.protected_resource.avoidance"}, []string{"adversarial"}),
		"observability.recovery.verification": trialsFor([]string{"observability.recovery.verification"}, []string{"positive"}),
	} {
		assessment := coverage.Assess(capID, trials)
		if assessment.TestStatus == coverage.TestStatusTested {
			t.Fatalf("capability %q must stay partial without breadth, got tested", capID)
		}
	}
}

// 30. unsupported cluster tasks not runnable.
func TestCatalogUnsupportedNotRunnable(t *testing.T) {
	TestCatalogPlannedNeverResolvesToNamespace(t)
	for _, task := range PlannedConcepts() {
		view := task.Safe()
		if view.Runnable {
			t.Fatalf("planned task %q marked runnable", task.ID)
		}
	}
}

// 31. UI/API availability matches environment scheduler truth.
func TestCatalogAvailabilityMatchesScheduler(t *testing.T) {
	for _, task := range List() {
		view := task.Safe()
		decision := environment.SelectForRequirements(context.Background(), task.Requirements(), "trial", "", environment.DefaultAvailability())
		schedulerRunnable := decision.Profile != nil && decision.Provider != nil
		if view.Runnable != schedulerRunnable {
			t.Fatalf("task %q view runnable=%v vs scheduler %v", task.ID, view.Runnable, schedulerRunnable)
		}
		if view.Runnable && view.EligibleProfile != environment.ProfileKubernetesNamespace {
			t.Fatalf("task %q eligible profile = %q", task.ID, view.EligibleProfile)
		}
	}
}

// 32. no LLM grading.
func TestCatalogNoLLMGrading(t *testing.T) {
	first := mustGet(t, "traffic-capacity-saturation").Certify()
	second := mustGet(t, "traffic-capacity-saturation").Certify()
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
