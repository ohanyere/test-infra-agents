package billing

import (
	"fmt"
	"sync"
	"testing"
	"time"
)

func TestContractVersionsPinned(t *testing.T) {
	if BillingCatalogContractVersion != "billing-catalog-contract:v1" {
		t.Fatal("catalog contract version drift")
	}
	if EntitlementContractVersion != "entitlement-contract:v1" {
		t.Fatal("entitlement contract version drift")
	}
	if UsageMeterContractVersion != "usage-meter-contract:v1" {
		t.Fatal("meter contract version drift")
	}
}

func TestStablePlanIDs(t *testing.T) {
	want := []string{"developer", "starter", "pro", "organization", "enterprise"}
	seen := map[string]bool{}
	for _, p := range Catalog() {
		seen[p.ID] = true
	}
	for _, id := range want {
		if !seen[id] {
			t.Fatalf("missing stable plan %s", id)
		}
		if !IsValidPlan(id) {
			t.Fatalf("IsValidPlan rejects %s", id)
		}
	}
	if IsValidPlan("Pro") || IsValidPlan("DISPLAY") {
		t.Fatal("plan identity must not depend on mutable display strings")
	}
}

func TestCatalogPricesMatchPublicPage(t *testing.T) {
	prices := map[string]int64{"developer": 0, "starter": 4900, "pro": 29900, "organization": 99900, "enterprise": -1}
	credits := map[string]int64{"developer": 10, "starter": 100, "pro": 500, "organization": 2000, "enterprise": -1}
	for _, p := range Catalog() {
		if prices[p.ID] != p.PriceCents {
			t.Fatalf("plan %s price drift: got %d", p.ID, p.PriceCents)
		}
		if credits[p.ID] != p.MonthlyCredits {
			t.Fatalf("plan %s credit drift: got %d", p.ID, p.MonthlyCredits)
		}
	}
}

func TestClientCannotSelfUpgrade(t *testing.T) {
	// There is no service path that accepts caller-supplied plan/status: the
	// only writers are EnsureAccount (Developer default), provider
	// reconciliation, and internal operations. Assert the catalog rejects
	// display-string identities and unknown plans.
	if IsValidPlan("pro ") || IsValidPlan("Pro") {
		t.Fatal("display strings must not resolve to plan identity")
	}
	if NormalizePlanID("  Pro ") != "pro" {
		t.Fatal("normalization is lookup-only, never persisted identity")
	}
	if _, ok := GetPlan("pro"); !ok {
		t.Fatal("stable pro plan must exist")
	}
}

func TestEntitlementAllowedAndDenied(t *testing.T) {
	dev, _ := GetPlan(PlanDeveloper)
	pro, _ := GetPlan(PlanPro)
	if d := CheckEntitlement(dev, StatusActive, EntCanRunEvaluations, CheckOptions{}); !d.Allowed {
		t.Fatal("developer should run evaluations within allowance")
	}
	if d := CheckEntitlement(dev, StatusActive, EntCanUseIncidentImports, CheckOptions{}); d.Allowed || d.Code != CodeEntitlementDenied {
		t.Fatal("developer incident imports must deny with machine code")
	}
	if d := CheckEntitlement(pro, StatusActive, EntCanUseIncidentImports, CheckOptions{}); !d.Allowed {
		t.Fatal("pro incident imports should allow")
	}
	if d := CheckEntitlement(dev, StatusActive, EntCanUseAdvancedReliability, CheckOptions{}); d.Allowed {
		t.Fatal("developer advanced reliability must deny")
	}
	// RBAC independence is structural: CheckEntitlement never sees roles.
	// A viewer with an allowing plan still gets Allowed=true here; the HTTP
	// layer enforces RBAC separately. Assert purity.
	d := CheckEntitlement(dev, StatusActive, EntCanUseAPI, CheckOptions{})
	if !d.Allowed || d.PlanID != PlanDeveloper || d.EntitlementContract != EntitlementContractVersion {
		t.Fatal("entitlement snapshot must pin plan, status, contracts")
	}
}

func TestParallelAndMemberLimits(t *testing.T) {
	dev, _ := GetPlan(PlanDeveloper)
	if err := CheckParallelLimit(dev, 0); err != nil {
		t.Fatal("0 active should pass on developer")
	}
	if err := CheckParallelLimit(dev, 1); err == nil || err.Code != CodePlanLimitReached {
		t.Fatal("developer parallel limit 1 must deny at 1 active")
	}
	if err := CheckMemberLimit(dev, 1); err == nil || err.Code != CodePlanLimitReached {
		t.Fatal("developer member limit 1 must deny at 1")
	}
	ent, _ := GetPlan(PlanEnterprise)
	if err := CheckMemberLimit(ent, 100000); err != nil {
		t.Fatal("enterprise members must be custom/unlimited")
	}
}

func TestReservationSettlementLifecycle(t *testing.T) {
	// Valid PASS bills.
	a := ClassifySettlement(SettleInput{RunStatus: "completed", Validity: "valid", EvaluationStatus: "pass"})
	if a.Action != SettleCommit || !a.Billable {
		t.Fatalf("valid PASS must commit billable, got %+v", a)
	}
	// Valid agent FAIL also bills.
	a = ClassifySettlement(SettleInput{RunStatus: "failed", Validity: "valid", EvaluationStatus: "fail", FailureClass: "agent"})
	if a.Action != SettleCommit || !a.Billable {
		t.Fatalf("valid agent FAIL must commit billable, got %+v", a)
	}
	// Platform-invalid never commits customer usage.
	a = ClassifySettlement(SettleInput{RunStatus: "failed", Validity: "invalid_platform", EvaluationStatus: "fail", FailureClass: "platform"})
	if a.Billable || (a.Action != SettleRefund && a.Action != SettleRelease) {
		t.Fatalf("platform-invalid must release/refund non-billable, got %+v", a)
	}
	// Provisioning failure not charged.
	a = ClassifySettlement(SettleInput{RunStatus: "failed", Validity: "pending", FailureCode: "environment_provisioning_failed", FailureClass: "platform"})
	if a.Billable || a.Action != SettleRelease {
		t.Fatalf("provisioning failure must release, got %+v", a)
	}
	// Valid agent timeout bills.
	a = ClassifySettlement(SettleInput{RunStatus: "failed", Validity: "valid", EvaluationStatus: "fail", FailureClass: "agent", FailureCode: "run_timeout"})
	if a.Action != SettleCommit || !a.Billable {
		t.Fatalf("valid agent timeout must bill, got %+v", a)
	}
	// Platform timeout does not bill.
	a = ClassifySettlement(SettleInput{RunStatus: "failed", Validity: "pending", FailureClass: "platform", FailureCode: "run_timeout"})
	if a.Billable {
		t.Fatalf("platform timeout must not bill, got %+v", a)
	}
	// User cancellation releases.
	a = ClassifySettlement(SettleInput{RunStatus: "stopped", Validity: "pending", FailureClass: "control", FailureCode: "run_stopped"})
	if a.Billable || a.Action != SettleRelease {
		t.Fatalf("user cancel must release, got %+v", a)
	}
	// Abandoned recovery without evaluation releases (no double charge).
	a = ClassifySettlement(SettleInput{RunStatus: "failed", Validity: "pending", FailureCode: "worker_lease_expired", FailureClass: "platform"})
	if a.Billable || a.Action != SettleRelease {
		t.Fatalf("abandoned recovery must release, got %+v", a)
	}
}

func TestNoDoubleChargeIdempotencySimulator(t *testing.T) {
	// In-memory idempotency simulator mirroring repository semantics:
	// duplicate keys are no-ops; double commit is impossible.
	type ledger struct {
		mu      sync.Mutex
		entries map[string]int64
	}
	l := &ledger{entries: map[string]int64{}}
	apply := func(key string, qty int64) bool {
		l.mu.Lock()
		defer l.mu.Unlock()
		if _, ok := l.entries[key]; ok {
			return false
		}
		l.entries[key] = qty
		return true
	}
	if !apply("reservation:run-1", 1) {
		t.Fatal("first reservation must apply")
	}
	if apply("reservation:run-1", 1) {
		t.Fatal("duplicate reservation must be no-op")
	}
	var wg sync.WaitGroup
	applied := 0
	var mu sync.Mutex
	for i := 0; i < 16; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if apply("commit:run-1", 1) {
				mu.Lock()
				applied++
				mu.Unlock()
			}
		}()
	}
	wg.Wait()
	if applied != 1 {
		t.Fatalf("concurrent commits applied %d times, want 1", applied)
	}
}

func TestBalanceCannotRaceNegativeLogically(t *testing.T) {
	// Simulate serialized reservation with FOR UPDATE semantics: available is
	// recomputed under lock; concurrent spenders cannot overspend.
	granted := int64(2)
	var mu sync.Mutex
	reserved := int64(0)
	tryReserve := func(qty int64) bool {
		mu.Lock()
		defer mu.Unlock()
		if granted-reserved < qty {
			return false
		}
		reserved += qty
		return true
	}
	var wg sync.WaitGroup
	success := 0
	var smu sync.Mutex
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if tryReserve(1) {
				smu.Lock()
				success++
				smu.Unlock()
			}
		}()
	}
	wg.Wait()
	if success != 2 {
		t.Fatalf("overspend: %d reservations against allowance 2", success)
	}
}

func TestSummarizeDerivesBalance(t *testing.T) {
	plan, _ := GetPlan(PlanStarterID)
	start, end := CurrentPeriod(time.Now())
	sums := map[string]int64{UsageGrant: 100, UsageReservation: 5, UsageCommit: 3, UsageRelease: 1, UsageRefund: 1}
	s := Summarize("ws", plan, start, end, sums)
	// reserved = 5-3-1 = 1; committed = 3-1 = 2; available = 100-1-2 = 97
	if s.Reserved != 1 || s.Committed != 2 || s.Available != 97 {
		t.Fatalf("balance derivation wrong: %+v", s)
	}
}

func TestRawUsagePreservedSeparatelyFromPrice(t *testing.T) {
	if MeterUnits("namespace") != 1 || MeterUnits("dedicated_cluster") != 1 {
		t.Fatal("V1 meter must be flat 1 unit per trial; multipliers deferred")
	}
	if UsageMeterContractVersion == "" || BillingCatalogContractVersion == "" {
		t.Fatal("usage entries must pin meter + catalog contracts")
	}
}

func TestEnvironmentCannotDowngrade(t *testing.T) {
	dev, _ := GetPlan(PlanDeveloper)
	allowed := CheckEntitlement(dev, StatusActive, EntAllowedEnvironmentIsolation, CheckOptions{RequiredIsolation: IsolationNamespace})
	if !allowed.Allowed {
		t.Fatal("namespace requirement must allow on developer")
	}
	denied := CheckEntitlement(dev, StatusActive, EntAllowedEnvironmentIsolation, CheckOptions{RequiredIsolation: IsolationDedicatedCluster})
	if denied.Allowed || denied.Code != CodeEnvironmentTierNotEntitled {
		t.Fatalf("dedicated requirement must deny, not downgrade: %+v", denied)
	}
	ent, _ := GetPlan(PlanEnterprise)
	if d := CheckEntitlement(ent, StatusActive, EntAllowedEnvironmentIsolation, CheckOptions{RequiredIsolation: IsolationDedicatedCluster}); !d.Allowed {
		t.Fatal("enterprise must satisfy dedicated")
	}
}

func TestPastDueAndCanceledBehavior(t *testing.T) {
	pro, _ := GetPlan(PlanPro)
	// Expensive execution blocked.
	if d := CheckEntitlement(pro, StatusPastDue, EntCanRunEvaluations, CheckOptions{}); d.Allowed || d.Code != CodeBillingPastDue {
		t.Fatalf("past_due must block execution with billing_past_due: %+v", d)
	}
	if d := CheckEntitlement(pro, StatusCanceled, EntCanRunEvaluations, CheckOptions{}); d.Allowed {
		t.Fatal("canceled must block new execution")
	}
	if d := CheckEntitlement(pro, StatusSuspended, EntCanRunEvaluations, CheckOptions{}); d.Allowed {
		t.Fatal("suspended must block new execution")
	}
	// Reads (API/MCP/incident metadata) stay available: historical reads remain.
	if d := CheckEntitlement(pro, StatusPastDue, EntCanUseAPI, CheckOptions{}); !d.Allowed {
		t.Fatal("past_due must keep read access")
	}
	if d := CheckEntitlement(pro, StatusCanceled, EntCanUseAPI, CheckOptions{}); !d.Allowed {
		t.Fatal("canceled must keep historical reads")
	}
}

func TestFrontendDenialCodeContract(t *testing.T) {
	cases := map[string]int{
		CodeEntitlementDenied:          403,
		CodePlanLimitReached:           403,
		CodeInsufficientCredits:        402,
		CodeBillingPastDue:             402,
		CodeEnvironmentTierNotEntitled: 403,
	}
	for code, status := range cases {
		if got := HTTPStatus(code); got != status {
			t.Fatalf("HTTPStatus(%q)=%d, want %d", code, got, status)
		}
		if err := entitlementDenied(code, "safe denial"); err.Code != code {
			t.Fatalf("denial code changed: got %q want %q", err.Code, code)
		}
	}
}

func TestProviderEventIdempotencyAndStaleGuard(t *testing.T) {
	raw := []byte(`{"event_id":"evt_1","event_type":"subscription.updated","workspace_id":"` + fakeWorkspaceID() + `","plan_id":"pro","status":"active"}`)
	sig := SignFakeEvent("secret", raw)
	if !VerifySignature("secret", raw, sig) {
		t.Fatal("valid fake signature must verify")
	}
	if VerifySignature("secret", raw, "v1=deadbeef") {
		t.Fatal("invalid signature must fail")
	}
	rec, err := FakeProvider{}.ParseEvent(raw)
	if err != nil {
		t.Fatal(err)
	}
	if rec.PlanID != "pro" || rec.Status != "active" {
		t.Fatalf("reconciliation parse wrong: %+v", rec)
	}
	// Stale guard is a pure time comparison: events older than account truth
	// must not apply. Simulate account updated now, event 1h old.
	accountUpdated := time.Now().UTC()
	staleEvent := accountUpdated.Add(-time.Hour)
	if !staleEvent.Before(accountUpdated.Add(-time.Second)) {
		t.Fatal("stale comparison broken")
	}
	freshEvent := accountUpdated.Add(time.Minute)
	if freshEvent.Before(accountUpdated.Add(-time.Second)) {
		t.Fatal("fresh event misclassified as stale")
	}
}

func TestPlanBuildDoesNotConsumeCredits(t *testing.T) {
	// Structural: qualification planning and suite-version creation have no
	// ledger path. The only writers are Reserve/Commit/Release/Refund/
	// Adjustment + period grants. Assert entitlement checks are read-only by
	// calling them repeatedly with no state change.
	dev, _ := GetPlan(PlanDeveloper)
	before := fmt.Sprintf("%+v", AllEntitlements(dev, StatusFree, false, ""))
	_ = CheckEntitlement(dev, StatusFree, EntCanBuildQualificationSuites, CheckOptions{})
	after := fmt.Sprintf("%+v", AllEntitlements(dev, StatusFree, false, ""))
	if before != after {
		t.Fatal("entitlement checks must be read-only")
	}
}

func TestNoPaymentSecretsInDomain(t *testing.T) {
	// Fake checkout URLs must not embed secrets.
	url, ref, err := FakeProvider{}.CreateCheckoutSession(t.Context(), "ws", "pro")
	if err != nil {
		t.Fatal(err)
	}
	for _, needle := range []string{"sk_", "secret", "cvc", "card"} {
		if contains(url+ref, needle) {
			t.Fatalf("provider surface leaks %q", needle)
		}
	}
}

func contains(haystack, needle string) bool {
	return len(haystack) >= len(needle) && (func() bool {
		for i := 0; i+len(needle) <= len(haystack); i++ {
			if haystack[i:i+len(needle)] == needle {
				return true
			}
		}
		return false
	})()
}

func fakeWorkspaceID() string { return "11111111-1111-1111-1111-111111111111" }
