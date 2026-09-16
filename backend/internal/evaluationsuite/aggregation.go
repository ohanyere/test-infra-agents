package evaluationsuite

import (
	"sort"
	"strings"
)

const MinimumConsistencySamples = 3

func Aggregate(executionID string, rows []TrialRow) Results {
	result := Results{ExecutionID: executionID}
	byAgent := map[string]*Counts{}
	byPair := map[string]*Counts{}
	pairIDs := map[string][2]string{}
	for _, row := range rows {
		addTrial(&result.Overall, row)
		if byAgent[row.AgentVersionID] == nil {
			byAgent[row.AgentVersionID] = &Counts{}
		}
		addTrial(byAgent[row.AgentVersionID], row)
		key := row.AgentVersionID + "\x00" + row.ScenarioVersionID
		if byPair[key] == nil {
			byPair[key] = &Counts{}
			pairIDs[key] = [2]string{row.AgentVersionID, row.ScenarioVersionID}
		}
		addTrial(byPair[key], row)
		result.Trials = append(result.Trials, trialResult(row))
	}
	finalizeCounts(&result.Overall)
	agentIDs := make([]string, 0, len(byAgent))
	for id := range byAgent {
		agentIDs = append(agentIDs, id)
	}
	sort.Strings(agentIDs)
	for _, id := range agentIDs {
		finalizeCounts(byAgent[id])
		result.ByAgent = append(result.ByAgent, AgentCounts{AgentVersionID: id, Counts: *byAgent[id]})
	}
	pairs := make([]string, 0, len(byPair))
	for key := range byPair {
		pairs = append(pairs, key)
	}
	sort.Strings(pairs)
	for _, key := range pairs {
		finalizeCounts(byPair[key])
		ids := pairIDs[key]
		result.ByScenario = append(result.ByScenario, AgentScenarioCounts{AgentVersionID: ids[0], ScenarioVersionID: ids[1], Counts: *byPair[key]})
	}
	sort.Slice(result.Trials, func(i, j int) bool {
		a, b := result.Trials[i], result.Trials[j]
		if a.AgentVersionID != b.AgentVersionID {
			return a.AgentVersionID < b.AgentVersionID
		}
		if a.ScenarioVersionID != b.ScenarioVersionID {
			return a.ScenarioVersionID < b.ScenarioVersionID
		}
		if a.TrialOrdinal != b.TrialOrdinal {
			return a.TrialOrdinal < b.TrialOrdinal
		}
		return a.RunID < b.RunID
	})
	return result
}

func addTrial(counts *Counts, row TrialRow) {
	counts.Total++
	counts.Scheduled++
	if platformInvalidTerminal(row) {
		counts.Completed++
		counts.InvalidPlatform++
		return
	}
	switch row.EvaluationResult {
	case "pass":
		counts.Completed++
		counts.Valid++
		counts.Passed++
	case "fail":
		counts.Completed++
		counts.Valid++
		counts.Failed++
	case "inconclusive", "stopped":
		counts.Completed++
		counts.Valid++
		counts.Inconclusive++
	default:
		switch row.RunStatus {
		case "provisioning", "running":
			counts.Running++
		default:
			counts.Pending++
		}
		return
	}
	if row.SafetyViolations > 0 {
		counts.TrialsWithSafetyViolation++
		counts.TotalSafetyViolations += row.SafetyViolations
		for _, code := range row.SafetyCodes {
			addViolationCode(counts, code)
		}
	}
}

func platformInvalidTerminal(row TrialRow) bool {
	if !row.PlatformInvalid {
		return false
	}
	if row.EvaluationResult != "" {
		return true
	}
	switch row.RunStatus {
	case "completed", "failed", "stopped":
		return true
	default:
		return false
	}
}

func trialResult(row TrialRow) TrialResult {
	validity := ValidityPending
	evaluationResult := row.EvaluationResult
	if platformInvalidTerminal(row) {
		validity = ValidityInvalidPlatform
		// Platform-invalid evidence is retained in the run evaluation, but is not
		// surfaced as an agent outcome in consistency results.
		evaluationResult = ""
	} else if row.EvaluationResult != "" {
		validity = ValidityValid
	}
	return TrialResult{RunID: row.RunID, AgentVersionID: row.AgentVersionID, ScenarioVersionID: row.ScenarioVersionID, TrialOrdinal: row.TrialOrdinal, VariantID: row.VariantID, VariantSeed: row.VariantSeed, Validity: validity, EvaluationResult: evaluationResult, EvaluatorVersion: row.EvaluatorVersion}
}

func addViolationCode(counts *Counts, code string) {
	code = strings.TrimSpace(code)
	if code == "" {
		return
	}
	for index := range counts.SafetyViolationCodes {
		if counts.SafetyViolationCodes[index].Code == code {
			counts.SafetyViolationCodes[index].Count++
			return
		}
	}
	counts.SafetyViolationCodes = append(counts.SafetyViolationCodes, ViolationCode{Code: code, Count: 1})
}

func finalizeCounts(counts *Counts) {
	evaluable := counts.Passed + counts.Failed
	if evaluable > 0 {
		value := 100 * float64(counts.Passed) / float64(evaluable)
		counts.PassRate = &value
	}
	if counts.Valid > 0 {
		value := 100 * float64(counts.TrialsWithSafetyViolation) / float64(counts.Valid)
		counts.SafetyViolationRate = &value
	}
	counts.InsufficientSamples = counts.Valid < MinimumConsistencySamples
	sort.Slice(counts.SafetyViolationCodes, func(i, j int) bool {
		return counts.SafetyViolationCodes[i].Code < counts.SafetyViolationCodes[j].Code
	})
}

func SummarizeCriteria(rows []CriterionObservationRow) []CriterionSummary {
	type group struct {
		row    CriterionObservationRow
		values []float64
	}
	groups := map[string]*group{}
	for _, row := range rows {
		if row.PlatformInvalid {
			continue
		}
		unit := "<nil>"
		if row.Unit != nil {
			unit = *row.Unit
		}
		key := strings.Join([]string{row.AgentVersionID, row.ScenarioVersionID, row.CriterionID, row.Operator, string(row.TargetValue), unit}, "\x00")
		if groups[key] == nil {
			groups[key] = &group{row: row}
		}
		groups[key].values = append(groups[key].values, row.ObservedValue)
	}
	keys := make([]string, 0, len(groups))
	for key := range groups {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	out := make([]CriterionSummary, 0, len(keys))
	for _, key := range keys {
		item := groups[key]
		sort.Float64s(item.values)
		sum := 0.0
		for _, value := range item.values {
			sum += value
		}
		middle := len(item.values) / 2
		median := item.values[middle]
		if len(item.values)%2 == 0 {
			median = (item.values[middle-1] + item.values[middle]) / 2
		}
		out = append(out, CriterionSummary{AgentVersionID: item.row.AgentVersionID, ScenarioVersionID: item.row.ScenarioVersionID, CriterionID: item.row.CriterionID, CriterionKey: item.row.CriterionKey, Operator: item.row.Operator, TargetValue: append([]byte(nil), item.row.TargetValue...), Unit: item.row.Unit, Count: len(item.values), Min: item.values[0], Max: item.values[len(item.values)-1], Mean: sum / float64(len(item.values)), Median: median})
	}
	return out
}
