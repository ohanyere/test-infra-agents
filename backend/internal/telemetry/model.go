package telemetry

import (
	"errors"
	"github.com/evalplane/evalplane/backend/internal/kubeenv"
	"time"
)

const (
	ComponentLabelKey      = "evalplane.dev/component"
	TelemetryComponent     = "telemetry"
	ConfigMapName          = "eval-prometheus-config"
	DeploymentName         = "eval-prometheus"
	ServiceName            = "eval-prometheus"
	ScrapeServiceName      = "eval-reference-metrics"
	DefaultPrometheusImage = "prom/prometheus:v3.13.3"
)

var (
	ErrOwnership = errors.New("telemetry ownership mismatch")
	ErrConflict  = errors.New("telemetry resource conflict")
)

type EnvironmentContext struct {
	Identity    kubeenv.RunIdentity
	Environment kubeenv.Environment
}
type Snapshot struct {
	Timestamp       time.Time
	RequestRateRPS  *float64
	ErrorRatePct    *float64
	P95LatencyMS    *float64
	AvailabilityPct *float64
}
type Config struct {
	PrometheusImage string
	ScrapeInterval  time.Duration
}

func DefaultConfig() Config {
	return Config{PrometheusImage: DefaultPrometheusImage, ScrapeInterval: 5 * time.Second}
}
