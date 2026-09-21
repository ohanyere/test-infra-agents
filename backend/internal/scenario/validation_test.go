package scenario

import (
	"encoding/json"
	"errors"
	"github.com/evalplane/evalplane/backend/internal/identity"
	"testing"
)

func validScenario() CreateInput {
	return CreateInput{Name: "Scenario", Slug: "scenario", Visibility: "workspace", InitialVersion: VersionInput{Category: "traffic", Difficulty: "medium", DurationSeconds: 600, Environment: json.RawMessage(`{}`), Conditions: json.RawMessage(`[]`), Observables: []string{"cpu"}, Criteria: []CriterionInput{{Key: "availability", Name: "Availability", MetricKey: "application.availability_pct", Operator: "gte", TargetValue: json.RawMessage(`99.9`)}}}}
}
func TestScenarioValidation(t *testing.T) {
	tests := map[string]func(*CreateInput){"invalid slug": func(v *CreateInput) { v.Slug = "Bad Slug" }, "public visibility": func(v *CreateInput) { v.Visibility = "public" }, "invalid difficulty": func(v *CreateInput) { v.InitialVersion.Difficulty = "extreme" }, "invalid duration": func(v *CreateInput) { v.InitialVersion.DurationSeconds = 0 }, "malformed category": func(v *CreateInput) { v.InitialVersion.Category = "Traffic Ops" }, "duplicate observable": func(v *CreateInput) { v.InitialVersion.Observables = []string{"cpu", "cpu"} }, "duplicate criterion": func(v *CreateInput) {
		v.InitialVersion.Criteria = append(v.InitialVersion.Criteria, v.InitialVersion.Criteria[0])
	}, "invalid operator": func(v *CreateInput) { v.InitialVersion.Criteria[0].Operator = "approximately" }, "invalid metric": func(v *CreateInput) { v.InitialVersion.Criteria[0].MetricKey = "bad metric" }}
	for name, mutate := range tests {
		t.Run(name, func(t *testing.T) {
			v := validScenario()
			mutate(&v)
			if _, e := ValidateCreate(v); !errors.Is(e, identity.ErrValidation) {
				t.Fatalf("error=%v", e)
			}
		})
	}
}
func TestScenarioEmptyPatch(t *testing.T) {
	if _, e := ValidateUpdate(UpdateInput{}); !errors.Is(e, identity.ErrValidation) {
		t.Fatalf("error=%v", e)
	}
}

func TestScenarioAllowsWorkloadImageAction(t *testing.T) {
	in := validScenario()
	in.InitialVersion.AllowedActions = []string{"inspect", "set_workload_image", "no_op"}
	if _, err := ValidateCreate(in); err != nil {
		t.Fatalf("GitOps recovery action rejected: %v", err)
	}
}
