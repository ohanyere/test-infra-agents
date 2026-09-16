package qualplan

import (
	"time"
)

// Blocker is one structured machine-readable planning obstacle. Detail is a
// short operator-facing reason; evaluator-only data never appears here.
type Blocker struct {
	Code         string `json:"code"`
	CapabilityID string `json:"capability_id,omitempty"`
	FamilyID     string `json:"family_id,omitempty"`
	Detail       string `json:"detail,omitempty"`
	// BlocksSuite reports whether this blocker prevents authoritative suite
	// creation. Informational blockers (e.g. missing qualification policy)
	// do not block suite construction.
	BlocksSuite bool `json:"blocks_suite"`
}

// SelectedTask is one deterministic suite entry chosen by the planner.
type SelectedTask struct {
	BuiltinTaskID      string `json:"builtin_task_id"`
	ScenarioID         string `json:"scenario_id"`
	ScenarioVersionID  string `json:"scenario_version_id"`
	FamilyID           string `json:"family_id"`
	CaseType           string `json:"case_type"`
	TrialCount         int    `json:"trial_count"`
	SortOrder          int    `json:"sort_order"`
	ContentHash        string `json:"content_hash"`
	EnvironmentProfile string `json:"environment_profile,omitempty"`
	// CertificationID identifies the certification record behind the
	// authoritative-eligibility verdict at plan time.
	CertificationID string `json:"certification_id,omitempty"`
	// RequiredComponents and MinimumIsolation pin the task's environment
	// requirements so an old plan never depends on mutable current state.
	RequiredComponents []string `json:"required_components,omitempty"`
	MinimumIsolation   string   `json:"minimum_isolation,omitempty"`
}

// CapabilityPlan is the per-capability planning outcome.
type CapabilityPlan struct {
	CapabilityID        string         `json:"capability_id"`
	CoverageContract    string         `json:"coverage_contract_version"`
	RequiredFamilies    []string       `json:"required_families"`
	MinDistinctFamilies int            `json:"min_distinct_families"`
	CoveredFamilies     []string       `json:"covered_families"`
	Satisfied           bool           `json:"satisfied"`
	SelectedTasks       []SelectedTask `json:"selected_tasks"`
	EligibleCandidates  int            `json:"eligible_candidate_count"`
	Blockers            []Blocker      `json:"blockers"`
}

// Plan is one server-derived qualification plan. State, selections, trial
// counts, and blockers are all server-derived; callers supply only the
// AgentVersion and an optional claimed-capability subset.
type Plan struct {
	ID                         string           `json:"id"`
	WorkspaceID                string           `json:"workspace_id"`
	AgentID                    string           `json:"agent_id"`
	AgentVersionID             string           `json:"agent_version_id"`
	State                      string           `json:"state"`
	CapabilityIDs              []string         `json:"capability_ids"`
	PlannerContractVersion     string           `json:"planner_contract_version"`
	CoverageContractVersion    string           `json:"coverage_contract_version"`
	TaskFamilyContractVersion  string           `json:"task_family_contract_version"`
	CapabilityCatalogVersion   string           `json:"capability_catalog_version"`
	CertificationContract      string           `json:"certification_contract_version"`
	EvaluatorVersion           string           `json:"evaluator_version"`
	EnvironmentProfileContract string           `json:"environment_profile_contract_version"`
	Capabilities               []CapabilityPlan `json:"capabilities"`
	Blockers                   []Blocker        `json:"blockers"`
	QualificationPolicyAvail   bool             `json:"qualification_policy_available"`
	GeneratedSuiteVersionID    *string          `json:"generated_suite_version_id,omitempty"`
	GeneratedSuiteID           *string          `json:"generated_suite_id,omitempty"`
	CreatedBy                  *string          `json:"created_by"`
	CreatedAt                  time.Time        `json:"created_at"`
}

// SuiteBuild is the result of building an authoritative suite from a READY
// plan. Creation never executes the suite.
type SuiteBuild struct {
	PlanID         string `json:"plan_id"`
	SuiteID        string `json:"suite_id"`
	SuiteVersionID string `json:"suite_version_id"`
	EntryCount     int    `json:"entry_count"`
	TotalTrials    int    `json:"total_trials"`
}

// BuildRequest carries the only caller-controlled build input: none. Suite
// contents are fully server-derived from the READY plan.
type BuildRequest struct{}
