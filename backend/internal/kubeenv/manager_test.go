package kubeenv

import (
	"context"
	"errors"
	"testing"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	utilvalidation "k8s.io/apimachinery/pkg/util/validation"
	"k8s.io/client-go/kubernetes/fake"
)

const (
	testRunID       = "550e8400-e29b-41d4-a716-446655440000"
	testOtherRunID  = "550e8400-e29b-41d4-a716-446655440001"
	testBatchID     = "660e8400-e29b-41d4-a716-446655440000"
	testWorkspaceID = "770e8400-e29b-41d4-a716-446655440000"
)

func TestNamespaceForRunIsDeterministicAndValid(t *testing.T) {
	first, err := NamespaceForRun(testRunID)
	if err != nil {
		t.Fatal(err)
	}
	second, err := NamespaceForRun(testRunID)
	if err != nil {
		t.Fatal(err)
	}
	if first != second || first != "eval-550e8400e29b41d4a716446655440000" {
		t.Fatalf("names=%q,%q", first, second)
	}
	if problems := utilvalidation.IsDNS1123Label(first); len(problems) > 0 {
		t.Fatalf("invalid namespace: %v", problems)
	}
}

func TestCreateEnvironmentCreatesOwnedBoundedNamespaceIdempotently(t *testing.T) {
	ctx := context.Background()
	client := fake.NewSimpleClientset()
	manager := mustManager(t, client)
	identity := testIdentity()

	created, err := manager.CreateEnvironment(ctx, identity)
	if err != nil {
		t.Fatal(err)
	}
	if created.RunID != testRunID || created.Reference != "k8s://"+created.Namespace {
		t.Fatalf("environment=%+v", created)
	}
	namespace, err := client.CoreV1().Namespaces().Get(ctx, created.Namespace, metav1.GetOptions{})
	if err != nil {
		t.Fatal(err)
	}
	wantLabels := ownershipLabels(identity)
	for key, value := range wantLabels {
		if namespace.Labels[key] != value {
			t.Fatalf("label %s=%q want=%q", key, namespace.Labels[key], value)
		}
	}
	quota, err := client.CoreV1().ResourceQuotas(created.Namespace).Get(ctx, quotaName, metav1.GetOptions{})
	if err != nil || len(quota.Spec.Hard) != 5 {
		t.Fatalf("quota=%+v error=%v", quota.Spec.Hard, err)
	}
	limitRange, err := client.CoreV1().LimitRanges(created.Namespace).Get(ctx, limitRangeName, metav1.GetOptions{})
	if err != nil || len(limitRange.Spec.Limits) != 1 || len(limitRange.Spec.Limits[0].Default) != 2 || len(limitRange.Spec.Limits[0].DefaultRequest) != 2 {
		t.Fatalf("limitRange=%+v error=%v", limitRange.Spec.Limits, err)
	}
	second, err := manager.CreateEnvironment(ctx, identity)
	if err != nil || second != created {
		t.Fatalf("second=%+v error=%v", second, err)
	}
	namespaces, err := client.CoreV1().Namespaces().List(ctx, metav1.ListOptions{})
	if err != nil || len(namespaces.Items) != 1 {
		t.Fatalf("namespaces=%d error=%v", len(namespaces.Items), err)
	}
}

func TestCreateEnvironmentRejectsNamespaceOwnershipConflicts(t *testing.T) {
	for name, labels := range map[string]map[string]string{
		"wrong run": {ManagedLabelKey: "true", RunIDLabelKey: testOtherRunID, BatchIDLabelKey: testBatchID, WorkspaceIDLabelKey: testWorkspaceID},
		"unmanaged": {RunIDLabelKey: testRunID},
	} {
		t.Run(name, func(t *testing.T) {
			ctx := context.Background()
			namespace, err := NamespaceForRun(testRunID)
			if err != nil {
				t.Fatal(err)
			}
			client := fake.NewSimpleClientset(&corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: namespace, Labels: labels}})
			manager := mustManager(t, client)
			if _, err = manager.CreateEnvironment(ctx, testIdentity()); !errors.Is(err, ErrEnvironmentConflict) || !errors.Is(err, ErrEnvironmentOwnership) {
				t.Fatalf("error=%v", err)
			}
		})
	}
}

func TestGetEnvironmentVerifiesOwnership(t *testing.T) {
	ctx := context.Background()
	client := fake.NewSimpleClientset()
	manager := mustManager(t, client)
	created, err := manager.CreateEnvironment(ctx, testIdentity())
	if err != nil {
		t.Fatal(err)
	}
	got, err := manager.GetEnvironment(ctx, testRunID)
	if err != nil || got != created {
		t.Fatalf("got=%+v error=%v", got, err)
	}
	namespace, err := client.CoreV1().Namespaces().Get(ctx, created.Namespace, metav1.GetOptions{})
	if err != nil {
		t.Fatal(err)
	}
	namespace.Labels[ManagedLabelKey] = "false"
	if _, err = client.CoreV1().Namespaces().Update(ctx, namespace, metav1.UpdateOptions{}); err != nil {
		t.Fatal(err)
	}
	if _, err = manager.GetEnvironment(ctx, testRunID); !errors.Is(err, ErrEnvironmentOwnership) {
		t.Fatalf("error=%v", err)
	}
}

func TestDeleteEnvironmentIsOwnershipSafeAndIdempotent(t *testing.T) {
	ctx := context.Background()
	t.Run("owned", func(t *testing.T) {
		client := fake.NewSimpleClientset()
		manager := mustManager(t, client)
		created, err := manager.CreateEnvironment(ctx, testIdentity())
		if err != nil {
			t.Fatal(err)
		}
		if err = manager.DeleteEnvironment(ctx, testRunID); err != nil {
			t.Fatal(err)
		}
		if _, err = client.CoreV1().Namespaces().Get(ctx, created.Namespace, metav1.GetOptions{}); err == nil {
			t.Fatal("namespace still exists")
		}
		if err = manager.DeleteEnvironment(ctx, testRunID); err != nil {
			t.Fatalf("second delete: %v", err)
		}
	})
	t.Run("wrong ownership", func(t *testing.T) {
		namespace, err := NamespaceForRun(testRunID)
		if err != nil {
			t.Fatal(err)
		}
		client := fake.NewSimpleClientset(&corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: namespace, Labels: map[string]string{ManagedLabelKey: "true", RunIDLabelKey: testOtherRunID}}})
		manager := mustManager(t, client)
		if err = manager.DeleteEnvironment(ctx, testRunID); !errors.Is(err, ErrEnvironmentOwnership) {
			t.Fatalf("error=%v", err)
		}
		if _, err = client.CoreV1().Namespaces().Get(ctx, namespace, metav1.GetOptions{}); err != nil {
			t.Fatalf("namespace was deleted: %v", err)
		}
	})
	t.Run("already absent", func(t *testing.T) {
		manager := mustManager(t, fake.NewSimpleClientset())
		if err := manager.DeleteEnvironment(ctx, testRunID); err != nil {
			t.Fatal(err)
		}
	})
}

func mustManager(t *testing.T, client *fake.Clientset) *Manager {
	t.Helper()
	manager, err := NewManager(client)
	if err != nil {
		t.Fatal(err)
	}
	return manager
}

func testIdentity() RunIdentity {
	return RunIdentity{RunID: testRunID, BatchID: testBatchID, WorkspaceID: testWorkspaceID}
}
