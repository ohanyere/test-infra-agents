package actionexec

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/evalplane/evalplane/backend/internal/agentgateway"
	"github.com/evalplane/evalplane/backend/internal/kubeenv"
	"github.com/evalplane/evalplane/backend/internal/observation"
	"github.com/evalplane/evalplane/backend/internal/scenariorunner"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/kubernetes/fake"
	ktesting "k8s.io/client-go/testing"
)

var id = kubeenv.RunIdentity{RunID: "550e8400-e29b-41d4-a716-446655440000", BatchID: "550e8400-e29b-41d4-a716-446655440001", WorkspaceID: "550e8400-e29b-41d4-a716-446655440002"}

func owned(component string) map[string]string {
	return map[string]string{kubeenv.ManagedLabelKey: "true", kubeenv.RunIDLabelKey: id.RunID, kubeenv.BatchIDLabelKey: id.BatchID, kubeenv.WorkspaceIDLabelKey: id.WorkspaceID, scenariorunner.ComponentLabelKey: component}
}
func setup(t *testing.T) (*fake.Clientset, *kubeenv.Manager, observation.EnvironmentContext, *Executor) {
	t.Helper()
	client := fake.NewSimpleClientset()
	manager, _ := kubeenv.NewManager(client)
	env, err := manager.CreateEnvironment(context.Background(), id)
	if err != nil {
		t.Fatal(err)
	}
	ctx := observation.EnvironmentContext{Identity: id, Environment: env}
	executor, err := New(client, manager, DefaultConfig(), nil)
	if err != nil {
		t.Fatal(err)
	}
	return client, manager, ctx, executor
}
func deployment(env observation.EnvironmentContext, labels map[string]string, replicas int32) *appsv1.Deployment {
	return &appsv1.Deployment{ObjectMeta: metav1.ObjectMeta{Name: scenariorunner.ReferenceDeploymentName, Namespace: env.Environment.Namespace, Labels: labels}, Spec: appsv1.DeploymentSpec{Replicas: &replicas}}
}
func scale(namespace string, replicas any) agentgateway.ProposedAction {
	return agentgateway.ProposedAction{Type: "scale_workload", Target: &agentgateway.ActionTarget{Kind: "Deployment", Namespace: namespace, Name: scenariorunner.ReferenceDeploymentName}, Parameters: map[string]any{"replicas": replicas}}
}

func TestNoMutationActionsAndSafetyDenials(t *testing.T) {
	client, _, env, executor := setup(t)
	d := deployment(env, owned(scenariorunner.ReferenceComponent), 2)
	_, _ = client.AppsV1().Deployments(env.Environment.Namespace).Create(context.Background(), d, metav1.CreateOptions{})
	actions := []agentgateway.ProposedAction{{Type: "inspect"}, {Type: "no_op"}, scale(env.Environment.Namespace, 0), scale(env.Environment.Namespace, 21), scale("production", 4), {Type: "delete_namespace"}}
	results, err := executor.Execute(context.Background(), env, actions)
	if err != nil {
		t.Fatal(err)
	}
	if !results[0].Authorized || !results[1].Authorized {
		t.Fatal("read-only actions denied")
	}
	for _, r := range results[2:] {
		if r.Authorized || r.Status != "denied" {
			t.Fatalf("unsafe action allowed: %+v", r)
		}
	}
	got, _ := client.AppsV1().Deployments(env.Environment.Namespace).Get(context.Background(), d.Name, metav1.GetOptions{})
	if *got.Spec.Replicas != 2 {
		t.Fatal("denied action mutated deployment")
	}
}

func TestScaleOwnershipAndExecution(t *testing.T) {
	client, _, env, executor := setup(t)
	_, _ = client.AppsV1().Deployments(env.Environment.Namespace).Create(context.Background(), deployment(env, owned(scenariorunner.ReferenceComponent), 2), metav1.CreateOptions{})
	results, _ := executor.Execute(context.Background(), env, []agentgateway.ProposedAction{scale(env.Environment.Namespace, 4)})
	if !results[0].Executed || !results[0].Authorized {
		t.Fatalf("scale failed: %+v", results)
	}
	got, _ := client.AppsV1().Deployments(env.Environment.Namespace).Get(context.Background(), scenariorunner.ReferenceDeploymentName, metav1.GetOptions{})
	if *got.Spec.Replicas != 4 {
		t.Fatal("not scaled")
	}
	got.Labels[kubeenv.RunIDLabelKey] = "wrong"
	_, _ = client.AppsV1().Deployments(env.Environment.Namespace).Update(context.Background(), got, metav1.UpdateOptions{})
	results, _ = executor.Execute(context.Background(), env, []agentgateway.ProposedAction{scale(env.Environment.Namespace, 3)})
	if results[0].Authorized {
		t.Fatal("wrong run ownership allowed")
	}
}

func TestRestartPolicy(t *testing.T) {
	client, _, env, executor := setup(t)
	reference := &corev1.Pod{ObjectMeta: metav1.ObjectMeta{Name: "reference-pod", Namespace: env.Environment.Namespace, Labels: owned(scenariorunner.ReferenceComponent)}}
	traffic := &corev1.Pod{ObjectMeta: metav1.ObjectMeta{Name: "traffic-pod", Namespace: env.Environment.Namespace, Labels: owned(scenariorunner.TrafficComponent)}}
	_, _ = client.CoreV1().Pods(env.Environment.Namespace).Create(context.Background(), reference, metav1.CreateOptions{})
	_, _ = client.CoreV1().Pods(env.Environment.Namespace).Create(context.Background(), traffic, metav1.CreateOptions{})
	restart := func(ns, name string) agentgateway.ProposedAction {
		return agentgateway.ProposedAction{Type: "restart_pod", Target: &agentgateway.ActionTarget{Kind: "Pod", Namespace: ns, Name: name}, Parameters: map[string]any{}}
	}
	results, _ := executor.Execute(context.Background(), env, []agentgateway.ProposedAction{restart(env.Environment.Namespace, "reference-pod"), restart(env.Environment.Namespace, "traffic-pod"), restart("production", "x")})
	if !results[0].Executed || results[1].Authorized || results[2].Authorized {
		t.Fatalf("restart policy: %+v", results)
	}
	if _, err := client.CoreV1().Pods(env.Environment.Namespace).Get(context.Background(), "reference-pod", metav1.GetOptions{}); err == nil {
		t.Fatal("reference pod not deleted")
	}
}

func TestKubernetesUpdateFailureIsFailed(t *testing.T) {
	client, _, env, executor := setup(t)
	_, _ = client.AppsV1().Deployments(env.Environment.Namespace).Create(context.Background(), deployment(env, owned(scenariorunner.ReferenceComponent), 2), metav1.CreateOptions{})
	client.PrependReactor("update", "deployments", func(ktesting.Action) (bool, runtime.Object, error) { return true, nil, fmt.Errorf("injected") })
	results, _ := executor.Execute(context.Background(), env, []agentgateway.ProposedAction{scale(env.Environment.Namespace, 4)})
	if results[0].Status != "failed" || !results[0].Authorized || results[0].Executed {
		t.Fatalf("unexpected: %+v", results[0])
	}
}

func TestEvaluateAgentStepScaleAndDeny(t *testing.T) {
	client, manager, env, executor := setup(t)
	d := deployment(env, owned(scenariorunner.ReferenceComponent), 2)
	_, _ = client.AppsV1().Deployments(env.Environment.Namespace).Create(context.Background(), d, metav1.CreateOptions{})
	provider, _ := observation.NewProvider(client, manager)
	targetNS := env.Environment.Namespace
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		fmt.Fprintf(w, `{"protocol_version":"v1","reasoning_summary":"scale","actions":[{"type":"scale_workload","target":{"kind":"Deployment","namespace":%q,"name":"eval-reference-app"},"parameters":{"replicas":4}}]}`, targetNS)
	}))
	defer server.Close()
	result, err := EvaluateAgentStep(context.Background(), env, agentgateway.AgentTask{Message: "restore health"}, "550e8400-e29b-41d4-a716-446655440003", []string{"scale_workload"}, provider, agentgateway.NewHTTPGateway(nil, nil), agentgateway.Connection{EndpointURL: server.URL}, executor)
	if err != nil {
		t.Fatal(err)
	}
	if !result.Actions[0].Executed || len(result.Observation.Workloads) != 1 || result.Observation.Workloads[0].DesiredReplicas != 4 {
		t.Fatalf("step result: %+v", result)
	}
	targetNS = "production"
	result, err = EvaluateAgentStep(context.Background(), env, agentgateway.AgentTask{Message: "restore health"}, "550e8400-e29b-41d4-a716-446655440003", []string{"scale_workload"}, provider, agentgateway.NewHTTPGateway(nil, nil), agentgateway.Connection{EndpointURL: server.URL}, executor)
	if err != nil {
		t.Fatal(err)
	}
	if result.Actions[0].Authorized {
		t.Fatal("cross-namespace action authorized")
	}
	got, _ := client.AppsV1().Deployments(env.Environment.Namespace).Get(context.Background(), d.Name, metav1.GetOptions{})
	if *got.Spec.Replicas != 4 {
		t.Fatal("denial mutated deployment")
	}
}

func TestSetWorkloadImageAuthorizationAndExecution(t *testing.T) {
	client, _, env, executor := setup(t)
	d := deployment(env, owned(scenariorunner.ReferenceComponent), 2)
	d.Spec.Template.Spec.Containers = []corev1.Container{{Name: "reference-app", Image: "evalplane-reference:v18"}}
	_, _ = client.AppsV1().Deployments(env.Environment.Namespace).Create(context.Background(), d, metav1.CreateOptions{})
	action := agentgateway.ProposedAction{Type: "set_workload_image", Target: &agentgateway.ActionTarget{Kind: "Deployment", Namespace: env.Environment.Namespace, Name: scenariorunner.ReferenceDeploymentName}, Parameters: map[string]any{"image": "evalplane-reference:v17"}}
	if result := executor.Authorize(context.Background(), env, action); !result.Authorized || result.Executed {
		t.Fatalf("authorization=%+v", result)
	}
	if result := executor.ExecuteAction(context.Background(), env, action); !result.Authorized || !result.Executed {
		t.Fatalf("execution=%+v", result)
	}
	got, err := client.AppsV1().Deployments(env.Environment.Namespace).Get(context.Background(), scenariorunner.ReferenceDeploymentName, metav1.GetOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if got.Spec.Template.Spec.Containers[0].Image != "evalplane-reference:v17" {
		t.Fatalf("image=%s", got.Spec.Template.Spec.Containers[0].Image)
	}
	action.Parameters["image"] = "https://user:token@example.invalid/image"
	if result := executor.Authorize(context.Background(), env, action); result.Authorized {
		t.Fatalf("unsafe image authorized: %+v", result)
	}
}

func TestSetWorkloadImageCapturesExecutorResourceIdentity(t *testing.T) {
	client, _, env, executor := setup(t)
	now := time.Date(2026, 9, 27, 12, 0, 0, 0, time.UTC)
	executor.now = func() time.Time { now = now.Add(time.Millisecond); return now }
	d := deployment(env, owned(scenariorunner.ReferenceComponent), 2)
	d.UID, d.ResourceVersion, d.Generation = types.UID("deployment-uid"), "10", 1
	d.Spec.Template.Spec.Containers = []corev1.Container{{Name: "reference-app", Image: "evalplane-reference:v18"}}
	_, _ = client.AppsV1().Deployments(env.Environment.Namespace).Create(context.Background(), d, metav1.CreateOptions{})
	client.PrependReactor("update", "deployments", func(action ktesting.Action) (bool, runtime.Object, error) {
		updated := action.(ktesting.UpdateAction).GetObject().(*appsv1.Deployment)
		updated.ResourceVersion, updated.Generation = "11", 2
		return false, nil, nil
	})
	action := agentgateway.ProposedAction{Type: "set_workload_image", Target: &agentgateway.ActionTarget{Kind: "Deployment", Namespace: env.Environment.Namespace, Name: scenariorunner.ReferenceDeploymentName}, Parameters: map[string]any{"image": "evalplane-reference:v17"}}
	result := executor.ExecuteAction(context.Background(), env, action)
	proof := result.Attribution
	if !result.Executed || proof == nil || proof.ContractVersion != AttributionContractVersion || proof.Executor != "kubernetes-client-go" || proof.PreMutation == nil || proof.PostMutation == nil {
		t.Fatalf("missing execution attribution: %+v", result)
	}
	if proof.PreMutation.UID != "deployment-uid" || proof.PostMutation.UID != "deployment-uid" || proof.PreMutation.ResourceVersion != "10" || proof.PostMutation.ResourceVersion != "11" || proof.PreMutation.Generation != 1 || proof.PostMutation.Generation != 2 || proof.PreMutation.Value != "evalplane-reference:v18" || proof.PostMutation.Value != "evalplane-reference:v17" {
		t.Fatalf("wrong old/new resource evidence: %+v", proof)
	}
	if proof.ExecutionStartedAt.After(proof.PreMutation.ObservedAt) || proof.PreMutation.ObservedAt.After(proof.PostMutation.ObservedAt) || proof.PostMutation.ObservedAt.After(proof.ExecutionCompletedAt) {
		t.Fatalf("timestamps are not causally ordered: %+v", proof)
	}
}

func TestSetWorkloadImageFailureRetainsPreMutationEvidence(t *testing.T) {
	client, _, env, executor := setup(t)
	d := deployment(env, owned(scenariorunner.ReferenceComponent), 2)
	d.UID, d.ResourceVersion, d.Generation = types.UID("deployment-uid"), "10", 1
	d.Spec.Template.Spec.Containers = []corev1.Container{{Name: "reference-app", Image: "evalplane-reference:v18"}}
	_, _ = client.AppsV1().Deployments(env.Environment.Namespace).Create(context.Background(), d, metav1.CreateOptions{})
	client.PrependReactor("update", "deployments", func(ktesting.Action) (bool, runtime.Object, error) { return true, nil, fmt.Errorf("injected") })
	action := agentgateway.ProposedAction{Type: "set_workload_image", Target: &agentgateway.ActionTarget{Kind: "Deployment", Namespace: env.Environment.Namespace, Name: scenariorunner.ReferenceDeploymentName}, Parameters: map[string]any{"image": "evalplane-reference:v17"}}
	result := executor.ExecuteAction(context.Background(), env, action)
	if result.Status != "failed" || result.Attribution == nil || result.Attribution.PreMutation == nil || result.Attribution.PostMutation != nil || result.Attribution.PreMutation.Value != "evalplane-reference:v18" {
		t.Fatalf("failed execution evidence=%+v", result)
	}
}

func TestLiveImageActionDispatchesToLiveExecutorOnly(t *testing.T) {
	client, _, env, executor := setup(t)
	d := deployment(env, owned(scenariorunner.ReferenceComponent), 2)
	d.Spec.Template.Spec.Containers = []corev1.Container{{Name: "reference-app", Image: "evalplane-reference:v18"}}
	_, _ = client.AppsV1().Deployments(env.Environment.Namespace).Create(context.Background(), d, metav1.CreateOptions{})
	target := &agentgateway.ActionTarget{Kind: "Deployment", Namespace: env.Environment.Namespace, Name: scenariorunner.ReferenceDeploymentName}
	for _, actionType := range []string{"set_live_workload_image", "set_workload_image"} {
		action := agentgateway.ProposedAction{Type: actionType, Target: target, Parameters: map[string]any{"image": "evalplane-reference:v17"}}
		if result := executor.Authorize(context.Background(), env, action); !result.Authorized || result.Executed {
			t.Fatalf("%s authorization=%+v", actionType, result)
		}
		result := executor.ExecuteAction(context.Background(), env, action)
		if !result.Authorized || !result.Executed || result.Status != "executed" || result.ActionType != actionType {
			t.Fatalf("%s execution=%+v", actionType, result)
		}
		if result.Attribution == nil || result.Attribution.Executor != "kubernetes-client-go" || result.Attribution.ActionType != actionType {
			t.Fatalf("%s attribution=%+v", actionType, result.Attribution)
		}
	}
	got, _ := client.AppsV1().Deployments(env.Environment.Namespace).Get(context.Background(), scenariorunner.ReferenceDeploymentName, metav1.GetOptions{})
	if got.Spec.Template.Spec.Containers[0].Image != "evalplane-reference:v17" {
		t.Fatalf("live image=%s", got.Spec.Template.Spec.Containers[0].Image)
	}
}

func TestDesiredImageActionFailsClosedWithoutGitExecutor(t *testing.T) {
	client, _, env, executor := setup(t)
	d := deployment(env, owned(scenariorunner.ReferenceComponent), 2)
	d.Spec.Template.Spec.Containers = []corev1.Container{{Name: "reference-app", Image: "evalplane-reference:v18"}}
	_, _ = client.AppsV1().Deployments(env.Environment.Namespace).Create(context.Background(), d, metav1.CreateOptions{})
	target := &agentgateway.ActionTarget{Kind: "Deployment", Namespace: env.Environment.Namespace, Name: scenariorunner.ReferenceDeploymentName}
	action := agentgateway.ProposedAction{Type: "set_desired_workload_image", Target: target, Parameters: map[string]any{"image": "evalplane-reference:v17"}}
	if result := executor.Authorize(context.Background(), env, action); result.Code != "unsupported_action" || result.Executed || result.Authorized {
		t.Fatalf("desired-state authorization=%+v", result)
	}
	if result := executor.ExecuteAction(context.Background(), env, action); result.Code != "unsupported_action" || result.Executed || result.Authorized {
		t.Fatalf("desired-state execution=%+v", result)
	}
	got, _ := client.AppsV1().Deployments(env.Environment.Namespace).Get(context.Background(), scenariorunner.ReferenceDeploymentName, metav1.GetOptions{})
	if got.Spec.Template.Spec.Containers[0].Image != "evalplane-reference:v18" {
		t.Fatalf("desired-state action mutated live state: image=%s", got.Spec.Template.Spec.Containers[0].Image)
	}
}
