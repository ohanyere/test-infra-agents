package scenarioauthoring

import (
	"context"
	"encoding/json"
	"time"

	"github.com/evalplane/evalplane/backend/internal/scenario"
)

type AgentSpec struct {
	Task                  string          `json:"task"`
	VisibleContext        json.RawMessage `json:"visible_context"`
	AvailableObservations []string        `json:"available_observations"`
	AllowedActions        []string        `json:"allowed_actions"`
	AllowedResources      json.RawMessage `json:"allowed_resources"`
}
type DraftCriterion struct {
	Key                  string          `json:"key"`
	Name                 string          `json:"name"`
	MetricKey            string          `json:"metric_key"`
	Operator             string          `json:"operator"`
	TargetValue          json.RawMessage `json:"target_value,omitempty"`
	Unit                 *string         `json:"unit,omitempty"`
	RequiresConfirmation bool            `json:"requires_confirmation,omitempty"`
}
type ScenarioDraft struct {
	Name                    string           `json:"name"`
	Description             string           `json:"description"`
	Category                string           `json:"category"`
	Difficulty              string           `json:"difficulty"`
	DurationSeconds         int              `json:"duration_seconds"`
	AgentSpec               AgentSpec        `json:"agent_spec"`
	Environment             json.RawMessage  `json:"environment"`
	Conditions              json.RawMessage  `json:"conditions"`
	ExpectedBehaviors       []string         `json:"expected_behaviors"`
	ForbiddenBehaviors      []string         `json:"forbidden_behaviors"`
	Criteria                []DraftCriterion `json:"criteria"`
	GroundTruth             json.RawMessage  `json:"ground_truth"`
	ExecutionSpec           json.RawMessage  `json:"execution_spec"`
	Assumptions             []string         `json:"assumptions"`
	Warnings                []string         `json:"warnings"`
	RunnableNow             bool             `json:"runnable_now"`
	UnsupportedCapabilities []string         `json:"unsupported_capabilities"`
	SourceMetadata          json.RawMessage  `json:"source_metadata,omitempty"`
}
type DescriptionInput struct {
	Description string `json:"description"`
}
type EvidenceFile struct {
	Name    string
	Content []byte
}
type IncidentInput struct {
	Description       string
	Timeline          string
	Files             []EvidenceFile
	EvidenceSummary   []string
	Facts             []string
	SanitizedEvidence string
	Provenance        *IncidentProvenance
}
type IncidentProvenance struct {
	SourceType               string     `json:"source_type"`
	Provider                 string     `json:"provider"`
	ExternalIncidentID       string     `json:"external_incident_id"`
	SourceConnectionID       string     `json:"source_connection_id"`
	SourceIncidentUpdatedAt  *time.Time `json:"source_incident_updated_at,omitempty"`
	ImportedAt               time.Time  `json:"imported_at"`
	ConnectorContractVersion string     `json:"connector_contract_version"`
	RedactionContract        string     `json:"redaction_contract"`
}
type DraftResult struct {
	Draft             ScenarioDraft `json:"draft"`
	EvidenceSummary   []string      `json:"evidence_summary,omitempty"`
	Facts             []string      `json:"observed_facts,omitempty"`
	Inferences        []string      `json:"ai_inferences,omitempty"`
	NeedsConfirmation []string      `json:"needs_confirmation,omitempty"`
}
type ScenarioDraftGenerator interface {
	FromDescription(context.Context, DescriptionInput) (ScenarioDraft, error)
	FromIncident(context.Context, IncidentInput) (ScenarioDraft, error)
}
type IncidentConnector interface {
	Provider() string
	FetchIncident(context.Context, string) (IncidentInput, error)
}
type CapabilityCatalog struct {
	Platform          []string `json:"platform"`
	ScenarioExecution []string `json:"scenario_execution"`
	AgentActions      []string `json:"agent_actions"`
	Observations      []string `json:"observations"`
}

func CurrentCapabilities() CapabilityCatalog {
	return CapabilityCatalog{Platform: []string{"kubernetes"}, ScenarioExecution: []string{"traffic_spike"}, AgentActions: []string{"inspect", "scale_workload", "restart_pod", "no_op"}, Observations: []string{"kubernetes_workloads", "kubernetes_pods", "kubernetes_events", "request_rate", "error_rate", "p95_latency", "availability"}}
}
func (d ScenarioDraft) VersionInput(method string) scenario.VersionInput {
	d = NewSanitizer().SanitizeDraft(d, nil)
	criteria := make([]scenario.CriterionInput, 0, len(d.Criteria))
	for i, c := range d.Criteria {
		if c.RequiresConfirmation || len(c.TargetValue) == 0 {
			continue
		}
		criteria = append(criteria, scenario.CriterionInput{Key: c.Key, Name: c.Name, MetricKey: c.MetricKey, Operator: c.Operator, TargetValue: c.TargetValue, Unit: c.Unit, SortOrder: i})
	}
	return scenario.VersionInput{Description: d.Description, Category: d.Category, Difficulty: d.Difficulty, DurationSeconds: d.DurationSeconds, Environment: d.Environment, Conditions: d.Conditions, Observables: d.AgentSpec.AvailableObservations, ExpectedBehaviors: d.ExpectedBehaviors, ForbiddenBehaviors: d.ForbiddenBehaviors, Criteria: criteria, AuthoringMethod: method, AgentTask: d.AgentSpec.Task, AgentVisibleContext: d.AgentSpec.VisibleContext, AllowedActions: d.AgentSpec.AllowedActions, AllowedResources: d.AgentSpec.AllowedResources, GroundTruth: d.GroundTruth, ExecutionSpec: d.ExecutionSpec, SourceMetadata: d.SourceMetadata}
}
