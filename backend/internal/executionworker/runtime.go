package executionworker

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"github.com/evalplane/evalplane/backend/internal/agentgateway"
	"github.com/evalplane/evalplane/backend/internal/execution"
	"github.com/evalplane/evalplane/backend/internal/kubeenv"
	"github.com/evalplane/evalplane/backend/internal/observation"
	"github.com/evalplane/evalplane/backend/internal/scenario"
	"github.com/evalplane/evalplane/backend/internal/scenariorunner"
	"github.com/evalplane/evalplane/backend/internal/telemetry"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	"net/http"
	"time"
)

// KubernetesRuntime composes existing managers. It is intended to run inside the
// cluster so the per-run Prometheus Service DNS is reachable. Tests use Runtime fakes.
type KubernetesRuntime struct {
	client       kubernetes.Interface
	environments *kubeenv.Manager
	scenarios    *scenariorunner.Runner
	telemetry    *telemetry.Manager
}

func NewKubernetesRuntime(client kubernetes.Interface, config scenariorunner.Config) (*KubernetesRuntime, error) {
	if !config.ReferenceInstrumented {
		return nil, fmt.Errorf("worker requires an explicitly configured instrumented reference image")
	}
	environments, err := kubeenv.NewManager(client)
	if err != nil {
		return nil, err
	}
	runner, err := scenariorunner.New(client, config, nil)
	if err != nil {
		return nil, err
	}
	metrics, err := telemetry.NewManager(client, environments, telemetry.DefaultConfig())
	if err != nil {
		return nil, err
	}
	return &KubernetesRuntime{client: client, environments: environments, scenarios: runner, telemetry: metrics}, nil
}
func (r *KubernetesRuntime) Provision(ctx context.Context, id kubeenv.RunIdentity) (kubeenv.Environment, error) {
	return r.environments.CreateEnvironment(ctx, id)
}
func scenarioEnv(env observation.EnvironmentContext) scenariorunner.EnvironmentContext {
	return scenariorunner.EnvironmentContext{Identity: env.Identity, Environment: env.Environment}
}
func (r *KubernetesRuntime) Prepare(ctx context.Context, env observation.EnvironmentContext, _ scenario.Version, hidden execution.HiddenVariant) error {
	variant, err := parseRuntimeVariant(hidden)
	if err != nil {
		return err
	}
	options := scenariorunner.WorkloadOptions{CapacityPerPod: variant.CapacityPerPod}
	if variant.InitialReplicas != nil {
		replicas := int32(*variant.InitialReplicas)
		options.InitialReplicas = &replicas
	}
	if _, err := r.scenarios.DeployReferenceWorkloadWithOptions(ctx, scenarioEnv(env), options); err != nil {
		return err
	}
	if err := r.scenarios.WaitForReferenceReady(ctx, scenarioEnv(env)); err != nil {
		return err
	}
	if err := r.telemetry.CreateTelemetry(ctx, telemetry.EnvironmentContext{Identity: env.Identity, Environment: env.Environment}); err != nil {
		return err
	}
	for {
		d, err := r.client.AppsV1().Deployments(env.Environment.Namespace).Get(ctx, telemetry.DeploymentName, metav1.GetOptions{})
		if err != nil {
			return err
		}
		if d.Status.ObservedGeneration >= d.Generation && d.Status.AvailableReplicas > 0 {
			return nil
		}
		if err = wait(ctx, time.Second); err != nil {
			return err
		}
	}
}
func (r *KubernetesRuntime) Start(ctx context.Context, env observation.EnvironmentContext, version scenario.Version, hidden execution.HiddenVariant) error {
	variant, err := parseRuntimeVariant(hidden)
	if err != nil {
		return err
	}
	profile, err := executionProfile(version)
	if err != nil {
		return err
	}
	if variant.TrafficVUs != nil {
		profile = scaleTrafficProfile(profile, *variant.TrafficVUs)
	}
	if variant.TrafficDelaySeconds != nil && *variant.TrafficDelaySeconds > 0 {
		if err = wait(ctx, time.Duration(*variant.TrafficDelaySeconds)*time.Second); err != nil {
			return err
		}
	}
	_, err = r.scenarios.StartTrafficSpike(ctx, scenarioEnv(env), profile)
	return err
}

type runtimeVariant struct {
	TrafficVUs          *int `json:"traffic_vus"`
	InitialReplicas     *int `json:"initial_replicas"`
	CapacityPerPod      *int `json:"capacity_per_pod"`
	TrafficDelaySeconds *int `json:"traffic_delay_seconds"`
}

func parseRuntimeVariant(hidden execution.HiddenVariant) (runtimeVariant, error) {
	var out runtimeVariant
	if len(hidden.Config) == 0 || string(hidden.Config) == "{}" {
		return out, nil
	}
	decoder := json.NewDecoder(bytes.NewReader(hidden.Config))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&out); err != nil {
		return out, ErrUnsupported
	}
	if out.TrafficVUs != nil && (*out.TrafficVUs < 1 || *out.TrafficVUs > 1000) {
		return out, ErrUnsupported
	}
	if out.InitialReplicas != nil && (*out.InitialReplicas < 1 || *out.InitialReplicas > 20) {
		return out, ErrUnsupported
	}
	if out.CapacityPerPod != nil && (*out.CapacityPerPod < 1 || *out.CapacityPerPod > 10000) {
		return out, ErrUnsupported
	}
	if out.TrafficDelaySeconds != nil && (*out.TrafficDelaySeconds < 0 || *out.TrafficDelaySeconds > 60) {
		return out, ErrUnsupported
	}
	return out, nil
}

func scaleTrafficProfile(profile scenariorunner.TrafficSpikeProfile, peak int) scenariorunner.TrafficSpikeProfile {
	basePeak := 0
	for _, stage := range profile.Stages {
		if stage.Target > basePeak {
			basePeak = stage.Target
		}
	}
	if basePeak == 0 {
		return profile
	}
	out := scenariorunner.TrafficSpikeProfile{Stages: make([]scenariorunner.TrafficStage, len(profile.Stages))}
	for index, stage := range profile.Stages {
		target := 0
		if stage.Target > 0 {
			target = (stage.Target*peak + basePeak/2) / basePeak
			if target < 1 {
				target = 1
			}
		}
		out.Stages[index] = scenariorunner.TrafficStage{Target: target, Duration: stage.Duration}
	}
	return out
}
func (r *KubernetesRuntime) Finish(ctx context.Context, env observation.EnvironmentContext) error {
	return r.scenarios.WaitForTrafficCompletion(ctx, scenarioEnv(env))
}
func (r *KubernetesRuntime) Observe(ctx context.Context, env observation.EnvironmentContext) (agentgateway.Observation, error) {
	query, err := telemetry.NewQueryClient(fmt.Sprintf("http://%s.%s.svc:9090", telemetry.ServiceName, env.Environment.Namespace), &http.Client{}, 5*time.Second)
	if err != nil {
		return agentgateway.Observation{}, err
	}
	provider, err := observation.NewProviderWithTelemetry(r.client, r.environments, query)
	if err != nil {
		return agentgateway.Observation{}, err
	}
	return provider.Observe(ctx, env)
}
func (r *KubernetesRuntime) Cleanup(ctx context.Context, id kubeenv.RunIdentity) error {
	return r.environments.DeleteOwnedEnvironment(ctx, id)
}

// Only the existing traffic_spike capability is executable. Missing profiles use
// the runner's existing development profile; explicit profiles are validated.
func executionProfile(version scenario.Version) (scenariorunner.TrafficSpikeProfile, error) {
	var spec struct {
		Type     string `json:"type"`
		Runnable *bool  `json:"runnable_now"`
		Profile  *struct {
			Stages []struct {
				Target          int `json:"target"`
				DurationSeconds int `json:"duration_seconds"`
			} `json:"stages"`
		} `json:"profile"`
	}
	if json.Unmarshal(version.ExecutionSpec, &spec) != nil || spec.Type != "traffic_spike" || (spec.Runnable != nil && !*spec.Runnable) {
		return scenariorunner.TrafficSpikeProfile{}, ErrUnsupported
	}
	if spec.Profile == nil {
		return scenariorunner.DevelopmentTrafficSpikeProfile(), nil
	}
	out := scenariorunner.TrafficSpikeProfile{}
	if len(spec.Profile.Stages) == 0 || len(spec.Profile.Stages) > 20 {
		return out, ErrUnsupported
	}
	total := 0
	for _, s := range spec.Profile.Stages {
		if s.Target < 0 || s.Target > 1000 || s.DurationSeconds < 1 || s.DurationSeconds > 3600 {
			return out, ErrUnsupported
		}
		total += s.DurationSeconds
		out.Stages = append(out.Stages, scenariorunner.TrafficStage{Target: s.Target, Duration: time.Duration(s.DurationSeconds) * time.Second})
	}
	if total > 3600 {
		return out, ErrUnsupported
	}
	return out, nil
}
