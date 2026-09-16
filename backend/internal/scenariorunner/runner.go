package scenariorunner

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/evalplane/evalplane/backend/internal/evidence"
	"github.com/evalplane/evalplane/backend/internal/kubeenv"
	appsv1 "k8s.io/api/apps/v1"
	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/intstr"
	"k8s.io/client-go/kubernetes"
)

type EventSink interface {
	AppendRunEvent(context.Context, evidence.AppendRunEventInput) (evidence.RunEvent, error)
}

type Runner struct {
	client kubernetes.Interface
	config Config
	events EventSink
}

func New(client kubernetes.Interface, config Config, events EventSink) (*Runner, error) {
	if client == nil {
		return nil, errors.New("Kubernetes client is required")
	}
	if err := validateConfig(config); err != nil {
		return nil, err
	}
	return &Runner{client: client, config: config, events: events}, nil
}

func (r *Runner) DeployReferenceWorkload(ctx context.Context, environment EnvironmentContext) (Workload, error) {
	return r.DeployReferenceWorkloadWithOptions(ctx, environment, WorkloadOptions{})
}

func (r *Runner) DeployReferenceWorkloadWithOptions(ctx context.Context, environment EnvironmentContext, options WorkloadOptions) (Workload, error) {
	if options.InitialReplicas != nil && (*options.InitialReplicas < 1 || *options.InitialReplicas > 20) {
		return Workload{}, errors.New("initial replicas must be between 1 and 20")
	}
	if options.CapacityPerPod != nil && (*options.CapacityPerPod < 1 || *options.CapacityPerPod > 10000) {
		return Workload{}, errors.New("capacity per pod must be between 1 and 10000")
	}
	environment, err := r.verifyEnvironment(ctx, environment)
	if err != nil {
		return Workload{}, err
	}
	labels := resourceLabels(environment.Identity, ReferenceComponent)
	selector := map[string]string{kubeenv.RunIDLabelKey: environment.Identity.RunID, ComponentLabelKey: ReferenceComponent}
	replicas := r.config.ReferenceReplicas
	if options.InitialReplicas != nil {
		replicas = *options.InitialReplicas
	}
	containerPort := int32(80)
	probePath := "/"
	if r.config.ReferenceInstrumented {
		containerPort = 8080
		probePath = "/healthz"
	}
	container := corev1.Container{
		Name:  "reference-app",
		Image: r.config.ReferenceImage,
		Ports: []corev1.ContainerPort{{Name: "http", ContainerPort: containerPort, Protocol: corev1.ProtocolTCP}},
		Resources: corev1.ResourceRequirements{
			Requests: corev1.ResourceList{corev1.ResourceCPU: resource.MustParse("100m"), corev1.ResourceMemory: resource.MustParse("128Mi")},
			Limits:   corev1.ResourceList{corev1.ResourceCPU: resource.MustParse("500m"), corev1.ResourceMemory: resource.MustParse("256Mi")},
		},
		ReadinessProbe: httpProbe(probePath),
		LivenessProbe:  httpProbe(probePath),
	}
	if options.CapacityPerPod != nil {
		container.Env = []corev1.EnvVar{{Name: "MAX_CONCURRENT_WORK", Value: fmt.Sprintf("%d", *options.CapacityPerPod)}}
	}
	desiredDeployment := &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{Name: ReferenceDeploymentName, Namespace: environment.Environment.Namespace, Labels: labels},
		Spec: appsv1.DeploymentSpec{
			Replicas: &replicas,
			Selector: &metav1.LabelSelector{MatchLabels: selector},
			Template: corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{Labels: labels},
				Spec:       corev1.PodSpec{Containers: []corev1.Container{container}},
			},
		},
	}
	deployments := r.client.AppsV1().Deployments(environment.Environment.Namespace)
	existingDeployment, err := deployments.Get(ctx, ReferenceDeploymentName, metav1.GetOptions{})
	switch {
	case apierrors.IsNotFound(err):
		_, err = deployments.Create(ctx, desiredDeployment, metav1.CreateOptions{})
	case err != nil:
		return Workload{}, kubernetesError("get reference deployment", err)
	default:
		if err = verifyResourceOwnership(existingDeployment.Labels, environment.Identity, ReferenceComponent); err != nil {
			return Workload{}, conflict(ReferenceDeploymentName, err)
		}
		desiredDeployment.ResourceVersion = existingDeployment.ResourceVersion
		_, err = deployments.Update(ctx, desiredDeployment, metav1.UpdateOptions{})
	}
	if err != nil {
		return Workload{}, kubernetesError("ensure reference deployment", err)
	}

	desiredService := &corev1.Service{
		ObjectMeta: metav1.ObjectMeta{Name: ReferenceServiceName, Namespace: environment.Environment.Namespace, Labels: labels},
		Spec: corev1.ServiceSpec{
			Type:     corev1.ServiceTypeClusterIP,
			Selector: selector,
			Ports:    []corev1.ServicePort{{Name: "http", Port: 80, TargetPort: intstr.FromString("http"), Protocol: corev1.ProtocolTCP}},
		},
	}
	services := r.client.CoreV1().Services(environment.Environment.Namespace)
	existingService, err := services.Get(ctx, ReferenceServiceName, metav1.GetOptions{})
	switch {
	case apierrors.IsNotFound(err):
		_, err = services.Create(ctx, desiredService, metav1.CreateOptions{})
	case err != nil:
		return Workload{}, kubernetesError("get reference service", err)
	default:
		if err = verifyResourceOwnership(existingService.Labels, environment.Identity, ReferenceComponent); err != nil {
			return Workload{}, conflict(ReferenceServiceName, err)
		}
	}
	if err != nil {
		return Workload{}, kubernetesError("ensure reference service", err)
	}
	return Workload{Namespace: environment.Environment.Namespace, Deployment: ReferenceDeploymentName, Service: ReferenceServiceName}, nil
}

func (r *Runner) WaitForReferenceReady(ctx context.Context, environment EnvironmentContext) error {
	environment, err := r.verifyEnvironment(ctx, environment)
	if err != nil {
		return err
	}
	waitCtx, cancel := context.WithTimeout(ctx, r.config.ReferenceReadyTimeout)
	defer cancel()
	ticker := time.NewTicker(r.config.PollInterval)
	defer ticker.Stop()
	for {
		deployment, getErr := r.client.AppsV1().Deployments(environment.Environment.Namespace).Get(waitCtx, ReferenceDeploymentName, metav1.GetOptions{})
		if getErr != nil {
			return kubernetesError("get reference deployment readiness", getErr)
		}
		if err = verifyResourceOwnership(deployment.Labels, environment.Identity, ReferenceComponent); err != nil {
			return err
		}
		expected := r.config.ReferenceReplicas
		if deployment.Spec.Replicas != nil {
			expected = *deployment.Spec.Replicas
		}
		if deployment.Status.ObservedGeneration >= deployment.Generation && deployment.Status.ReadyReplicas >= expected && deployment.Status.AvailableReplicas >= expected {
			return nil
		}
		select {
		case <-waitCtx.Done():
			return fmt.Errorf("wait for reference workload readiness: %w", waitCtx.Err())
		case <-ticker.C:
		}
	}
}

func (r *Runner) StartTrafficSpike(ctx context.Context, environment EnvironmentContext, profile TrafficSpikeProfile) (TrafficExecution, error) {
	if err := validateProfile(profile); err != nil {
		return TrafficExecution{}, err
	}
	environment, err := r.verifyEnvironment(ctx, environment)
	if err != nil {
		return TrafficExecution{}, err
	}
	labels := resourceLabels(environment.Identity, TrafficComponent)
	jobs := r.client.BatchV1().Jobs(environment.Environment.Namespace)
	existingJob, err := jobs.Get(ctx, TrafficJobName, metav1.GetOptions{})
	if err == nil {
		if err = verifyResourceOwnership(existingJob.Labels, environment.Identity, TrafficComponent); err != nil {
			return TrafficExecution{}, conflict(TrafficJobName, err)
		}
		return TrafficExecution{Namespace: environment.Environment.Namespace, ConfigMap: TrafficConfigMapName, Job: TrafficJobName}, nil
	}
	if !apierrors.IsNotFound(err) {
		return TrafficExecution{}, kubernetesError("get traffic job", err)
	}

	configMaps := r.client.CoreV1().ConfigMaps(environment.Environment.Namespace)
	desiredConfigMap := &corev1.ConfigMap{ObjectMeta: metav1.ObjectMeta{Name: TrafficConfigMapName, Namespace: environment.Environment.Namespace, Labels: labels}, Data: map[string]string{"script.js": k6Script(profile)}}
	existingConfigMap, err := configMaps.Get(ctx, TrafficConfigMapName, metav1.GetOptions{})
	switch {
	case apierrors.IsNotFound(err):
		_, err = configMaps.Create(ctx, desiredConfigMap, metav1.CreateOptions{})
	case err != nil:
		return TrafficExecution{}, kubernetesError("get traffic script config map", err)
	default:
		if err = verifyResourceOwnership(existingConfigMap.Labels, environment.Identity, TrafficComponent); err != nil {
			return TrafficExecution{}, conflict(TrafficConfigMapName, err)
		}
		desiredConfigMap.ResourceVersion = existingConfigMap.ResourceVersion
		_, err = configMaps.Update(ctx, desiredConfigMap, metav1.UpdateOptions{})
	}
	if err != nil {
		return TrafficExecution{}, kubernetesError("ensure traffic script config map", err)
	}

	backoffLimit := int32(0)
	activeDeadline := int64((profileDuration(profile) + time.Minute).Seconds())
	job := &batchv1.Job{
		ObjectMeta: metav1.ObjectMeta{Name: TrafficJobName, Namespace: environment.Environment.Namespace, Labels: labels},
		Spec: batchv1.JobSpec{
			BackoffLimit:          &backoffLimit,
			ActiveDeadlineSeconds: &activeDeadline,
			Template: corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{Labels: labels},
				Spec: corev1.PodSpec{
					RestartPolicy: corev1.RestartPolicyNever,
					Containers: []corev1.Container{{
						Name:         "k6",
						Image:        r.config.TrafficImage,
						Args:         []string{"run", "/scripts/script.js"},
						VolumeMounts: []corev1.VolumeMount{{Name: "script", MountPath: "/scripts", ReadOnly: true}},
						Resources: corev1.ResourceRequirements{
							Requests: corev1.ResourceList{corev1.ResourceCPU: resource.MustParse("100m"), corev1.ResourceMemory: resource.MustParse("128Mi")},
							Limits:   corev1.ResourceList{corev1.ResourceCPU: resource.MustParse("1"), corev1.ResourceMemory: resource.MustParse("512Mi")},
						},
					}},
					Volumes: []corev1.Volume{{Name: "script", VolumeSource: corev1.VolumeSource{ConfigMap: &corev1.ConfigMapVolumeSource{LocalObjectReference: corev1.LocalObjectReference{Name: TrafficConfigMapName}}}}},
				},
			},
		},
	}
	if _, err = jobs.Create(ctx, job, metav1.CreateOptions{}); err != nil {
		return TrafficExecution{}, kubernetesError("create traffic job", err)
	}
	return TrafficExecution{Namespace: environment.Environment.Namespace, ConfigMap: TrafficConfigMapName, Job: TrafficJobName}, nil
}

func (r *Runner) WaitForTrafficCompletion(ctx context.Context, environment EnvironmentContext) error {
	environment, err := r.verifyEnvironment(ctx, environment)
	if err != nil {
		return err
	}
	waitCtx, cancel := context.WithTimeout(ctx, r.config.TrafficTimeout)
	defer cancel()
	ticker := time.NewTicker(r.config.PollInterval)
	defer ticker.Stop()
	for {
		job, getErr := r.client.BatchV1().Jobs(environment.Environment.Namespace).Get(waitCtx, TrafficJobName, metav1.GetOptions{})
		if getErr != nil {
			return kubernetesError("get traffic job completion", getErr)
		}
		if err = verifyResourceOwnership(job.Labels, environment.Identity, TrafficComponent); err != nil {
			return err
		}
		for _, condition := range job.Status.Conditions {
			if condition.Status != corev1.ConditionTrue {
				continue
			}
			switch condition.Type {
			case batchv1.JobComplete:
				return nil
			case batchv1.JobFailed:
				return ErrTrafficFailed
			}
		}
		select {
		case <-waitCtx.Done():
			return fmt.Errorf("wait for traffic completion: %w", waitCtx.Err())
		case <-ticker.C:
		}
	}
}

func (r *Runner) RunTrafficSpikeScenario(ctx context.Context, environment EnvironmentContext, profile TrafficSpikeProfile) (result ScenarioResult, err error) {
	result = ScenarioResult{ExecutionType: "traffic_spike"}
	fail := func(runErr error) (ScenarioResult, error) {
		_ = r.appendEvent(ctx, environment.Identity.RunID, "system", "traffic spike scenario failed")
		return result, runErr
	}
	if err = r.appendEvent(ctx, environment.Identity.RunID, "system", "reference workload deployment started"); err != nil {
		return result, err
	}
	if _, err = r.DeployReferenceWorkload(ctx, environment); err != nil {
		return fail(err)
	}
	if err = r.WaitForReferenceReady(ctx, environment); err != nil {
		return fail(err)
	}
	if err = r.appendEvent(ctx, environment.Identity.RunID, "system", "reference workload ready"); err != nil {
		return result, err
	}
	if err = r.appendEvent(ctx, environment.Identity.RunID, "scenario", "traffic spike started"); err != nil {
		return result, err
	}
	if _, err = r.StartTrafficSpike(ctx, environment, profile); err != nil {
		return fail(err)
	}
	if err = r.WaitForTrafficCompletion(ctx, environment); err != nil {
		return fail(err)
	}
	if err = r.appendEvent(ctx, environment.Identity.RunID, "scenario", "traffic spike completed"); err != nil {
		return result, err
	}
	result.Succeeded = true
	return result, nil
}

func (r *Runner) CleanupScenarioResources(ctx context.Context, environment EnvironmentContext) error {
	environment, err := r.verifyEnvironment(ctx, environment)
	if err != nil {
		return err
	}
	namespace := environment.Environment.Namespace
	type cleanup struct {
		name      string
		component string
		get       func() (map[string]string, error)
		remove    func() error
	}
	resources := []cleanup{
		{name: TrafficJobName, component: TrafficComponent, get: func() (map[string]string, error) {
			value, e := r.client.BatchV1().Jobs(namespace).Get(ctx, TrafficJobName, metav1.GetOptions{})
			if e != nil {
				return nil, e
			}
			return value.Labels, nil
		}, remove: func() error {
			return r.client.BatchV1().Jobs(namespace).Delete(ctx, TrafficJobName, metav1.DeleteOptions{})
		}},
		{name: TrafficConfigMapName, component: TrafficComponent, get: func() (map[string]string, error) {
			value, e := r.client.CoreV1().ConfigMaps(namespace).Get(ctx, TrafficConfigMapName, metav1.GetOptions{})
			if e != nil {
				return nil, e
			}
			return value.Labels, nil
		}, remove: func() error {
			return r.client.CoreV1().ConfigMaps(namespace).Delete(ctx, TrafficConfigMapName, metav1.DeleteOptions{})
		}},
		{name: ReferenceServiceName, component: ReferenceComponent, get: func() (map[string]string, error) {
			value, e := r.client.CoreV1().Services(namespace).Get(ctx, ReferenceServiceName, metav1.GetOptions{})
			if e != nil {
				return nil, e
			}
			return value.Labels, nil
		}, remove: func() error {
			return r.client.CoreV1().Services(namespace).Delete(ctx, ReferenceServiceName, metav1.DeleteOptions{})
		}},
		{name: ReferenceDeploymentName, component: ReferenceComponent, get: func() (map[string]string, error) {
			value, e := r.client.AppsV1().Deployments(namespace).Get(ctx, ReferenceDeploymentName, metav1.GetOptions{})
			if e != nil {
				return nil, e
			}
			return value.Labels, nil
		}, remove: func() error {
			return r.client.AppsV1().Deployments(namespace).Delete(ctx, ReferenceDeploymentName, metav1.DeleteOptions{})
		}},
	}
	present := make([]cleanup, 0, len(resources))
	for _, resource := range resources {
		labels, getErr := resource.get()
		if apierrors.IsNotFound(getErr) {
			continue
		}
		if getErr != nil {
			return kubernetesError("inspect scenario resource before cleanup", getErr)
		}
		if err = verifyResourceOwnership(labels, environment.Identity, resource.component); err != nil {
			return conflict(resource.name, err)
		}
		present = append(present, resource)
	}
	for _, resource := range present {
		if deleteErr := resource.remove(); deleteErr != nil && !apierrors.IsNotFound(deleteErr) {
			return kubernetesError("delete scenario resource", deleteErr)
		}
	}
	return nil
}

func (r *Runner) verifyEnvironment(ctx context.Context, value EnvironmentContext) (EnvironmentContext, error) {
	value, err := validateEnvironment(value)
	if err != nil {
		return value, err
	}
	namespace, err := r.client.CoreV1().Namespaces().Get(ctx, value.Environment.Namespace, metav1.GetOptions{})
	if apierrors.IsNotFound(err) {
		return value, kubeenv.ErrEnvironmentNotFound
	}
	if err != nil {
		return value, kubernetesError("get evaluation namespace", err)
	}
	labels := namespace.Labels
	if labels[kubeenv.ManagedLabelKey] != "true" || labels[kubeenv.RunIDLabelKey] != value.Identity.RunID || labels[kubeenv.BatchIDLabelKey] != value.Identity.BatchID || labels[kubeenv.WorkspaceIDLabelKey] != value.Identity.WorkspaceID {
		return value, kubeenv.ErrEnvironmentOwnership
	}
	return value, nil
}

func (r *Runner) appendEvent(ctx context.Context, runID, eventType, summary string) error {
	if r.events == nil {
		return nil
	}
	_, err := r.events.AppendRunEvent(ctx, evidence.AppendRunEventInput{RunID: runID, EventType: eventType, OccurredAt: time.Now().UTC(), Summary: summary, Payload: json.RawMessage(`{}`)})
	return err
}
