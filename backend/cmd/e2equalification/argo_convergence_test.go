package main

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/evalplane/evalplane/backend/internal/kubeenv"
	"github.com/evalplane/evalplane/backend/internal/observation"
	"gopkg.in/yaml.v3"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	k8sruntime "k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/dynamic/fake"
	k8stesting "k8s.io/client-go/testing"
)

func convergenceTestEnvironment() observation.EnvironmentContext {
	return observation.EnvironmentContext{
		Identity:    kubeenv.RunIdentity{RunID: "11111111-1111-4111-8111-111111111111", BatchID: "22222222-2222-4222-8222-222222222222", WorkspaceID: "33333333-3333-4333-8333-333333333333"},
		Environment: kubeenv.Environment{Namespace: "eval-11111111111141118111111111111111"},
	}
}

func TestConvergenceRecordsSuccessfulUnknownApplicationRead(t *testing.T) {
	client := fake.NewSimpleDynamicClient(k8sruntime.NewScheme())
	r := &runtime{dynamic: client, gitRepo: "git://git-fixture.example/repo.git", gitRevision: strings.Repeat("a", 40)}
	ctx, cancel := context.WithTimeout(context.Background(), 500*time.Millisecond)
	defer cancel()
	err := r.convergeGitOps(ctx, convergenceTestEnvironment())
	if err == nil || !strings.Contains(err.Error(), "successful_reads=") || !strings.Contains(err.Error(), "last_read_result=success") || !strings.Contains(err.Error(), "sync=Unknown") || !strings.Contains(err.Error(), `last_read_error="none"`) {
		t.Fatalf("successful Unknown read was not distinguished from a read failure: %v", err)
	}
	if _, err := client.Resource(argoApplications).Namespace(argoNamespace).Get(context.Background(), argoApplication, metav1.GetOptions{}); err != nil {
		t.Fatalf("create/read Application identity differs: %v", err)
	}
}

func TestConvergencePreservesApplicationReadError(t *testing.T) {
	client := fake.NewSimpleDynamicClient(k8sruntime.NewScheme())
	client.PrependReactor("get", "applications", func(k8stesting.Action) (bool, k8sruntime.Object, error) {
		return true, nil, apierrors.NewForbidden(schema.GroupResource{Group: "argoproj.io", Resource: "applications"}, argoApplication, errors.New("read denied"))
	})
	r := &runtime{dynamic: client, gitRepo: "git://git-fixture.example/repo.git", gitRevision: strings.Repeat("a", 40)}
	ctx, cancel := context.WithTimeout(context.Background(), 500*time.Millisecond)
	defer cancel()
	err := r.convergeGitOps(ctx, convergenceTestEnvironment())
	if err == nil || !strings.Contains(err.Error(), "successful_reads=0") || !strings.Contains(err.Error(), "failed_reads=") || !strings.Contains(err.Error(), "last_read_result=error") || !strings.Contains(err.Error(), "forbidden") {
		t.Fatalf("Kubernetes read error was hidden: %v", err)
	}
}

func TestConvergenceReturnsApplicationCreateErrorImmediately(t *testing.T) {
	client := fake.NewSimpleDynamicClient(k8sruntime.NewScheme())
	client.PrependReactor("create", "applications", func(k8stesting.Action) (bool, k8sruntime.Object, error) {
		return true, nil, apierrors.NewForbidden(schema.GroupResource{Group: "argoproj.io", Resource: "applications"}, argoApplication, errors.New("create denied"))
	})
	r := &runtime{dynamic: client, gitRepo: "git://git-fixture.example/repo.git", gitRevision: strings.Repeat("a", 40)}
	err := r.convergeGitOps(context.Background(), convergenceTestEnvironment())
	if err == nil || !strings.Contains(err.Error(), "create Argo application") || !apierrors.IsForbidden(err) {
		t.Fatalf("Application create error was not returned immediately: %v", err)
	}
}

type appProjectManifest struct {
	APIVersion string `yaml:"apiVersion"`
	Kind       string `yaml:"kind"`
	Metadata   struct {
		Name      string `yaml:"name"`
		Namespace string `yaml:"namespace"`
	} `yaml:"metadata"`
	Spec struct {
		SourceRepos  []string `yaml:"sourceRepos"`
		Destinations []struct {
			Server    string `yaml:"server"`
			Namespace string `yaml:"namespace"`
		} `yaml:"destinations"`
		NamespaceResourceWhitelist []struct {
			Group string `yaml:"group"`
			Kind  string `yaml:"kind"`
		} `yaml:"namespaceResourceWhitelist"`
		ClusterResourceWhitelist []any `yaml:"clusterResourceWhitelist"`
	} `yaml:"spec"`
}

func loadQualificationAppProject(t *testing.T) appProjectManifest {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join("..", "..", "e2e", "argocd-project.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	var project appProjectManifest
	if err := yaml.Unmarshal(raw, &project); err != nil {
		t.Fatal(err)
	}
	return project
}

func TestQualificationApplicationUsesDedicatedRestrictedProject(t *testing.T) {
	project := loadQualificationAppProject(t)
	if project.APIVersion != "argoproj.io/v1alpha1" || project.Kind != "AppProject" || project.Metadata.Name != argoProject || project.Metadata.Namespace != argoNamespace {
		t.Fatalf("unexpected AppProject identity: %+v", project)
	}
	if len(project.Spec.SourceRepos) != 1 || project.Spec.SourceRepos[0] != "git://git-fixture.evalplane-e2e-harness.svc/repo.git" {
		t.Fatalf("AppProject source repositories are not restricted to the fixture: %v", project.Spec.SourceRepos)
	}
	if len(project.Spec.Destinations) != 1 || project.Spec.Destinations[0].Server != "https://kubernetes.default.svc" || project.Spec.Destinations[0].Namespace != "eval-*" {
		t.Fatalf("AppProject destination is not restricted to qualification namespaces: %+v", project.Spec.Destinations)
	}
	if len(project.Spec.NamespaceResourceWhitelist) != 1 || project.Spec.NamespaceResourceWhitelist[0].Group != "apps" || project.Spec.NamespaceResourceWhitelist[0].Kind != "Deployment" || len(project.Spec.ClusterResourceWhitelist) != 0 {
		t.Fatalf("AppProject resource permissions are broader than the fixture requires: namespace=%+v cluster=%+v", project.Spec.NamespaceResourceWhitelist, project.Spec.ClusterResourceWhitelist)
	}

	r := &runtime{gitRepo: project.Spec.SourceRepos[0], gitRevision: strings.Repeat("a", 40)}
	app := r.qualificationArgoApplication(convergenceTestEnvironment())
	applicationProject, _, _ := unstructured.NestedString(app.Object, "spec", "project")
	repository, _, _ := unstructured.NestedString(app.Object, "spec", "source", "repoURL")
	destinationServer, _, _ := unstructured.NestedString(app.Object, "spec", "destination", "server")
	destinationNamespace, _, _ := unstructured.NestedString(app.Object, "spec", "destination", "namespace")
	if applicationProject != argoProject || applicationProject == "default" || repository != project.Spec.SourceRepos[0] || destinationServer != project.Spec.Destinations[0].Server || !strings.HasPrefix(destinationNamespace, "eval-") {
		t.Fatalf("Application does not conform to AppProject: project=%q repo=%q destination=%q", applicationProject, repository, destinationServer)
	}
}

func TestWorkflowCreatesAndChecksAppProjectBeforeQualification(t *testing.T) {
	raw, err := os.ReadFile(filepath.Join("..", "..", "..", ".github", "workflows", "verification-e2e.yml"))
	if err != nil {
		t.Fatal(err)
	}
	workflow := string(raw)
	projectApply := strings.Index(workflow, "kubectl apply -f backend/e2e/argocd-project.yaml")
	qualificationStep := strings.Index(workflow, "- name: Configure qualification inputs")
	qualificationPod := strings.Index(workflow, "kubectl apply -f backend/e2e/qualification.yaml")
	if projectApply < 0 || qualificationStep < 0 || qualificationPod < 0 {
		t.Fatalf("workflow lacks AppProject setup or qualification step: apply=%d step=%d pod=%d", projectApply, qualificationStep, qualificationPod)
	}
	projectCheck := strings.Index(workflow[qualificationStep:], "kubectl -n argocd get appproject "+argoProject)
	if projectCheck < 0 || projectApply >= qualificationStep || qualificationStep >= qualificationPod {
		t.Fatalf("workflow does not create and check AppProject before qualification: apply=%d step=%d check=%d pod=%d", projectApply, qualificationStep, projectCheck, qualificationPod)
	}
}

func TestArgoConvergenceStatusDistinguishesUnreconciledAndComparisonError(t *testing.T) {
	app := &unstructured.Unstructured{Object: map[string]any{
		"apiVersion": "argoproj.io/v1alpha1", "kind": "Application",
		"metadata": map[string]any{"name": argoApplication, "namespace": argoNamespace, "resourceVersion": "7", "generation": int64(1)},
	}}
	client := fake.NewSimpleDynamicClient(k8sruntime.NewScheme(), app)
	r := &runtime{dynamic: client}
	expired, cancel := context.WithCancel(context.Background())
	cancel()
	unreconciled := r.argoConvergenceStatus(expired)
	for _, want := range []string{`"reconciled_at":"absent"`, `"sync_field":"absent"`, `"health_field":"absent"`, `"resource_count":"absent"`} {
		if !strings.Contains(unreconciled, want) {
			t.Fatalf("unreconciled status %q lacks %q", unreconciled, want)
		}
	}
	app.Object["status"] = map[string]any{
		"reconciledAt":   "2026-09-22T00:00:00Z",
		"sync":           map[string]any{"status": "Unknown", "revision": "abc123"},
		"health":         map[string]any{"status": "Unknown"},
		"operationState": map[string]any{"phase": "Error"},
		"resources":      []any{},
		"conditions":     []any{map[string]any{"type": "ComparisonError", "message": "repository unavailable"}},
	}
	if _, err := client.Resource(argoApplications).Namespace(argoNamespace).Update(context.Background(), app, metav1.UpdateOptions{}); err != nil {
		t.Fatal(err)
	}
	reconciled := r.argoConvergenceStatus(context.Background())
	for _, want := range []string{`"reconciled_at":"2026-09-22T00:00:00Z"`, `"sync_field":"Unknown"`, `"type":"ComparisonError"`, `"message":"repository unavailable"`, `"operation_phase":"Error"`, `"resource_count":"0"`} {
		if !strings.Contains(reconciled, want) {
			t.Fatalf("reconciled status %q lacks %q", reconciled, want)
		}
	}
}
