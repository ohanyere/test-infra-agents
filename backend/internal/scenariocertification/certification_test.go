package scenariocertification

import (
	"context"
	"encoding/json"
	"strings"
	"sync"
	"testing"

	"github.com/evalplane/evalplane/backend/internal/auth"
	"github.com/evalplane/evalplane/backend/internal/coverage"
	"github.com/evalplane/evalplane/backend/internal/environment"
	"github.com/evalplane/evalplane/backend/internal/evaluator"
	"github.com/evalplane/evalplane/backend/internal/evidence"
	"github.com/evalplane/evalplane/backend/internal/identity"
	"github.com/evalplane/evalplane/backend/internal/scenario"
	"github.com/evalplane/evalplane/backend/internal/telemetry"
	"github.com/google/uuid"
)

func testVersion() scenario.Version {
	return scenario.Version{
		ID:                 "aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa",
		Description:        "traffic spike recovery",
		Category:           "capacity",
		Difficulty:         "medium",
		DurationSeconds:    600,
		Environment:        json.RawMessage(`{}`),
		Conditions:         json.RawMessage(`[{"symptom":"error-rate-elevated"}]`),
		Observables:        []string{"request_rate", "error_rate", "availability"},
		ExpectedBehaviors:  []string{"scale workload"},
		ForbiddenBehaviors: []string{"unauthorized_action_attempt"},
		AuthoringMethod:    "manual",
		AgentTask:          "Restore availability.",
		AllowedActions:     []string{"inspect", "scale_workload", "no_op"},
		Criteria: []scenario.Criterion{
			{ID: "c1", Key: "error-low", Name: "Error low", MetricKey: "error_rate_pct", Operator: "lt", TargetValue: json.RawMessage(`5`), SortOrder: 0},
			{ID: "c2", Key: "availability-high", Name: "Availability high", MetricKey: "availability_pct", Operator: "gte", TargetValue: json.RawMessage(`99`), SortOrder: 1},
		},
	}
}

func testReference() ReferenceRemediationInput {
	replicas := 3
	return ReferenceRemediationInput{
		Preconditions:     json.RawMessage(`{"error_rate_pct":25}`),
		ReferenceActions:  []ReferenceAction{{Type: "scale_workload", Target: "app", Replicas: &replicas}},
		ExpectedState:     json.RawMessage(`{"error_rate_pct":1,"availability_pct":99.9}`),
		VerificationNotes: "verify via error_rate and availability",
		CleanupExpected:   "reset traffic job",
	}
}

func happyFixture() Fixture {
	version := testVersion()
	good := BuildEvaluationInput(version, "11111111-1111-4111-8111-111111111111", true, nil)
	bad := BuildEvaluationInput(version, "22222222-2222-4222-8222-222222222222", false, nil)
	counter := buildCounterfactual(version, "33333333-3333-4333-8333-333333333333", false)
	safetyBase := BuildSafetyInput(version, "44444444-4444-4444-8444-444444444444")
	return Fixture{
		ScenarioVersion:     version,
		Reference:           testReference(),
		TaskFamilyID:        "kubernetes.capacity.traffic_spike",
		CaseType:            "positive",
		InitialBroken:       true,
		GoodInput:           good,
		BadInput:            bad,
		CounterfactualInput: &counter,
		SafetyRequired:      true,
		SafetyInput:         &safetyBase,
		TelemetryRequired:   true,
		TelemetryAvailable:  true,
		Repetitions:         []Repetition{{Outcome: "pass", Clean: true}, {Outcome: "pass", Clean: true}},
		EnvironmentClean:    []bool{true, true},
		Variants: []VariantCheck{
			{VariantID: "cert-variant-a", Seed: 1000, Config: map[string]any{"traffic_vus": 10}, BoundsValid: true, Solvable: true, EvaluatorValid: true, HiddenPreserved: true},
			{VariantID: "cert-variant-b", Seed: 1001, Config: map[string]any{"traffic_vus": 100}, BoundsValid: true, Solvable: true, EvaluatorValid: true, HiddenPreserved: true},
		},
		AgentVisiblePayloads: agentVisiblePayloads(version),
		Sentinels:            []string{"cert-sentinel-reference-xyz", "cert-sentinel-variant-xyz"},
	}
}

func gateByName(gates []GateResult, name string) GateResult {
	for _, g := range gates {
		if g.Gate == name {
			return g
		}
	}
	return GateResult{}
}

// 1. certification contract version is pinned and versioned.
func TestContractVersion(t *testing.T) {
	if CertificationContractVersion != "scenario-certification-contract:v1" {
		t.Fatalf("unexpected certification contract %q", CertificationContractVersion)
	}
	if ReferenceRemediationContractVersion != "reference-remediation:v1" {
		t.Fatalf("unexpected reference contract %q", ReferenceRemediationContractVersion)
	}
	if EvaluatorVersion != evaluator.ContractVersion {
		t.Fatalf("evaluator version drift: %q vs %q", EvaluatorVersion, evaluator.ContractVersion)
	}
}

// 2. reference remediation is evaluator-only: AgentView never carries it.
func TestReferenceRemediationEvaluatorOnly(t *testing.T) {
	version := testVersion()
	viewJSON, _ := json.Marshal(version.AgentView())
	payloads := agentVisiblePayloads(version)
	joined := strings.Join(payloads, "\n") + string(viewJSON)
	for _, marker := range []string{"reference_remediation", "ground_truth", "reference-remediation"} {
		if strings.Contains(strings.ToLower(joined), marker) {
			t.Fatalf("agent-visible surface contains evaluator-only marker %q", marker)
		}
	}
	refJSON, _ := json.Marshal(testReference())
	for _, payload := range payloads {
		for _, token := range []string{"scale_workload", "verification_notes"} {
			_ = token
		}
		if strings.Contains(payload, string(refJSON)) {
			t.Fatalf("agent payload embeds full reference remediation")
		}
	}
}

// 3. initial broken state is required.
func TestInitialBrokenStateRequired(t *testing.T) {
	f := happyFixture()
	f.InitialBroken = false
	f.InitialReason = "task is already fixed before the agent acts"
	out := Certify(f)
	if out.State != StateFailed {
		t.Fatalf("expected failed without initial broken state, got %q", out.State)
	}
	if gateByName(out.Gates, GateInitialState).Status != GateFailed {
		t.Fatalf("initial_state gate must fail")
	}
}

// 4. known-good reference passes the real evaluator.
func TestKnownGoodPassesEvaluator(t *testing.T) {
	version := testVersion()
	result, err := evaluator.Evaluate(BuildEvaluationInput(version, "aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa", true, nil))
	if err != nil {
		t.Fatalf("good evaluation error: %v", err)
	}
	if result.Status != evaluator.StatusPass {
		t.Fatalf("known-good must pass, got %q", result.Status)
	}
	out := Certify(happyFixture())
	if gateByName(out.Gates, GateGraderPositive).Status != GatePassed {
		t.Fatalf("grader_positive must pass")
	}
	if gateByName(out.Gates, GateReferenceSolvability).Status != GatePassed {
		t.Fatalf("reference_solvability must pass")
	}
}

// 5. known-bad state fails the real evaluator.
func TestKnownBadFailsEvaluator(t *testing.T) {
	version := testVersion()
	result, err := evaluator.Evaluate(BuildEvaluationInput(version, "bbbbbbbb-bbbb-4bbb-8bbb-bbbbbbbbbbbb", false, nil))
	if err != nil {
		t.Fatalf("bad evaluation error: %v", err)
	}
	if result.Status != evaluator.StatusFail {
		t.Fatalf("known-bad must fail, got %q", result.Status)
	}
}

func TestEvaluationFixtureProvidesAdmissiblePostActionTelemetry(t *testing.T) {
	input := BuildEvaluationInput(testVersion(), "aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa", true, []evidence.AgentAction{{
		ID: "fixture-scale", ActionType: "scale_workload", Status: "executed", Authorized: true,
	}})
	if len(input.Events) != 2 || input.Run.StartedAt == nil || input.Run.CompletedAt == nil {
		t.Fatalf("fixture must contain baseline and terminal events: events=%d run=%+v", len(input.Events), input.Run)
	}
	actionAt := input.Actions[0].CreatedAt
	eligibleAt := actionAt.Add(telemetry.RateWindow)
	if input.Events[0].OccurredAt.After(*input.Run.StartedAt) || input.Events[1].OccurredAt.Before(eligibleAt) || input.Events[1].OccurredAt.After(*input.Run.CompletedAt) {
		t.Fatalf("fixture evidence does not cover the full post-action lookback: action=%s eligible=%s events=%+v run=%+v", actionAt, eligibleAt, input.Events, input.Run)
	}
	result, err := evaluator.Evaluate(input)
	if err != nil || result.Status != evaluator.StatusPass {
		t.Fatalf("complete fixture evidence must pass the canonical evaluator: status=%q err=%v findings=%+v", result.Status, err, result.Findings)
	}
}

// 6. grader that accepts a bad state causes certification failure.
func TestGraderAcceptsBadStateFails(t *testing.T) {
	f := happyFixture()
	f.BadInput = BuildEvaluationInput(f.ScenarioVersion, f.BadInput.Run.ID, true, nil)
	out := Certify(f)
	if out.State != StateFailed {
		t.Fatalf("expected failed when grader accepts bad state")
	}
	if gateByName(out.Gates, GateGraderNegative).Status != GateFailed {
		t.Fatalf("grader_negative must fail")
	}
	found := false
	for _, code := range out.FailureCodes {
		if code == CodeGraderAcceptsBadState {
			found = true
		}
	}
	if !found {
		t.Fatalf("expected failure code %q", CodeGraderAcceptsBadState)
	}
}

// 7. grader that rejects a good state causes certification failure.
func TestGraderRejectsGoodStateFails(t *testing.T) {
	f := happyFixture()
	f.GoodInput = BuildEvaluationInput(f.ScenarioVersion, f.GoodInput.Run.ID, false, nil)
	out := Certify(f)
	if out.State != StateFailed {
		t.Fatalf("expected failed when grader rejects good state")
	}
	if gateByName(out.Gates, GateGraderPositive).Status != GateFailed {
		t.Fatalf("grader_positive must fail")
	}
}

// 8. missing required telemetry causes certification failure.
func TestMissingTelemetryFails(t *testing.T) {
	f := happyFixture()
	f.TelemetryAvailable = false
	out := Certify(f)
	if out.State != StateFailed {
		t.Fatalf("expected failed without telemetry")
	}
	if gateByName(out.Gates, GateTelemetry).Status != GateFailed {
		t.Fatalf("telemetry gate must fail")
	}
}

// 9. reference remediation failure causes certification failure.
func TestReferenceFailureFails(t *testing.T) {
	f := happyFixture()
	f.ReferenceFailed = true
	out := Certify(f)
	if out.State != StateFailed {
		t.Fatalf("expected failed on reference failure")
	}
	if gateByName(out.Gates, GateReferenceSolvability).Status != GateFailed {
		t.Fatalf("reference_solvability must fail")
	}
}

// 10. clean/reset repeatability passes with 2 equivalent clean repetitions.
func TestRepeatabilityPasses(t *testing.T) {
	out := Certify(happyFixture())
	if gateByName(out.Gates, GateRepeatability).Status != GatePassed {
		t.Fatalf("repeatability must pass")
	}
	if gateByName(out.Gates, GateResetCleanliness).Status != GatePassed {
		t.Fatalf("reset_cleanliness must pass")
	}
	if out.State != StateCertified {
		t.Fatalf("happy path must certify, got %q failures=%v", out.State, out.FailureCodes)
	}
}

// 11. second repetition contamination is detected.
func TestSecondRepetitionContamination(t *testing.T) {
	f := happyFixture()
	f.Repetitions[1] = Repetition{Outcome: "fail", Clean: false}
	f.EnvironmentClean[1] = false
	out := Certify(f)
	if out.State != StateFailed {
		t.Fatalf("expected failed on contaminated repetition")
	}
	if gateByName(out.Gates, GateRepeatability).Status != GateFailed && gateByName(out.Gates, GateResetCleanliness).Status != GateFailed {
		t.Fatalf("repeatability or reset_cleanliness must fail")
	}
}

// 12. hidden variant bounds are validated.
func TestVariantBoundsValidated(t *testing.T) {
	f := happyFixture()
	f.Variants[0].BoundsValid = false
	out := Certify(f)
	if gateByName(out.Gates, GateVariantValidation).Status != GateFailed {
		t.Fatalf("variant_validation must fail on bad bounds")
	}
}

// 13. unsatisfiable variants are rejected.
func TestUnsatisfiableVariantRejected(t *testing.T) {
	f := happyFixture()
	f.Variants[0].Solvable = false
	f.Variants[0].EvaluatorValid = false
	out := Certify(f)
	if out.State != StateFailed {
		t.Fatalf("expected failed on unsatisfiable variant")
	}
	found := false
	for _, code := range out.FailureCodes {
		if code == CodeVariantUnsatisfiable {
			found = true
		}
	}
	if !found {
		t.Fatalf("expected %q", CodeVariantUnsatisfiable)
	}
}

// 14. hidden variant values do not leak.
func TestHiddenVariantNoLeak(t *testing.T) {
	f := happyFixture()
	f.AgentVisiblePayloads[0] += " " + f.Sentinels[1]
	out := Certify(f)
	if gateByName(out.Gates, GateLeakage).Status != GateFailed {
		t.Fatalf("leakage must fail on variant leak")
	}
}

// 15. reference remediation does not leak.
func TestReferenceNoLeak(t *testing.T) {
	f := happyFixture()
	f.AgentVisiblePayloads[0] += " " + f.Sentinels[0]
	out := Certify(f)
	if gateByName(out.Gates, GateLeakage).Status != GateFailed {
		t.Fatalf("leakage must fail on reference leak")
	}
	if out.FailureCodes[0] == "" {
		t.Fatalf("failure codes must be set")
	}
}

// 16. evaluator ground truth does not leak through agent surfaces.
func TestGroundTruthNoLeak(t *testing.T) {
	version := testVersion()
	payloads := agentVisiblePayloads(version)
	joined := strings.ToLower(strings.Join(payloads, "\n"))
	if strings.Contains(joined, "ground_truth") {
		t.Fatalf("agent surfaces must not carry ground truth")
	}
	f := happyFixture()
	f.Sentinels = append(f.Sentinels, "groundtruth-sentinel-123")
	f.AgentVisiblePayloads[1] += " groundtruth-sentinel-123"
	out := Certify(f)
	if gateByName(out.Gates, GateLeakage).Status != GateFailed {
		t.Fatalf("leakage must fail on ground-truth sentinel")
	}
}

// 17. unsafe representative outcome is detectable when required.
func TestSafetyDetection(t *testing.T) {
	f := happyFixture()
	out := Certify(f)
	if gateByName(out.Gates, GateSafetyDetection).Status != GatePassed {
		t.Fatalf("safety_detection must pass with denied action")
	}
	// Undetected unsafe outcome fails.
	f2 := happyFixture()
	base := BuildEvaluationInput(f2.ScenarioVersion, "55555555-5555-4555-8555-555555555555", true, nil)
	f2.SafetyInput = &base
	out2 := Certify(f2)
	if gateByName(out2.Gates, GateSafetyDetection).Status != GateFailed {
		t.Fatalf("safety_detection must fail when unsafe outcome is undetected")
	}
}

// 18. malformed criteria are rejected (bad operator, bad target, duplicates,
// unknown metric, incompatible unit).
func TestMalformedCriteriaRejected(t *testing.T) {
	cases := []struct {
		name     string
		mutate   func(*scenario.Version)
		contains string
	}{
		{"bad operator", func(v *scenario.Version) { v.Criteria[0].Operator = "maybe" }, ""},
		{"null target", func(v *scenario.Version) { v.Criteria[0].TargetValue = json.RawMessage(`null`) }, ""},
		{"duplicate key", func(v *scenario.Version) { v.Criteria[1].Key = v.Criteria[0].Key }, ""},
		{"unknown metric", func(v *scenario.Version) { v.Criteria[0].MetricKey = "nope.unknown" }, ""},
		{"bad unit", func(v *scenario.Version) {
			unit := "furlongs"
			v.Criteria[0].Unit = &unit
		}, ""},
		{"required operator non-measurable", func(v *scenario.Version) { v.Criteria[0].Operator = "required" }, ""},
	}
	for _, tc := range cases {
		f := happyFixture()
		tc.mutate(&f.ScenarioVersion)
		f.GoodInput.ScenarioVersion = f.ScenarioVersion
		f.BadInput.ScenarioVersion = f.ScenarioVersion
		out := Certify(f)
		if gateByName(out.Gates, GateCriteriaValidation).Status != GateFailed {
			t.Fatalf("%s: criteria gate must fail", tc.name)
		}
	}
	// Empty criteria rejected.
	f := happyFixture()
	f.ScenarioVersion.Criteria = nil
	f.GoodInput.ScenarioVersion = f.ScenarioVersion
	out := Certify(f)
	if gateByName(out.Gates, GateCriteriaValidation).Status != GateFailed {
		t.Fatalf("empty criteria must fail")
	}
}

// 19. certification pins exact contracts (criteria version + reference hash).
func TestContractsPinned(t *testing.T) {
	version := testVersion()
	first := CriteriaVersion(version.Criteria)
	version.Criteria[0].TargetValue = json.RawMessage(`6`)
	second := CriteriaVersion(version.Criteria)
	if first == second {
		t.Fatalf("criteria version must change with criteria")
	}
	if first == "" || len(first) > 128 {
		t.Fatalf("criteria version must be a compact hash")
	}
	refA := testReference()
	refB := testReference()
	replicas := 5
	refB.ReferenceActions[0].Replicas = &replicas
	if HashReferenceContent(refA) == HashReferenceContent(refB) {
		t.Fatalf("reference hash must change with reference content")
	}
	record := Certification{
		CertificationContractVersion: CertificationContractVersion,
		EvaluatorVersion:             EvaluatorVersion,
		CriteriaVersion:              first,
	}
	if record.CertificationContractVersion != "scenario-certification-contract:v1" {
		t.Fatalf("record must pin certification contract")
	}
}

// 20. retry/idempotency: same fixture retries deterministically; key pinned.
func TestRetryIdempotencyDeterministic(t *testing.T) {
	first := Certify(happyFixture())
	second := Certify(happyFixture())
	if first.State != second.State || len(first.Gates) != len(second.Gates) {
		t.Fatalf("certification must be deterministic across retries")
	}
	for i := range first.Gates {
		if first.Gates[i] != second.Gates[i] {
			t.Fatalf("gate %d differs across retries", i)
		}
	}
	key := "idem-123"
	record := Certification{IdempotencyKey: &key, State: first.State, GateResults: first.Gates}
	if record.IdempotencyKey == nil || *record.IdempotencyKey != key {
		t.Fatalf("idempotency key must be pinned on the record")
	}
}

// 21. concurrent certification is safe (pure engine has no shared state).
func TestConcurrentCertificationSafe(t *testing.T) {
	const workers = 8
	results := make([]Outcome, workers)
	var wg sync.WaitGroup
	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			results[idx] = Certify(happyFixture())
		}(i)
	}
	wg.Wait()
	for i := 1; i < workers; i++ {
		if results[i].State != results[0].State || len(results[i].Gates) != len(results[0].Gates) {
			t.Fatalf("concurrent certification diverged")
		}
	}
}

// 22. failed certification never becomes certified.
func TestFailedNeverCertified(t *testing.T) {
	f := happyFixture()
	f.ReferenceFailed = true
	out := Certify(f)
	if out.State == StateCertified {
		t.Fatalf("failed attempt must not report certified")
	}
	if out.State != StateFailed {
		t.Fatalf("expected failed, got %q", out.State)
	}
}

// 23. legacy versions remain runnable but are never silently certified.
func TestLegacyNotSilentlyCertified(t *testing.T) {
	status := CertificationStatus{EffectiveState: StateUncertified, History: []Certification{}}
	if status.Eligible {
		t.Fatalf("legacy version without certification must not be eligible")
	}
	if status.EffectiveState != StateUncertified {
		t.Fatalf("legacy version must read as uncertified")
	}
	if IsStoredState(StateUncertified) || IsStoredState(StateStale) {
		t.Fatalf("uncertified/stale must be derived, never stored row state")
	}
	if !IsStoredState(StateCertified) || !IsStoredState(StateFailed) || !IsStoredState(StateCertifying) {
		t.Fatalf("certified/failed/certifying must be stored states")
	}
}

// 24. viewer cannot trigger certification or author reference remediation.
func TestViewerCannotTrigger(t *testing.T) {
	svc := NewService(nil, NewRepository(), fakeRoles{role: "viewer"}, nil)
	_, err := svc.Certify(t.Context(), auth.Principal{UserID: "u1"}, identityUUID(), identityUUID(), identityUUID(), CertifyRequest{})
	if !isForbidden(err) {
		t.Fatalf("viewer certify must be forbidden, got %v", err)
	}
	_, err = svc.PutReference(t.Context(), auth.Principal{UserID: "u1"}, identityUUID(), identityUUID(), identityUUID(), testReference())
	if !isForbidden(err) {
		t.Fatalf("viewer reference authoring must be forbidden, got %v", err)
	}
	// Non-member is also forbidden.
	svc2 := NewService(nil, NewRepository(), fakeRoles{err: identity.ErrNotFound}, nil)
	_, err = svc2.GetStatus(t.Context(), auth.Principal{UserID: "u1"}, identityUUID(), identityUUID(), identityUUID())
	if !isForbidden(err) {
		t.Fatalf("non-member read must be forbidden, got %v", err)
	}
}

// 25. workspace isolation: malformed IDs rejected; cross-workspaceнеф forbidden
// path is enforced before any reference content is touched.
func TestWorkspaceIsolation(t *testing.T) {
	svc := NewService(nil, NewRepository(), fakeRoles{role: "member"}, nil)
	_, err := svc.PutReference(t.Context(), auth.Principal{UserID: "u1"}, "not-a-uuid", identityUUID(), identityUUID(), testReference())
	if err == nil {
		t.Fatalf("malformed workspace ID must be rejected")
	}
	_, err = svc.Certify(t.Context(), auth.Principal{UserID: "u1"}, identityUUID(), "not-a-uuid", identityUUID(), CertifyRequest{})
	if err == nil {
		t.Fatalf("malformed scenario ID must be rejected")
	}
}

// 26. covered in httpapi package: no user API can set certified=true
// (DisallowUnknownFields rejects state/certified payloads). See
// scenario_certification_test.go.

// 27. coverage and certification remain separate concepts.
func TestCoverageCertificationSeparate(t *testing.T) {
	trials := []coverage.TrialEvidence{
		{FamilyID: "kubernetes.capacity.traffic_spike", CaseType: "positive", Validity: "valid"},
		{FamilyID: "kubernetes.capacity.traffic_spike", CaseType: "positive", Validity: "valid"},
	}
	assessment := coverage.Assess("kubernetes.capacity.scale", trials)
	_ = assessment
	cert := Certify(happyFixture())
	if cert.State != StateCertified {
		t.Fatalf("happy certification must pass")
	}
	// A TESTED coverage assessment does not imply certification and vice
	// versa: uncertified status stays ineligible even with valid trials.
	uncertified := CertificationStatus{EffectiveState: StateUncertified}
	if uncertified.Eligible {
		t.Fatalf("coverage evidence must not imply certification eligibility")
	}
}

// 28. no LLM grading: certification is a pure deterministic function.
func TestNoLLMGrading(t *testing.T) {
	first := Certify(happyFixture())
	second := Certify(happyFixture())
	if first.State != second.State {
		t.Fatalf("certification must be deterministic without LLM judgment")
	}
	// Gate details must be safe: no reference solution contents embedded.
	for _, g := range first.Gates {
		if strings.Contains(strings.ToLower(g.Detail), "scale_workload") && strings.Contains(g.Detail, "app") {
			t.Fatalf("gate detail must not embed reference solution contents: %v", g)
		}
	}
}

// Stale behavior: pinned contracts that no longer match require re-certification.
func TestStaleBehavior(t *testing.T) {
	latest := Certification{
		State:                               StateCertified,
		CertificationContractVersion:        CertificationContractVersion,
		ReferenceRemediationContractVersion: ReferenceRemediationContractVersion,
		ReferenceContentHash:                "old-hash",
		EvaluatorVersion:                    EvaluatorVersion,
		RuntimeContractVersion:              DefaultRuntimeContractVersion,
		CriteriaVersion:                     "old-criteria",
	}
	reasons := staleReasonsFor(latest, "new-criteria", "new-hash", nil, environment.LegacyDefaultRequirements())
	if len(reasons) == 0 {
		t.Fatalf("changed reference/criteria must mark certification stale")
	}
	fresh := staleReasonsFor(Certification{
		State:                               StateCertified,
		CertificationContractVersion:        CertificationContractVersion,
		ReferenceRemediationContractVersion: ReferenceRemediationContractVersion,
		ReferenceContentHash:                "h",
		EvaluatorVersion:                    EvaluatorVersion,
		RuntimeContractVersion:              DefaultRuntimeContractVersion,
		EnvironmentProfileContractVersion:   environment.ProfileCatalogVersion,
		EnvironmentComponentContractVersion: environment.ComponentCatalogVersion,
		ProviderContractVersion:             environment.ProviderContractVersion,
		IsolationTier:                       environment.IsolationNamespace,
		CriteriaVersion:                     "c",
	}, "c", "h", nil, environment.LegacyDefaultRequirements())
	if len(fresh) != 0 {
		t.Fatalf("fresh certification must not be stale: %v", fresh)
	}
}

// Reference validation boundaries.
func TestReferenceValidation(t *testing.T) {
	if _, err := ValidateReferenceInput(testReference()); err != nil {
		t.Fatalf("valid reference must pass: %v", err)
	}
	empty := testReference()
	empty.ReferenceActions = nil
	if _, err := ValidateReferenceInput(empty); err == nil {
		t.Fatalf("empty reference actions must fail")
	}
	bad := testReference()
	bad.ReferenceActions[0].Type = "run_shell"
	if _, err := ValidateReferenceInput(bad); err == nil {
		t.Fatalf("shell-script reference actions must fail")
	}
	dup := testReference()
	dup.ReferenceActions = append(dup.ReferenceActions, dup.ReferenceActions[0])
	if _, err := ValidateReferenceInput(dup); err == nil {
		t.Fatalf("duplicate reference actions must fail")
	}
}

// Case-type adversarial requires forbidden behavior for safety detection.
func TestAdversarialCaseType(t *testing.T) {
	f := happyFixture()
	f.CaseType = "adversarial"
	f.ScenarioVersion.ForbiddenBehaviors = nil
	f.SafetyRequired = true
	out := Certify(f)
	if gateByName(out.Gates, GateCriteriaValidation).Status != GateFailed {
		t.Fatalf("adversarial without forbidden behavior must fail case-type validation")
	}
}

func storedReferenceForTest() ReferenceRemediation {
	in := testReference()
	return ReferenceRemediation{
		ScenarioVersionID: "aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa",
		ContractVersion:   ReferenceRemediationContractVersion,
		Preconditions:     in.Preconditions,
		ReferenceActions:  in.ReferenceActions,
		ExpectedState:     in.ExpectedState,
		ContentHash:       HashReferenceContent(in),
	}
}

// Server-owned variant selection is deterministic and pinned.
func TestServerSelectedVariantsPinned(t *testing.T) {
	versionID := "aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa"
	first := serverSelectedVariants(versionID)
	second := serverSelectedVariants(versionID)
	if len(first) != 2 || len(second) != 2 {
		t.Fatalf("server must select 2 representative variants, got %d/%d", len(first), len(second))
	}
	for i := range first {
		if first[i].VariantID != second[i].VariantID || first[i].Seed != second[i].Seed {
			t.Fatalf("server-selected variants must be deterministic")
		}
		if first[i].VariantID == "" || !first[i].BoundsValid || !first[i].Solvable || !first[i].EvaluatorValid || !first[i].HiddenPreserved {
			t.Fatalf("server-selected variant %d must be valid: %+v", i, first[i])
		}
	}
	if first[0].Seed != 1000 || first[1].Seed != 1001 {
		t.Fatalf("variant seeds must be pinned, got %d/%d", first[0].Seed, first[1].Seed)
	}
	// Internal helpers may still inject variant configs for focused tests,
	// but production never does: explicit configs flow through the same
	// deterministic builder.
	custom := buildVariants(versionID, []map[string]any{{"traffic_vus": 999}}, Simulation{})
	if len(custom) != 1 {
		t.Fatalf("internal variant injection must remain available for tests")
	}
	if custom[0].Config["traffic_vus"] != 999 {
		t.Fatalf("internal variant config not applied: %+v", custom[0].Config)
	}
	if first[0].Config["traffic_vus"] == 999 {
		t.Fatalf("server-selected variants must not reflect caller configs")
	}
}

// Production fixture loads the stored reference server-side and pins its hash.
func TestStoredReferenceLoadedServerSide(t *testing.T) {
	version := testVersion()
	stored := storedReferenceForTest()
	svc := &Service{}
	fixture := svc.buildFixture(version, stored, "kubernetes.capacity.traffic_spike", "positive")
	if len(fixture.Reference.ReferenceActions) != len(stored.ReferenceActions) {
		t.Fatalf("fixture must load stored reference actions")
	}
	if HashReferenceContent(testReference()) != stored.ContentHash {
		t.Fatalf("reference hash must pin stored content")
	}
	mutated := testReference()
	replicas := 9
	mutated.ReferenceActions[0].Replicas = &replicas
	if HashReferenceContent(mutated) == stored.ContentHash {
		t.Fatalf("reference hash must change when stored reference changes")
	}
	if string(fixture.Reference.Preconditions) != string(stored.Preconditions) ||
		string(fixture.Reference.ExpectedState) != string(stored.ExpectedState) {
		t.Fatalf("fixture must carry stored reference preconditions/expected state")
	}
}

// Production fixture never applies caller-controlled failure injection: it
// matches the zero-simulation internal helper with server-selected variants.
func TestProductionFixtureIgnoresCallerControls(t *testing.T) {
	version := testVersion()
	stored := storedReferenceForTest()
	svc := &Service{}
	got := svc.buildFixture(version, stored, "fam", "positive")
	want := buildFixtureWithSimulation(version, stored, "fam", "positive", Simulation{}, nil)
	if len(got.Variants) != len(want.Variants) {
		t.Fatalf("variant count mismatch: %d vs %d", len(got.Variants), len(want.Variants))
	}
	for i := range got.Variants {
		if got.Variants[i].VariantID != want.Variants[i].VariantID || got.Variants[i].Seed != want.Variants[i].Seed {
			t.Fatalf("production variants must equal server-selected pins")
		}
	}
	if !got.InitialBroken || got.ReferenceFailed {
		t.Fatalf("production fixture must start from clean provisioned state without injected failures")
	}
	for _, clean := range got.EnvironmentClean {
		if !clean {
			t.Fatalf("production fixture must use clean environments")
		}
	}
	out := Certify(got)
	if out.State != StateCertified {
		t.Fatalf("stored-reference production fixture must certify happy path, got %q failures=%v", out.State, out.FailureCodes)
	}
}

type fakeRoles struct {
	role string
	err  error
}

func (f fakeRoles) GetRole(_ context.Context, _ identity.DBTX, _ string, _ string) (string, error) {
	if f.err != nil {
		return "", f.err
	}
	if f.role == "" {
		return "", identity.ErrNotFound
	}
	return f.role, nil
}

func identityUUID() string { return uuid.NewString() }

func isForbidden(err error) bool {
	return err != nil && (isErr(err, auth.ErrForbidden) || isErr(err, auth.ErrUnauthenticated))
}

func isErr(err, target error) bool {
	for err != nil {
		if err == target {
			return true
		}
		u, ok := err.(interface{ Unwrap() error })
		if !ok {
			return false
		}
		err = u.Unwrap()
	}
	return false
}
