package gitops

import (
	"context"
	"testing"
	"time"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/dynamic/fake"
)

func argoFixture(status string) *unstructured.Unstructured {
	return &unstructured.Unstructured{Object: map[string]any{"apiVersion": "argoproj.io/v1alpha1", "kind": "Application", "metadata": map[string]any{"name": "checkout", "namespace": "argocd", "resourceVersion": "7"}, "spec": map[string]any{"source": map[string]any{"targetRevision": "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"}, "syncPolicy": map[string]any{"automated": map[string]any{"selfHeal": true}}}, "status": map[string]any{"sync": map[string]any{"status": status, "revision": "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"}, "health": map[string]any{"status": "Healthy"}}}}
}

func TestArgoReaderStates(t *testing.T) {
	for _, status := range []string{"Synced", "OutOfSync"} {
		t.Run(status, func(t *testing.T) {
			client := fake.NewSimpleDynamicClient(runtime.NewScheme(), argoFixture(status))
			got, err := (ArgoReader{Client: client, Namespace: "argocd", Application: "checkout", Now: func() time.Time { return time.Unix(1, 0) }}).Read(context.Background())
			if err != nil || got.SyncStatus != status || !got.Automated || !got.SelfHeal {
				t.Fatalf("state=%+v err=%v", got, err)
			}
		})
	}
}

func TestArgoReaderUnknown(t *testing.T) {
	client := fake.NewSimpleDynamicClient(runtime.NewScheme(), argoFixture("Reconciling"))
	got, err := (ArgoReader{Client: client, Namespace: "argocd", Application: "checkout"}).Read(context.Background())
	if err != nil || got.SyncStatus != "Unknown" {
		t.Fatalf("state=%+v err=%v", got, err)
	}
}

func TestArgoReaderUnavailable(t *testing.T) {
	client := fake.NewSimpleDynamicClient(runtime.NewScheme())
	_, err := (ArgoReader{Client: client, Namespace: "argocd", Application: "missing"}).Read(context.Background())
	if err == nil {
		t.Fatal("missing Application was not inconclusive/unavailable")
	}
}
