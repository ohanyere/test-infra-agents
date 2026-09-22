package main

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/evalplane/evalplane/backend/internal/kubeenv"
	"github.com/evalplane/evalplane/backend/internal/observation"
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
		Environment: kubeenv.Environment{Namespace: "test-run"},
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
