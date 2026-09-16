package kubeenv

import (
	"context"
	"errors"
	"k8s.io/client-go/kubernetes/fake"
	"testing"
)

func TestWorkerCleanupRequiresFullIdentity(t *testing.T) {
	client := fake.NewSimpleClientset()
	manager, _ := NewManager(client)
	id := RunIdentity{RunID: "11111111-1111-4111-8111-111111111111", BatchID: "22222222-2222-4222-8222-222222222222", WorkspaceID: "33333333-3333-4333-8333-333333333333"}
	if _, err := manager.CreateEnvironment(context.Background(), id); err != nil {
		t.Fatal(err)
	}
	other := id
	other.WorkspaceID = "44444444-4444-4444-8444-444444444444"
	if err := manager.DeleteOwnedEnvironment(context.Background(), other); !errors.Is(err, ErrEnvironmentOwnership) {
		t.Fatal(err)
	}
	if _, err := manager.GetEnvironment(context.Background(), id.RunID); err != nil {
		t.Fatal("foreign cleanup deleted environment")
	}
	for i := 0; i < 2; i++ {
		if err := manager.DeleteOwnedEnvironment(context.Background(), id); err != nil {
			t.Fatal(err)
		}
	}
}
