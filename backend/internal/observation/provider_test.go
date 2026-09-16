package observation

import (
	"context"
	"testing"
	"time"

	"github.com/evalplane/evalplane/backend/internal/kubeenv"
	"github.com/evalplane/evalplane/backend/internal/telemetry"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/validation"
	"k8s.io/client-go/kubernetes/fake"
)

var testIdentity = kubeenv.RunIdentity{RunID: "550e8400-e29b-41d4-a716-446655440000", BatchID: "550e8400-e29b-41d4-a716-446655440001", WorkspaceID: "550e8400-e29b-41d4-a716-446655440002"}

func labels() map[string]string {
	return map[string]string{kubeenv.ManagedLabelKey: "true", kubeenv.RunIDLabelKey: testIdentity.RunID, kubeenv.BatchIDLabelKey: testIdentity.BatchID, kubeenv.WorkspaceIDLabelKey: testIdentity.WorkspaceID}
}
func setup(t *testing.T) (*fake.Clientset, *kubeenv.Manager, EnvironmentContext) {
	t.Helper()
	client := fake.NewSimpleClientset()
	manager, _ := kubeenv.NewManager(client)
	env, err := manager.CreateEnvironment(context.Background(), testIdentity)
	if err != nil {
		t.Fatal(err)
	}
	return client, manager, EnvironmentContext{Identity: testIdentity, Environment: env}
}

func TestObserveOwnedResourcesAndEvents(t *testing.T) {
	client, manager, env := setup(t)
	replicas := int32(2)
	d := &appsv1.Deployment{ObjectMeta: metav1.ObjectMeta{Name: "app", Namespace: env.Environment.Namespace, Labels: labels()}, Spec: appsv1.DeploymentSpec{Replicas: &replicas, Template: corev1.PodTemplateSpec{Spec: corev1.PodSpec{Containers: []corev1.Container{{Resources: corev1.ResourceRequirements{Requests: corev1.ResourceList{corev1.ResourceCPU: resource.MustParse("100m")}}}}}}}, Status: appsv1.DeploymentStatus{ReadyReplicas: 1, AvailableReplicas: 1, UpdatedReplicas: 2}}
	_, _ = client.AppsV1().Deployments(env.Environment.Namespace).Create(context.Background(), d, metav1.CreateOptions{})
	ready := true
	controller := true
	podLabels := labels()
	pod := &corev1.Pod{ObjectMeta: metav1.ObjectMeta{Name: "app-pod", Namespace: env.Environment.Namespace, Labels: podLabels, OwnerReferences: []metav1.OwnerReference{{Name: "app", Controller: &controller}}}, Status: corev1.PodStatus{Phase: corev1.PodRunning, ContainerStatuses: []corev1.ContainerStatus{{Ready: ready, RestartCount: 3}}}}
	_, _ = client.CoreV1().Pods(env.Environment.Namespace).Create(context.Background(), pod, metav1.CreateOptions{})
	_, _ = client.CoreV1().Events(env.Environment.Namespace).Create(context.Background(), &corev1.Event{ObjectMeta: metav1.ObjectMeta{Name: "event", Namespace: env.Environment.Namespace}, Reason: "Ready", Message: "ready", Type: "Normal", LastTimestamp: metav1.NewTime(time.Now())}, metav1.CreateOptions{})
	provider, _ := NewProvider(client, manager)
	got, err := provider.Observe(context.Background(), env)
	if err != nil {
		t.Fatal(err)
	}
	if len(got.Workloads) != 1 || got.Workloads[0].DesiredReplicas != 2 || got.Workloads[0].ReadyReplicas != 1 {
		t.Fatalf("workloads: %+v", got.Workloads)
	}
	if len(got.Pods) != 1 || got.Pods[0].RestartCount != 3 || !got.Pods[0].Ready {
		t.Fatalf("pods: %+v", got.Pods)
	}
	if len(got.RecentEvents) != 1 {
		t.Fatalf("events: %+v", got.RecentEvents)
	}
	if len(got.Metrics) != 0 {
		t.Fatal("metrics were fabricated")
	}
}

func TestObserveIsolationAndOwnership(t *testing.T) {
	client, manager, env := setup(t)
	otherNS := "unrelated"
	if errs := validation.IsDNS1123Label(otherNS); len(errs) > 0 {
		t.Fatal(errs)
	}
	_, _ = client.CoreV1().Namespaces().Create(context.Background(), &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: otherNS}}, metav1.CreateOptions{})
	_, _ = client.CoreV1().Pods(otherNS).Create(context.Background(), &corev1.Pod{ObjectMeta: metav1.ObjectMeta{Name: "foreign", Namespace: otherNS, Labels: labels()}}, metav1.CreateOptions{})
	provider, _ := NewProvider(client, manager)
	got, err := provider.Observe(context.Background(), env)
	if err != nil {
		t.Fatal(err)
	}
	if len(got.Pods) != 0 {
		t.Fatal("unrelated namespace exposed")
	}
	env.Environment.Namespace = otherNS
	if _, err = provider.Observe(context.Background(), env); err == nil {
		t.Fatal("expected ownership failure")
	}
}

type fakeTelemetry struct {
	snapshot telemetry.Snapshot
	err      error
}

func (f fakeTelemetry) Snapshot(context.Context, telemetry.EnvironmentContext) (telemetry.Snapshot, error) {
	return f.snapshot, f.err
}
func number(v float64) *float64 { return &v }
func TestObservationTelemetryEnrichmentAndUnavailable(t *testing.T) {
	client, manager, env := setup(t)
	provider, _ := NewProviderWithTelemetry(client, manager, fakeTelemetry{snapshot: telemetry.Snapshot{RequestRateRPS: number(12), ErrorRatePct: number(4.7), P95LatencyMS: number(842), AvailabilityPct: number(95.3)}})
	got, e := provider.Observe(context.Background(), env)
	if e != nil {
		t.Fatal(e)
	}
	if got.Metrics["request_rate_rps"] != float64(12) || got.Metrics["telemetry_available"] != true {
		t.Fatalf("metrics missing: %+v", got.Metrics)
	}
	provider, _ = NewProviderWithTelemetry(client, manager, fakeTelemetry{err: telemetry.ErrUnavailable})
	got, e = provider.Observe(context.Background(), env)
	if e != nil {
		t.Fatal(e)
	}
	if got.Metrics["telemetry_available"] != false {
		t.Fatal("unavailability hidden")
	}
	if _, ok := got.Metrics["error_rate_pct"]; ok {
		t.Fatal("fabricated zero")
	}
}
