package scenario

import (
	"encoding/json"
	"fmt"
	"regexp"
	"strings"

	"github.com/evalplane/evalplane/backend/internal/evaluationmetric"
	"github.com/evalplane/evalplane/backend/internal/evidencecontract"
	"github.com/evalplane/evalplane/backend/internal/identity"
	"github.com/evalplane/evalplane/backend/internal/observablecatalog"
)

var kebabPattern = regexp.MustCompile(`^[a-z0-9]+(?:-[a-z0-9]+)*$`)
var metricPattern = regexp.MustCompile(`^[a-z][a-z0-9_]*(?:\.[a-z][a-z0-9_]*)*$`)

func ValidateCreate(in CreateInput) (CreateInput, error) {
	in.Name, in.Slug = strings.TrimSpace(in.Name), strings.TrimSpace(in.Slug)
	if in.Visibility == "" {
		in.Visibility = "workspace"
	}
	if in.Name == "" || len(in.Name) > 160 {
		return in, invalid("scenario name is required and must be at most 160 characters")
	}
	if err := identity.ValidateSlug(in.Slug); err != nil {
		return in, err
	}
	if in.Visibility != "workspace" && in.Visibility != "private" {
		return in, invalid("visibility must be workspace or private")
	}
	version, err := ValidateVersion(in.InitialVersion)
	in.InitialVersion = version
	return in, err
}
func ValidateVersion(in VersionInput) (VersionInput, error) {
	var err error
	in.Description, in.Category = strings.TrimSpace(in.Description), strings.TrimSpace(in.Category)
	if len(in.Description) > 8000 {
		return in, invalid("description must be at most 8000 characters")
	}
	if in.Category == "" || len(in.Category) > 100 || !kebabPattern.MatchString(in.Category) {
		return in, invalid("category must be lowercase kebab-case")
	}
	if in.Difficulty != "easy" && in.Difficulty != "medium" && in.Difficulty != "hard" {
		return in, invalid("difficulty must be easy, medium, or hard")
	}
	if in.DurationSeconds < 1 || in.DurationSeconds > 86400 {
		return in, invalid("duration_seconds must be between 1 and 86400")
	}
	if len(in.Environment) == 0 {
		in.Environment = json.RawMessage(`{}`)
	}
	if len(in.Conditions) == 0 {
		in.Conditions = json.RawMessage(`[]`)
	}
	if in.AuthoringMethod == "" {
		in.AuthoringMethod = "manual"
	}
	if in.AuthoringMethod != "manual" && in.AuthoringMethod != "description" && in.AuthoringMethod != "incident_import" {
		return in, invalid("authoring_method is invalid")
	}
	in.AgentTask = strings.TrimSpace(in.AgentTask)
	if len(in.AgentTask) > 4000 {
		return in, invalid("agent_task must be at most 4000 characters")
	}
	if len(in.AgentVisibleContext) == 0 {
		in.AgentVisibleContext = json.RawMessage(`{}`)
	}
	if len(in.AllowedResources) == 0 {
		in.AllowedResources = json.RawMessage(`[]`)
	}
	if len(in.GroundTruth) == 0 {
		in.GroundTruth = json.RawMessage(`{}`)
	}
	if len(in.ExecutionSpec) == 0 {
		in.ExecutionSpec = json.RawMessage(`{}`)
	}
	if len(in.SourceMetadata) == 0 {
		in.SourceMetadata = json.RawMessage(`{}`)
	}
	if err := validateJSONObject(in.AgentVisibleContext, "agent_visible_context"); err != nil {
		return in, err
	}
	if err := validateJSONArray(in.AllowedResources, "allowed_resources"); err != nil {
		return in, err
	}
	if err := validateJSONObject(in.GroundTruth, "ground_truth"); err != nil {
		return in, err
	}
	if err := validateJSONObject(in.ExecutionSpec, "execution_spec"); err != nil {
		return in, err
	}
	if err := validateJSONObject(in.SourceMetadata, "source_metadata"); err != nil {
		return in, err
	}
	if in.EvidenceRequirements.IsZero() {
		in.EvidenceRequirements = evidencecontract.DefaultRequirements()
	}
	if err := in.EvidenceRequirements.Validate(); err != nil {
		return in, invalid(err.Error())
	}
	if in.AllowedActions, err = validateAllowedActions(in.AllowedActions); err != nil {
		return in, err
	}
	if err := validateJSONObject(in.Environment, "environment"); err != nil {
		return in, err
	}
	if err := validateJSONArray(in.Conditions, "conditions"); err != nil {
		return in, err
	}
	if in.Observables, err = validateObservables(in.Observables); err != nil {
		return in, err
	}
	if in.ExpectedBehaviors, err = validateTextItems(in.ExpectedBehaviors, "expected_behaviors", 64); err != nil {
		return in, err
	}
	if in.ForbiddenBehaviors, err = validateTextItems(in.ForbiddenBehaviors, "forbidden_behaviors", 64); err != nil {
		return in, err
	}
	keys := map[string]struct{}{}
	for i := range in.Criteria {
		criterion, err := validateCriterion(in.Criteria[i])
		if err != nil {
			return in, err
		}
		if _, exists := keys[criterion.Key]; exists {
			return in, invalid("criteria keys must be unique")
		}
		definition, _ := evaluationmetric.Lookup(criterion.MetricKey)
		if definition.Telemetry && in.EvidenceRequirements.Telemetry != evidencecontract.Required {
			return in, invalid("telemetry evidence must be REQUIRED when a criterion uses a telemetry metric")
		}
		keys[criterion.Key] = struct{}{}
		in.Criteria[i] = criterion
	}
	return in, nil
}
func validateAllowedActions(values []string) ([]string, error) {
	if values == nil {
		values = []string{}
	}
	seen := map[string]bool{}
	for _, v := range values {
		if v != "inspect" && v != "scale_workload" && v != "restart_pod" && v != "set_workload_image" && v != "no_op" {
			return values, invalid("allowed_actions contains an unsupported action")
		}
		if seen[v] {
			return values, invalid("allowed_actions must not contain duplicates")
		}
		seen[v] = true
	}
	return values, nil
}
func ValidateUpdate(in UpdateInput) (UpdateInput, error) {
	if in.Name == nil && in.Visibility == nil && in.Status == nil {
		return in, invalid("update must contain at least one mutable field")
	}
	if in.Name != nil {
		v := strings.TrimSpace(*in.Name)
		in.Name = &v
		if v == "" || len(v) > 160 {
			return in, invalid("scenario name is required and must be at most 160 characters")
		}
	}
	if in.Visibility != nil && *in.Visibility != "workspace" && *in.Visibility != "private" {
		return in, invalid("visibility must be workspace or private")
	}
	if in.Status != nil && *in.Status != "active" && *in.Status != "archived" {
		return in, invalid("status must be active or archived")
	}
	return in, nil
}
func ValidateFilters(in Filters, public bool) (Filters, error) {
	in.Query, in.Category = strings.TrimSpace(in.Query), strings.TrimSpace(in.Category)
	if in.Category != "" && (len(in.Category) > 100 || !kebabPattern.MatchString(in.Category)) {
		return in, invalid("category must be lowercase kebab-case")
	}
	if in.Difficulty != "" && in.Difficulty != "easy" && in.Difficulty != "medium" && in.Difficulty != "hard" {
		return in, invalid("difficulty must be easy, medium, or hard")
	}
	if !public && in.Status == "" {
		in.Status = "active"
	}
	if !public && in.Status != "active" && in.Status != "archived" && in.Status != "all" {
		return in, invalid("status must be active, archived, or all")
	}
	return in, nil
}
func validateCriterion(in CriterionInput) (CriterionInput, error) {
	in.Key, in.Name, in.MetricKey = strings.TrimSpace(in.Key), strings.TrimSpace(in.Name), strings.TrimSpace(in.MetricKey)
	if in.Key == "" || len(in.Key) > 100 || !kebabPattern.MatchString(in.Key) {
		return in, invalid("criterion key must be lowercase kebab-case")
	}
	if in.Name == "" || len(in.Name) > 200 {
		return in, invalid("criterion name is required and must be at most 200 characters")
	}
	if len(in.MetricKey) > 200 || !metricPattern.MatchString(in.MetricKey) {
		return in, invalid("criterion metric_key is invalid")
	}
	if _, supported := evaluationmetric.Lookup(in.MetricKey); !supported {
		return in, invalid("criterion metric_key is unsupported by the deterministic evaluator")
	}
	switch in.Operator {
	case "gt", "gte", "lt", "lte", "eq", "neq", "required":
	default:
		return in, invalid("criterion operator is invalid")
	}
	if len(in.TargetValue) == 0 || string(in.TargetValue) == "null" || !json.Valid(in.TargetValue) {
		return in, invalid("criterion target_value is required and must be valid non-null JSON")
	}
	if in.SortOrder < 0 {
		return in, invalid("criterion sort_order must be non-negative")
	}
	return in, nil
}
func validateJSONObject(raw json.RawMessage, label string) error {
	if len(raw) == 0 {
		raw = json.RawMessage(`{}`)
	}
	var v map[string]any
	if json.Unmarshal(raw, &v) != nil || v == nil {
		return invalid(label + " must be a JSON object")
	}
	return nil
}
func validateJSONArray(raw json.RawMessage, label string) error {
	if len(raw) == 0 {
		raw = json.RawMessage(`[]`)
	}
	var v []any
	if json.Unmarshal(raw, &v) != nil || v == nil {
		return invalid(label + " must be a JSON array")
	}
	return nil
}
func validateObservables(values []string) ([]string, error) {
	seen := map[observablecatalog.ID]struct{}{}
	if values == nil {
		values = []string{}
	}
	if len(values) > 64 {
		return values, invalid("observables contains too many values")
	}
	canonical := make([]string, 0, len(values))
	for _, value := range values {
		id, err := observablecatalog.Parse(value)
		if err != nil {
			if err == observablecatalog.ErrMalformed {
				return values, invalid("observables must contain lowercase kebab-case values or known legacy aliases")
			}
			return values, invalid("observables contains an unsupported value")
		}
		if _, ok := seen[id]; ok {
			return values, invalid("observables must not contain duplicates")
		}
		seen[id] = struct{}{}
		canonical = append(canonical, string(id))
	}
	return canonical, nil
}
func validateTextItems(values []string, label string, max int) ([]string, error) {
	if values == nil {
		values = []string{}
	}
	if len(values) > max {
		return values, invalid(label + " contains too many values")
	}
	for i, v := range values {
		v = strings.TrimSpace(v)
		if v == "" || len(v) > 1000 {
			return values, invalid(label + " must contain non-empty values of at most 1000 characters")
		}
		values[i] = v
	}
	return values, nil
}
func invalid(message string) error { return fmt.Errorf("%w: %s", identity.ErrValidation, message) }
