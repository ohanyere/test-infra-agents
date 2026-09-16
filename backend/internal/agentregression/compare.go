package agentregression

import (
	"bytes"
	"encoding/json"
	"math"
	"sort"
	"strconv"

	"github.com/evalplane/evalplane/backend/internal/mcpserver"
	"github.com/evalplane/evalplane/backend/internal/runtimecontract"
)

type pairKey struct {
	scenario string
	ordinal  int
}

func Compare(comparison Comparison, trials []TrialRow, criteria []CriterionRow) Results {
	result := Results{Comparison: comparison}
	grouped := map[pairKey]*TrialPair{}
	for _, row := range trials {
		key := pairKey{row.ScenarioVersionID, row.TrialOrdinal}
		pair := grouped[key]
		if pair == nil {
			pair = &TrialPair{ScenarioVersionID: row.ScenarioVersionID, TrialOrdinal: row.TrialOrdinal}
			grouped[key] = pair
		}
		side := trialSide(row)
		switch row.AgentVersionID {
		case comparison.BaselineAgentVersionID:
			pair.Baseline = side
			pair.VariantID, pair.VariantSeed = row.VariantID, row.VariantSeed
		case comparison.CandidateAgentVersionID:
			pair.Candidate = side
			if pair.VariantID == "" {
				pair.VariantID, pair.VariantSeed = row.VariantID, row.VariantSeed
			}
		}
	}
	keys := make([]pairKey, 0, len(grouped))
	for key := range grouped {
		keys = append(keys, key)
	}
	sort.Slice(keys, func(i, j int) bool {
		if keys[i].scenario == keys[j].scenario {
			return keys[i].ordinal < keys[j].ordinal
		}
		return keys[i].scenario < keys[j].scenario
	})
	rowBySide := map[pairKey]map[string]TrialRow{}
	for _, row := range trials {
		key := pairKey{row.ScenarioVersionID, row.TrialOrdinal}
		if rowBySide[key] == nil {
			rowBySide[key] = map[string]TrialRow{}
		}
		rowBySide[key][row.AgentVersionID] = row
	}
	for _, key := range keys {
		pair := grouped[key]
		base, hasBase := rowBySide[key][comparison.BaselineAgentVersionID]
		candidate, hasCandidate := rowBySide[key][comparison.CandidateAgentVersionID]
		classifyPair(pair, base, candidate, hasBase, hasCandidate, &result)
		result.Pairs = append(result.Pairs, *pair)
		result.Reproducibility = append(result.Reproducibility, manifest(comparison, *pair))
	}
	result.Safety = compareSafety(result.Pairs)
	result.Criteria = compareCriteria(result.Pairs, criteria, comparison)
	result.Scenarios = scenarioBreakdowns(result.Pairs)
	finalize(&result)
	return result
}

func trialSide(row TrialRow) TrialSide {
	validity := "valid"
	if row.PlatformInvalid {
		validity = "invalid_platform"
	} else if row.EvaluationResult == "" {
		validity = "pending"
	}
	return TrialSide{AgentVersionID: row.AgentVersionID, RunID: row.RunID, EvaluationID: row.EvaluationID, Validity: validity, Result: row.EvaluationResult, EvaluatorVersion: row.EvaluatorVersion, SafetyCodes: unique(row.SafetyCodes)}
}

func classifyPair(pair *TrialPair, base, candidate TrialRow, hasBase, hasCandidate bool, result *Results) {
	result.EvidenceQuality.ScheduledPairs++
	if !hasBase || !hasCandidate || pair.Baseline.Validity == "pending" || pair.Candidate.Validity == "pending" {
		pair.Incompatibility = "pending_trial"
		result.EvidenceQuality.PendingPairs++
		return
	}
	if pair.Baseline.Validity == "invalid_platform" && pair.Candidate.Validity == "invalid_platform" {
		pair.Incompatibility = "both_platform_invalid"
		result.EvidenceQuality.BothInvalidPairs++
		return
	}
	if pair.Baseline.Validity == "invalid_platform" {
		pair.Incompatibility = "baseline_platform_invalid"
		result.EvidenceQuality.InvalidBaselinePairs++
		return
	}
	if pair.Candidate.Validity == "invalid_platform" {
		pair.Incompatibility = "candidate_platform_invalid"
		result.EvidenceQuality.InvalidCandidatePairs++
		return
	}
	if base.VariantID == "" || base.VariantID != candidate.VariantID || base.VariantSeed != candidate.VariantSeed {
		pair.Incompatibility = "hidden_variant_mismatch"
		result.EvidenceQuality.IncompatiblePairs++
		return
	}
	if base.EvaluatorVersion == "" || base.EvaluatorVersion != candidate.EvaluatorVersion {
		pair.Incompatibility = "evaluator_contract_mismatch"
		result.EvidenceQuality.IncompatiblePairs++
		return
	}
	pair.Comparable = true
	result.EvidenceQuality.ComparablePairs++
	pair.Transition = outcomeTransition(base.EvaluationResult, candidate.EvaluationResult)
	addOutcome(&result.Baseline, base.EvaluationResult)
	addOutcome(&result.Candidate, candidate.EvaluationResult)
	addTransition(&result.Transitions, pair.Transition)
}

func outcomeTransition(baseline, candidate string) string {
	switch {
	case baseline == "pass" && candidate == "pass":
		return "unchanged_pass"
	case baseline == "pass" && candidate == "fail":
		return "regression"
	case baseline == "fail" && candidate == "pass":
		return "improvement"
	case baseline == "fail" && candidate == "fail":
		return "unchanged_failure"
	case baseline == "pass" && candidate == "inconclusive":
		return "candidate_inconclusive"
	case baseline == "inconclusive" && candidate == "pass":
		return "candidate_resolved_inconclusive"
	case baseline == "inconclusive" && candidate == "fail":
		return "candidate_failed_from_inconclusive"
	case baseline == "fail" && candidate == "inconclusive":
		return "candidate_inconclusive"
	default:
		return "unchanged_inconclusive"
	}
}

func addOutcome(out *OutcomeCounts, status string) {
	switch status {
	case "pass":
		out.Passed++
	case "fail":
		out.Failed++
	default:
		out.Inconclusive++
	}
}

func addTransition(out *TransitionCounts, transition string) {
	switch transition {
	case "regression":
		out.Regressions++
	case "improvement":
		out.Improvements++
	case "candidate_inconclusive":
		out.CandidateInconclusive++
	case "candidate_resolved_inconclusive":
		out.ResolvedInconclusive++
	case "candidate_failed_from_inconclusive":
		out.InconclusiveTransitions++
	default:
		out.Unchanged++
	}
}

func compareSafety(pairs []TrialPair) SafetyComparison {
	newCodes, resolved, persistent := map[string]int{}, map[string]int{}, map[string]int{}
	for _, pair := range pairs {
		if !pair.Comparable {
			continue
		}
		base, candidate := set(pair.Baseline.SafetyCodes), set(pair.Candidate.SafetyCodes)
		for code := range candidate {
			if base[code] {
				persistent[code]++
			} else {
				newCodes[code]++
			}
		}
		for code := range base {
			if !candidate[code] {
				resolved[code]++
			}
		}
	}
	return SafetyComparison{New: codeCounts(newCodes), Resolved: codeCounts(resolved), Persistent: codeCounts(persistent)}
}

type criterionKey struct{ scenario, key, operator, target, unit string }
type criterionPairKey struct {
	pairKey
	contract criterionKey
}

func compareCriteria(pairs []TrialPair, rows []CriterionRow, comparison Comparison) []CriterionComparison {
	comparable := map[pairKey]bool{}
	for _, p := range pairs {
		comparable[pairKey{p.ScenarioVersionID, p.TrialOrdinal}] = p.Comparable
	}
	group := map[criterionPairKey]map[string]CriterionRow{}
	for _, row := range rows {
		pk := pairKey{row.ScenarioVersionID, row.TrialOrdinal}
		if !comparable[pk] {
			continue
		}
		unit := ""
		if row.Unit != nil {
			unit = *row.Unit
		}
		ck := criterionKey{row.ScenarioVersionID, row.CriterionKey, row.Operator, string(row.TargetValue), unit}
		gk := criterionPairKey{pk, ck}
		if group[gk] == nil {
			group[gk] = map[string]CriterionRow{}
		}
		group[gk][row.AgentVersionID] = row
	}
	agg := map[criterionKey]*CriterionComparison{}
	for key, sides := range group {
		base, bok := sides[comparison.BaselineAgentVersionID]
		candidate, cok := sides[comparison.CandidateAgentVersionID]
		if !bok || !cok {
			continue
		}
		item := agg[key.contract]
		if item == nil {
			target := append(json.RawMessage(nil), base.TargetValue...)
			item = &CriterionComparison{ScenarioVersionID: base.ScenarioVersionID, CriterionKey: base.CriterionKey, Operator: base.Operator, TargetValue: target, Unit: base.Unit}
			agg[key.contract] = item
		}
		item.ComparedPairs++
		switch {
		case base.Status == "passed" && candidate.Status == "failed":
			item.Regressions++
		case base.Status == "failed" && candidate.Status == "passed":
			item.Improvements++
		case base.Status == candidate.Status && (base.Status == "passed" || base.Status == "failed"):
			item.Unchanged++
		default:
			item.Inconclusive++
		}
		if bv, bok := number(base.ObservedValue); bok {
			if cv, cok := number(candidate.ObservedValue); cok {
				item.MetricDeltas = append(item.MetricDeltas, MetricDelta{TrialOrdinal: key.ordinal, Baseline: bv, Candidate: cv, Delta: cv - bv, Movement: movement(base.Operator, base.TargetValue, bv, cv)})
			}
		}
	}
	out := make([]CriterionComparison, 0, len(agg))
	for _, item := range agg {
		sort.Slice(item.MetricDeltas, func(i, j int) bool { return item.MetricDeltas[i].TrialOrdinal < item.MetricDeltas[j].TrialOrdinal })
		out = append(out, *item)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].ScenarioVersionID == out[j].ScenarioVersionID {
			return out[i].CriterionKey < out[j].CriterionKey
		}
		return out[i].ScenarioVersionID < out[j].ScenarioVersionID
	})
	return out
}

func movement(operator string, targetRaw json.RawMessage, baseline, candidate float64) string {
	if candidate == baseline {
		return "unchanged"
	}
	target, ok := number(targetRaw)
	switch operator {
	case ">", ">=", "gt", "gte":
		if candidate > baseline {
			return "toward_requirement"
		}
		return "away_from_requirement"
	case "<", "<=", "lt", "lte":
		if candidate < baseline {
			return "toward_requirement"
		}
		return "away_from_requirement"
	case "==", "eq":
		if ok && math.Abs(candidate-target) < math.Abs(baseline-target) {
			return "toward_requirement"
		}
		return "away_from_requirement"
	case "!=", "ne":
		if ok && baseline == target && candidate != target {
			return "toward_requirement"
		}
		if ok && baseline != target && candidate == target {
			return "away_from_requirement"
		}
	}
	return "changed"
}

func scenarioBreakdowns(pairs []TrialPair) []ScenarioBreakdown {
	group := map[string][]TrialPair{}
	for _, p := range pairs {
		group[p.ScenarioVersionID] = append(group[p.ScenarioVersionID], p)
	}
	ids := make([]string, 0, len(group))
	for id := range group {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	out := make([]ScenarioBreakdown, 0, len(ids))
	for _, id := range ids {
		var r Results
		for _, p := range group[id] {
			r.EvidenceQuality.ScheduledPairs++
			if p.Comparable {
				r.EvidenceQuality.ComparablePairs++
				addOutcome(&r.Baseline, p.Baseline.Result)
				addOutcome(&r.Candidate, p.Candidate.Result)
				addTransition(&r.Transitions, p.Transition)
			} else {
				switch p.Incompatibility {
				case "both_platform_invalid":
					r.EvidenceQuality.BothInvalidPairs++
				case "baseline_platform_invalid":
					r.EvidenceQuality.InvalidBaselinePairs++
				case "candidate_platform_invalid":
					r.EvidenceQuality.InvalidCandidatePairs++
				case "pending_trial":
					r.EvidenceQuality.PendingPairs++
				default:
					r.EvidenceQuality.IncompatiblePairs++
				}
			}
		}
		rates(&r.Baseline, r.EvidenceQuality.ComparablePairs)
		rates(&r.Candidate, r.EvidenceQuality.ComparablePairs)
		out = append(out, ScenarioBreakdown{id, r.EvidenceQuality, r.Baseline, r.Candidate, r.Transitions})
	}
	return out
}

func finalize(result *Results) {
	rates(&result.Baseline, result.EvidenceQuality.ComparablePairs)
	rates(&result.Candidate, result.EvidenceQuality.ComparablePairs)
	if result.Baseline.PassRate != nil && result.Candidate.PassRate != nil {
		d := *result.Candidate.PassRate - *result.Baseline.PassRate
		result.DeltaPassRate = &d
	}
	result.InsufficientEvidence = result.EvidenceQuality.ComparablePairs < MinimumComparablePairs
	regressed := result.Transitions.Regressions > 0 || len(result.Safety.New) > 0
	improved := result.Transitions.Improvements > 0 || len(result.Safety.Resolved) > 0
	for _, c := range result.Criteria {
		regressed = regressed || c.Regressions > 0
		improved = improved || c.Improvements > 0
	}
	switch {
	case regressed && improved:
		result.Classification = ClassificationMixed
	case regressed:
		result.Classification = ClassificationRegressed
	case result.InsufficientEvidence:
		result.Classification = ClassificationInsufficientEvidence
	case improved:
		result.Classification = ClassificationImproved
	default:
		result.Classification = ClassificationNoDetectedRegression
	}
}

func rates(out *OutcomeCounts, denominator int) {
	if denominator > 0 {
		v := float64(out.Passed) / float64(denominator)
		out.PassRate = &v
	}
}
func manifest(c Comparison, p TrialPair) ReproducibilityManifest {
	v := p.Baseline.EvaluatorVersion
	if v == "" {
		v = p.Candidate.EvaluatorVersion
	}
	out := ReproducibilityManifest{SuiteVersionID: c.SuiteVersionID, ScenarioVersionID: p.ScenarioVersionID, BaselineAgentVersionID: c.BaselineAgentVersionID, CandidateAgentVersionID: c.CandidateAgentVersionID, TrialOrdinal: p.TrialOrdinal, HiddenVariantID: p.VariantID, HiddenVariantSeed: p.VariantSeed, HiddenVariantConfigIdentity: p.VariantID, EvaluatorVersion: v, CapabilityCatalogVersion: runtimecontract.CapabilityCatalogVersion, InfrastructureCapabilityCatalogVersion: runtimecontract.InfrastructureCapabilityCatalogVersion, RuntimeContractVersion: runtimecontract.TrafficSpikeRuntimeVersion}
	if c.Baseline.ToolAccessMode == "mcp" || c.Candidate.ToolAccessMode == "mcp" {
		out.MCPProtocolVersion = mcpserver.ProtocolVersion
		out.MCPToolContract = mcpserver.ToolContract
	}
	return out
}
func number(raw json.RawMessage) (float64, bool) {
	v, err := strconv.ParseFloat(string(bytes.TrimSpace(raw)), 64)
	return v, err == nil && !math.IsNaN(v) && !math.IsInf(v, 0)
}
func set(values []string) map[string]bool {
	out := map[string]bool{}
	for _, v := range values {
		if v != "" {
			out[v] = true
		}
	}
	return out
}
func unique(values []string) []string {
	m := set(values)
	out := make([]string, 0, len(m))
	for v := range m {
		out = append(out, v)
	}
	sort.Strings(out)
	return out
}
func codeCounts(values map[string]int) []SafetyCodeCount {
	keys := make([]string, 0, len(values))
	for k := range values {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	out := make([]SafetyCodeCount, 0, len(keys))
	for _, k := range keys {
		out = append(out, SafetyCodeCount{k, values[k]})
	}
	return out
}
