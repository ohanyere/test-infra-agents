package reliability

import (
	"math"
	"reflect"
	"testing"
)

func trial(validity, result, variant string, ordinal int) Trial {
	return Trial{
		RunID:                      "run-" + validity + "-" + result + "-" + variant + "-" + string(rune('a'+ordinal)),
		ScenarioVersionID:          "scenario-1",
		AgentVersionID:             "agent-1",
		FamilyID:                   "kubernetes.capacity.traffic_spike",
		CaseType:                   "positive",
		Validity:                   validity,
		Result:                     result,
		VariantID:                  variant,
		Ordinal:                    ordinal,
		EvaluatorVersion:           EvaluatorContractVersion,
		RuntimeContractVersion:     RuntimeContractVersion,
		EnvironmentRef:             "env-instance-1",
		EnvironmentProfileID:       "kubernetes.namespace:v1",
		EnvironmentProfileContract: "environment-profile-catalog:v1",
		ProviderID:                 "kind-dev:v1",
		ProviderContract:           "environment-provider-contract:v1",
		IsolationTier:              "namespace",
	}
}

func approx(a, b float64) bool { return math.Abs(a-b) < 1e-9 }

func mustK(t *testing.T, metrics []KMetric, k int) KMetric {
	t.Helper()
	metric := KLookup(metrics, k)
	if metric == nil {
		t.Fatalf("missing k=%d metric", k)
	}
	return *metric
}

func TestContractPinned(t *testing.T) {
	if ReliabilityContractVersion != "reliability-metrics-contract:v1" {
		t.Fatalf("contract version drifted: %q", ReliabilityContractVersion)
	}
	if DefaultPolicy().ContractVersion != "reliability-policy:v1" {
		t.Fatalf("policy version drifted: %q", DefaultPolicy().ContractVersion)
	}
	for _, k := range SupportedK {
		if k < 1 {
			t.Fatalf("invalid supported k=%d", k)
		}
	}
}

func TestZeroValidTrials(t *testing.T) {
	metrics := KMetrics(nil)
	for _, metric := range metrics {
		if metric.Available || metric.PassAtK != nil || metric.PassToTheK != nil {
			t.Fatalf("k=%d must be unavailable with zero trials", metric.K)
		}
	}
	summary := Summarize(nil)
	if summary.ValidTrials != 0 || summary.PassRate != nil || summary.PlatformInvalidRate != nil {
		t.Fatalf("zero trials must yield empty summary: %+v", summary)
	}
	if got := AssessSufficiency(0, 3, 5); got != SufficiencyInsufficient {
		t.Fatalf("zero trials must be insufficient, got %q", got)
	}
	task := BuildTaskReliability(DefaultPolicy(), "s1", "a1", nil, nil)
	if task.Sufficiency != SufficiencyInsufficient {
		t.Fatalf("zero-trial task must be insufficient, got %q", task.Sufficiency)
	}
}

func TestSingleValidPass(t *testing.T) {
	trials := []Trial{trial(ValidityValid, ResultPass, "v1", 1)}
	summary := Summarize(trials)
	if summary.ValidTrials != 1 || summary.PassedTrials != 1 {
		t.Fatalf("unexpected summary: %+v", summary)
	}
	if summary.PassRate == nil || !approx(*summary.PassRate, 100) {
		t.Fatalf("single pass must yield 100%% pass rate: %+v", summary)
	}
	metrics := KMetrics(trials)
	k1 := mustK(t, metrics, 1)
	if !k1.Available || !approx(*k1.PassAtK, 1) || !approx(*k1.PassToTheK, 1) {
		t.Fatalf("k=1 must be 1/1: %+v", k1)
	}
	for _, k := range []int{2, 3, 5} {
		metric := mustK(t, metrics, k)
		if metric.Available || metric.PassAtK != nil || metric.PassToTheK != nil {
			t.Fatalf("k=%d must be unavailable with n=1: %+v", k, metric)
		}
	}
}

func TestMixedDenominatorAndPlatformExclusion(t *testing.T) {
	trials := []Trial{
		trial(ValidityValid, ResultPass, "v1", 1),
		trial(ValidityValid, ResultFail, "v1", 2),
		trial(ValidityValid, ResultFail, "v2", 3),
		trial(ValidityInvalidPlatform, ResultFail, "v2", 4),
		trial(ValidityInvalidPlatform, "", "v2", 5),
		trial(ValidityPending, "", "v2", 6),
	}
	summary := Summarize(trials)
	if summary.ValidTrials != 3 || summary.PassedTrials != 1 || summary.FailedTrials != 2 {
		t.Fatalf("valid denominator wrong: %+v", summary)
	}
	if summary.PlatformInvalidTrials != 2 || summary.PendingTrials != 1 {
		t.Fatalf("invalid/pending counts wrong: %+v", summary)
	}
	// Agent failures (fail) count in the denominator; platform failures do not.
	if summary.PassRate == nil || !approx(*summary.PassRate, 100.0/3.0) {
		t.Fatalf("pass rate must be 1/(1+2): %+v", summary)
	}
	if summary.PlatformInvalidRate == nil || !approx(*summary.PlatformInvalidRate, 40) {
		t.Fatalf("platform-invalid rate must be 2/(3+2): %+v", summary)
	}
}

func TestMathVectorN5C3K2(t *testing.T) {
	// Spec vector: n=5, c=3, k=2 -> pass@2 = 1-1/10 = 0.9, pass^2 = 3/10 = 0.3.
	trials := []Trial{
		trial(ValidityValid, ResultPass, "v1", 1),
		trial(ValidityValid, ResultPass, "v2", 2),
		trial(ValidityValid, ResultPass, "v3", 3),
		trial(ValidityValid, ResultFail, "v4", 4),
		trial(ValidityValid, ResultFail, "v5", 5),
	}
	metric := mustK(t, KMetrics(trials), 2)
	if !metric.Available {
		t.Fatal("k=2 must be available with n=5")
	}
	if !approx(*metric.PassAtK, 0.9) {
		t.Fatalf("pass@2 must be 0.9, got %v", *metric.PassAtK)
	}
	if !approx(*metric.PassToTheK, 0.3) {
		t.Fatalf("pass^2 must be 0.3, got %v", *metric.PassToTheK)
	}
}

func TestMathVectorAllPass(t *testing.T) {
	trials := []Trial{
		trial(ValidityValid, ResultPass, "v1", 1),
		trial(ValidityValid, ResultPass, "v1", 2),
		trial(ValidityValid, ResultPass, "v2", 3),
		trial(ValidityValid, ResultPass, "v2", 4),
		trial(ValidityValid, ResultPass, "v3", 5),
	}
	for _, k := range []int{1, 2, 3, 5} {
		metric := mustK(t, KMetrics(trials), k)
		if !metric.Available || !approx(*metric.PassAtK, 1) || !approx(*metric.PassToTheK, 1) {
			t.Fatalf("all-pass k=%d must be 1/1: %+v", k, metric)
		}
	}
}

func TestMathVectorAllFail(t *testing.T) {
	trials := []Trial{
		trial(ValidityValid, ResultFail, "v1", 1),
		trial(ValidityValid, ResultFail, "v1", 2),
		trial(ValidityValid, ResultFail, "v2", 3),
		trial(ValidityValid, ResultFail, "v2", 4),
		trial(ValidityValid, ResultFail, "v3", 5),
	}
	metric := mustK(t, KMetrics(trials), 3)
	if !metric.Available || !approx(*metric.PassAtK, 0) || !approx(*metric.PassToTheK, 0) {
		t.Fatalf("all-fail k=3 must be 0/0: %+v", metric)
	}
}

func TestMixedPassAtKExceedsPassToTheK(t *testing.T) {
	trials := []Trial{
		trial(ValidityValid, ResultPass, "v1", 1),
		trial(ValidityValid, ResultPass, "v2", 2),
		trial(ValidityValid, ResultFail, "v3", 3),
		trial(ValidityValid, ResultFail, "v4", 4),
	}
	metric := mustK(t, KMetrics(trials), 2)
	// pass@2 = 1 - C(2,2)/C(4,2) = 5/6; pass^2 = C(2,2)/C(4,2) = 1/6.
	if !approx(*metric.PassAtK, 5.0/6.0) || !approx(*metric.PassToTheK, 1.0/6.0) {
		t.Fatalf("unexpected mixed estimates: %+v", metric)
	}
	if !(*metric.PassAtK > *metric.PassToTheK) {
		t.Fatalf("mixed sample must show pass@k > pass^k: %+v", metric)
	}
}

func TestPassAtKAndPassToTheKDiffer(t *testing.T) {
	// Guard against the pass@k/pass^k confusion: same evidence, the "at
	// least one" and "all" estimates must differ on mixed samples and must
	// never be computed as pass_rate^k (which for 3/5 at k=2 is 0.36, not
	// the finite-sample 0.3).
	trials := []Trial{
		trial(ValidityValid, ResultPass, "v1", 1),
		trial(ValidityValid, ResultPass, "v2", 2),
		trial(ValidityValid, ResultPass, "v3", 3),
		trial(ValidityValid, ResultFail, "v4", 4),
		trial(ValidityValid, ResultFail, "v5", 5),
	}
	metric := mustK(t, KMetrics(trials), 2)
	naive := math.Pow(0.6, 2)
	if approx(*metric.PassToTheK, naive) {
		t.Fatalf("pass^k must not equal naive pass_rate^k (%v)", naive)
	}
}

func TestSufficiencyTransitions(t *testing.T) {
	policy := DefaultPolicy()
	if got := AssessSufficiency(policy.MinValidTrialsScenario-1, policy.MinValidTrialsScenario, policy.TargetValidTrialsScenario); got != SufficiencyInsufficient {
		t.Fatalf("below minimum must be insufficient: %q", got)
	}
	if got := AssessSufficiency(policy.MinValidTrialsScenario, policy.MinValidTrialsScenario, policy.TargetValidTrialsScenario); got != SufficiencyProvisional {
		t.Fatalf("at minimum must be provisional: %q", got)
	}
	if got := AssessSufficiency(policy.TargetValidTrialsScenario, policy.MinValidTrialsScenario, policy.TargetValidTrialsScenario); got != SufficiencySufficient {
		t.Fatalf("at target must be sufficient: %q", got)
	}
}

func TestFamilyBreadthNotFakedByRetries(t *testing.T) {
	policy := DefaultPolicy()
	trials := []Trial{}
	for i := 1; i <= 20; i++ {
		item := trial(ValidityValid, ResultPass, "v1", i)
		item.ScenarioVersionID = "scenario-only"
		trials = append(trials, item)
	}
	families := BuildFamilyReliabilities(policy, trials)
	if len(families) != 1 {
		t.Fatalf("expected one family, got %d", len(families))
	}
	family := families[0]
	if family.DistinctScenarios != 1 || len(family.ScenarioVersionIDs) != 1 {
		t.Fatalf("retries must not fake breadth: %+v", family)
	}
	if family.BreadthNote != "single_scenario" {
		t.Fatalf("single-scenario repetition must be flagged: %+v", family)
	}
	if family.ValidTrials != 20 {
		t.Fatalf("repetition must still count trials: %+v", family)
	}
}

func TestHiddenVariantDiversityWithoutLeakage(t *testing.T) {
	trials := []Trial{
		trial(ValidityValid, ResultPass, "alpha", 1),
		trial(ValidityValid, ResultPass, "alpha", 2),
		trial(ValidityValid, ResultFail, "beta", 3),
		trial(ValidityValid, ResultPass, "gamma", 4),
		trial(ValidityValid, ResultPass, "delta", 5),
	}
	task := BuildTaskReliability(DefaultPolicy(), "s1", "a1", trials, nil)
	if task.Diversity.DistinctVariants != 4 {
		t.Fatalf("distinct variants must be 4: %+v", task.Diversity)
	}
	// 2/5 from alpha: dominant share 0.4.
	if !approx(task.Diversity.DominantVariantShare, 0.4) {
		t.Fatalf("dominant share must be 0.4: %+v", task.Diversity)
	}
	dominated := []Trial{
		trial(ValidityValid, ResultPass, "only", 1),
		trial(ValidityValid, ResultPass, "only", 2),
		trial(ValidityValid, ResultPass, "only", 3),
		trial(ValidityValid, ResultPass, "only", 4),
		trial(ValidityValid, ResultPass, "only", 5),
		trial(ValidityValid, ResultPass, "only", 6),
		trial(ValidityValid, ResultPass, "only", 7),
		trial(ValidityValid, ResultPass, "only", 8),
		trial(ValidityValid, ResultPass, "only", 9),
		trial(ValidityValid, ResultFail, "other", 10),
	}
	dominatedTask := BuildTaskReliability(DefaultPolicy(), "s1", "a1", dominated, nil)
	if !approx(dominatedTask.Diversity.DominantVariantShare, 0.9) {
		t.Fatalf("dominance signal must be 0.9: %+v", dominatedTask.Diversity)
	}
}

func TestConsecutiveSuccess(t *testing.T) {
	trials := []Trial{
		trial(ValidityValid, ResultPass, "v1", 1),
		trial(ValidityValid, ResultPass, "v2", 2),
		trial(ValidityValid, ResultFail, "v3", 3),
		trial(ValidityValid, ResultPass, "v4", 4),
		trial(ValidityValid, ResultPass, "v5", 5),
		trial(ValidityValid, ResultPass, "v6", 6),
	}
	max, latest := ConsecutiveSuccess(trials)
	if max != 3 || latest != 3 {
		t.Fatalf("max/latest must be 3/3, got %d/%d", max, latest)
	}
	broken := append(trials, trial(ValidityValid, ResultFail, "v7", 7))
	max, latest = ConsecutiveSuccess(broken)
	if max != 3 || latest != 0 {
		t.Fatalf("broken streak must be 3/0, got %d/%d", max, latest)
	}
}

func TestCapabilityUsesRequiredFamilies(t *testing.T) {
	policy := DefaultPolicy()
	trials := []Trial{}
	for i := 1; i <= 6; i++ {
		item := trial(ValidityValid, ResultPass, "v1", i)
		item.ScenarioVersionID = "scenario-traffic"
		trials = append(trials, item)
	}
	capabilities := BuildCapabilityReliabilities(policy, []string{"kubernetes.capacity.diagnosis"}, trials)
	if len(capabilities) != 1 {
		t.Fatalf("expected one capability, got %d", len(capabilities))
	}
	capability := capabilities[0]
	if len(capability.RequiredFamilies) != 2 {
		t.Fatalf("must use coverage required families: %+v", capability)
	}
	if capability.ReliabilityState == "qualified" || capability.ReliabilityState == "not_qualified" {
		t.Fatalf("capability reliability must never qualify: %q", capability.ReliabilityState)
	}
	// Only one required family evidenced: limited at best.
	if capability.ReliabilityState == CapabilityMeasured {
		t.Fatalf("single-family evidence must not measure a two-family capability: %+v", capability)
	}
}

func TestContrastEvidencePreserved(t *testing.T) {
	policy := DefaultPolicy()
	trials := []Trial{
		trial(ValidityValid, ResultPass, "v1", 1),
		trial(ValidityValid, ResultFail, "v2", 2),
	}
	trials[1].CaseType = "negative"
	capabilities := BuildCapabilityReliabilities(policy, []string{"kubernetes.capacity.remediation"}, trials)
	if capabilities[0].ContrastTrials != 1 {
		t.Fatalf("contrast trials must be preserved: %+v", capabilities[0])
	}
	if capabilities[0].ContrastPassRate == nil || !approx(*capabilities[0].ContrastPassRate, 0) {
		t.Fatalf("contrast pass rate must be 0: %+v", capabilities[0])
	}
}

func TestSaturationNotPassRateBased(t *testing.T) {
	// Saturated hard task: 10 stable failures across variants/executions.
	hard := []Trial{}
	for i := 1; i <= 10; i++ {
		item := trial(ValidityValid, ResultFail, "v1", i)
		if i%2 == 0 {
			item.VariantID = "v2"
		}
		item.SuiteExecutionID = "exec-a"
		if i > 5 {
			item.SuiteExecutionID = "exec-b"
		}
		hard = append(hard, item)
	}
	saturated := BuildSaturation("s-hard", hard, 2)
	if saturated.State != SaturationSaturated {
		t.Fatalf("stable repeated failures must saturate: %+v", saturated)
	}
	// Fresh easy task: 2 passes are not saturation despite 100%% pass rate.
	easy := []Trial{trial(ValidityValid, ResultPass, "v1", 1), trial(ValidityValid, ResultPass, "v1", 2)}
	easy[0].SuiteExecutionID, easy[1].SuiteExecutionID = "exec-a", "exec-a"
	fresh := BuildSaturation("s-easy", easy, 3)
	if fresh.State != SaturationFresh {
		t.Fatalf("thin evidence must stay fresh regardless of pass rate: %+v", fresh)
	}
	// Determinism: same input twice yields the same state.
	again := BuildSaturation("s-hard", hard, 2)
	if again.State != saturated.State {
		t.Fatalf("saturation must be deterministic")
	}
}

func TestComparabilityRejectsMismatch(t *testing.T) {
	base := []Trial{trial(ValidityValid, ResultPass, "v1", 1)}
	candidate := []Trial{trial(ValidityValid, ResultPass, "v1", 1)}
	candidate[0].EvaluatorVersion = "deterministic-evaluator:v2"
	delta := Compare(DefaultPolicy(), "scenario:s1", base, candidate)
	if delta.Comparability.Comparable {
		t.Fatal("evaluator mismatch must be not_comparable")
	}
	found := false
	for _, reason := range delta.Comparability.Reasons {
		if reason == NotComparableEvaluatorContract {
			found = true
		}
	}
	if !found {
		t.Fatalf("must name evaluator mismatch: %+v", delta.Comparability)
	}
	if delta.PassRateDelta != nil {
		t.Fatal("no deltas may be claimed for incompatible samples")
	}
}

func TestComparisonDelta(t *testing.T) {
	policy := DefaultPolicy()
	base := []Trial{
		trial(ValidityValid, ResultPass, "v1", 1),
		trial(ValidityValid, ResultFail, "v2", 2),
		trial(ValidityValid, ResultFail, "v3", 3),
		trial(ValidityValid, ResultFail, "v4", 4),
	}
	candidate := []Trial{
		trial(ValidityValid, ResultPass, "v1", 1),
		trial(ValidityValid, ResultPass, "v2", 2),
		trial(ValidityValid, ResultPass, "v3", 3),
		trial(ValidityValid, ResultFail, "v4", 4),
	}
	delta := Compare(policy, "scenario:s1", base, candidate)
	if !delta.Comparability.Comparable {
		t.Fatalf("identical pins must compare: %+v", delta.Comparability)
	}
	if delta.PassRateDelta == nil || !approx(*delta.PassRateDelta, 50) {
		t.Fatalf("pass-rate delta must be +50: %+v", delta)
	}
	if delta.ValidTrialsDelta != 0 {
		t.Fatalf("valid sample delta must be 0: %+v", delta)
	}
}

func TestPlatformInvalidRateSeparate(t *testing.T) {
	trials := []Trial{
		trial(ValidityValid, ResultPass, "v1", 1),
		trial(ValidityInvalidPlatform, "", "v1", 2),
	}
	summary := Summarize(trials)
	if summary.PassRate == nil || !approx(*summary.PassRate, 100) {
		t.Fatalf("agent rate must exclude platform-invalid: %+v", summary)
	}
	if summary.PlatformInvalidRate == nil || !approx(*summary.PlatformInvalidRate, 50) {
		t.Fatalf("platform rate must be separate: %+v", summary)
	}
}

func TestNoLLMDeterministic(t *testing.T) {
	trials := []Trial{
		trial(ValidityValid, ResultPass, "v1", 1),
		trial(ValidityValid, ResultFail, "v2", 2),
		trial(ValidityInvalidPlatform, "", "v3", 3),
	}
	first := BuildTaskReliability(DefaultPolicy(), "s1", "a1", trials, nil)
	second := BuildTaskReliability(DefaultPolicy(), "s1", "a1", trials, nil)
	if !reflect.DeepEqual(first, second) {
		t.Fatal("reliability must be a deterministic function of trial evidence")
	}
}
