package evaluationsuite

import (
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"sort"
	"strconv"
)

func AssignVariants(executionID, suiteVersionID string, scenarios []ScenarioEntry, runs []RunAssignmentInput) []VariantAssignment {
	definitions := make(map[string][]HiddenVariantConfig, len(scenarios))
	for _, scenario := range scenarios {
		definitions[scenario.ScenarioVersionID] = append([]HiddenVariantConfig(nil), scenario.HiddenVariants...)
	}
	assignments := make([]VariantAssignment, 0, len(runs))
	for _, run := range runs {
		variants := definitions[run.ScenarioVersionID]
		config := HiddenVariantConfig{}
		variantIndex := 0
		if len(variants) > 0 {
			variantIndex = (run.TrialOrdinal - 1) % len(variants)
			config = variants[variantIndex]
		}
		configJSON, _ := json.Marshal(config)
		identityHash := sha256.Sum256([]byte(suiteVersionID + "\x00" + run.ScenarioVersionID + "\x00" + string(configJSON) + "\x00" + strconv.Itoa(variantIndex)))
		seedHash := sha256.Sum256([]byte(executionID + "\x00" + suiteVersionID + "\x00" + run.ScenarioVersionID + "\x00" + strconv.Itoa(run.TrialOrdinal)))
		assignments = append(assignments, VariantAssignment{
			RunID:             run.RunID,
			SuiteExecutionID:  executionID,
			SuiteVersionID:    suiteVersionID,
			ScenarioVersionID: run.ScenarioVersionID,
			AgentVersionID:    run.AgentVersionID,
			TrialOrdinal:      run.TrialOrdinal,
			VariantID:         "variant-" + hex.EncodeToString(identityHash[:8]),
			VariantSeed:       int64(binary.BigEndian.Uint64(seedHash[:8]) & (1<<63 - 1)),
			HiddenConfig:      config,
		})
	}
	sort.Slice(assignments, func(i, j int) bool {
		if assignments[i].ScenarioVersionID != assignments[j].ScenarioVersionID {
			return assignments[i].ScenarioVersionID < assignments[j].ScenarioVersionID
		}
		if assignments[i].AgentVersionID != assignments[j].AgentVersionID {
			return assignments[i].AgentVersionID < assignments[j].AgentVersionID
		}
		if assignments[i].TrialOrdinal != assignments[j].TrialOrdinal {
			return assignments[i].TrialOrdinal < assignments[j].TrialOrdinal
		}
		return assignments[i].RunID < assignments[j].RunID
	})
	return assignments
}
