package qualplan

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"sort"
	"strconv"
	"strings"

	"github.com/evalplane/evalplane/backend/internal/environment"
	"github.com/evalplane/evalplane/backend/internal/evidencecontract"
	"github.com/evalplane/evalplane/backend/internal/gitopsqual"
	"github.com/evalplane/evalplane/backend/internal/observablecatalog"
	"github.com/evalplane/evalplane/backend/internal/scenario"
	"github.com/evalplane/evalplane/backend/internal/srequal"
)

// BuiltinTaskContractVersion identifies the built-in task definition
// contract consumed by the bridge. A changed catalog definition changes the
// content hash and therefore materializes a new immutable ScenarioVersion;
// existing versions are never modified.
const BuiltinTaskContractVersion = "builtin-task:v1"

// BuiltinCriterion is one deterministic outcome criterion on a built-in task.
type BuiltinCriterion struct {
	Key       string
	Name      string
	MetricKey string
	Operator  string
	Target    json.RawMessage
	Unit      *string
}

// BuiltinTask is the unified bridge view over the srequal (Stage-30) and
// gitopsqual (Stage-31) server-owned catalogs. Qualification SuiteVersion
// entries must reference exact ScenarioVersions, never catalog tasks
// directly, so there is exactly one suite item type.
type BuiltinTask struct {
	ID               string
	Name             string
	Description      string
	Domain           string
	Family           string
	CaseType         string
	Difficulty       string
	Duration         string
	Capabilities     []string
	AgentTask        string
	VisibleContext   json.RawMessage
	Observables      []string
	AllowedActions   []string
	AllowedResources json.RawMessage
	Conditions       json.RawMessage
	Criteria         []BuiltinCriterion
	Forbidden        []string
	GroundTruth      json.RawMessage
	Requirements     environment.Requirements
}

// ListBuiltinTasks returns every runnable server-owned built-in task in
// stable ID order. Planned/dedicated-only concepts are excluded: they carry
// dedicated_cluster requirements and never resolve to the namespace profile,
// so planning surfaces them as unavailable rather than materializing
// unrunnable versions.
func ListBuiltinTasks() []BuiltinTask {
	out := []BuiltinTask{}
	for _, task := range srequal.List() {
		out = append(out, fromSRETask(task))
	}
	for _, task := range gitopsqual.List() {
		out = append(out, fromGitOpsTask(task))
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out
}

// GetBuiltinTask returns the runnable built-in task for id.
func GetBuiltinTask(id string) (BuiltinTask, bool) {
	for _, task := range ListBuiltinTasks() {
		if task.ID == id {
			return task, true
		}
	}
	return BuiltinTask{}, false
}

func fromSRETask(task srequal.Task) BuiltinTask {
	criteria := make([]BuiltinCriterion, 0, len(task.Criteria))
	for _, criterion := range task.Criteria {
		criteria = append(criteria, BuiltinCriterion{
			Key: criterion.Key, Name: criterion.Name, MetricKey: criterion.MetricKey,
			Operator: criterion.Operator, Target: criterion.Target, Unit: criterion.Unit,
		})
	}
	return BuiltinTask{
		ID: task.ID, Name: task.Name, Description: task.Description,
		Domain: task.Domain, Family: task.Family, CaseType: task.CaseType,
		Difficulty: task.Difficulty, Duration: task.Duration,
		Capabilities:     append([]string(nil), task.Capabilities...),
		AgentTask:        task.AgentTask,
		VisibleContext:   cloneRaw(task.VisibleContext, `{}`),
		Observables:      append([]string(nil), task.Observables...),
		AllowedActions:   append([]string(nil), task.AllowedActions...),
		AllowedResources: cloneRaw(task.AllowedResources, `[]`),
		Conditions:       cloneRaw(task.Conditions, `[]`),
		Criteria:         criteria,
		Forbidden:        append([]string(nil), task.ForbiddenBehaviors...),
		GroundTruth:      cloneRaw(task.GroundTruth, `{}`),
		Requirements:     task.Requirements(),
	}
}

func fromGitOpsTask(task gitopsqual.Task) BuiltinTask {
	criteria := make([]BuiltinCriterion, 0, len(task.Criteria))
	for _, criterion := range task.Criteria {
		criteria = append(criteria, BuiltinCriterion{
			Key: criterion.Key, Name: criterion.Name, MetricKey: criterion.MetricKey,
			Operator: criterion.Operator, Target: criterion.Target, Unit: criterion.Unit,
		})
	}
	return BuiltinTask{
		ID: task.ID, Name: task.Name, Description: task.Description,
		Domain: task.Domain, Family: task.Family, CaseType: task.CaseType,
		Difficulty: task.Difficulty, Duration: task.Duration,
		Capabilities:     append([]string(nil), task.Capabilities...),
		AgentTask:        task.AgentTask,
		VisibleContext:   cloneRaw(task.VisibleContext, `{}`),
		Observables:      append([]string(nil), task.Observables...),
		AllowedActions:   append([]string(nil), task.AllowedActions...),
		AllowedResources: cloneRaw(task.AllowedResources, `[]`),
		Conditions:       cloneRaw(task.Conditions, `[]`),
		Criteria:         criteria,
		Forbidden:        append([]string(nil), task.ForbiddenBehaviors...),
		GroundTruth:      cloneRaw(task.GroundTruth, `{}`),
		Requirements:     task.Requirements(),
	}
}

// BridgeSlug is the deterministic workspace scenario slug for a built-in
// task. One slug per task per workspace: exact workspace ownership.
func BridgeSlug(taskID string) string {
	return "builtin-" + strings.TrimSpace(taskID)
}

// ContentHash deterministically identifies the exact built-in task
// definition that a materialized ScenarioVersion was built from. Any
// definition change yields a different hash and therefore a new immutable
// version; repeated planning with an unchanged definition reuses the same
// version (idempotent materialization).
func ContentHash(task BuiltinTask) string {
	observables := append([]string(nil), task.Observables...)
	sort.Strings(observables)
	actions := append([]string(nil), task.AllowedActions...)
	sort.Strings(actions)
	capabilities := append([]string(nil), task.Capabilities...)
	sort.Strings(capabilities)
	canonical, _ := json.Marshal(map[string]any{
		"builtin_task_contract":              BuiltinTaskContractVersion,
		"builtin_task_id":                    task.ID,
		"agent_task":                         task.AgentTask,
		"visible_context":                    json.RawMessage(cloneRaw(task.VisibleContext, `{}`)),
		"observables":                        observables,
		"allowed_actions":                    actions,
		"allowed_resources":                  json.RawMessage(cloneRaw(task.AllowedResources, `[]`)),
		"conditions":                         json.RawMessage(cloneRaw(task.Conditions, `[]`)),
		"criteria":                           task.Criteria,
		"forbidden_behaviors":                task.Forbidden,
		"ground_truth":                       json.RawMessage(cloneRaw(task.GroundTruth, `{}`)),
		"task_family":                        task.Family,
		"case_type":                          task.CaseType,
		"requirements":                       task.Requirements,
		"verification_evidence_requirements": evidenceRequirementsFor(task),
		"capabilities":                       capabilities,
	})
	sum := sha256.Sum256(canonical)
	return hex.EncodeToString(sum[:])
}

// SourceMetadata builds the reproducibility marker stored on materialized
// versions. It identifies the originating built-in task contract/version
// and retains public read requirements without exposing evaluator-only truth.
func SourceMetadata(task BuiltinTask, hash string) json.RawMessage {
	raw, _ := json.Marshal(map[string]any{
		"origin":                "builtin-catalog-bridge",
		"builtin_task_id":       task.ID,
		"builtin_task_contract": BuiltinTaskContractVersion,
		"content_hash":          hash,
		"planner_contract":      PlannerContractVersion,
		"evidence_requirements": append([]string(nil), task.Observables...),
	})
	return raw
}

// ContentHashOfVersion extracts the bridge content hash recorded on a
// materialized version, or "" when absent.
func ContentHashOfVersion(version scenario.Version) string {
	if len(version.SourceMetadata) == 0 {
		return ""
	}
	var decoded map[string]any
	if json.Unmarshal(version.SourceMetadata, &decoded) != nil {
		return ""
	}
	hash, _ := decoded["content_hash"].(string)
	return hash
}

// ToVersionInput maps a built-in task onto scenario authoring input.
// Evaluator-only reference remediation is deliberately NOT mapped: it lives
// in the Stage-28 reference table (authored through PutReference) and is
// never part of the scenario row. Ground truth persists on the version row
// but never reaches the agent (AgentView excludes it by construction).
func ToVersionInput(task BuiltinTask, hash string) scenario.VersionInput {
	criteria := make([]scenario.CriterionInput, 0, len(task.Criteria))
	for index, criterion := range task.Criteria {
		criteria = append(criteria, scenario.CriterionInput{
			Key: criterion.Key, Name: criterion.Name, MetricKey: criterion.MetricKey,
			Operator: criterion.Operator, TargetValue: criterion.Target,
			Unit: criterion.Unit, SortOrder: index,
		})
	}
	return scenario.VersionInput{
		Description:          task.Description,
		Category:             strings.ToLower(strings.TrimSpace(task.Domain)),
		Difficulty:           task.Difficulty,
		DurationSeconds:      durationSeconds(task.Duration),
		Environment:          json.RawMessage(`{}`),
		Conditions:           cloneRaw(task.Conditions, `[]`),
		Observables:          agentVisibleObservables(task),
		ExpectedBehaviors:    []string{},
		ForbiddenBehaviors:   append([]string(nil), task.Forbidden...),
		Criteria:             criteria,
		AuthoringMethod:      "manual",
		AgentTask:            task.AgentTask,
		AgentVisibleContext:  cloneRaw(task.VisibleContext, `{}`),
		AllowedResources:     cloneRaw(task.AllowedResources, `[]`),
		AllowedActions:       append([]string(nil), task.AllowedActions...),
		GroundTruth:          cloneRaw(task.GroundTruth, `{}`),
		ExecutionSpec:        executionSpecFor(task),
		SourceMetadata:       SourceMetadata(task, hash),
		EvidenceRequirements: evidenceRequirementsFor(task),
	}
}

func evidenceRequirementsFor(task BuiltinTask) evidencecontract.Requirements {
	if _, gitopsTask := gitopsqual.Get(task.ID); gitopsTask {
		return evidencecontract.GitOpsRequirements()
	}
	return evidencecontract.DefaultRequirements()
}

// Built-in task read requirements describe evaluator or mediated-provider
// evidence. Only the SRE catalog reads backed by this runtime become agent-visible
// scenario observables. GitOps/delivery reads remain in source metadata until
// their own mediated providers exist.
func agentVisibleObservables(task BuiltinTask) []string {
	if _, supported := srequal.Get(task.ID); !supported {
		return []string{}
	}
	out := []string{}
	seen := map[observablecatalog.ID]bool{}
	for _, requirement := range task.Observables {
		var id observablecatalog.ID
		switch requirement {
		case "workloads", "deployments":
			id = observablecatalog.Workloads
		case "pods":
			id = observablecatalog.Pods
		case "events":
			id = observablecatalog.Events
		case "request-rate":
			id = observablecatalog.RequestRate
		case "error-rate":
			id = observablecatalog.ErrorRate
		case "latency":
			id = observablecatalog.P95Latency
		case "availability":
			id = observablecatalog.Availability
		}
		if id != "" && !seen[id] {
			out = append(out, string(id))
			seen[id] = true
		}
	}
	return out
}

// ToCreateInput maps a built-in task onto scenario creation input.
func ToCreateInput(task BuiltinTask, hash string) scenario.CreateInput {
	return scenario.CreateInput{
		Name:           task.Name,
		Slug:           BridgeSlug(task.ID),
		Visibility:     "workspace",
		InitialVersion: ToVersionInput(task, hash),
	}
}

// executionSpecFor keeps execution honesty: only traffic-spike family tasks
// claim the traffic_spike runtime (served by the development profile);
// everything else declares an unimplemented runtime so the worker refuses
// with ErrUnsupported instead of executing the wrong physics.
func executionSpecFor(task BuiltinTask) json.RawMessage {
	if task.Family == "kubernetes.capacity.traffic_spike" {
		return json.RawMessage(`{"type":"traffic_spike"}`)
	}
	return json.RawMessage(`{"type":"unimplemented"}`)
}

// durationSeconds parses "<N> min" durations from the catalog.
func durationSeconds(value string) int {
	fields := strings.Fields(strings.TrimSpace(value))
	if len(fields) == 0 {
		return 600
	}
	minutes, err := strconv.Atoi(fields[0])
	if err != nil || minutes < 1 {
		return 600
	}
	seconds := minutes * 60
	if seconds < 1 || seconds > 86400 {
		return 600
	}
	return seconds
}

func cloneRaw(value json.RawMessage, fallback string) json.RawMessage {
	if len(value) == 0 {
		return json.RawMessage(fallback)
	}
	out := append(json.RawMessage(nil), value...)
	if !json.Valid(out) {
		return json.RawMessage(fallback)
	}
	return out
}

// ValidateBuiltinMappings fails fast when a catalog task cannot be
// represented as a persisted ScenarioVersion under current validation.
func ValidateBuiltinMappings() error {
	for _, task := range ListBuiltinTasks() {
		hash := ContentHash(task)
		if _, err := scenario.ValidateVersion(ToVersionInput(task, hash)); err != nil {
			return fmt.Errorf("builtin task %q is not materializable: %w", task.ID, err)
		}
		if _, err := scenario.ValidateCreate(ToCreateInput(task, hash)); err != nil {
			return fmt.Errorf("builtin task %q is not creatable: %w", task.ID, err)
		}
	}
	return nil
}
