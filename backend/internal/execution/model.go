package execution

import (
	"context"
	"encoding/json"
	"errors"
	"time"
)

var ErrNoRunAvailable = errors.New("no run available")

type BatchState struct {
	ID          string     `json:"id"`
	WorkspaceID string     `json:"workspace_id"`
	Status      string     `json:"status"`
	CreatedAt   time.Time  `json:"created_at"`
	StartedAt   *time.Time `json:"started_at"`
	CompletedAt *time.Time `json:"completed_at"`
}

type ScenarioDescription struct {
	ID        string `json:"scenario_id"`
	VersionID string `json:"scenario_version_id"`
	Version   int    `json:"version"`
}

type AgentDescription struct {
	ID            string          `json:"agent_id"`
	VersionID     string          `json:"agent_version_id"`
	VersionLabel  string          `json:"version_label"`
	InterfaceKind string          `json:"interface_kind"`
	Capabilities  []string        `json:"capabilities"`
	Metadata      json.RawMessage `json:"metadata"`
}

type BatchDescription struct {
	EnvironmentMode string          `json:"environment_mode"`
	Config          json.RawMessage `json:"config"`
}

// HiddenVariant is trusted execution input. JSON exclusion is a defense in
// depth boundary: it must never be serialized into an Agent Gateway request.
type HiddenVariant struct {
	ID     string          `json:"-"`
	Seed   int64           `json:"-"`
	Config json.RawMessage `json:"-"`
}

type ExecutionRun struct {
	RunID         string              `json:"run_id"`
	BatchID       string              `json:"batch_id"`
	WorkspaceID   string              `json:"workspace_id"`
	AttemptNumber int                 `json:"attempt_number"`
	Scenario      ScenarioDescription `json:"scenario"`
	Agent         AgentDescription    `json:"agent"`
	Batch         BatchDescription    `json:"batch"`
	HiddenVariant HiddenVariant       `json:"-"`
}

// AbandonedRun is an atomically acquired stale run. PreviousStatus and
// EnvironmentRef are diagnostic context only; execution must never be replayed.
type AbandonedRun struct {
	ExecutionRun
	PreviousStatus string  `json:"previous_status"`
	EnvironmentRef *string `json:"environment_ref"`
}

// Executor is the minimum contract a Stage 10 execution implementation must satisfy.
type Executor interface {
	Execute(context.Context, ExecutionRun) error
}
