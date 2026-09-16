package coverage

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestTaskFamilyIDsUnique(t *testing.T) {
	families := ListTaskFamilies()
	if len(families) < 15 {
		t.Fatalf("V1 catalog must structure kubernetes/observability/gitops/delivery/safety families, got %d", len(families))
	}
	seen := map[string]bool{}
	for _, family := range families {
		if family.ID == "" || family.Domain == "" || family.Label == "" {
			t.Fatalf("family missing fields: %+v", family)
		}
		if strings.Contains(family.ID, " ") {
			t.Fatalf("family ID must be a stable dotted ID, got %q", family.ID)
		}
		if family.ContractVersion != TaskFamilyContractVersion {
			t.Fatalf("family %q contract=%q", family.ID, family.ContractVersion)
		}
		if seen[family.ID] {
			t.Fatalf("duplicate family %q", family.ID)
		}
		seen[family.ID] = true
	}
	for _, required := range []string{
		"kubernetes.workload.baseline",
		"kubernetes.capacity.traffic_spike",
		"kubernetes.capacity.resource_pressure",
		"kubernetes.deployment.recovery",
		"kubernetes.networking.dependency",
		"observability.metrics.diagnosis",
		"observability.telemetry.missing_data",
		"observability.recovery.verification",
		"gitops.drift.deployment",
		"gitops.desired_state.remediation",
		"gitops.argocd.recovery",
		"delivery.rollout.recovery",
		"delivery.progressive.error_regression",
		"safety.boundary.adherence",
		"safety.protected_resource.avoidance",
		"safety.stop_after_recovery",
	} {
		if !seen[required] {
			t.Fatalf("missing required V1 family %q", required)
		}
	}
}

func TestUnknownTaskFamilyRejected(t *testing.T) {
	if _, err := ValidateTaskFamilyID("kubernetes.capacity.node_failure"); err == nil {
		t.Fatal("unknown task family must be rejected")
	}
	if _, err := ValidateTaskFamilyID(""); err == nil {
		t.Fatal("blank task family must be rejected")
	}
	if _, err := ValidateTaskFamilyID("kubernetes.capacity.traffic_spike"); err != nil {
		t.Fatalf("known family rejected: %v", err)
	}
}

func TestCaseTypeValidation(t *testing.T) {
	for _, valid := range []string{"positive", "negative", "decoy", "adversarial", "edge"} {
		if _, err := ValidateCaseType(valid); err != nil {
			t.Fatalf("valid case type %q rejected: %v", valid, err)
		}
	}
	for _, invalid := range []string{"", "POSITIVE", "unit-test", "traffic_spike"} {
		if _, err := ValidateCaseType(invalid); err == nil {
			t.Fatalf("invalid case type %q accepted", invalid)
		}
	}
}

func TestCoverageContractVersions(t *testing.T) {
	if TaskFamilyContractVersion != "task-family-contract:v1" {
		t.Fatalf("task family contract=%q", TaskFamilyContractVersion)
	}
	if CoverageContractVersion != "capability-coverage-contract:v1" {
		t.Fatalf("coverage contract=%q", CoverageContractVersion)
	}
	if CapabilityCatalogVersion != "infrastructure-capability-catalog:v1" {
		t.Fatalf("capability catalog=%q", CapabilityCatalogVersion)
	}
	assessment := Assess("kubernetes.capacity.remediation", nil)
	if assessment.CoverageContractVersion != CoverageContractVersion ||
		assessment.TaskFamilyContractVersion != TaskFamilyContractVersion ||
		assessment.CapabilityCatalogVersion != CapabilityCatalogVersion {
		t.Fatalf("historical assessment must retain exact contract versions: %+v", assessment)
	}
}

func validTrial(family, caseType, variant string) TrialEvidence {
	return TrialEvidence{
		RunID: "run-" + family + "-" + caseType + "-" + variant, ScenarioVersionID: "sv-" + family,
		FamilyID: family, CaseType: caseType, Validity: TrialValidityValid,
		EvaluationResult: "pass", VariantID: variant,
	}
}

func TestClaimWithNoEvidenceIsUntested(t *testing.T) {
	assessment := Assess("kubernetes.capacity.remediation", nil)
	if assessment.TestStatus != TestStatusUntested {
		t.Fatalf("status=%q", assessment.TestStatus)
	}
	if !hasCode(assessment, NoValidEvidence) {
		t.Fatalf("missing no_valid_evidence: %+v", assessment.MissingRequirements)
	}
}

func TestSomeEvidenceIsPartial(t *testing.T) {
	trials := []TrialEvidence{
		validTrial("kubernetes.capacity.traffic_spike", CaseTypePositive, "v1"),
		validTrial("kubernetes.capacity.traffic_spike", CaseTypePositive, "v1"),
	}
	assessment := Assess("kubernetes.capacity.remediation", trials)
	if assessment.TestStatus != TestStatusPartial {
		t.Fatalf("status=%q %+v", assessment.TestStatus, assessment.MissingRequirements)
	}
	if !hasCode(assessment, MissingRequiredFamily) {
		t.Fatalf("one family must not satisfy two-family breadth: %+v", assessment.MissingRequirements)
	}
}

func TestAllRequirementsMetIsTested(t *testing.T) {
	trials := []TrialEvidence{
		validTrial("kubernetes.capacity.traffic_spike", CaseTypePositive, "v1"),
		validTrial("kubernetes.capacity.traffic_spike", CaseTypePositive, "v2"),
		{RunID: "r3", ScenarioVersionID: "sv-b", FamilyID: "kubernetes.capacity.resource_pressure", CaseType: CaseTypeDecoy, Validity: TrialValidityValid, EvaluationResult: "fail", VariantID: "v1"},
		{RunID: "r4", ScenarioVersionID: "sv-b", FamilyID: "kubernetes.capacity.resource_pressure", CaseType: CaseTypeDecoy, Validity: TrialValidityValid, EvaluationResult: "fail", VariantID: "v2"},
	}
	assessment := Assess("kubernetes.capacity.remediation", trials)
	if assessment.TestStatus != TestStatusTested {
		t.Fatalf("status=%q missing=%+v", assessment.TestStatus, assessment.MissingRequirements)
	}
	if len(assessment.MissingRequirements) != 0 {
		t.Fatalf("tested must have no missing requirements: %+v", assessment.MissingRequirements)
	}
	if assessment.RequiredFamilyCount != 2 || assessment.CoveredFamilyCount != 2 {
		t.Fatalf("family counts wrong: %+v", assessment)
	}
	if assessment.ValidTrialCount != 4 {
		t.Fatalf("valid trials=%d", assessment.ValidTrialCount)
	}
}

func TestAgentFailureStillCountsAsValidCoverage(t *testing.T) {
	// TESTED asks "did we validly test?", not "did the agent pass?".
	trials := []TrialEvidence{
		{RunID: "r1", ScenarioVersionID: "sv-a", FamilyID: "kubernetes.capacity.traffic_spike", CaseType: CaseTypePositive, Validity: TrialValidityValid, EvaluationResult: "fail", VariantID: "v1"},
		{RunID: "r2", ScenarioVersionID: "sv-a", FamilyID: "kubernetes.capacity.traffic_spike", CaseType: CaseTypePositive, Validity: TrialValidityValid, EvaluationResult: "fail", VariantID: "v2"},
		{RunID: "r3", ScenarioVersionID: "sv-b", FamilyID: "kubernetes.capacity.resource_pressure", CaseType: CaseTypeNegative, Validity: TrialValidityValid, EvaluationResult: "fail", VariantID: "v1"},
		{RunID: "r4", ScenarioVersionID: "sv-b", FamilyID: "kubernetes.capacity.resource_pressure", CaseType: CaseTypeNegative, Validity: TrialValidityValid, EvaluationResult: "inconclusive", VariantID: "v2"},
	}
	assessment := Assess("kubernetes.capacity.remediation", trials)
	if assessment.TestStatus != TestStatusTested {
		t.Fatalf("agent failure must still count as valid coverage: %+v", assessment.MissingRequirements)
	}
}

func TestInvalidPlatformDoesNotCount(t *testing.T) {
	trials := []TrialEvidence{
		validTrial("kubernetes.capacity.traffic_spike", CaseTypePositive, "v1"),
		validTrial("kubernetes.capacity.traffic_spike", CaseTypePositive, "v2"),
		{RunID: "bad1", ScenarioVersionID: "sv-b", FamilyID: "kubernetes.capacity.resource_pressure", CaseType: CaseTypeNegative, Validity: TrialValidityInvalidPlatform, VariantID: "v1"},
		{RunID: "bad2", ScenarioVersionID: "sv-b", FamilyID: "kubernetes.capacity.resource_pressure", CaseType: CaseTypeNegative, Validity: TrialValidityInvalidPlatform, VariantID: "v2"},
	}
	assessment := Assess("kubernetes.capacity.remediation", trials)
	if assessment.TestStatus == TestStatusTested {
		t.Fatal("platform-invalid execution must never satisfy coverage")
	}
	if assessment.TestStatus != TestStatusPartial {
		t.Fatalf("status=%q", assessment.TestStatus)
	}
}

func TestPlatformInvalidOnlyNeverBecomesTested(t *testing.T) {
	trials := []TrialEvidence{
		{RunID: "bad1", ScenarioVersionID: "sv-a", FamilyID: "kubernetes.capacity.traffic_spike", CaseType: CaseTypePositive, Validity: TrialValidityInvalidPlatform, VariantID: "v1"},
		{RunID: "bad2", ScenarioVersionID: "sv-b", FamilyID: "kubernetes.capacity.resource_pressure", CaseType: CaseTypeNegative, Validity: TrialValidityInvalidPlatform, VariantID: "v1"},
	}
	assessment := Assess("kubernetes.capacity.remediation", trials)
	if assessment.TestStatus != TestStatusUntested {
		t.Fatalf("platform-invalid-only must be untested, got %q", assessment.TestStatus)
	}
	if !hasCode(assessment, PlatformInvalidOnly) {
		t.Fatalf("missing platform_invalid_only: %+v", assessment.MissingRequirements)
	}
}

func TestSameFamilyRepeatedDoesNotSatisfyBreadth(t *testing.T) {
	trials := []TrialEvidence{}
	for i := 0; i < 10; i++ {
		trials = append(trials, TrialEvidence{
			RunID: "r", ScenarioVersionID: "sv-a", FamilyID: "kubernetes.capacity.traffic_spike",
			CaseType: CaseTypePositive, Validity: TrialValidityValid, EvaluationResult: "pass",
			VariantID: "variant",
		})
	}
	assessment := Assess("kubernetes.capacity.remediation", trials)
	if assessment.TestStatus == TestStatusTested {
		t.Fatal("ten trials of one family must not equal two independent families")
	}
	if assessment.ValidTrialCount != 10 {
		t.Fatalf("valid trials=%d", assessment.ValidTrialCount)
	}
	if assessment.CoveredFamilyCount != 1 {
		t.Fatalf("covered families=%d", assessment.CoveredFamilyCount)
	}
}

func TestHiddenVariantsCountDepthNotBreadth(t *testing.T) {
	trials := []TrialEvidence{
		validTrial("kubernetes.capacity.traffic_spike", CaseTypePositive, "variant-a"),
		validTrial("kubernetes.capacity.traffic_spike", CaseTypePositive, "variant-b"),
		validTrial("kubernetes.capacity.traffic_spike", CaseTypePositive, "variant-c"),
		validTrial("kubernetes.capacity.traffic_spike", CaseTypePositive, "variant-d"),
	}
	assessment := Assess("kubernetes.capacity.remediation", trials)
	if assessment.TestStatus == TestStatusTested {
		t.Fatal("hidden variants in one family must improve depth, not family breadth")
	}
	if assessment.DistinctVariantCount != 4 {
		t.Fatalf("distinct variants=%d", assessment.DistinctVariantCount)
	}
	if assessment.CoveredFamilyCount != 1 {
		t.Fatalf("covered families=%d", assessment.CoveredFamilyCount)
	}
	// Depth is exposed per family without leaking hidden values.
	found := false
	for _, family := range assessment.Families {
		if family.FamilyID == "kubernetes.capacity.traffic_spike" && family.DistinctVariants == 4 && family.ValidTrials == 4 {
			found = true
		}
	}
	if !found {
		t.Fatalf("family depth missing: %+v", assessment.Families)
	}
}

func TestContrastCaseEnforcedWhereConfigured(t *testing.T) {
	// remediation requires positive + one negative/decoy/adversarial.
	positiveOnly := []TrialEvidence{
		validTrial("kubernetes.capacity.traffic_spike", CaseTypePositive, "v1"),
		validTrial("kubernetes.capacity.traffic_spike", CaseTypePositive, "v2"),
		validTrial("kubernetes.capacity.resource_pressure", CaseTypePositive, "v1"),
		validTrial("kubernetes.capacity.resource_pressure", CaseTypePositive, "v2"),
	}
	assessment := Assess("kubernetes.capacity.remediation", positiveOnly)
	if assessment.TestStatus == TestStatusTested {
		t.Fatal("missing contrast case must block TESTED")
	}
	if !hasCode(assessment, MissingRequiredCaseType) {
		t.Fatalf("missing missing_required_case_type: %+v", assessment.MissingRequirements)
	}
}

func TestCapabilityWithNoRequirementIsPartialUnavailable(t *testing.T) {
	// delivery.progressive_delivery.recovery has no published V1 requirement.
	assessment := Assess("delivery.progressive_delivery.recovery", []TrialEvidence{
		validTrial("delivery.progressive.error_regression", CaseTypePositive, "v1"),
	})
	if assessment.TestStatus != TestStatusPartial {
		t.Fatalf("absence of requirements must not become TESTED, got %q", assessment.TestStatus)
	}
	if !hasCode(assessment, CoverageRequirementUnavailable) {
		t.Fatalf("missing coverage_requirement_unavailable: %+v", assessment.MissingRequirements)
	}
}

func TestTestedNeverImpliesQualified(t *testing.T) {
	trials := []TrialEvidence{
		validTrial("kubernetes.capacity.traffic_spike", CaseTypePositive, "v1"),
		validTrial("kubernetes.capacity.traffic_spike", CaseTypePositive, "v2"),
		{RunID: "r3", ScenarioVersionID: "sv-b", FamilyID: "kubernetes.capacity.resource_pressure", CaseType: CaseTypeAdversarial, Validity: TrialValidityValid, EvaluationResult: "pass", VariantID: "v1"},
		{RunID: "r4", ScenarioVersionID: "sv-b", FamilyID: "kubernetes.capacity.resource_pressure", CaseType: CaseTypeAdversarial, Validity: TrialValidityValid, EvaluationResult: "pass", VariantID: "v2"},
	}
	assessment := Assess("kubernetes.capacity.remediation", trials)
	if assessment.TestStatus != TestStatusTested {
		t.Fatalf("expected tested, got %q", assessment.TestStatus)
	}
	// TESTED is coverage only: the serialized assessment must never carry
	// qualified / not_qualified states (those belong to qualification policy).
	raw, err := json.Marshal(assessment)
	if err != nil {
		t.Fatal(err)
	}
	body := string(raw)
	if strings.Contains(body, "qualified") || strings.Contains(body, "not_qualified") {
		t.Fatalf("coverage assessment must not infer qualification: %s", body)
	}
}

func hasCode(assessment Assessment, code string) bool {
	for _, missing := range assessment.MissingRequirements {
		if missing.Code == code {
			return true
		}
	}
	return false
}
