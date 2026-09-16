package telemetry

import (
	"context"
	"errors"
	"fmt"
	"github.com/evalplane/evalplane/backend/internal/kubeenv"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/intstr"
	"k8s.io/client-go/kubernetes"
	"strings"
)

type EnvironmentVerifier interface {
	GetEnvironment(context.Context, string) (kubeenv.Environment, error)
}
type Manager struct {
	client   kubernetes.Interface
	verifier EnvironmentVerifier
	config   Config
}

func NewManager(client kubernetes.Interface, verifier EnvironmentVerifier, config Config) (*Manager, error) {
	if client == nil || verifier == nil {
		return nil, errors.New("Kubernetes client and environment verifier are required")
	}
	if config.ScrapeInterval <= 0 || config.PrometheusImage == "" || strings.HasSuffix(config.PrometheusImage, ":latest") {
		return nil, errors.New("valid pinned Prometheus image and scrape interval are required")
	}
	return &Manager{client: client, verifier: verifier, config: config}, nil
}
func (m *Manager) CreateTelemetry(ctx context.Context, env EnvironmentContext) error {
	if err := m.verifyEnvironment(ctx, env); err != nil {
		return err
	}
	ns := env.Environment.Namespace
	l := labels(env.Identity)
	// Scrape each pod separately: a load-balanced Service mixes counter sources.
	scrape := &corev1.Service{ObjectMeta: metav1.ObjectMeta{Name: ScrapeServiceName, Namespace: ns, Labels: l}, Spec: corev1.ServiceSpec{ClusterIP: corev1.ClusterIPNone, Selector: map[string]string{ComponentLabelKey: "reference-app", kubeenv.RunIDLabelKey: env.Identity.RunID}, Ports: []corev1.ServicePort{{Name: "metrics", Port: 8080, TargetPort: intstr.FromInt32(8080)}}}}
	if err := ensureService(m.client, ctx, scrape, env.Identity); err != nil {
		return err
	}
	config := fmt.Sprintf("global:\n  scrape_interval: %s\nscrape_configs:\n  - job_name: eval-reference-app\n    metrics_path: /metrics\n    dns_sd_configs:\n      - names: ['%s.%s.svc.cluster.local']\n        type: A\n        port: 8080\n        refresh_interval: 5s\n", m.config.ScrapeInterval, ScrapeServiceName, ns)
	cm := &corev1.ConfigMap{ObjectMeta: metav1.ObjectMeta{Name: ConfigMapName, Namespace: ns, Labels: l}, Data: map[string]string{"prometheus.yml": config}}
	if err := ensure(m.client.CoreV1().ConfigMaps(ns), ctx, cm, env.Identity); err != nil {
		return err
	}
	replicas := int32(1)
	d := &appsv1.Deployment{ObjectMeta: metav1.ObjectMeta{Name: DeploymentName, Namespace: ns, Labels: l}, Spec: appsv1.DeploymentSpec{Replicas: &replicas, Selector: &metav1.LabelSelector{MatchLabels: map[string]string{ComponentLabelKey: TelemetryComponent, kubeenv.RunIDLabelKey: env.Identity.RunID}}, Template: corev1.PodTemplateSpec{ObjectMeta: metav1.ObjectMeta{Labels: l}, Spec: corev1.PodSpec{Containers: []corev1.Container{{Name: "prometheus", Image: m.config.PrometheusImage, Args: []string{"--config.file=/etc/prometheus/prometheus.yml", "--storage.tsdb.retention.time=2h"}, Ports: []corev1.ContainerPort{{Name: "http", ContainerPort: 9090}}, Resources: corev1.ResourceRequirements{Requests: corev1.ResourceList{corev1.ResourceCPU: resource.MustParse("100m"), corev1.ResourceMemory: resource.MustParse("128Mi")}, Limits: corev1.ResourceList{corev1.ResourceCPU: resource.MustParse("500m"), corev1.ResourceMemory: resource.MustParse("512Mi")}}, VolumeMounts: []corev1.VolumeMount{{Name: "config", MountPath: "/etc/prometheus"}}}}, Volumes: []corev1.Volume{{Name: "config", VolumeSource: corev1.VolumeSource{ConfigMap: &corev1.ConfigMapVolumeSource{LocalObjectReference: corev1.LocalObjectReference{Name: ConfigMapName}}}}}}}}}
	if err := ensureDeployment(m.client, ctx, d, env.Identity); err != nil {
		return err
	}
	s := &corev1.Service{ObjectMeta: metav1.ObjectMeta{Name: ServiceName, Namespace: ns, Labels: l}, Spec: corev1.ServiceSpec{Type: corev1.ServiceTypeClusterIP, Selector: map[string]string{ComponentLabelKey: TelemetryComponent, kubeenv.RunIDLabelKey: env.Identity.RunID}, Ports: []corev1.ServicePort{{Name: "http", Port: 9090, TargetPort: intstr.FromString("http")}}}}
	return ensureService(m.client, ctx, s, env.Identity)
}
func (m *Manager) CleanupTelemetry(ctx context.Context, env EnvironmentContext) error {
	if err := m.verifyEnvironment(ctx, env); err != nil {
		return err
	}
	ns := env.Environment.Namespace
	checks := []struct {
		name string
		get  func() (map[string]string, error)
		del  func() error
	}{{DeploymentName, func() (map[string]string, error) {
		v, e := m.client.AppsV1().Deployments(ns).Get(ctx, DeploymentName, metav1.GetOptions{})
		if e != nil {
			return nil, e
		}
		return v.Labels, nil
	}, func() error {
		return m.client.AppsV1().Deployments(ns).Delete(ctx, DeploymentName, metav1.DeleteOptions{})
	}}, {ServiceName, func() (map[string]string, error) {
		v, e := m.client.CoreV1().Services(ns).Get(ctx, ServiceName, metav1.GetOptions{})
		if e != nil {
			return nil, e
		}
		return v.Labels, nil
	}, func() error { return m.client.CoreV1().Services(ns).Delete(ctx, ServiceName, metav1.DeleteOptions{}) }}, {ScrapeServiceName, func() (map[string]string, error) {
		v, e := m.client.CoreV1().Services(ns).Get(ctx, ScrapeServiceName, metav1.GetOptions{})
		if e != nil {
			return nil, e
		}
		return v.Labels, nil
	}, func() error {
		return m.client.CoreV1().Services(ns).Delete(ctx, ScrapeServiceName, metav1.DeleteOptions{})
	}}, {ConfigMapName, func() (map[string]string, error) {
		v, e := m.client.CoreV1().ConfigMaps(ns).Get(ctx, ConfigMapName, metav1.GetOptions{})
		if e != nil {
			return nil, e
		}
		return v.Labels, nil
	}, func() error {
		return m.client.CoreV1().ConfigMaps(ns).Delete(ctx, ConfigMapName, metav1.DeleteOptions{})
	}}}
	for _, x := range checks {
		l, e := x.get()
		if apierrors.IsNotFound(e) {
			continue
		}
		if e != nil {
			return e
		}
		if !owned(l, env.Identity) {
			return fmt.Errorf("%w: %s", ErrOwnership, x.name)
		}
		if e = x.del(); e != nil && !apierrors.IsNotFound(e) {
			return e
		}
	}
	return nil
}
func (m *Manager) verifyEnvironment(ctx context.Context, env EnvironmentContext) error {
	v, e := m.verifier.GetEnvironment(ctx, env.Identity.RunID)
	if e != nil {
		return e
	}
	if v.Namespace != env.Environment.Namespace || env.Environment.RunID != env.Identity.RunID {
		return ErrOwnership
	}
	return nil
}
func labels(id kubeenv.RunIdentity) map[string]string {
	return map[string]string{kubeenv.ManagedLabelKey: "true", kubeenv.RunIDLabelKey: id.RunID, kubeenv.BatchIDLabelKey: id.BatchID, kubeenv.WorkspaceIDLabelKey: id.WorkspaceID, kubeenv.ManagedByLabelKey: "evalplane", ComponentLabelKey: TelemetryComponent}
}
func owned(l map[string]string, id kubeenv.RunIdentity) bool {
	return l[kubeenv.ManagedLabelKey] == "true" && l[kubeenv.RunIDLabelKey] == id.RunID && l[kubeenv.BatchIDLabelKey] == id.BatchID && l[kubeenv.WorkspaceIDLabelKey] == id.WorkspaceID && l[ComponentLabelKey] == TelemetryComponent
}

type configMaps interface {
	Create(context.Context, *corev1.ConfigMap, metav1.CreateOptions) (*corev1.ConfigMap, error)
	Get(context.Context, string, metav1.GetOptions) (*corev1.ConfigMap, error)
}

func ensure(c configMaps, ctx context.Context, want *corev1.ConfigMap, id kubeenv.RunIdentity) error {
	got, e := c.Create(ctx, want, metav1.CreateOptions{})
	if apierrors.IsAlreadyExists(e) {
		got, e = c.Get(ctx, want.Name, metav1.GetOptions{})
	}
	if e != nil {
		return e
	}
	if !owned(got.Labels, id) {
		return fmt.Errorf("%w: %s", ErrConflict, want.Name)
	}
	return nil
}
func ensureDeployment(c kubernetes.Interface, ctx context.Context, w *appsv1.Deployment, id kubeenv.RunIdentity) error {
	got, e := c.AppsV1().Deployments(w.Namespace).Create(ctx, w, metav1.CreateOptions{})
	if apierrors.IsAlreadyExists(e) {
		got, e = c.AppsV1().Deployments(w.Namespace).Get(ctx, w.Name, metav1.GetOptions{})
	}
	if e != nil {
		return e
	}
	if !owned(got.Labels, id) {
		return fmt.Errorf("%w: %s", ErrConflict, w.Name)
	}
	return nil
}
func ensureService(c kubernetes.Interface, ctx context.Context, w *corev1.Service, id kubeenv.RunIdentity) error {
	got, e := c.CoreV1().Services(w.Namespace).Create(ctx, w, metav1.CreateOptions{})
	if apierrors.IsAlreadyExists(e) {
		got, e = c.CoreV1().Services(w.Namespace).Get(ctx, w.Name, metav1.GetOptions{})
	}
	if e != nil {
		return e
	}
	if !owned(got.Labels, id) {
		return fmt.Errorf("%w: %s", ErrConflict, w.Name)
	}
	return nil
}
