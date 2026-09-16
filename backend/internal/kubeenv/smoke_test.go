package kubeenv

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"os"
	"testing"
	"time"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func TestKubernetesEnvironmentSmoke(t *testing.T) {
	kubeconfig := os.Getenv("TEST_KUBECONFIG")
	if kubeconfig == "" {
		t.Skip("TEST_KUBECONFIG is not set")
	}
	client, err := NewClientset(kubeconfig)
	if err != nil {
		t.Fatal(err)
	}
	manager, err := NewManager(client)
	if err != nil {
		t.Fatal(err)
	}
	identity := RunIdentity{RunID: randomUUID(t), BatchID: randomUUID(t), WorkspaceID: randomUUID(t)}
	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	defer cancel()
	defer func() {
		cleanupCtx, cleanupCancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cleanupCancel()
		if cleanupErr := manager.DeleteEnvironment(cleanupCtx, identity.RunID); cleanupErr != nil {
			t.Errorf("cleanup environment: %v", cleanupErr)
		}
	}()
	environment, err := manager.CreateEnvironment(ctx, identity)
	if err != nil {
		t.Fatal(err)
	}
	namespace, err := client.CoreV1().Namespaces().Get(ctx, environment.Namespace, metav1.GetOptions{})
	if err != nil || namespace.Labels[RunIDLabelKey] != identity.RunID {
		t.Fatalf("namespace labels=%v error=%v", namespace.Labels, err)
	}
	if _, err = client.CoreV1().ResourceQuotas(environment.Namespace).Get(ctx, quotaName, metav1.GetOptions{}); err != nil {
		t.Fatal(err)
	}
	if err = manager.DeleteEnvironment(ctx, identity.RunID); err != nil {
		t.Fatal(err)
	}
	if _, err = client.CoreV1().Namespaces().Get(ctx, environment.Namespace, metav1.GetOptions{}); !apierrors.IsNotFound(err) {
		t.Fatalf("namespace still present or unexpected error: %v", err)
	}
}

func randomUUID(t *testing.T) string {
	t.Helper()
	var value [16]byte
	if _, err := rand.Read(value[:]); err != nil {
		t.Fatal(err)
	}
	value[6] = (value[6] & 0x0f) | 0x40
	value[8] = (value[8] & 0x3f) | 0x80
	encoded := hex.EncodeToString(value[:])
	return encoded[:8] + "-" + encoded[8:12] + "-" + encoded[12:16] + "-" + encoded[16:20] + "-" + encoded[20:]
}
