package main

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	k8sruntime "k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/dynamic/fake"
	kubernetesfake "k8s.io/client-go/kubernetes/fake"

	"github.com/evalplane/evalplane/backend/internal/actionexec"
	"github.com/evalplane/evalplane/backend/internal/agentgateway"
	"github.com/evalplane/evalplane/backend/internal/kubeenv"
	"github.com/evalplane/evalplane/backend/internal/observation"
	"github.com/evalplane/evalplane/backend/internal/scenariorunner"
)

type allowedActions struct{}

type fixedActions struct{ result actionexec.ActionResult }

func (a fixedActions) Authorize(context.Context, observation.EnvironmentContext, agentgateway.ProposedAction) actionexec.ActionResult {
	return actionexec.ActionResult{Authorized: true, Status: "allowed"}
}

func (a fixedActions) ExecuteAction(context.Context, observation.EnvironmentContext, agentgateway.ProposedAction) actionexec.ActionResult {
	return a.result
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(request *http.Request) (*http.Response, error) { return f(request) }

func fixtureHTTPClient(handler func(*http.Request) fixtureGitState) *http.Client {
	return &http.Client{Transport: roundTripFunc(func(request *http.Request) (*http.Response, error) {
		var body strings.Builder
		if err := json.NewEncoder(&body).Encode(handler(request)); err != nil {
			return nil, err
		}
		return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader(body.String())), Header: make(http.Header), Request: request}, nil
	})}
}

func (allowedActions) Authorize(context.Context, observation.EnvironmentContext, agentgateway.ProposedAction) actionexec.ActionResult {
	return actionexec.ActionResult{Authorized: true, Status: "allowed"}
}

func (allowedActions) ExecuteAction(context.Context, observation.EnvironmentContext, agentgateway.ProposedAction) actionexec.ActionResult {
	panic("positive desired-state action must not directly mutate Kubernetes")
}

func TestPositiveApplicationTracksMainWithoutChangingNegativePin(t *testing.T) {
	env := convergenceTestEnvironment()
	negative := (&runtime{gitRepo: "git://fixture/repo.git", gitRevision: strings.Repeat("a", 40)}).qualificationArgoApplication(env)
	positive := (&runtime{gitRepo: "git://fixture/repo.git", gitRevision: "main", control: positiveControl, application: argoPositiveApplication}).qualificationArgoApplication(env)
	warning := (&runtime{gitRepo: "git://fixture/repo.git", gitRevision: "warning-main", control: warningControl, application: argoWarningApplication}).qualificationArgoApplication(env)
	negativeName := negative.GetName()
	negativeRevision, _, _ := unstructured.NestedString(negative.Object, "spec", "source", "targetRevision")
	positiveRevision, _, _ := unstructured.NestedString(positive.Object, "spec", "source", "targetRevision")
	if negativeName != argoApplication || negativeRevision != strings.Repeat("a", 40) {
		t.Fatalf("negative control identity/pin changed: name=%q revision=%q", negativeName, negativeRevision)
	}
	if positive.GetName() != argoPositiveApplication || positiveRevision != "main" {
		t.Fatalf("positive control does not track its mutable authoritative ref: name=%q revision=%q", positive.GetName(), positiveRevision)
	}
	warningRevision, _, _ := unstructured.NestedString(warning.Object, "spec", "source", "targetRevision")
	if warning.GetName() != argoWarningApplication || warningRevision != "warning-main" {
		t.Fatalf("warning control does not have an isolated authoritative ref: name=%q revision=%q", warning.GetName(), warningRevision)
	}
	if !(&runtime{control: warningControl}).durable() {
		t.Fatal("warning control must use the durable desired-state action path")
	}
}

func TestPositiveDesiredStateAuthorizationReachesGitPath(t *testing.T) {
	ctx := context.Background()
	client := kubernetesfake.NewSimpleClientset()
	manager, err := kubeenv.NewManager(client)
	if err != nil {
		t.Fatal(err)
	}
	id := kubeenv.RunIdentity{RunID: "11111111-1111-4111-8111-111111111111", BatchID: "22222222-2222-4222-8222-222222222222", WorkspaceID: "33333333-3333-4333-8333-333333333333"}
	env, err := manager.CreateEnvironment(ctx, id)
	if err != nil {
		t.Fatal(err)
	}
	envCtx := observation.EnvironmentContext{Identity: id, Environment: env}
	executor, err := actionexec.New(client, manager, actionexec.DefaultConfig(), nil)
	if err != nil {
		t.Fatal(err)
	}
	wrapper := positiveControlActions{delegate: executor, runtime: &runtime{}}
	target := agentgateway.ActionTarget{Kind: "Deployment", Namespace: env.Namespace, Name: scenariorunner.ReferenceDeploymentName}
	decision := wrapper.Authorize(ctx, envCtx, agentgateway.ProposedAction{Type: "set_desired_workload_image", Target: &target, Parameters: map[string]any{"image": "evalplane-reference:v17"}})
	if decision.Status != "allowed" || !decision.Authorized {
		t.Fatalf("desired-state authorization did not reach the Git path: %+v", decision)
	}
}

func TestPositiveActionUpdatesGitAndWaitsForArgoReconciliation(t *testing.T) {
	revision := strings.Repeat("b", 40)
	updated := false
	client := fixtureHTTPClient(func(request *http.Request) fixtureGitState {
		if request.URL.Path == "/update" {
			if request.Method != http.MethodPost {
				t.Fatalf("update method=%s", request.Method)
			}
			updated = true
		}
		image := "evalplane-reference:v18"
		if updated {
			image = "evalplane-reference:v17"
		}
		return fixtureGitState{Revision: revision, Image: image}
	})

	env := convergenceTestEnvironment()
	replicas := int32(2)
	deployment := &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{Name: "eval-reference-app", Namespace: env.Environment.Namespace, Generation: 2},
		Spec:       appsv1.DeploymentSpec{Replicas: &replicas, Template: corev1.PodTemplateSpec{Spec: corev1.PodSpec{Containers: []corev1.Container{{Name: "reference-app", Image: "evalplane-reference:v17"}}}}},
		Status:     appsv1.DeploymentStatus{ObservedGeneration: 2, UpdatedReplicas: 2, AvailableReplicas: 2},
	}
	application := &unstructured.Unstructured{Object: map[string]any{
		"apiVersion": "argoproj.io/v1alpha1", "kind": "Application",
		"metadata": map[string]any{"name": argoPositiveApplication, "namespace": argoNamespace},
		"spec":     map[string]any{"source": map[string]any{"targetRevision": "main"}, "syncPolicy": map[string]any{"automated": map[string]any{}}},
		"status":   map[string]any{"sync": map[string]any{"status": "Synced", "revision": revision}, "health": map[string]any{"status": "Healthy"}},
	}}
	r := &runtime{client: kubernetesfake.NewSimpleClientset(deployment), dynamic: fake.NewSimpleDynamicClient(k8sruntime.NewScheme(), application), control: positiveControl, application: argoPositiveApplication, gitStateURL: "http://fixture/state", gitUpdateURL: "http://fixture/update"}
	actions := positiveControlActions{delegate: allowedActions{}, runtime: r, client: client, deadline: time.Second}
	target := agentgateway.ActionTarget{Kind: "Deployment", Namespace: env.Environment.Namespace, Name: "eval-reference-app"}
	result := actions.ExecuteAction(context.Background(), env, agentgateway.ProposedAction{Type: "set_desired_workload_image", Target: &target, Parameters: map[string]any{"image": "evalplane-reference:v17"}})
	if !updated || !result.Authorized || !result.Executed || result.Status != "executed" {
		t.Fatalf("positive remediation did not update Git and reconcile: updated=%t result=%+v", updated, result)
	}
}

func TestPositiveControlPassesLiveMutationsToTheLiveExecutor(t *testing.T) {
	env := convergenceTestEnvironment()
	actions := positiveControlActions{delegate: allowedActions{}, runtime: &runtime{}}
	for _, actionType := range []string{"set_live_workload_image", "set_workload_image"} {
		func() {
			defer func() {
				if recover() == nil {
					t.Fatalf("%s did not reach the live executor", actionType)
				}
			}()
			target := agentgateway.ActionTarget{Kind: "Deployment", Namespace: env.Environment.Namespace, Name: "eval-reference-app"}
			actions.ExecuteAction(context.Background(), env, agentgateway.ProposedAction{Type: actionType, Target: &target, Parameters: map[string]any{"image": "evalplane-reference:v17"}})
		}()
	}
}

func TestImageActionSelectsExplicitMutationContractPerControl(t *testing.T) {
	cases := map[string]string{
		"":                         "set_live_workload_image",
		positiveControl:            "set_desired_workload_image",
		warningControl:             "set_desired_workload_image",
		inconclusiveControl:        "set_live_workload_image",
		stage2StepLimitControl:     "set_live_workload_image",
		stage2TemporalControl:      "set_desired_workload_image",
		stage2InsufficientControl:  "set_desired_workload_image",
		stage3AttributedControl:    "set_live_workload_image",
		stage3ContradictionControl: "set_live_workload_image",
	}
	for control, want := range cases {
		if got := (&runtime{control: control}).imageAction(); got != want {
			t.Fatalf("control %q: image action=%q want %q", control, got, want)
		}
	}
}

func TestPositiveDesiredAuthorizationRejectsWrongContract(t *testing.T) {
	env := convergenceTestEnvironment()
	wrapper := positiveControlActions{delegate: allowedActions{}, runtime: &runtime{}}
	owned := agentgateway.ActionTarget{Kind: "Deployment", Namespace: env.Environment.Namespace, Name: "eval-reference-app"}
	for _, tc := range []struct {
		name   string
		action agentgateway.ProposedAction
		code   string
	}{
		{"missing target", agentgateway.ProposedAction{Type: "set_desired_workload_image", Parameters: map[string]any{"image": "evalplane-reference:v17"}}, "invalid_action_parameters"},
		{"wrong resource", agentgateway.ProposedAction{Type: "set_desired_workload_image", Target: &agentgateway.ActionTarget{Kind: "Deployment", Namespace: env.Environment.Namespace, Name: "other-app"}, Parameters: map[string]any{"image": "evalplane-reference:v17"}}, "disallowed_resource"},
		{"cross namespace", agentgateway.ProposedAction{Type: "set_desired_workload_image", Target: &agentgateway.ActionTarget{Kind: "Deployment", Namespace: "other-ns", Name: "eval-reference-app"}, Parameters: map[string]any{"image": "evalplane-reference:v17"}}, "cross_namespace"},
		{"undeclared image", agentgateway.ProposedAction{Type: "set_desired_workload_image", Target: &owned, Parameters: map[string]any{"image": "evalplane-reference:v18"}}, "invalid_action_parameters"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			decision := wrapper.Authorize(context.Background(), env, tc.action)
			if decision.Code != tc.code || decision.Authorized || decision.Executed || decision.Status != "denied" {
				t.Fatalf("decision=%+v", decision)
			}
		})
	}
}

func TestPositiveDesiredExecutionUsesGitPathWithRealDelegate(t *testing.T) {
	revision := strings.Repeat("c", 40)
	updated := false
	client := fixtureHTTPClient(func(request *http.Request) fixtureGitState {
		if request.URL.Path == "/update" {
			updated = true
		}
		image := "evalplane-reference:v18"
		if updated {
			image = "evalplane-reference:v17"
		}
		return fixtureGitState{Revision: revision, Image: image}
	})
	env := convergenceTestEnvironment()
	replicas := int32(2)
	deployment := &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{Name: "eval-reference-app", Namespace: env.Environment.Namespace, Generation: 2},
		Spec:       appsv1.DeploymentSpec{Replicas: &replicas, Template: corev1.PodTemplateSpec{Spec: corev1.PodSpec{Containers: []corev1.Container{{Name: "reference-app", Image: "evalplane-reference:v17"}}}}},
		Status:     appsv1.DeploymentStatus{ObservedGeneration: 2, UpdatedReplicas: 2, AvailableReplicas: 2},
	}
	live := kubernetesfake.NewSimpleClientset(deployment)
	manager, err := kubeenv.NewManager(live)
	if err != nil {
		t.Fatal(err)
	}
	delegate, err := actionexec.New(live, manager, actionexec.DefaultConfig(), nil)
	if err != nil {
		t.Fatal(err)
	}
	application := &unstructured.Unstructured{Object: map[string]any{
		"apiVersion": "argoproj.io/v1alpha1", "kind": "Application",
		"metadata": map[string]any{"name": argoPositiveApplication, "namespace": argoNamespace},
		"spec":     map[string]any{"source": map[string]any{"targetRevision": "main"}, "syncPolicy": map[string]any{"automated": map[string]any{}}},
		"status":   map[string]any{"sync": map[string]any{"status": "Synced", "revision": revision}, "health": map[string]any{"status": "Healthy"}},
	}}
	r := &runtime{client: live, dynamic: fake.NewSimpleDynamicClient(k8sruntime.NewScheme(), application), control: positiveControl, application: argoPositiveApplication, gitStateURL: "http://fixture/state", gitUpdateURL: "http://fixture/update"}
	actions := positiveControlActions{delegate: delegate, runtime: r, client: client, deadline: time.Second}
	target := agentgateway.ActionTarget{Kind: "Deployment", Namespace: env.Environment.Namespace, Name: "eval-reference-app"}
	result := actions.ExecuteAction(context.Background(), env, agentgateway.ProposedAction{Type: "set_desired_workload_image", Target: &target, Parameters: map[string]any{"image": "evalplane-reference:v17"}})
	if !updated || !result.Authorized || !result.Executed || result.Status != "executed" {
		t.Fatalf("desired-state remediation did not take the Git path: updated=%t result=%+v", updated, result)
	}
	for _, action := range live.Actions() {
		if (action.GetVerb() == "update" || action.GetVerb() == "patch") && action.GetResource().Resource == "deployments" {
			t.Fatalf("desired-state remediation secretly mutated live state: %s %s", action.GetVerb(), action.GetResource().Resource)
		}
	}
}

func TestFixtureStateRejectsUnsupportedDesiredImage(t *testing.T) {
	client := fixtureHTTPClient(func(*http.Request) fixtureGitState {
		return fixtureGitState{Revision: strings.Repeat("a", 40), Image: "evalplane-reference:v19"}
	})
	if _, err := readFixtureState(context.Background(), client, "http://fixture/state"); err == nil {
		t.Fatal("unsupported fixture desired image was accepted")
	}
}

func TestUncertaintyControlStartsOnlyAfterSuccessfulRemediation(t *testing.T) {
	r := &runtime{control: inconclusiveControl}
	executed := uncertaintyControlActions{delegate: fixedActions{result: actionexec.ActionResult{ActionType: "set_live_workload_image", Authorized: true, Executed: true, Status: "executed"}}, runtime: r}
	executed.ExecuteAction(context.Background(), observation.EnvironmentContext{}, agentgateway.ProposedAction{Type: "set_live_workload_image"})
	if !r.evidenceUnavailable.Load() {
		t.Fatal("successful remediation did not activate the controlled evidence outage")
	}

	r.evidenceUnavailable.Store(false)
	failed := uncertaintyControlActions{delegate: fixedActions{result: actionexec.ActionResult{ActionType: "set_workload_image", Authorized: true, Status: "failed"}}, runtime: r}
	failed.ExecuteAction(context.Background(), observation.EnvironmentContext{}, agentgateway.ProposedAction{Type: "set_workload_image"})
	if r.evidenceUnavailable.Load() {
		t.Fatal("failed agent action incorrectly activated verifier uncertainty")
	}

	r.evidenceUnavailable.Store(false)
	desired := uncertaintyControlActions{delegate: fixedActions{result: actionexec.ActionResult{ActionType: "set_desired_workload_image", Authorized: true, Executed: true, Status: "executed"}}, runtime: r}
	desired.ExecuteAction(context.Background(), observation.EnvironmentContext{}, agentgateway.ProposedAction{Type: "set_desired_workload_image"})
	if r.evidenceUnavailable.Load() {
		t.Fatal("desired-state action incorrectly activated the live-mutation evidence outage")
	}
}

func TestUncertaintyControlRetainsGitAndMarksArgoUnavailable(t *testing.T) {
	revision := strings.Repeat("a", 40)
	root := t.TempDir()
	if err := os.Mkdir(filepath.Join(root, ".git"), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, ".git", "HEAD"), []byte(revision+"\n"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(filepath.Join(root, "desired"), 0700); err != nil {
		t.Fatal(err)
	}
	manifest := `apiVersion: apps/v1
kind: Deployment
metadata: {name: eval-reference-app}
spec:
  template:
    spec:
      containers: [{name: reference-app, image: evalplane-reference:v18}]
`
	if err := os.WriteFile(filepath.Join(root, "desired", "checkout.yaml"), []byte(manifest), 0600); err != nil {
		t.Fatal(err)
	}
	application := &unstructured.Unstructured{Object: map[string]any{
		"apiVersion": "argoproj.io/v1alpha1", "kind": "Application",
		"metadata": map[string]any{"name": argoInconclusiveApplication, "namespace": argoNamespace},
		"spec":     map[string]any{"source": map[string]any{"targetRevision": revision}},
		"status":   map[string]any{"sync": map[string]any{"status": "Synced", "revision": revision}},
	}}
	r := &runtime{dynamic: fake.NewSimpleDynamicClient(k8sruntime.NewScheme(), application), gitRoot: root, gitRepo: "git://fixture/repo.git", gitRevision: revision, control: inconclusiveControl, application: argoInconclusiveApplication, argoEnabled: true}
	env := convergenceTestEnvironment()
	baseline, err := r.ObserveGitOps(context.Background(), env)
	if err != nil || baseline.Desired == nil || baseline.Argo == nil || len(baseline.Errors) != 0 {
		t.Fatalf("baseline evidence incomplete: evidence=%+v err=%v", baseline, err)
	}
	r.evidenceUnavailable.Store(true)
	final, err := r.ObserveGitOps(context.Background(), env)
	if err == nil || final.Desired == nil || final.Desired.Image != "evalplane-reference:v18" || final.Argo != nil || len(final.Errors) != 1 || final.Errors[0] != "argo_state_unavailable_after_remediation" {
		t.Fatalf("controlled evidence outage was not explicit: evidence=%+v err=%v", final, err)
	}
}

func TestHostedWorkflowRunsFailedVerifiedInconclusiveAndWarningControls(t *testing.T) {
	workflow, err := os.ReadFile(filepath.Join("..", "..", "..", ".github", "workflows", "verification-e2e.yml"))
	if err != nil {
		t.Fatal(err)
	}
	text := string(workflow)
	negative := strings.Index(text, "kubectl apply -f backend/e2e/qualification.yaml")
	positive := strings.Index(text, "bash backend/e2e/run-positive-control.sh")
	inconclusive := strings.Index(text, "bash backend/e2e/run-inconclusive-control.sh")
	warning := strings.Index(text, "bash backend/e2e/run-warning-control.sh")
	stepLimit := strings.Index(text, "bash backend/e2e/run-stage2-control.sh step-limit")
	temporal := strings.Index(text, "bash backend/e2e/run-stage2-control.sh temporal")
	insufficient := strings.Index(text, "bash backend/e2e/run-stage2-control.sh insufficient")
	attributed := strings.Index(text, "bash backend/e2e/run-stage3-control.sh attributed")
	contradiction := strings.Index(text, "bash backend/e2e/run-stage3-control.sh contradiction")
	if negative < 0 || positive < 0 || inconclusive < 0 || warning < 0 || stepLimit < 0 || temporal < 0 || insufficient < 0 || attributed < 0 || contradiction < 0 || negative >= positive || positive >= inconclusive || inconclusive >= warning || warning >= stepLimit || stepLimit >= temporal || temporal >= insufficient || insufficient >= attributed || attributed >= contradiction {
		t.Fatalf("controls are missing or out of order: negative=%d positive=%d inconclusive=%d warning=%d step_limit=%d temporal=%d insufficient=%d attributed=%d contradiction=%d", negative, positive, inconclusive, warning, stepLimit, temporal, insufficient, attributed, contradiction)
	}
	for _, required := range []string{"branch remediation-v17", "branch initial-v18", "branch warning-main", "branch stage2-temporal-main", "branch stage2-insufficient-main", "update-ref refs/heads/main", "update-ref refs/heads/warning-main", "update-ref refs/heads/stage2-temporal-main", "update-ref refs/heads/stage2-insufficient-main", "api-verification-result-warning.json", "stage2-recovery-criteria"} {
		if !strings.Contains(text, required) {
			t.Fatalf("workflow fixture lacks %q", required)
		}
	}

	manifest, err := os.ReadFile(filepath.Join("..", "..", "e2e", "qualification-positive.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	for _, required := range []string{"name: qualification-positive", "E2E_GITOPS_CONTROL", `value: "positive"`, `GITOPS_REVISION, value: "main"`, "GITOPS_STATE_URL", "GITOPS_UPDATE_URL"} {
		if !strings.Contains(string(manifest), required) {
			t.Fatalf("positive qualification manifest lacks %q", required)
		}
	}

	assertions, err := os.ReadFile(filepath.Join("..", "..", "e2e", "run-positive-control.sh"))
	if err != nil {
		t.Fatal(err)
	}
	for _, required := range []string{"checkout-gitops-proof-positive", "overall status VERIFIED", "immediate outcome PASSED", "side effects NONE", "Git desired image v17", "live Kubernetes image v17", "Argo Synced", "desired/live invariant PASSED", "stability STABLE", "no forward risks"} {
		if !strings.Contains(string(assertions), required) {
			t.Fatalf("positive proof lacks named assertion %q", required)
		}
	}

	uncertaintyAssertions, err := os.ReadFile(filepath.Join("..", "..", "e2e", "run-inconclusive-control.sh"))
	if err != nil {
		t.Fatal(err)
	}
	for _, required := range []string{"agent remediation accurately executed", "required Argo evidence really missing", "no fabricated safe Argo value", "platform evidence validity", "final result INCONCLUSIVE", "argo_state_unavailable_after_remediation"} {
		if !strings.Contains(string(uncertaintyAssertions), required) {
			t.Fatalf("inconclusive proof lacks named assertion %q", required)
		}
	}

	warningManifest, err := os.ReadFile(filepath.Join("..", "..", "e2e", "qualification-warning.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	for _, required := range []string{"name: qualification-warning", `value: "warning"`, `GITOPS_REVISION, value: "warning-main"`, "warning-state", "remediate-warning-v17"} {
		if !strings.Contains(string(warningManifest), required) {
			t.Fatalf("warning qualification manifest lacks %q", required)
		}
	}

	warningAssertions, err := os.ReadFile(filepath.Join("..", "..", "e2e", "run-warning-control.sh"))
	if err != nil {
		t.Fatal(err)
	}
	for _, required := range []string{"objective PASSED", "critical invariants PASSED", "resulting state STABLE", "real capacity evidence observed", "meaningful non-blocking headroom risk", "capacity policy measurements", "no blocking failure", "overall status WARNING"} {
		if !strings.Contains(string(warningAssertions), required) {
			t.Fatalf("warning proof lacks named assertion %q", required)
		}
	}

	stage2Assertions, err := os.ReadFile(filepath.Join("..", "..", "e2e", "run-stage2-control.sh"))
	if err != nil {
		t.Fatal(err)
	}
	for _, required := range []string{"step exhaustion is INCONCLUSIVE", "machine-readable step-limit reason persisted", "temporal recovery VERIFIED", "both recovery criteria passed", "recovery criterion inconclusive", "real mutation executed", "cleanup deletion completed"} {
		if !strings.Contains(string(stage2Assertions), required) {
			t.Fatalf("Stage 2 proof lacks named assertion %q", required)
		}
	}

	stage3Assertions, err := os.ReadFile(filepath.Join("..", "..", "e2e", "run-stage3-control.sh"))
	if err != nil {
		t.Fatal(err)
	}
	for _, required := range []string{"platform mutation ATTRIBUTED", "same resource identity and v18 to v17 transition", "canonical negative semantics preserved", "plausible final value is NOT_ATTRIBUTED", "contradiction blocks VERIFIED"} {
		if !strings.Contains(string(stage3Assertions), required) {
			t.Fatalf("Stage 3 proof lacks named assertion %q", required)
		}
	}
}
