package scenario

import (
	"encoding/json"
	"time"

	"github.com/evalplane/evalplane/backend/internal/identity"
)

type Criterion struct {
	ID          string          `json:"id"`
	Key         string          `json:"key"`
	Name        string          `json:"name"`
	MetricKey   string          `json:"metric_key"`
	Operator    string          `json:"operator"`
	TargetValue json.RawMessage `json:"target_value"`
	Unit        *string         `json:"unit,omitempty"`
	SortOrder   int             `json:"sort_order"`
	CreatedAt   time.Time       `json:"created_at"`
}
type Version struct {
	ID                  string          `json:"id"`
	Version             int             `json:"version"`
	Description         string          `json:"description"`
	Category            string          `json:"category"`
	Difficulty          string          `json:"difficulty"`
	DurationSeconds     int             `json:"duration_seconds"`
	Environment         json.RawMessage `json:"environment"`
	Conditions          json.RawMessage `json:"conditions"`
	Observables         []string        `json:"observables"`
	ExpectedBehaviors   []string        `json:"expected_behaviors"`
	ForbiddenBehaviors  []string        `json:"forbidden_behaviors"`
	AuthoringMethod     string          `json:"authoring_method"`
	AgentTask           string          `json:"agent_task"`
	AgentVisibleContext json.RawMessage `json:"agent_visible_context"`
	AllowedActions      []string        `json:"allowed_actions"`
	AllowedResources    json.RawMessage `json:"allowed_resources"`
	GroundTruth         json.RawMessage `json:"ground_truth"`
	ExecutionSpec       json.RawMessage `json:"execution_spec"`
	SourceMetadata      json.RawMessage `json:"source_metadata"`
	Criteria            []Criterion     `json:"criteria"`
	// TaskFamilyID and CaseType are the explicit coverage assignment for this
	// ScenarioVersion (task-family-contract:v1). Empty when unassigned;
	// unassigned versions never contribute coverage. Never inferred.
	TaskFamilyID              string    `json:"task_family_id,omitempty"`
	CaseType                  string    `json:"case_type,omitempty"`
	TaskFamilyContractVersion string    `json:"task_family_contract_version,omitempty"`
	CreatedAt                 time.Time `json:"created_at"`
}
type Scenario struct {
	ID               string    `json:"id"`
	OwnerWorkspaceID *string   `json:"owner_workspace_id"`
	Slug             string    `json:"slug"`
	Name             string    `json:"name"`
	Kind             string    `json:"kind"`
	Visibility       string    `json:"visibility"`
	Status           string    `json:"status"`
	LatestVersion    *Version  `json:"latest_version"`
	CreatedAt        time.Time `json:"created_at"`
	UpdatedAt        time.Time `json:"updated_at"`
}
type CriterionInput struct {
	Key, Name, MetricKey, Operator string
	TargetValue                    json.RawMessage
	Unit                           *string
	SortOrder                      int
}
type VersionInput struct {
	Description, Category, Difficulty                  string
	DurationSeconds                                    int
	Environment, Conditions                            json.RawMessage
	Observables, ExpectedBehaviors, ForbiddenBehaviors []string
	Criteria                                           []CriterionInput
	AuthoringMethod, AgentTask                         string
	AgentVisibleContext, AllowedResources              json.RawMessage
	AllowedActions                                     []string
	GroundTruth, ExecutionSpec, SourceMetadata         json.RawMessage
}

type AgentSpec struct {
	Task                string          `json:"task"`
	VisibleContext      json.RawMessage `json:"visible_context"`
	AllowedObservations []string        `json:"allowed_observations"`
	AllowedActions      []string        `json:"allowed_actions"`
	AllowedResources    json.RawMessage `json:"allowed_resources"`
}
type AgentView struct {
	Task                string          `json:"task"`
	VisibleContext      json.RawMessage `json:"visible_context"`
	AllowedObservations []string        `json:"allowed_observations"`
	AllowedActions      []string        `json:"allowed_actions"`
	AllowedResources    json.RawMessage `json:"allowed_resources"`
}

type EvaluatorSpec struct {
	GroundTruth        json.RawMessage `json:"ground_truth"`
	ExecutionSpec      json.RawMessage `json:"execution_spec"`
	Conditions         json.RawMessage `json:"conditions"`
	ExpectedBehaviors  []string        `json:"expected_behaviors"`
	ForbiddenBehaviors []string        `json:"forbidden_behaviors"`
}

func (v Version) AgentView() AgentView {
	return AgentView{Task: v.AgentTask, VisibleContext: cloneJSON(v.AgentVisibleContext, `{}`), AllowedObservations: append([]string(nil), v.Observables...), AllowedActions: append([]string(nil), v.AllowedActions...), AllowedResources: cloneJSON(v.AllowedResources, `[]`)}
}
func cloneJSON(value json.RawMessage, fallback string) json.RawMessage {
	if len(value) == 0 {
		return json.RawMessage(fallback)
	}
	return append(json.RawMessage(nil), value...)
}

type CreateInput struct {
	Name, Slug, Visibility string
	InitialVersion         VersionInput
}
type UpdateInput struct{ Name, Visibility, Status *string }
type Filters struct {
	Pagination                          identity.Pagination
	Query, Category, Difficulty, Status string
}
