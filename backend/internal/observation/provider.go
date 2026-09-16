package observation

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"time"

	"github.com/evalplane/evalplane/backend/internal/agentgateway"
	"github.com/evalplane/evalplane/backend/internal/kubeenv"
	"github.com/evalplane/evalplane/backend/internal/telemetry"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
)

var ErrOwnership = errors.New("observation ownership mismatch")

type EnvironmentContext struct {
	Identity    kubeenv.RunIdentity
	Environment kubeenv.Environment
}
type EnvironmentVerifier interface {
	GetEnvironment(context.Context, string) (kubeenv.Environment, error)
}
type Provider struct {
	client    kubernetes.Interface
	verifier  EnvironmentVerifier
	telemetry TelemetryProvider
}
type TelemetryProvider interface {
	Snapshot(context.Context, telemetry.EnvironmentContext) (telemetry.Snapshot, error)
}

func NewProvider(client kubernetes.Interface, verifier EnvironmentVerifier) (*Provider, error) {
	if client == nil || verifier == nil {
		return nil, errors.New("Kubernetes client and environment verifier are required")
	}
	return &Provider{client: client, verifier: verifier}, nil
}
func NewProviderWithTelemetry(client kubernetes.Interface, verifier EnvironmentVerifier, provider TelemetryProvider) (*Provider, error) {
	value, err := NewProvider(client, verifier)
	if err != nil {
		return nil, err
	}
	value.telemetry = provider
	return value, nil
}

func (p *Provider) Observe(ctx context.Context, env EnvironmentContext) (agentgateway.Observation, error) {
	verified, err := p.verifier.GetEnvironment(ctx, env.Identity.RunID)
	if err != nil {
		return agentgateway.Observation{}, err
	}
	if verified.Namespace != env.Environment.Namespace || env.Environment.RunID != env.Identity.RunID {
		return agentgateway.Observation{}, ErrOwnership
	}
	labels := fmt.Sprintf("%s=true,%s=%s", kubeenv.ManagedLabelKey, kubeenv.RunIDLabelKey, env.Identity.RunID)
	deployments, err := p.client.AppsV1().Deployments(verified.Namespace).List(ctx, metav1.ListOptions{LabelSelector: labels})
	if err != nil {
		return agentgateway.Observation{}, fmt.Errorf("list owned deployments: %w", err)
	}
	pods, err := p.client.CoreV1().Pods(verified.Namespace).List(ctx, metav1.ListOptions{LabelSelector: labels})
	if err != nil {
		return agentgateway.Observation{}, fmt.Errorf("list owned pods: %w", err)
	}
	events, err := p.client.CoreV1().Events(verified.Namespace).List(ctx, metav1.ListOptions{})
	if err != nil {
		return agentgateway.Observation{}, fmt.Errorf("list namespace events: %w", err)
	}
	out := agentgateway.Observation{Timestamp: time.Now().UTC(), Workloads: make([]agentgateway.WorkloadObservation, 0, len(deployments.Items)), Pods: make([]agentgateway.PodObservation, 0, len(pods.Items)), Metrics: map[string]any{}, RecentEvents: []agentgateway.RecentEvent{}}
	for i := range deployments.Items {
		if !owned(deployments.Items[i].Labels, env.Identity) {
			continue
		}
		out.Workloads = append(out.Workloads, workload(&deployments.Items[i]))
	}
	for i := range pods.Items {
		if !owned(pods.Items[i].Labels, env.Identity) {
			continue
		}
		out.Pods = append(out.Pods, pod(&pods.Items[i]))
	}
	for _, event := range events.Items {
		out.RecentEvents = append(out.RecentEvents, agentgateway.RecentEvent{Timestamp: eventTime(event), Type: event.Type, Reason: event.Reason, Summary: event.Message, InvolvedObjectKind: event.InvolvedObject.Kind, InvolvedObjectName: event.InvolvedObject.Name})
	}
	sort.Slice(out.Workloads, func(i, j int) bool { return out.Workloads[i].Name < out.Workloads[j].Name })
	sort.Slice(out.Pods, func(i, j int) bool { return out.Pods[i].Name < out.Pods[j].Name })
	sort.Slice(out.RecentEvents, func(i, j int) bool { return out.RecentEvents[i].Timestamp.Before(out.RecentEvents[j].Timestamp) })
	if p.telemetry != nil {
		out.Metrics["telemetry_available"] = false
		snapshot, telemetryErr := p.telemetry.Snapshot(ctx, telemetry.EnvironmentContext{Identity: env.Identity, Environment: env.Environment})
		if telemetryErr == nil {
			out.Metrics["telemetry_available"] = true
			setMetric(out.Metrics, "request_rate_rps", snapshot.RequestRateRPS)
			setMetric(out.Metrics, "error_rate_pct", snapshot.ErrorRatePct)
			setMetric(out.Metrics, "p95_latency_ms", snapshot.P95LatencyMS)
			setMetric(out.Metrics, "availability_pct", snapshot.AvailabilityPct)
		}
	}
	return out, nil
}
func setMetric(metrics map[string]any, name string, value *float64) {
	if value != nil {
		metrics[name] = *value
	}
}

func owned(labels map[string]string, id kubeenv.RunIdentity) bool {
	return labels[kubeenv.ManagedLabelKey] == "true" && labels[kubeenv.RunIDLabelKey] == id.RunID && labels[kubeenv.BatchIDLabelKey] == id.BatchID && labels[kubeenv.WorkspaceIDLabelKey] == id.WorkspaceID
}
func workload(d *appsv1.Deployment) agentgateway.WorkloadObservation {
	w := agentgateway.WorkloadObservation{Kind: "Deployment", Name: d.Name, Namespace: d.Namespace, DesiredReplicas: d.Status.Replicas, ReadyReplicas: d.Status.ReadyReplicas, AvailableReplicas: d.Status.AvailableReplicas, UpdatedReplicas: d.Status.UpdatedReplicas}
	if d.Spec.Replicas != nil {
		w.DesiredReplicas = *d.Spec.Replicas
	}
	if len(d.Spec.Template.Spec.Containers) > 0 {
		r := d.Spec.Template.Spec.Containers[0].Resources
		w.CPURequest = r.Requests.Cpu().String()
		w.CPULimit = r.Limits.Cpu().String()
		w.MemoryRequest = r.Requests.Memory().String()
		w.MemoryLimit = r.Limits.Memory().String()
	}
	return w
}
func pod(p *corev1.Pod) agentgateway.PodObservation {
	o := agentgateway.PodObservation{Name: p.Name, Namespace: p.Namespace, Phase: string(p.Status.Phase)}
	for _, s := range p.Status.ContainerStatuses {
		o.RestartCount += s.RestartCount
		o.Ready = o.Ready || s.Ready
	}
	for _, owner := range p.OwnerReferences {
		if owner.Controller != nil && *owner.Controller {
			o.OwningWorkload = owner.Name
			break
		}
	}
	return o
}
func eventTime(e corev1.Event) time.Time {
	if !e.EventTime.IsZero() {
		return e.EventTime.Time
	}
	if !e.LastTimestamp.IsZero() {
		return e.LastTimestamp.Time
	}
	return e.CreationTimestamp.Time
}
