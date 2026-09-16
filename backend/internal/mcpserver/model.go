package mcpserver

import (
	"encoding/json"
	"time"

	"github.com/evalplane/evalplane/backend/internal/agent"
	"github.com/evalplane/evalplane/backend/internal/agentgateway"
	"github.com/evalplane/evalplane/backend/internal/kubeenv"
	"github.com/evalplane/evalplane/backend/internal/observation"
	"github.com/evalplane/evalplane/backend/internal/scenario"
)

const (
	ProtocolVersion = "2026-07-28"
	ToolContract    = "evalplane-mcp-tools:v1"
)

const (
	ToolObserveWorkloads = "evalplane_observe_workloads"
	ToolObservePods      = "evalplane_observe_pods"
	ToolObserveEvents    = "evalplane_observe_events"
	ToolGetTelemetry     = "evalplane_get_telemetry"
	ToolScaleWorkload    = "evalplane_scale_workload"
	ToolRestartPod       = "evalplane_restart_pod"
	ToolNoOp             = "evalplane_no_op"
)

type Grant struct {
	ID, WorkspaceID, RunID, AgentVersionID string
	TokenHash                              []byte
	ExpiresAt                              time.Time
	RevokedAt                              *time.Time
	ToolCalls, MaxToolCalls                int
}

type Access struct {
	Endpoint  string    `json:"endpoint"`
	Token     string    `json:"token"`
	ExpiresAt time.Time `json:"expires_at"`
}

type Scope struct {
	WorkspaceID string
	RunID       string
	Version     agent.Version
	Environment observation.EnvironmentContext
	AgentView   scenario.AgentView
}

type ToolOutput struct {
	Available   *bool                              `json:"available,omitempty"`
	Workloads   []agentgateway.WorkloadObservation `json:"workloads,omitempty"`
	Pods        []agentgateway.PodObservation      `json:"pods,omitempty"`
	Events      []agentgateway.RecentEvent         `json:"events,omitempty"`
	Metrics     map[string]any                     `json:"metrics,omitempty"`
	Status      string                             `json:"status,omitempty"`
	Code        string                             `json:"code,omitempty"`
	Observation json.RawMessage                    `json:"observation,omitempty"`
}

type ScaleInput struct {
	Kind      string `json:"kind" jsonschema:"target Kubernetes kind; must be Deployment"`
	Namespace string `json:"namespace" jsonschema:"run-owned namespace"`
	Name      string `json:"name" jsonschema:"run-owned workload name"`
	Replicas  int    `json:"replicas" jsonschema:"desired replicas, from 1 through 20"`
}

type RestartInput struct {
	Kind      string `json:"kind" jsonschema:"target Kubernetes kind; must be Pod"`
	Namespace string `json:"namespace" jsonschema:"run-owned namespace"`
	Name      string `json:"name" jsonschema:"run-owned pod name"`
}

type EmptyInput struct{}

func (s Scope) Identity() kubeenv.RunIdentity { return s.Environment.Identity }
