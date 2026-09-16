package scenarioauthoring

import (
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"strings"
)

var keyPattern = regexp.MustCompile(`^[a-z0-9]+(?:-[a-z0-9]+)*$`)

func CompileDraft(d ScenarioDraft) (ScenarioDraft, error) {
	d.Name = strings.TrimSpace(d.Name)
	d.Description = strings.TrimSpace(d.Description)
	d.AgentSpec.Task = strings.TrimSpace(d.AgentSpec.Task)
	if d.Name == "" || len(d.Name) > 160 || d.Category == "" || !keyPattern.MatchString(d.Category) || d.DurationSeconds < 1 || d.DurationSeconds > 86400 || d.AgentSpec.Task == "" {
		return d, errors.New("draft has invalid required fields")
	}
	if d.Difficulty != "easy" && d.Difficulty != "medium" && d.Difficulty != "hard" {
		return d, errors.New("draft difficulty is invalid")
	}
	objects := []struct {
		value *json.RawMessage
		name  string
		array bool
	}{{&d.AgentSpec.VisibleContext, "visible_context", false}, {&d.AgentSpec.AllowedResources, "allowed_resources", true}, {&d.Environment, "environment", false}, {&d.Conditions, "conditions", true}, {&d.GroundTruth, "ground_truth", false}, {&d.ExecutionSpec, "execution_spec", false}}
	for _, item := range objects {
		if len(*item.value) == 0 {
			if item.array {
				*item.value = json.RawMessage(`[]`)
			} else {
				*item.value = json.RawMessage(`{}`)
			}
		}
		var value any
		if json.Unmarshal(*item.value, &value) != nil {
			return d, fmt.Errorf("%s must be valid JSON", item.name)
		}
		if item.array {
			if _, ok := value.([]any); !ok {
				return d, fmt.Errorf("%s must be an array", item.name)
			}
		} else if _, ok := value.(map[string]any); !ok {
			return d, fmt.Errorf("%s must be an object", item.name)
		}
	}
	allowed := map[string]bool{"inspect": true, "scale_workload": true, "restart_pod": true, "no_op": true}
	for _, action := range d.AgentSpec.AllowedActions {
		if !allowed[action] {
			return d, fmt.Errorf("unsupported action %q", action)
		}
	}
	keys := map[string]bool{}
	for i := range d.Criteria {
		c := &d.Criteria[i]
		if c.Key == "" || !keyPattern.MatchString(c.Key) || keys[c.Key] {
			return d, errors.New("criteria require unique kebab-case keys")
		}
		keys[c.Key] = true
		if c.Operator != "gt" && c.Operator != "gte" && c.Operator != "lt" && c.Operator != "lte" && c.Operator != "eq" && c.Operator != "neq" && c.Operator != "required" {
			return d, errors.New("criterion operator is invalid")
		}
		if len(c.TargetValue) == 0 || string(c.TargetValue) == "null" {
			c.RequiresConfirmation = true
			d.Warnings = appendUnique(d.Warnings, "A measurable threshold was not specified for criterion "+c.Key+".")
			continue
		}
		var number any
		if json.Unmarshal(c.TargetValue, &number) != nil {
			return d, errors.New("criterion target is invalid JSON")
		}
	}
	executionType := ""
	var spec map[string]any
	_ = json.Unmarshal(d.ExecutionSpec, &spec)
	if value, ok := spec["type"].(string); ok {
		executionType = value
	}
	unsupported := append([]string(nil), d.UnsupportedCapabilities...)
	if executionType != "" && executionType != "traffic_spike" {
		unsupported = appendUnique(unsupported, executionType)
	}
	d.UnsupportedCapabilities = unsupported
	d.RunnableNow = len(unsupported) == 0
	for _, c := range d.Criteria {
		if c.RequiresConfirmation {
			d.RunnableNow = false
		}
	}
	return d, nil
}
func appendUnique(values []string, value string) []string {
	for _, v := range values {
		if v == value {
			return values
		}
	}
	return append(values, value)
}
