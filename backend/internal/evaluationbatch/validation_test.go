package evaluationbatch

import (
	"errors"
	"fmt"
	"github.com/evalplane/evalplane/backend/internal/identity"
	"testing"
)

const uuidA = "11111111-1111-4111-8111-111111111111"

func validInput() CreateInput {
	return CreateInput{ScenarioVersionID: uuidA, AgentVersionIDs: []string{"22222222-2222-4222-8222-222222222222"}, RunsPerAgent: 3, EnvironmentMode: "isolated"}
}
func TestEvaluationBatchValidation(t *testing.T) {
	tests := map[string]func(*CreateInput){"no agents": func(v *CreateInput) { v.AgentVersionIDs = nil }, "duplicate agents": func(v *CreateInput) { v.AgentVersionIDs = append(v.AgentVersionIDs, v.AgentVersionIDs[0]) }, "too many agents": func(v *CreateInput) {
		v.AgentVersionIDs = make([]string, 21)
		for i := range v.AgentVersionIDs {
			v.AgentVersionIDs[i] = fmt.Sprintf("%08d-1111-4111-8111-111111111111", i)
		}
	}, "invalid repetitions": func(v *CreateInput) { v.RunsPerAgent = 21 }, "invalid environment": func(v *CreateInput) { v.EnvironmentMode = "production" }}
	for n, mutate := range tests {
		t.Run(n, func(t *testing.T) {
			v := validInput()
			mutate(&v)
			if _, e := ValidateCreate(v); !errors.Is(e, identity.ErrValidation) {
				t.Fatalf("error=%v", e)
			}
		})
	}
}
