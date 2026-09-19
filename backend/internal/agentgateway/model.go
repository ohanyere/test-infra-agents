package agentgateway

import (
	"context"
	"encoding/json"
	"errors"
	"time"
)

const (
	ProtocolV1       = "v1"
	DefaultTimeout   = 30 * time.Second
	MaxResponseBytes = int64(1 << 20)
	MaxActions       = 10
)

var (
	ErrInvalidConnection   = errors.New("invalid agent connection")
	ErrInvalidRequest      = errors.New("invalid agent request")
	ErrInvalidResponse     = errors.New("invalid agent response")
	ErrUnsupportedProtocol = errors.New("unsupported agent protocol")
	ErrResponseTooLarge    = errors.New("agent response too large")
	ErrTimeout             = errors.New("agent request timed out")
	ErrCanceled            = errors.New("agent request canceled")
	ErrAuthentication      = errors.New("agent authentication rejected")
)

type Connection struct {
	EndpointURL, BearerToken string
	Timeout                  time.Duration
}
type AgentTask struct {
	Message string `json:"message"`
}
type WorkloadObservation struct {
	Kind              string `json:"kind"`
	Name              string `json:"name"`
	Namespace         string `json:"namespace"`
	ReadyReplicas     int32  `json:"ready_replicas"`
	DesiredReplicas   int32  `json:"desired_replicas"`
	AvailableReplicas int32  `json:"available_replicas"`
	UpdatedReplicas   int32  `json:"updated_replicas"`
	CPURequest        string `json:"cpu_request,omitempty"`
	CPULimit          string `json:"cpu_limit,omitempty"`
	MemoryRequest     string `json:"memory_request,omitempty"`
	MemoryLimit       string `json:"memory_limit,omitempty"`
	Image             string `json:"image,omitempty"`
	ResourceVersion   string `json:"resource_version,omitempty"`
}
type AutoscalerObservation struct {
	Name            string `json:"name"`
	Namespace       string `json:"namespace"`
	TargetKind      string `json:"target_kind"`
	TargetName      string `json:"target_name"`
	MinReplicas     int32  `json:"min_replicas"`
	MaxReplicas     int32  `json:"max_replicas"`
	CurrentReplicas int32  `json:"current_replicas"`
	DesiredReplicas int32  `json:"desired_replicas"`
}
type RecentEvent struct {
	Timestamp          time.Time `json:"timestamp"`
	Type               string    `json:"type"`
	Reason             string    `json:"reason,omitempty"`
	Summary            string    `json:"summary,omitempty"`
	InvolvedObjectKind string    `json:"involved_object_kind,omitempty"`
	InvolvedObjectName string    `json:"involved_object_name,omitempty"`
}

type PodObservation struct {
	Name           string `json:"name"`
	Namespace      string `json:"namespace"`
	Phase          string `json:"phase"`
	Ready          bool   `json:"ready"`
	RestartCount   int32  `json:"restart_count"`
	OwningWorkload string `json:"owning_workload,omitempty"`
}
type Observation struct {
	Timestamp    time.Time               `json:"timestamp"`
	Workloads    []WorkloadObservation   `json:"workloads"`
	Pods         []PodObservation        `json:"pods"`
	Autoscalers  []AutoscalerObservation `json:"autoscalers,omitempty"`
	Metrics      map[string]any          `json:"metrics"`
	RecentEvents []RecentEvent           `json:"recent_events"`
}
type Request struct {
	ProtocolVersion  string          `json:"protocol_version"`
	SessionID        string          `json:"session_id"`
	RunID            string          `json:"run_id"`
	AgentVersionID   string          `json:"agent_version_id"`
	Task             AgentTask       `json:"task"`
	Observation      Observation     `json:"observation"`
	AvailableActions []string        `json:"available_actions"`
	VisibleContext   json.RawMessage `json:"visible_context,omitempty"`
	AllowedResources json.RawMessage `json:"allowed_resources,omitempty"`
	ToolAccess       *ToolAccess     `json:"tool_access,omitempty"`
}
type ToolAccess struct {
	Mode      string    `json:"mode"`
	Endpoint  string    `json:"endpoint"`
	Token     string    `json:"token"`
	ExpiresAt time.Time `json:"expires_at"`
}
type ActionTarget struct {
	Kind      string `json:"kind"`
	Namespace string `json:"namespace"`
	Name      string `json:"name"`
}
type ProposedAction struct {
	Type       string         `json:"type"`
	Target     *ActionTarget  `json:"target,omitempty"`
	Parameters map[string]any `json:"parameters"`
}
type Response struct {
	ProtocolVersion  string           `json:"protocol_version"`
	ReasoningSummary string           `json:"reasoning_summary"`
	Actions          []ProposedAction `json:"actions"`
}
type Gateway interface {
	Invoke(context.Context, Connection, Request) (Response, error)
}
