package telemetry

import (
	"context"
	"errors"
	"fmt"
	"github.com/evalplane/evalplane/backend/internal/kubeenv"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes/fake"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

var identity = kubeenv.RunIdentity{RunID: "550e8400-e29b-41d4-a716-446655440000", BatchID: "550e8400-e29b-41d4-a716-446655440001", WorkspaceID: "550e8400-e29b-41d4-a716-446655440002"}

func setup(t *testing.T) (*fake.Clientset, *kubeenv.Manager, EnvironmentContext) {
	t.Helper()
	client := fake.NewSimpleClientset()
	manager, _ := kubeenv.NewManager(client)
	env, e := manager.CreateEnvironment(context.Background(), identity)
	if e != nil {
		t.Fatal(e)
	}
	return client, manager, EnvironmentContext{Identity: identity, Environment: env}
}
func TestTelemetryResourcesOwnershipIdempotencyAndCleanup(t *testing.T) {
	client, verifier, env := setup(t)
	manager, e := NewManager(client, verifier, DefaultConfig())
	if e != nil {
		t.Fatal(e)
	}
	if e = manager.CreateTelemetry(context.Background(), env); e != nil {
		t.Fatal(e)
	}
	if e = manager.CreateTelemetry(context.Background(), env); e != nil {
		t.Fatal(e)
	}
	cm, _ := client.CoreV1().ConfigMaps(env.Environment.Namespace).Get(context.Background(), ConfigMapName, metav1.GetOptions{})
	if !strings.Contains(cm.Data["prometheus.yml"], "dns_sd_configs") || !strings.Contains(cm.Data["prometheus.yml"], ScrapeServiceName) {
		t.Fatal("scrape target missing")
	}
	scrape, err := client.CoreV1().Services(env.Environment.Namespace).Get(context.Background(), ScrapeServiceName, metav1.GetOptions{})
	if err != nil || scrape.Spec.ClusterIP != "None" || scrape.Spec.Selector[kubeenv.RunIDLabelKey] != identity.RunID {
		t.Fatal("scrapes must target individual owned reference pods")
	}
	d, _ := client.AppsV1().Deployments(env.Environment.Namespace).Get(context.Background(), DeploymentName, metav1.GetOptions{})
	if d.Spec.Template.Spec.Containers[0].Image != DefaultPrometheusImage || strings.HasSuffix(d.Spec.Template.Spec.Containers[0].Image, ":latest") {
		t.Fatal("image not pinned")
	}
	s, _ := client.CoreV1().Services(env.Environment.Namespace).Get(context.Background(), ServiceName, metav1.GetOptions{})
	if s.Spec.Type != corev1.ServiceTypeClusterIP || s.Labels[kubeenv.RunIDLabelKey] != identity.RunID {
		t.Fatal("service ownership/type invalid")
	}
	if e = manager.CleanupTelemetry(context.Background(), env); e != nil {
		t.Fatal(e)
	}
	if _, e = client.CoreV1().Namespaces().Get(context.Background(), env.Environment.Namespace, metav1.GetOptions{}); e != nil {
		t.Fatal("namespace removed")
	}
	if e = manager.CleanupTelemetry(context.Background(), env); e != nil {
		t.Fatal(e)
	}
}
func TestTelemetryOwnershipMismatchRejected(t *testing.T) {
	client, verifier, env := setup(t)
	_, _ = client.CoreV1().ConfigMaps(env.Environment.Namespace).Create(context.Background(), &corev1.ConfigMap{ObjectMeta: metav1.ObjectMeta{Name: ConfigMapName, Namespace: env.Environment.Namespace}}, metav1.CreateOptions{})
	manager, _ := NewManager(client, verifier, DefaultConfig())
	if e := manager.CreateTelemetry(context.Background(), env); !errors.Is(e, ErrConflict) {
		t.Fatalf("got %v", e)
	}
}
func TestQuerySnapshotAndUnavailableValues(t *testing.T) {
	calls := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		calls++
		fmt.Fprint(w, fmt.Sprintf(`{"status":"success","data":{"resultType":"vector","result":[{"metric":{},"value":[1,%q]}]}}`, fmt.Sprintf("%d", calls)))
	}))
	defer server.Close()
	client, _ := NewQueryClient(server.URL, nil, time.Second)
	snapshot, e := client.Snapshot(context.Background(), EnvironmentContext{})
	if e != nil {
		t.Fatal(e)
	}
	if *snapshot.RequestRateRPS != 1 || *snapshot.ErrorRatePct != 2 || *snapshot.P95LatencyMS != 3 || *snapshot.AvailabilityPct != 4 {
		t.Fatalf("bad snapshot: %+v", snapshot)
	}
	empty := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		fmt.Fprint(w, `{"status":"success","data":{"resultType":"vector","result":[]}}`)
	}))
	defer empty.Close()
	client, _ = NewQueryClient(empty.URL, nil, time.Second)
	snapshot, e = client.Snapshot(context.Background(), EnvironmentContext{})
	if e != nil || snapshot.RequestRateRPS != nil {
		t.Fatalf("empty fabricated: %+v %v", snapshot, e)
	}
}
func TestQueryFailuresAndTimeout(t *testing.T) {
	for name, body := range map[string]string{"malformed": "{", "status": `{"status":"error","error":"bad"}`, "nan": `{"status":"success","data":{"resultType":"vector","result":[{"value":[1,"NaN"]}]}}`} {
		t.Run(name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { fmt.Fprint(w, body) }))
			defer server.Close()
			client, _ := NewQueryClient(server.URL, nil, time.Second)
			snapshot, e := client.Snapshot(context.Background(), EnvironmentContext{})
			if name == "nan" {
				if e != nil || snapshot.RequestRateRPS != nil {
					t.Fatalf("nonfinite: %+v %v", snapshot, e)
				}
			} else if e == nil {
				t.Fatal("expected error")
			}
		})
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { time.Sleep(100 * time.Millisecond) }))
	defer server.Close()
	client, _ := NewQueryClient(server.URL, nil, 5*time.Millisecond)
	if _, e := client.Snapshot(context.Background(), EnvironmentContext{}); !errors.Is(e, ErrUnavailable) {
		t.Fatalf("timeout: %v", e)
	}
}
