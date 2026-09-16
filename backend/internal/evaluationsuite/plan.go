package evaluationsuite

import (
	"encoding/json"

	"github.com/evalplane/evalplane/backend/internal/evaluationbatch"
)

// BuildBatchPlan projects an immutable suite version into ordinary evaluation
// batches. It never resolves latest versions: all IDs come from pinned inputs.
func BuildBatchPlan(version Version, agentVersionIDs []string) []evaluationbatch.CreateInput {
	plan := make([]evaluationbatch.CreateInput, 0, len(version.Scenarios))
	for _, scenario := range version.Scenarios {
		plan = append(plan, evaluationbatch.CreateInput{
			ScenarioVersionID: scenario.ScenarioVersionID,
			AgentVersionIDs:   append([]string(nil), agentVersionIDs...),
			RunsPerAgent:      scenario.TrialCount,
			EnvironmentMode:   "isolated",
			Config:            json.RawMessage(`{}`),
		})
	}
	return plan
}
