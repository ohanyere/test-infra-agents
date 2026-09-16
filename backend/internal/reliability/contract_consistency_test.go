package reliability

import (
	"reflect"
	"testing"

	"github.com/evalplane/evalplane/backend/internal/coverage"
)

func semanticTrial(envRef, profile, provider, isolation string, ordinal int, result string) Trial {
	item := trial(ValidityValid, result, "v1", ordinal)
	item.EnvironmentRef = envRef
	item.EnvironmentProfileID = profile
	item.EnvironmentProfileContract = "environment-profile-catalog:v1"
	item.ProviderID = provider
	item.ProviderContract = "environment-provider-contract:v1"
	item.IsolationTier = isolation
	return item
}

// 1. reliability report pins coverage contract version.
func TestConsistencyReportPinsCoverageContract(t *testing.T) {
	if CurrentCoverageContractVersion != coverage.CoverageContractV2 {
		t.Fatalf("current coverage contract must be V2, got %q", CurrentCoverageContractVersion)
	}
	policy := DefaultPolicy()
	trials := []Trial{trial(ValidityValid, ResultPass, "v1", 1)}
	caps := BuildCapabilityReliabilities(policy, []string{"kubernetes.capacity.diagnosis"}, trials)
	if len(caps) != 1 {
		t.Fatal("expected one capability")
	}
	if caps[0].CoverageContract != coverage.CoverageContractVersion {
		t.Fatalf("V1 capability must pin v1, got %q", caps[0].CoverageContract)
	}
	gitops := BuildCapabilityReliabilities(policy, []string{"gitops.drift.detection"}, trials)
	if len(gitops) != 1 {
		t.Fatal("expected one gitops capability")
	}
	if gitops[0].CoverageContract != coverage.CoverageContractV2 {
		t.Fatalf("V2 capability must pin v2, got %q", gitops[0].CoverageContract)
	}
}

// 2. V1 coverage semantics remain unchanged.
func TestConsistencyV1SemanticsUnchanged(t *testing.T) {
	if len(coverage.ListRequirements()) != 8 {
		t.Fatalf("V1 must stay at 8 requirements")
	}
	v1, ok := coverage.GetRequirement("kubernetes.capacity.remediation")
	if !ok {
		t.Fatal("V1 remediation requirement missing")
	}
	if v1.ContractVersion != coverage.CoverageContractVersion {
		t.Fatalf("V1 contract mutated: %q", v1.ContractVersion)
	}
	if len(v1.RequiredCaseTypeGroups) != 1 {
		t.Fatalf("V1 contrast group weakened: %+v", v1)
	}
	policy := DefaultPolicy()
	trials := []Trial{trial(ValidityValid, ResultPass, "v1", 1)}
	v1caps := BuildCapabilityReliabilitiesV1(policy, []string{"kubernetes.capacity.diagnosis"}, trials)
	if !v1caps[0].RequirementAvailable || v1caps[0].CoverageContract != CoverageContractV1 {
		t.Fatalf("V1 builder must stay on v1: %+v", v1caps[0])
	}
	// V1 view must not see V2-only capabilities.
	missing := BuildCapabilityReliabilitiesV1(policy, []string{"gitops.drift.detection"}, trials)
	if missing[0].RequirementAvailable {
		t.Fatal("V1 builder must report V2 capability as unavailable")
	}
}

// 3. V2 GitOps/delivery capability requirements are visible to reliability.
func TestConsistencyV2GitOpsVisible(t *testing.T) {
	policy := DefaultPolicy()
	trials := []Trial{trial(ValidityValid, ResultPass, "v1", 1)}
	for _, id := range []string{"gitops.drift.detection", "gitops.argocd.recovery", "delivery.rollout.recovery"} {
		caps := BuildCapabilityReliabilities(policy, []string{id}, trials)
		if !caps[0].RequirementAvailable {
			t.Fatalf("%q must be available under current contract", id)
		}
		if caps[0].CoverageContract != coverage.CoverageContractV2 {
			t.Fatalf("%q must pin v2, got %q", id, caps[0].CoverageContract)
		}
		if len(caps[0].RequiredFamilies) < 2 {
			t.Fatalf("%q lacks family breadth: %+v", id, caps[0])
		}
	}
}

// 4. requirement unavailable remains structured.
func TestConsistencyRequirementUnavailableStructured(t *testing.T) {
	policy := DefaultPolicy()
	trials := []Trial{trial(ValidityValid, ResultPass, "v1", 1)}
	caps := BuildCapabilityReliabilities(policy, []string{"aws.terraform.provisioning"}, trials)
	cap := caps[0]
	if cap.RequirementAvailable {
		t.Fatal("unknown capability must be unavailable")
	}
	if cap.ReliabilityState != CapabilityInsufficient {
		t.Fatalf("unavailable must be insufficient_evidence, got %q", cap.ReliabilityState)
	}
	if len(cap.RequiredFamilies) != 0 || len(cap.CoveredFamilies) != 0 {
		t.Fatalf("unavailable must not fabricate families: %+v", cap)
	}
	if cap.ValidTrialCount != 0 {
		t.Fatalf("unavailable must not count trials: %+v", cap)
	}
	if cap.Summary.Empirical.ValidTrials != 0 || cap.Summary.Empirical.PassRate != nil {
		t.Fatalf("unavailable must not fabricate summary: %+v", cap.Summary)
	}
	if cap.CoverageContract != CurrentCoverageContractVersion {
		t.Fatalf("unavailable must still pin selected contract, got %q", cap.CoverageContract)
	}
}

// 5. same profile/provider/isolation but different instance refs can be comparable.
func TestConsistencySemanticComparableAcrossRefs(t *testing.T) {
	base := []Trial{semanticTrial("env-a", "kubernetes.namespace:v1", "kind-dev:v1", "namespace", 1, ResultPass)}
	candidate := []Trial{semanticTrial("env-b", "kubernetes.namespace:v1", "kind-dev:v1", "namespace", 1, ResultPass)}
	result := CheckComparability(PinsForTrials("scenario:s1", base), PinsForTrials("scenario:s1", candidate))
	if !result.Comparable {
		t.Fatalf("same semantics with different refs must compare: %+v", result)
	}
}

// 6. namespace vs dedicated remains not comparable.
func TestConsistencyIsolationMismatchNotComparable(t *testing.T) {
	base := []Trial{semanticTrial("env-a", "kubernetes.namespace:v1", "kind-dev:v1", "namespace", 1, ResultPass)}
	candidate := []Trial{semanticTrial("env-b", "kubernetes.dedicated:v1", "dedicated-eks:v1", "dedicated_cluster", 1, ResultPass)}
	result := CheckComparability(PinsForTrials("scenario:s1", base), PinsForTrials("scenario:s1", candidate))
	if result.Comparable {
		t.Fatal("namespace vs dedicated must not compare")
	}
	found := false
	for _, reason := range result.Reasons {
		if reason == NotComparableEnvironment {
			found = true
		}
	}
	if !found {
		t.Fatalf("must name environment mismatch: %+v", result)
	}
}

// 7. provider/profile contract mismatch remains not comparable.
func TestConsistencyContractMismatchNotComparable(t *testing.T) {
	base := []Trial{semanticTrial("env-a", "kubernetes.namespace:v1", "kind-dev:v1", "namespace", 1, ResultPass)}
	candidate := []Trial{semanticTrial("env-b", "kubernetes.namespace:v1", "kind-dev:v1", "namespace", 1, ResultPass)}
	candidate[0].ProviderContract = "environment-provider-contract:v2"
	result := CheckComparability(PinsForTrials("scenario:s1", base), PinsForTrials("scenario:s1", candidate))
	if result.Comparable {
		t.Fatal("provider contract drift must not compare")
	}
	mismatch := []Trial{semanticTrial("env-a", "kubernetes.namespace:v1", "kind-dev:v1", "namespace", 1, ResultPass)}
	other := []Trial{semanticTrial("env-b", "kubernetes.virtual_cluster:v1", "kind-dev:v1", "namespace", 1, ResultPass)}
	result = CheckComparability(PinsForTrials("scenario:s1", mismatch), PinsForTrials("scenario:s1", other))
	if result.Comparable {
		t.Fatal("profile mismatch must not compare")
	}
}

// 8. exact environment ref retained as provenance.
func TestConsistencyEnvRefProvenance(t *testing.T) {
	trials := []Trial{
		semanticTrial("env-exact-a", "kubernetes.namespace:v1", "kind-dev:v1", "namespace", 1, ResultPass),
		semanticTrial("env-exact-b", "kubernetes.namespace:v1", "kind-dev:v1", "namespace", 2, ResultFail),
	}
	pins := PinsForTrials("scenario:s1", trials)
	if len(pins.EnvironmentRefs) != 2 || pins.EnvironmentRefs[0] != "env-exact-a" || pins.EnvironmentRefs[1] != "env-exact-b" {
		t.Fatalf("exact refs must be retained: %+v", pins.EnvironmentRefs)
	}
	task := BuildTaskReliability(DefaultPolicy(), "s1", "a1", trials, nil)
	if len(task.Pins.EnvironmentRefs) != 2 {
		t.Fatalf("task pins must retain refs: %+v", task.Pins)
	}
}

// 9. pass-rate denominator explicitly tested (Design B).
func TestConsistencyPassRateDenominator(t *testing.T) {
	trials := []Trial{
		trial(ValidityValid, ResultPass, "v1", 1),
		trial(ValidityValid, ResultFail, "v2", 2),
		trial(ValidityValid, ResultInconclusive, "v3", 3),
		trial(ValidityValid, ResultStopped, "v4", 4),
	}
	summary := Summarize(trials)
	if summary.ValidTrials != 4 || summary.PassedTrials != 1 || summary.FailedTrials != 1 {
		t.Fatalf("counts wrong: %+v", summary)
	}
	if summary.DecisiveTrials != 2 {
		t.Fatalf("decisive must be 2: %+v", summary)
	}
	if summary.PassRate == nil || !approx(*summary.PassRate, 50) {
		t.Fatalf("decisive pass rate must be 50: %+v", summary)
	}
	if summary.DecisivePassRate == nil || !approx(*summary.DecisivePassRate, *summary.PassRate) {
		t.Fatalf("decisive alias must equal pass rate: %+v", summary)
	}
	if summary.ValidSuccessRate == nil || !approx(*summary.ValidSuccessRate, 25) {
		t.Fatalf("valid success rate must be 25: %+v", summary)
	}
}

// 10. valid inconclusive trial behavior explicitly tested.
func TestConsistencyInconclusiveBehavior(t *testing.T) {
	trials := []Trial{
		trial(ValidityValid, ResultPass, "v1", 1),
		trial(ValidityValid, ResultInconclusive, "v2", 2),
		trial(ValidityValid, ResultStopped, "v3", 3),
	}
	summary := Summarize(trials)
	if summary.InconclusiveTrials != 1 || summary.StoppedTrials != 1 {
		t.Fatalf("inconclusive/stopped must be exposed: %+v", summary)
	}
	// K-metrics use n = all valid trials (3), c = passes (1).
	metric := mustK(t, KMetrics(trials), 1)
	if !metric.Available || !approx(*metric.PassAtK, 1.0/3.0) || !approx(*metric.PassToTheK, 1.0/3.0) {
		t.Fatalf("k=1 must use n=3,c=1: %+v", metric)
	}
	families := BuildFamilyReliabilities(DefaultPolicy(), trials)
	if families[0].InconclusiveTrials != 1 || families[0].StoppedTrials != 1 {
		t.Fatalf("family must expose inconclusive/stopped: %+v", families[0])
	}
}

// 11. platform-invalid remains excluded.
func TestConsistencyPlatformInvalidExcluded(t *testing.T) {
	trials := []Trial{
		trial(ValidityValid, ResultPass, "v1", 1),
		trial(ValidityInvalidPlatform, ResultFail, "v2", 2),
	}
	summary := Summarize(trials)
	if summary.ValidTrials != 1 || summary.PlatformInvalidTrials != 1 {
		t.Fatalf("valid/platform split wrong: %+v", summary)
	}
	if summary.DecisiveTrials != 1 {
		t.Fatalf("platform must not enter decisive denominator: %+v", summary)
	}
	if summary.PassRate == nil || !approx(*summary.PassRate, 100) {
		t.Fatalf("agent rate must exclude platform: %+v", summary)
	}
	if summary.ValidSuccessRate == nil || !approx(*summary.ValidSuccessRate, 100) {
		t.Fatalf("valid success rate must exclude platform: %+v", summary)
	}
}

// 12. pass@k/pass^k mathematical vectors unchanged.
func TestConsistencyKVectorUnchanged(t *testing.T) {
	trials := []Trial{
		trial(ValidityValid, ResultPass, "v1", 1),
		trial(ValidityValid, ResultPass, "v2", 2),
		trial(ValidityValid, ResultPass, "v3", 3),
		trial(ValidityValid, ResultFail, "v4", 4),
		trial(ValidityValid, ResultFail, "v5", 5),
	}
	metric := mustK(t, KMetrics(trials), 2)
	if !approx(*metric.PassAtK, 0.9) || !approx(*metric.PassToTheK, 0.3) {
		t.Fatalf("vector n=5,c=3,k=2 changed: %+v", metric)
	}
}

// 13. no historical contract mutation.
func TestConsistencyNoHistoricalMutation(t *testing.T) {
	if ReliabilityContractVersion != "reliability-metrics-contract:v1" {
		t.Fatalf("reliability contract mutated: %q", ReliabilityContractVersion)
	}
	if coverage.CoverageContractVersion != "capability-coverage-contract:v1" {
		t.Fatalf("coverage V1 mutated: %q", coverage.CoverageContractVersion)
	}
	if CoverageContractV1 != "capability-coverage-contract:v1" {
		t.Fatalf("reliability V1 pin mutated: %q", CoverageContractV1)
	}
	old := Summarize([]Trial{trial(ValidityValid, ResultPass, "v1", 1), trial(ValidityValid, ResultFail, "v2", 2)})
	if old.PassRate == nil || !approx(*old.PassRate, 50) {
		t.Fatalf("historical decisive rate mutated: %+v", old)
	}
}

// 14. no LLM logic introduced (pure deterministic functions of evidence).
func TestConsistencyNoLLMDeterministic(t *testing.T) {
	trials := []Trial{
		trial(ValidityValid, ResultPass, "v1", 1),
		trial(ValidityValid, ResultFail, "v2", 2),
		trial(ValidityValid, ResultInconclusive, "v3", 3),
	}
	first := BuildTaskReliability(DefaultPolicy(), "s1", "a1", trials, nil)
	second := BuildTaskReliability(DefaultPolicy(), "s1", "a1", trials, nil)
	if !reflect.DeepEqual(first, second) {
		t.Fatal("reliability must stay a deterministic pure function (no LLM/randomness)")
	}
	firstCaps := BuildCapabilityReliabilities(DefaultPolicy(), []string{"kubernetes.capacity.diagnosis"}, trials)
	secondCaps := BuildCapabilityReliabilities(DefaultPolicy(), []string{"kubernetes.capacity.diagnosis"}, trials)
	if !reflect.DeepEqual(firstCaps, secondCaps) {
		t.Fatal("capability reliability must stay deterministic")
	}
}
