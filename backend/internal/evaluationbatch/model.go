package evaluationbatch

import (
	"encoding/json"
	"time"

	"github.com/evalplane/evalplane/backend/internal/identity"
)

type ScenarioRef struct {
	ScenarioID        string `json:"scenario_id"`
	ScenarioVersionID string `json:"scenario_version_id"`
	Name              string `json:"name"`
	Version           int    `json:"version"`
}
type AgentRef struct {
	AgentID        string `json:"agent_id"`
	AgentVersionID string `json:"agent_version_id"`
	Name           string `json:"name"`
	VersionLabel   string `json:"version_label"`
}
type Batch struct {
	ID              string          `json:"id"`
	WorkspaceID     string          `json:"workspace_id"`
	Status          string          `json:"status"`
	EnvironmentMode string          `json:"environment_mode"`
	RunsPerAgent    int             `json:"runs_per_agent"`
	Config          json.RawMessage `json:"config"`
	SourceMetadata  SourceMetadata  `json:"source_metadata"`
	IdempotencyKey  string          `json:"idempotency_key,omitempty"`
	Scenario        ScenarioRef     `json:"scenario"`
	Agents          []AgentRef      `json:"agents,omitempty"`
	AgentCount      int             `json:"agent_count"`
	RunCount        int             `json:"run_count"`
	CreatedBy       *string         `json:"created_by"`
	CreatedAt       time.Time       `json:"created_at"`
	StartedAt       *time.Time      `json:"started_at"`
	CompletedAt     *time.Time      `json:"completed_at"`
}
type SourceMetadata struct {
	Provider    string `json:"provider,omitempty"`
	Repository  string `json:"repository,omitempty"`
	CommitSHA   string `json:"commit_sha,omitempty"`
	PullRequest string `json:"pull_request,omitempty"`
	Workflow    string `json:"workflow,omitempty"`
}
type Run struct {
	ID             string       `json:"id"`
	BatchID        string       `json:"batch_id"`
	Agent          AgentRef     `json:"agent"`
	Scenario       *ScenarioRef `json:"scenario,omitempty"`
	AttemptNumber  int          `json:"attempt_number"`
	Status         string       `json:"status"`
	EnvironmentRef *string      `json:"environment_ref"`
	CreatedAt      time.Time    `json:"created_at"`
	StartedAt      *time.Time   `json:"started_at"`
	CompletedAt    *time.Time   `json:"completed_at"`
}
type CreateInput struct {
	ScenarioVersionID string
	AgentVersionIDs   []string
	RunsPerAgent      int
	EnvironmentMode   string
	Config            json.RawMessage
	SourceMetadata    SourceMetadata
	IdempotencyKey    string
}
type Filters struct {
	Pagination identity.Pagination
	Status     string
}
