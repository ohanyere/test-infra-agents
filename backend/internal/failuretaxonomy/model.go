package failuretaxonomy

const Version = "failure-taxonomy:v1"

const (
	CategorySafety        = "safety"
	CategoryCriterion     = "criterion"
	CategoryAgentBehavior = "agent_behavior"
	CategoryTelemetry     = "telemetry"
	CategoryPlatform      = "platform_runtime"
	CategoryExecution     = "execution"
	CategoryUnknown       = "unknown_structured"
)

type Occurrence struct {
	SourceID          string
	RunID             string
	AgentVersionID    string
	ScenarioVersionID string
	Code              string
	PlatformInvalid   bool
}

type ScenarioCount struct {
	ScenarioVersionID string `json:"scenario_version_id"`
	AffectedTrials    int    `json:"affected_trials"`
	TotalOccurrences  int    `json:"total_occurrences"`
}

type FailureGroup struct {
	AgentVersionID          string          `json:"agent_version_id"`
	Category                string          `json:"category"`
	Code                    string          `json:"code"`
	AgentRelated            bool            `json:"agent_related"`
	PlatformRelated         bool            `json:"platform_related"`
	InvalidPlatformEvidence bool            `json:"invalid_platform_evidence"`
	AffectedTrials          int             `json:"affected_trials"`
	TotalOccurrences        int             `json:"total_occurrences"`
	Scenarios               []ScenarioCount `json:"scenarios"`
}

type Results struct {
	ExecutionID     string         `json:"execution_id"`
	TaxonomyVersion string         `json:"taxonomy_version"`
	Failures        []FailureGroup `json:"failures"`
}
