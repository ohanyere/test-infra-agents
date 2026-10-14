package scenariorunner

import (
	"errors"
	"time"

	"github.com/evalplane/evalplane/backend/internal/kubeenv"
)

var (
	ErrResourceConflict  = errors.New("scenario resource conflict")
	ErrResourceOwnership = errors.New("scenario resource ownership mismatch")
	ErrTrafficFailed     = errors.New("traffic job failed")
)

const (
	ReferenceDeploymentName = "eval-reference-app"
	ReferenceServiceName    = "eval-reference-app"
	TrafficConfigMapName    = "eval-traffic-script"
	TrafficJobName          = "eval-traffic"

	ComponentLabelKey  = "evalplane.dev/component"
	ReferenceComponent = "reference-app"
	TrafficComponent   = "traffic-generator"
)

type EnvironmentContext struct {
	Identity    kubeenv.RunIdentity
	Environment kubeenv.Environment
}

type Workload struct {
	Namespace  string
	Deployment string
	Service    string
}

type WorkloadOptions struct {
	InitialReplicas *int32
	CapacityPerPod  *int
}

type TrafficExecution struct {
	Namespace string
	ConfigMap string
	Job       string
}

type TrafficStage struct {
	Target   int
	Duration time.Duration
}

type TrafficSpikeProfile struct {
	Stages   []TrafficStage
	StartVUs *int
	// RequestPath optionally selects the uninstrumented reference-app health
	// endpoint. Empty preserves the existing root-path behavior.
	RequestPath string
}

func DevelopmentTrafficSpikeProfile() TrafficSpikeProfile {
	return TrafficSpikeProfile{Stages: []TrafficStage{
		{Target: 10, Duration: 15 * time.Second},
		{Target: 25, Duration: 20 * time.Second},
		{Target: 50, Duration: 30 * time.Second},
		{Target: 15, Duration: 15 * time.Second},
	}}
}

type ScenarioResult struct {
	ExecutionType string
	Succeeded     bool
}

type Config struct {
	ReferenceImage        string
	ReferenceInstrumented bool
	TrafficImage          string
	ReferenceReplicas     int32
	ReferenceReadyTimeout time.Duration
	TrafficTimeout        time.Duration
	PollInterval          time.Duration
}

func DefaultConfig() Config {
	return Config{
		ReferenceImage:        "nginx:1.27.5-alpine",
		ReferenceInstrumented: false, // Pinned fallback until the repository image is deliberately built/published.
		TrafficImage:          "grafana/k6:0.54.0",
		ReferenceReplicas:     2,
		ReferenceReadyTimeout: 2 * time.Minute,
		TrafficTimeout:        3 * time.Minute,
		PollInterval:          2 * time.Second,
	}
}
