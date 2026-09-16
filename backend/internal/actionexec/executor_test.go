package actionexec

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/evalplane/evalplane/backend/internal/agentgateway"
	"github.com/evalplane/evalplane/backend/internal/kubeenv"
	"github.com/evalplane/evalplane/backend/internal/observation"
	"github.com/evalplane/evalplane/backend/internal/scenariorunner"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
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
