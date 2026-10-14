package scenariorunner

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/evalplane/evalplane/backend/internal/kubeenv"
	appsv1 "k8s.io/api/apps/v1"
	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes/fake"
)

const (
	testRunID       = "550e8400-e29b-41d4-a716-446655440000"
	testOtherRunID  = "550e8400-e29b-41d4-a716-446655440001"
	testBatchID     = "660e8400-e29b-41d4-a716-446655440000"
	testWorkspaceID = "770e8400-e29b-41d4-a716-446655440000"
)

func TestDeployReferenceWorkloadCreatesOwnedResourcesIdempotently(t *testing.T) {
	ctx := context.Background()
	client, runner, environment := testRunner(t)
	workload, err := runner.DeployReferenceWorkload(ctx, environment)
	if err != nil {
		t.Fatal(err)
	}
	deployment, err := client.AppsV1().Deployments(workload.Namespace).Get(ctx, ReferenceDeploymentName, metav1.GetOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if deployment.Spec.Replicas == nil || *deployment.Spec.Replicas != 2 || deployment.Spec.Template.Spec.Containers[0].Image != "nginx:1.27.5-alpine" {
		t.Fatalf("deployment=%+v", deployment.Spec)
	}
	assertOwned(t, deployment.Labels, environment.Identity, ReferenceComponent)
	service, err := client.CoreV1().Services(workload.Namespace).Get(ctx, ReferenceServiceName, metav1.GetOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if service.Spec.Type != corev1.ServiceTypeClusterIP {
		t.Fatalf("service type=%s", service.Spec.Type)
	}
	assertOwned(t, service.Labels, environment.Identity, ReferenceComponent)
	if _, err = runner.DeployReferenceWorkload(ctx, environment); err != nil {
		t.Fatalf("second deployment: %v", err)
	}
	deployments, _ := client.AppsV1().Deployments(workload.Namespace).List(ctx, metav1.ListOptions{})
	services, _ := client.CoreV1().Services(workload.Namespace).List(ctx, metav1.ListOptions{})
	if len(deployments.Items) != 1 || len(services.Items) != 1 {
		t.Fatalf("deployments=%d services=%d", len(deployments.Items), len(services.Items))
	}
}

func TestDeployReferenceWorkloadAppliesBoundedHiddenRuntimeOptions(t *testing.T) {
	ctx := context.Background()
	client, runner, environment := testRunner(t)
	replicas, capacity := int32(3), 20
	if _, err := runner.DeployReferenceWorkloadWithOptions(ctx, environment, WorkloadOptions{InitialReplicas: &replicas, CapacityPerPod: &capacity}); err != nil {
		t.Fatal(err)
	}
	deployment, err := client.AppsV1().Deployments(environment.Environment.Namespace).Get(ctx, ReferenceDeploymentName, metav1.GetOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if deployment.Spec.Replicas == nil || *deployment.Spec.Replicas != 3 {
		t.Fatalf("replicas=%v", deployment.Spec.Replicas)
	}
	environmentVariables := deployment.Spec.Template.Spec.Containers[0].Env
	if len(environmentVariables) != 1 || environmentVariables[0].Name != "MAX_CONCURRENT_WORK" || environmentVariables[0].Value != "20" {
		t.Fatalf("environment=%+v", environmentVariables)
	}
}

func TestDeployReferenceWorkloadRejectsOwnershipMismatch(t *testing.T) {
	ctx := context.Background()
	client, runner, environment := testRunner(t)
	labels := resourceLabels(environment.Identity, ReferenceComponent)
	labels[kubeenv.RunIDLabelKey] = testOtherRunID
	_, err := client.AppsV1().Deployments(environment.Environment.Namespace).Create(ctx, &appsv1.Deployment{ObjectMeta: metav1.ObjectMeta{Name: ReferenceDeploymentName, Namespace: environment.Environment.Namespace, Labels: labels}}, metav1.CreateOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = runner.DeployReferenceWorkload(ctx, environment); !errors.Is(err, ErrResourceConflict) || !errors.Is(err, ErrResourceOwnership) {
		t.Fatalf("error=%v", err)
	}
}

func TestTrafficSpikeCreatesOwnedK6Resources(t *testing.T) {
	ctx := context.Background()
	client, runner, environment := testRunner(t)
	profile := TrafficSpikeProfile{Stages: []TrafficStage{{Target: 3, Duration: time.Second}, {Target: 8, Duration: 2 * time.Second}}}
	execution, err := runner.StartTrafficSpike(ctx, environment, profile)
	if err != nil {
		t.Fatal(err)
	}
	configMap, err := client.CoreV1().ConfigMaps(execution.Namespace).Get(ctx, TrafficConfigMapName, metav1.GetOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(configMap.Data["script.js"], "http://"+ReferenceServiceName) || !strings.Contains(configMap.Data["script.js"], "target: 8") {
		t.Fatalf("script=%s", configMap.Data["script.js"])
	}
	if strings.Contains(configMap.Data["script.js"], "startVUs") || strings.Contains(configMap.Data["script.js"], "scenarios:") {
		t.Fatalf("profile without explicit start_vus must retain the existing stages-based k6 defaults: %s", configMap.Data["script.js"])
	}
	assertOwned(t, configMap.Labels, environment.Identity, TrafficComponent)
	job, err := client.BatchV1().Jobs(execution.Namespace).Get(ctx, TrafficJobName, metav1.GetOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if job.Spec.Template.Spec.Containers[0].Image != "grafana/k6:0.54.0" || job.Spec.Template.Spec.Containers[0].Args[1] != "/scripts/script.js" {
		t.Fatalf("job=%+v", job.Spec.Template.Spec.Containers[0])
	}
	assertOwned(t, job.Labels, environment.Identity, TrafficComponent)
	if _, err = runner.StartTrafficSpike(ctx, environment, profile); err != nil {
		t.Fatalf("second start: %v", err)
	}
}

func TestTrafficSpikeCanExplicitlyStartWithZeroVUs(t *testing.T) {
	startVUs := 0
	script := k6Script(TrafficSpikeProfile{StartVUs: &startVUs, Stages: []TrafficStage{{Target: 0, Duration: 15 * time.Second}}})
	for _, required := range []string{"executor: 'ramping-vus'", "startVUs: 0", "{ duration: '15s', target: 0 }"} {
		if !strings.Contains(script, required) {
			t.Fatalf("explicit zero-VU script lacks %q: %s", required, script)
		}
	}
}

func TestReadyAndTrafficCompletionUseKubernetesStatus(t *testing.T) {
	ctx := context.Background()
	client, runner, environment := testRunner(t)
	if _, err := runner.DeployReferenceWorkload(ctx, environment); err != nil {
		t.Fatal(err)
	}
	deployment, _ := client.AppsV1().Deployments(environment.Environment.Namespace).Get(ctx, ReferenceDeploymentName, metav1.GetOptions{})
	deployment.Status.ObservedGeneration = deployment.Generation
	deployment.Status.ReadyReplicas = 2
	deployment.Status.AvailableReplicas = 2
	if _, err := client.AppsV1().Deployments(environment.Environment.Namespace).UpdateStatus(ctx, deployment, metav1.UpdateOptions{}); err != nil {
		t.Fatal(err)
	}
	if err := runner.WaitForReferenceReady(ctx, environment); err != nil {
		t.Fatal(err)
	}
	if _, err := runner.StartTrafficSpike(ctx, environment, TrafficSpikeProfile{Stages: []TrafficStage{{Target: 1, Duration: time.Second}}}); err != nil {
		t.Fatal(err)
	}
	job, _ := client.BatchV1().Jobs(environment.Environment.Namespace).Get(ctx, TrafficJobName, metav1.GetOptions{})
	job.Status.Conditions = []batchv1.JobCondition{{Type: batchv1.JobComplete, Status: corev1.ConditionTrue}}
	if _, err := client.BatchV1().Jobs(environment.Environment.Namespace).UpdateStatus(ctx, job, metav1.UpdateOptions{}); err != nil {
		t.Fatal(err)
	}
	if err := runner.WaitForTrafficCompletion(ctx, environment); err != nil {
		t.Fatal(err)
	}
}

func TestCleanupDeletesOnlyOwnedScenarioResourcesAndKeepsNamespace(t *testing.T) {
	ctx := context.Background()
	client, runner, environment := testRunner(t)
	if _, err := runner.DeployReferenceWorkload(ctx, environment); err != nil {
		t.Fatal(err)
	}
	if _, err := runner.StartTrafficSpike(ctx, environment, TrafficSpikeProfile{Stages: []TrafficStage{{Target: 1, Duration: time.Second}}}); err != nil {
		t.Fatal(err)
	}
	if err := runner.CleanupScenarioResources(ctx, environment); err != nil {
		t.Fatal(err)
	}
	if _, err := client.CoreV1().Namespaces().Get(ctx, environment.Environment.Namespace, metav1.GetOptions{}); err != nil {
		t.Fatalf("namespace removed: %v", err)
	}
	deployments, _ := client.AppsV1().Deployments(environment.Environment.Namespace).List(ctx, metav1.ListOptions{})
	services, _ := client.CoreV1().Services(environment.Environment.Namespace).List(ctx, metav1.ListOptions{})
	configMaps, _ := client.CoreV1().ConfigMaps(environment.Environment.Namespace).List(ctx, metav1.ListOptions{})
	jobs, _ := client.BatchV1().Jobs(environment.Environment.Namespace).List(ctx, metav1.ListOptions{})
	if len(deployments.Items)+len(services.Items)+len(configMaps.Items)+len(jobs.Items) != 0 {
		t.Fatalf("resources remain: deployments=%d services=%d configmaps=%d jobs=%d", len(deployments.Items), len(services.Items), len(configMaps.Items), len(jobs.Items))
	}
	if err := runner.CleanupScenarioResources(ctx, environment); err != nil {
		t.Fatalf("second cleanup: %v", err)
	}
}

func TestCleanupRefusesWrongOwnershipBeforeDeletingAnything(t *testing.T) {
	ctx := context.Background()
	client, runner, environment := testRunner(t)
	if _, err := runner.DeployReferenceWorkload(ctx, environment); err != nil {
		t.Fatal(err)
	}
	service, _ := client.CoreV1().Services(environment.Environment.Namespace).Get(ctx, ReferenceServiceName, metav1.GetOptions{})
	service.Labels[kubeenv.RunIDLabelKey] = testOtherRunID
	if _, err := client.CoreV1().Services(environment.Environment.Namespace).Update(ctx, service, metav1.UpdateOptions{}); err != nil {
		t.Fatal(err)
	}
	if err := runner.CleanupScenarioResources(ctx, environment); !errors.Is(err, ErrResourceOwnership) {
		t.Fatalf("error=%v", err)
	}
	if _, err := client.AppsV1().Deployments(environment.Environment.Namespace).Get(ctx, ReferenceDeploymentName, metav1.GetOptions{}); err != nil {
		t.Fatalf("deployment deleted before ownership preflight completed: %v", err)
	}
}

func testRunner(t *testing.T) (*fake.Clientset, *Runner, EnvironmentContext) {
	t.Helper()
	identity := kubeenv.RunIdentity{RunID: testRunID, BatchID: testBatchID, WorkspaceID: testWorkspaceID}
	namespace, err := kubeenv.NamespaceForRun(identity.RunID)
	if err != nil {
		t.Fatal(err)
	}
	environment := EnvironmentContext{Identity: identity, Environment: kubeenv.Environment{RunID: identity.RunID, Namespace: namespace, Reference: kubeenv.EnvironmentReference(namespace)}}
	namespaceObject := &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: namespace, Labels: map[string]string{
		kubeenv.ManagedLabelKey: "true", kubeenv.RunIDLabelKey: identity.RunID, kubeenv.BatchIDLabelKey: identity.BatchID, kubeenv.WorkspaceIDLabelKey: identity.WorkspaceID,
	}}}
	client := fake.NewSimpleClientset(namespaceObject)
	config := DefaultConfig()
	config.ReferenceReadyTimeout = time.Second
	config.TrafficTimeout = time.Second
	config.PollInterval = time.Millisecond
	runner, err := New(client, config, nil)
	if err != nil {
		t.Fatal(err)
	}
	return client, runner, environment
}

func assertOwned(t *testing.T, labels map[string]string, identity kubeenv.RunIdentity, component string) {
	t.Helper()
	if err := verifyResourceOwnership(labels, identity, component); err != nil {
		t.Fatalf("labels=%v error=%v", labels, err)
	}
}
