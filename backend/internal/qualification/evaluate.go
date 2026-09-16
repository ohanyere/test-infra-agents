package qualification

import (
	"encoding/json"
	"fmt"
	"sort"

	"github.com/evalplane/evalplane/backend/internal/agentregression"
	"github.com/evalplane/evalplane/backend/internal/evaluationsuite"
	"github.com/evalplane/evalplane/backend/internal/runtimecontract"
)

func Evaluate(policy PolicyVersion, agentVersionID string, facts AssessmentFacts) Assessment {
	rules := []RuleResult{}
	add := func(kind, status string, expected, observed any, reason string, refs ...string) {
		rules = append(rules, RuleResult{RuleType: kind, Status: status, Expected: raw(expected), Observed: raw(observed), Reason: reason, SupportingReferences: refs})
	}
	d := policy.Definition
	c := facts.Counts
	if c.Valid < d.MinimumValidTrials {
		add("minimum_valid_trials", RuleInconclusive, map[string]any{"gte": d.MinimumValidTrials}, c.Valid, "fewer valid trials than required", "suite_execution:"+facts.SuiteVersionID)
	} else {
		add("minimum_valid_trials", RulePassed, map[string]any{"gte": d.MinimumValidTrials}, c.Valid, "required valid-trial sample is available")
	}
	if c.PassRate == nil {
		add("minimum_overall_pass_rate", RuleInconclusive, map[string]any{"gte": d.MinimumOverallPassRate}, nil, "pass rate is unavailable because no valid evaluable outcomes exist")
	} else if *c.PassRate < d.MinimumOverallPassRate {
		add("minimum_overall_pass_rate", RuleFailed, map[string]any{"gte": d.MinimumOverallPassRate}, *c.PassRate, "overall pass rate is below the policy minimum")
	} else {
		add("minimum_overall_pass_rate", RulePassed, map[string]any{"gte": d.MinimumOverallPassRate}, *c.PassRate, "overall pass rate meets the policy minimum")
	}
	if d.MinimumPerScenarioPassRate != nil {
		scenarios := append([]evaluationsuite.AgentScenarioCounts(nil), facts.Scenarios...)
		sort.Slice(scenarios, func(i, j int) bool { return scenarios[i].ScenarioVersionID < scenarios[j].ScenarioVersionID })
		for _, s := range scenarios {
			kind := "minimum_per_scenario_pass_rate:" + s.ScenarioVersionID
			if s.PassRate == nil {
				add(kind, RuleInconclusive, map[string]any{"gte": *d.MinimumPerScenarioPassRate}, nil, "scenario pass rate is unavailable", "scenario_version:"+s.ScenarioVersionID)
			} else if *s.PassRate < *d.MinimumPerScenarioPassRate {
				add(kind, RuleFailed, map[string]any{"gte": *d.MinimumPerScenarioPassRate}, *s.PassRate, "scenario pass rate is below the policy minimum", "scenario_version:"+s.ScenarioVersionID)
			} else {
				add(kind, RulePassed, map[string]any{"gte": *d.MinimumPerScenarioPassRate}, *s.PassRate, "scenario pass rate meets the policy minimum", "scenario_version:"+s.ScenarioVersionID)
			}
		}
	}
	inconclusiveRate := rate(c.Inconclusive, c.Valid)
	if inconclusiveRate == nil {
		add("maximum_inconclusive_rate", RuleInconclusive, map[string]any{"lte": d.MaximumInconclusiveRate}, nil, "inconclusive rate is unavailable because there are no valid trials")
	} else if *inconclusiveRate > d.MaximumInconclusiveRate {
		add("maximum_inconclusive_rate", RuleFailed, map[string]any{"lte": d.MaximumInconclusiveRate}, *inconclusiveRate, "inconclusive rate exceeds the policy maximum")
	} else {
		add("maximum_inconclusive_rate", RulePassed, map[string]any{"lte": d.MaximumInconclusiveRate}, *inconclusiveRate, "inconclusive rate is within the policy maximum")
	}
	if d.MaximumInvalidPlatformRate != nil {
		invalidRate := rate(c.InvalidPlatform, c.Scheduled)
		if invalidRate == nil {
			add("maximum_invalid_platform_rate", RuleInconclusive, map[string]any{"lte": *d.MaximumInvalidPlatformRate}, nil, "invalid-platform rate is unavailable because no trials are scheduled")
		} else if *invalidRate > *d.MaximumInvalidPlatformRate {
			add("maximum_invalid_platform_rate", RuleInconclusive, map[string]any{"lte": *d.MaximumInvalidPlatformRate}, *invalidRate, "platform-invalid evidence exceeds the policy reliability limit")
		} else {
			add("maximum_invalid_platform_rate", RulePassed, map[string]any{"lte": *d.MaximumInvalidPlatformRate}, *invalidRate, "platform-invalid evidence is within the policy reliability limit")
		}
	}
	if c.TotalSafetyViolations > d.MaximumSafetyViolations {
		add("maximum_safety_violations", RuleFailed, map[string]any{"lte": d.MaximumSafetyViolations}, c.TotalSafetyViolations, "structured safety violations exceed the policy maximum")
	} else {
		add("maximum_safety_violations", RulePassed, map[string]any{"lte": d.MaximumSafetyViolations}, c.TotalSafetyViolations, "structured safety violations are within the policy maximum")
	}
	codes := map[string]int{}
	for _, item := range c.SafetyViolationCodes {
		codes[item.Code] = item.Count
	}
	for _, code := range d.ForbiddenSafetyCodes {
		status := RulePassed
		reason := "forbidden structured safety code was not observed"
		if codes[code] > 0 {
			status = RuleFailed
			reason = "forbidden structured safety code was observed"
		}
		add("forbidden_safety_code:"+code, status, map[string]any{"equals": 0}, codes[code], reason, "finding_code:"+code)
	}
	if d.RequireNoDetectedRegression {
		if facts.Regression == nil {
			add("no_detected_regression", RuleInconclusive, "no_detected_regression", nil, "required regression comparison is unavailable")
		} else {
			classification := facts.Regression.Classification
			switch classification {
			case agentregression.ClassificationImproved, agentregression.ClassificationNoDetectedRegression:
				add("no_detected_regression", RulePassed, "no detected regression", classification, "regression comparison satisfies the policy", "agent_regression:"+facts.Regression.Comparison.ID)
			case agentregression.ClassificationRegressed, agentregression.ClassificationMixed:
				add("no_detected_regression", RuleFailed, "no detected regression", classification, "regression comparison detected regression evidence", "agent_regression:"+facts.Regression.Comparison.ID)
			default:
				add("no_detected_regression", RuleInconclusive, "no detected regression", classification, "regression comparison has insufficient comparable evidence", "agent_regression:"+facts.Regression.Comparison.ID)
			}
		}
	}
	status := StatusQualified
	for _, rule := range rules {
		if rule.Status == RuleFailed {
			status = StatusNotQualified
			break
		}
		if rule.Status == RuleInconclusive {
			status = StatusInconclusive
		}
	}
	return Assessment{PolicyVersionID: policy.ID, SuiteVersionID: facts.SuiteVersionID, AgentVersionID: agentVersionID, Status: status, Rules: rules, Reproducibility: manifests(policy.ID, agentVersionID, facts)}
}

func manifests(policyID, agentID string, facts AssessmentFacts) []agentregression.ReproducibilityManifest {
	out := []agentregression.ReproducibilityManifest{}
	for _, trial := range facts.Trials {
		if trial.AgentVersionID != agentID {
			continue
		}
		out = append(out, agentregression.ReproducibilityManifest{PolicyVersionID: policyID, SuiteVersionID: facts.SuiteVersionID, ScenarioVersionID: trial.ScenarioVersionID, AgentVersionID: agentID, TrialOrdinal: trial.TrialOrdinal, HiddenVariantID: trial.VariantID, HiddenVariantSeed: trial.VariantSeed, HiddenVariantConfigIdentity: trial.VariantID, EvaluatorVersion: trial.EvaluatorVersion, CapabilityCatalogVersion: runtimecontract.CapabilityCatalogVersion, InfrastructureCapabilityCatalogVersion: runtimecontract.InfrastructureCapabilityCatalogVersion, RuntimeContractVersion: runtimecontract.TrafficSpikeRuntimeVersion})
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].ScenarioVersionID == out[j].ScenarioVersionID {
			return out[i].TrialOrdinal < out[j].TrialOrdinal
		}
		return out[i].ScenarioVersionID < out[j].ScenarioVersionID
	})
	return out
}
func rate(part, total int) *float64 {
	if total == 0 {
		return nil
	}
	v := 100 * float64(part) / float64(total)
	return &v
}
func raw(v any) json.RawMessage    { b, _ := json.Marshal(v); return b }
func invalid(message string) error { return fmt.Errorf("qualification: %s", message) }
