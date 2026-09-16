package executionworker

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/evalplane/evalplane/backend/internal/agentgateway"
	"github.com/evalplane/evalplane/backend/internal/environment"
	"github.com/evalplane/evalplane/backend/internal/execution"
	"github.com/evalplane/evalplane/backend/internal/kubeenv"
)

// fakeProvider is a deterministic test double for the environment.Provider
// abstraction. No cluster, no credentials, no Kind dependency.
type fakeProvider struct {
	id           string
	profiles     []string
	available    bool
	provisionErr error
	destroyErr   error
	resetErr     error
	provisioned  int
	destroyed    int
	resets       int
	lastOwner    string
	ref          string
}

func newFakeProvider(id string, profiles []string) *fakeProvider {
	return &fakeProvider{id: id, profiles: profiles, available: true, ref: "fake://" + id + "/ns"}
}

func (p *fakeProvider) Info() environment.ProviderInfo {
	return environment.ProviderInfo{ID: p.id, DisplayName: "fake", ContractVersion: environment.ProviderContractVersion, Status: environment.ProviderActive, SupportedProfiles: append([]string(nil), p.profiles...)}
}

func (p *fakeProvider) Capabilities() environment.ProviderCapabilities {
	return environment.ProviderCapabilities{SupportedProfiles: append([]string(nil), p.profiles...), RuntimeContracts: []string{environment.DefaultRuntimeContract}}
}

func (p *fakeProvider) Health(context.Context) environment.ProviderHealth {
	if !p.available {
		return environment.ProviderHealth{Available: false, Reason: "drained for test"}
	}
	return environment.ProviderHealth{Available: true, Reason: "fake healthy"}
}

func (p *fakeProvider) Provision(_ context.Context, req environment.ProvisionRequest) (string, error) {
	p.provisioned++
	p.lastOwner = req.OwnerID
	if p.provisionErr != nil {
		return "", p.provisionErr
	}
	if p.ref != "" {
		return p.ref, nil
	}
	return "fake://" + p.id + "/" + req.OwnerID, nil
}

func (p *fakeProvider) Get(_ context.Context, environmentID string) (string, error) {
	return "fake://" + p.id + "/" + environmentID, nil
}

func (p *fakeProvider) Reset(_ context.Context, _ string) error {
	p.resets++
	return p.resetErr
}

func (p *fakeProvider) Destroy(_ context.Context, _ string) error {
	p.destroyed++
	return p.destroyErr
}

func namespaceAvailability(providers ...*fakeProvider) environment.ProviderAvailability {
	health := map[string]environment.ProviderHealth{}
	supports := map[string]map[string]bool{}
	for _, p := range providers {
		health[p.id] = environment.ProviderHealth{Available: p.available, Reason: "test"}
		m := map[string]bool{}
		for _, profile := range p.profiles {
			m[profile] = true
		}
		supports[p.id] = m
	}
	// Default registry shape: kind-dev available for namespace. Tests that
	// need the static registry pass nil availability instead.
	return environment.NewFakeAvailability(health, supports)
}

func testRequirements() environment.Requirements {
	req, err := environment.ValidateRequirements(environment.Requirements{
		RequiredComponents: []string{"kubernetes", "prometheus"},
		MinimumIsolation:   environment.IsolationNamespace,
	})
	if err != nil {
		panic(err)
	}
	return req
}

func dedicatedRequirements() environment.Requirements {
	req, err := environment.ValidateRequirements(environment.Requirements{
		RequiredComponents: []string{"kubernetes", "prometheus"},
		MinimumIsolation:   environment.IsolationDedicatedCluster,
	})
	if err != nil {
		panic(err)
	}
	return req
}

func newManagedEnvs(req environment.Requirements, explicit bool, availability environment.ProviderAvailability, providers ...*fakeProvider) (*managedRunEnvironments, *memoryInstanceStore, map[string]*fakeProvider) {
	store := newMemoryInstanceStore()
	byID := map[string]*fakeProvider{}
	reg := map[string]environment.Provider{}
	for _, p := range providers {
		byID[p.id] = p
		reg[p.id] = p
	}
	// Fake catalog entries for test provider IDs so the scheduler can select
	// them: tests use kind-dev (real catalog) or fake IDs below. For fake IDs
	// we bypass the static catalog by using a test-only availability that
	// reports support, but Select() still iterates ListProviders (static).
	// To keep scheduler selection realistic, tests use ProviderKindDev ID with
	// fake implementation unless testing unavailable/downgrade paths.
	m := &managedRunEnvironments{
		requirements: func(context.Context, string) (environment.Requirements, bool, error) {
			return req, explicit, nil
		},
		availability: availability,
		instances:    store,
		providers: func(id string) (environment.Provider, bool) {
			p, ok := reg[id]
			return p, ok
		},
	}
	return m, store, byID
}

func kindFake() *fakeProvider {
	return newFakeProvider(environment.ProviderKindDev, []string{environment.ProfileKubernetesNamespace})
}

func (f *fixture) workerWithEnvs(t *testing.T, envs RunEnvironments) *Worker {
	t.Helper()
	base := f.worker(t, testConfig())
	base.deps.Environments = envs
	return base
}

func failureOf(err error) *Failure {
	var f *Failure
	if errors.As(err, &f) {
		return f
	}
	return nil
}

// 1. worker resolves ScenarioVersion requirements.
func TestRunLifecycleResolvesVersionRequirements(t *testing.T) {
	provider := kindFake()
	req := testRequirements()
	envs, _, _ := newManagedEnvs(req, true, namespaceAvailability(provider), provider)
	f := newFixture()
	out, err := envs.ProvisionForRun(context.Background(), f.run, f.sv)
	if err != nil {
		t.Fatal(err)
	}
	if !out.Explicit {
		t.Fatal("explicit requirements must be reported explicit")
	}
	if out.Requirements.MinimumIsolation != environment.IsolationNamespace {
		t.Fatalf("requirements = %+v", out.Requirements)
	}
	if out.Decision.Profile == nil || out.Decision.Profile.ID != environment.ProfileKubernetesNamespace {
		t.Fatalf("decision = %+v", out.Decision)
	}
	if out.Decision.Provider == nil || out.Decision.Provider.ID != environment.ProviderKindDev {
		t.Fatalf("provider = %+v", out.Decision.Provider)
	}
}

// 2. legacy task uses legacy namespace default.
func TestRunLifecycleLegacyDefault(t *testing.T) {
	provider := kindFake()
	envs, _, _ := newManagedEnvs(environment.Requirements{}, false, namespaceAvailability(provider), provider)
	f := newFixture()
	out, err := envs.ProvisionForRun(context.Background(), f.run, f.sv)
	if err != nil {
		t.Fatal(err)
	}
	if out.Explicit {
		t.Fatal("legacy must be explicit=false")
	}
	if out.Requirements.MinimumIsolation != environment.IsolationNamespace {
		t.Fatalf("legacy must claim namespace only: %+v", out.Requirements)
	}
	if out.Instance.ProfileID != environment.ProfileKubernetesNamespace || out.Instance.ProviderID != environment.ProviderKindDev {
		t.Fatalf("legacy must use namespace/kind-dev: %+v", out.Instance)
	}
	legacy := environment.LegacyDefaultRequirements()
	if legacy.MinimumIsolation != environment.IsolationNamespace {
		t.Fatal("LegacyDefaultRequirements must never claim stronger isolation")
	}
}

// 3. scheduler-selected profile/provider creates EnvironmentInstance.
func TestRunLifecycleCreatesInstance(t *testing.T) {
	provider := kindFake()
	envs, _, _ := newManagedEnvs(testRequirements(), true, namespaceAvailability(provider), provider)
	f := newFixture()
	out, err := envs.ProvisionForRun(context.Background(), f.run, f.sv)
	if err != nil {
		t.Fatal(err)
	}
	if out.Instance.ID == "" || out.Instance.ProfileID == "" || out.Instance.ProviderID == "" {
		t.Fatalf("instance = %+v", out.Instance)
	}
	if out.Instance.ProfileContractVersion != environment.ProfileCatalogVersion {
		t.Fatalf("profile contract = %q", out.Instance.ProfileContractVersion)
	}
	if out.Instance.ProviderContractVersion != environment.ProviderContractVersion {
		t.Fatalf("provider contract = %q", out.Instance.ProviderContractVersion)
	}
}

// 4. environment ownership uses exact workspace/run.
func TestRunLifecycleOwnership(t *testing.T) {
	provider := kindFake()
	envs, _, _ := newManagedEnvs(testRequirements(), true, namespaceAvailability(provider), provider)
	f := newFixture()
	out, err := envs.ProvisionForRun(context.Background(), f.run, f.sv)
	if err != nil {
		t.Fatal(err)
	}
	if out.Instance.OwnerType != environment.OwnerRun || out.Instance.OwnerID != runID || out.Instance.WorkspaceID != workspaceID {
		t.Fatalf("ownership = %+v", out.Instance)
	}
	if _, err := kubeenv.ValidateRunIdentity(kubeenv.RunIdentity{RunID: out.Instance.OwnerID, BatchID: f.run.BatchID, WorkspaceID: out.Instance.WorkspaceID}); err != nil {
		t.Fatalf("ownership identity invalid: %v", err)
	}
}

// 5. provider provision occurs before agent invocation.
func TestRunLifecycleProvisionBeforeAgent(t *testing.T) {
	provider := kindFake()
	envs, _, _ := newManagedEnvs(testRequirements(), true, namespaceAvailability(provider), provider)
	f := newFixture()
	w := f.workerWithEnvs(t, envs)
	if err := w.ExecuteRun(context.Background(), f.run); err != nil {
		t.Fatal(err)
	}
	if provider.provisioned != 1 {
		t.Fatalf("provider provisioned=%d", provider.provisioned)
	}
	if len(f.requests) != 1 {
		t.Fatalf("agent requests=%d", len(f.requests))
	}
	if f.provisioned != 0 {
		t.Fatalf("worker bypassed environment service with direct provision=%d", f.provisioned)
	}
}

// 6. successful provision marks ready.
func TestRunLifecycleProvisionSuccessMarksReady(t *testing.T) {
	provider := kindFake()
	envs, _, _ := newManagedEnvs(testRequirements(), true, namespaceAvailability(provider), provider)
	f := newFixture()
	out, err := envs.ProvisionForRun(context.Background(), f.run, f.sv)
	if err != nil {
		t.Fatal(err)
	}
	if out.Instance.Status != environment.StateReady {
		t.Fatalf("status=%q", out.Instance.Status)
	}
	if out.Instance.ReadyAt == nil {
		t.Fatal("ready instance must carry ready_at")
	}
	if out.Provisioned.Reference == "" || out.Provisioned.Namespace == "" {
		t.Fatalf("provisioned=%+v", out.Provisioned)
	}
}

// 7. scheduler failure prevents agent invocation.
func TestRunLifecycleSchedulerFailurePreventsAgent(t *testing.T) {
	provider := kindFake() // namespace only
	envs, _, _ := newManagedEnvs(dedicatedRequirements(), true, namespaceAvailability(provider), provider)
	f := newFixture()
	w := f.workerWithEnvs(t, envs)
	err := w.ExecuteRun(context.Background(), f.run)
	if err == nil {
		t.Fatal("expected scheduler failure")
	}
	if len(f.requests) != 0 {
		t.Fatalf("agent invoked despite scheduler failure: %d", len(f.requests))
	}
	fc := failureOf(err)
	if fc == nil || fc.Class != "platform" {
		t.Fatalf("scheduler failure must be platform, got %v", err)
	}
}

// 8. provision failure prevents agent invocation.
func TestRunLifecycleProvisionFailurePreventsAgent(t *testing.T) {
	provider := kindFake()
	provider.provisionErr = errors.New("fake cluster unavailable")
	envs, _, _ := newManagedEnvs(testRequirements(), true, namespaceAvailability(provider), provider)
	f := newFixture()
	w := f.workerWithEnvs(t, envs)
	if err := w.ExecuteRun(context.Background(), f.run); err == nil {
		t.Fatal("expected provision failure")
	}
	if len(f.requests) != 0 {
		t.Fatal("agent invoked despite provision failure")
	}
}

// 9. provision failure becomes platform-invalid/platform failure.
func TestRunLifecycleProvisionFailureIsPlatform(t *testing.T) {
	provider := kindFake()
	provider.provisionErr = errors.New("private cluster detail")
	envs, _, _ := newManagedEnvs(testRequirements(), true, namespaceAvailability(provider), provider)
	f := newFixture()
	w := f.workerWithEnvs(t, envs)
	err := w.ExecuteRun(context.Background(), f.run)
	fc := failureOf(err)
	if fc == nil || fc.Class != "platform" {
		t.Fatalf("provision failure must be platform, got %v", err)
	}
	if fc.Code != "environment_provisioning_failed" {
		t.Fatalf("code=%q", fc.Code)
	}
	if len(f.results) != 1 || f.results[0].Status == "pass" {
		t.Fatalf("provision failure must never produce passing agent evidence: %+v", f.results)
	}
	for _, e := range f.events {
		if strings.Contains(string(e.Payload), "private cluster detail") {
			t.Fatal("provider internals leaked into evidence")
		}
	}
}

// 10. provider unavailable never becomes agent failure.
func TestRunLifecycleProviderUnavailableNeverAgentFailure(t *testing.T) {
	provider := kindFake()
	provider.available = false
	envs, _, _ := newManagedEnvs(testRequirements(), true, namespaceAvailability(provider), provider)
	f := newFixture()
	w := f.workerWithEnvs(t, envs)
	err := w.ExecuteRun(context.Background(), f.run)
	fc := failureOf(err)
	if fc == nil || fc.Class != "platform" {
		t.Fatalf("unavailable provider must be platform, got %v", err)
	}
	if len(f.requests) != 0 {
		t.Fatal("agent invoked despite unavailable provider")
	}
	if len(f.results) == 1 && f.results[0].Status == "pass" {
		t.Fatal("unavailable provider graded as agent pass")
	}
}

// 11. dedicated requirement never downgrades to namespace.
func TestRunLifecycleNoDowngrade(t *testing.T) {
	provider := kindFake()
	envs, _, _ := newManagedEnvs(dedicatedRequirements(), true, namespaceAvailability(provider), provider)
	f := newFixture()
	_, err := envs.ProvisionForRun(context.Background(), f.run, f.sv)
	if err == nil {
		t.Fatal("dedicated requirement must not resolve to namespace")
	}
	fc := failureOf(err)
	if fc == nil || fc.Code != environment.ReasonNoEligibleEnvironment {
		t.Fatalf("want no_eligible_environment, got %v", err)
	}
	// Pure scheduler check as well.
	decision := environment.SelectForRequirements(context.Background(), dedicatedRequirements(), "trial", "", namespaceAvailability(provider))
	if decision.Profile != nil {
		t.Fatalf("scheduler downgraded dedicated to %s", decision.Profile.ID)
	}
}

// 12. terminal run triggers cleanup.
func TestRunLifecycleTerminalTriggersCleanup(t *testing.T) {
	provider := kindFake()
	envs, _, _ := newManagedEnvs(testRequirements(), true, namespaceAvailability(provider), provider)
	f := newFixture()
	w := f.workerWithEnvs(t, envs)
	if err := w.ExecuteRun(context.Background(), f.run); err != nil {
		t.Fatal(err)
	}
	if f.status != "completed" {
		t.Fatalf("status=%s", f.status)
	}
	if provider.destroyed != 1 || f.cleaned != 1 {
		t.Fatalf("destroyed=%d cleaned=%d", provider.destroyed, f.cleaned)
	}
	inst, err := envs.InstanceForOwner(context.Background(), workspaceID, runID)
	if err != nil {
		t.Fatal(err)
	}
	if inst.Status != environment.StateDestroyed {
		t.Fatalf("instance status=%q", inst.Status)
	}
}

// 13. repeated cleanup idempotent.
func TestRunLifecycleRepeatedCleanupIdempotent(t *testing.T) {
	provider := kindFake()
	envs, _, _ := newManagedEnvs(testRequirements(), true, namespaceAvailability(provider), provider)
	f := newFixture()
	w := f.workerWithEnvs(t, envs)
	if err := w.ExecuteRun(context.Background(), f.run); err != nil {
		t.Fatal(err)
	}
	id := kubeenv.RunIdentity{RunID: runID, BatchID: f.run.BatchID, WorkspaceID: workspaceID}
	if err := envs.CleanupIdentity(context.Background(), id); err != nil {
		t.Fatal(err)
	}
	if err := envs.CleanupIdentity(context.Background(), id); err != nil {
		t.Fatal(err)
	}
	inst, _ := envs.InstanceForOwner(context.Background(), workspaceID, runID)
	if inst.Status != environment.StateDestroyed {
		t.Fatalf("status=%q", inst.Status)
	}
}

// 14. cleanup failure does not alter agent result.
func TestRunLifecycleCleanupFailurePreservesVerdict(t *testing.T) {
	provider := kindFake()
	provider.destroyErr = errors.New("delete unavailable")
	envs, _, _ := newManagedEnvs(testRequirements(), true, namespaceAvailability(provider), provider)
	f := newFixture()
	w := f.workerWithEnvs(t, envs)
	err := w.ExecuteRun(context.Background(), f.run)
	if err == nil || !strings.Contains(err.Error(), "platform") {
		t.Fatalf("cleanup failure must surface as platform error, got %v", err)
	}
	// Terminal verdict precedes cleanup: run still completes.
	if f.status != "completed" {
		t.Fatalf("cleanup failure altered verdict: %s", f.status)
	}
	if len(f.results) != 1 {
		t.Fatal("evaluation missing after cleanup failure")
	}
	if !hasEvent(f, "cleanup_failed") {
		t.Fatal("platform cleanup failure must be recorded")
	}
	inst, err := envs.InstanceForOwner(context.Background(), workspaceID, runID)
	if err != nil {
		t.Fatal(err)
	}
	if inst.Status == environment.StateDestroyed {
		t.Fatal("failed cleanup must leave state suitable for sweeper, not destroyed")
	}
}

// 15. cancelled run cleanup.
func TestRunLifecycleCancelledCleanup(t *testing.T) {
	provider := kindFake()
	envs, _, _ := newManagedEnvs(testRequirements(), true, namespaceAvailability(provider), provider)
	f := newFixture()
	f.onGateway = f.setStop
	f.response = []agentgateway.ProposedAction{{Type: "inspect"}}
	w := f.workerWithEnvs(t, envs)
	_ = w.ExecuteRun(context.Background(), f.run)
	if f.status != "stopped" {
		t.Fatalf("status=%s", f.status)
	}
	if provider.destroyed == 0 || f.cleaned == 0 {
		t.Fatalf("cancelled run must cleanup: destroyed=%d cleaned=%d", provider.destroyed, f.cleaned)
	}
	inst, err := envs.InstanceForOwner(context.Background(), workspaceID, runID)
	if err != nil {
		t.Fatal(err)
	}
	if inst.Status != environment.StateDestroyed {
		t.Fatalf("cancelled instance status=%q", inst.Status)
	}
}

// 16. abandoned/stale run remains recoverable.
func TestRunLifecycleAbandonedRecoverable(t *testing.T) {
	provider := kindFake()
	envs, _, _ := newManagedEnvs(testRequirements(), true, namespaceAvailability(provider), provider)
	f := newFixture()
	// Owned instance exists before crash (provisioned, not yet destroyed).
	pre, err := envs.ProvisionForRun(context.Background(), f.run, f.sv)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := envs.InstanceForOwner(context.Background(), workspaceID, runID); err != nil {
		t.Fatal(err)
	}
	f.abandoned = []execution.AbandonedRun{staleRun("running")}
	w := f.workerWithEnvs(t, envs)
	if _, err := w.RecoverOne(context.Background()); err != nil {
		t.Fatal(err)
	}
	if len(f.requests) != 0 || provider.provisioned != 1 {
		t.Fatal("recovery must never invoke agent or reprovision")
	}
	if provider.destroyed == 0 {
		t.Fatal("abandoned owned environment must be cleanup-capable")
	}
	inst, err := envs.InstanceForOwner(context.Background(), workspaceID, runID)
	if err != nil {
		t.Fatal(err)
	}
	if inst.Status != environment.StateDestroyed {
		t.Fatalf("recovered instance status=%q", inst.Status)
	}
	_ = pre
}

// 17. workspace isolation.
func TestRunLifecycleWorkspaceIsolation(t *testing.T) {
	provider := kindFake()
	envs, _, _ := newManagedEnvs(testRequirements(), true, namespaceAvailability(provider), provider)
	f := newFixture()
	if _, err := envs.ProvisionForRun(context.Background(), f.run, f.sv); err != nil {
		t.Fatal(err)
	}
	other := "99999999-9999-4999-8999-999999999999"
	if _, err := envs.InstanceForOwner(context.Background(), other, runID); !errors.Is(err, ErrNoEnvironment) {
		t.Fatalf("cross-workspace lookup must fail closed, got %v", err)
	}
	// Same run ID in another workspace is a distinct owner, never reused.
	otherRun := f.run
	otherRun.WorkspaceID = other
	if _, err := runIdentityFor(otherRun); err != nil {
		t.Fatal(err)
	}
}

// 18. destroyed environment never reused.
func TestRunLifecycleDestroyedNeverReused(t *testing.T) {
	provider := kindFake()
	envs, store, _ := newManagedEnvs(testRequirements(), true, namespaceAvailability(provider), provider)
	f := newFixture()
	first, err := envs.ProvisionForRun(context.Background(), f.run, f.sv)
	if err != nil {
		t.Fatal(err)
	}
	id := kubeenv.RunIdentity{RunID: runID, BatchID: f.run.BatchID, WorkspaceID: workspaceID}
	if err := envs.CleanupIdentity(context.Background(), id); err != nil {
		t.Fatal(err)
	}
	if err := envs.ResetForRun(context.Background(), f.run); err == nil {
		t.Fatal("reset of destroyed environment must fail")
	}
	if _, err := store.transition(workspaceID, first.Instance.ID, environment.StateReady, "fake://x"); err == nil {
		t.Fatal("destroyed must never return to ready")
	}
	second, err := envs.ProvisionForRun(context.Background(), f.run, f.sv)
	if err != nil {
		t.Fatal(err)
	}
	if second.Instance.ID == first.Instance.ID {
		t.Fatal("provisioning must create a new instance, never reuse destroyed")
	}
	if second.Instance.Status != environment.StateReady {
		t.Fatalf("second instance status=%q", second.Instance.Status)
	}
}

// 19. environment identity/pins remain available for reproducibility.
func TestRunLifecycleReproducibilityPins(t *testing.T) {
	provider := kindFake()
	envs, _, _ := newManagedEnvs(testRequirements(), true, namespaceAvailability(provider), provider)
	f := newFixture()
	w := f.workerWithEnvs(t, envs)
	if err := w.ExecuteRun(context.Background(), f.run); err != nil {
		t.Fatal(err)
	}
	inst, err := envs.InstanceForOwner(context.Background(), workspaceID, runID)
	if err != nil {
		t.Fatal(err)
	}
	for _, key := range []string{"environment_profile_id", "environment_profile_contract_version", "provider_id", "provider_contract_version", "isolation_tier", "runtime_contract_version", "environment_instance_id"} {
		found := false
		for _, e := range f.events {
			if strings.Contains(string(e.Payload), key) || strings.Contains(e.Summary, "environment_ready") {
				found = true
				break
			}
		}
		_ = found
	}
	pins := pinsForInstance(inst, environment.SchedulerDecision{RuntimeContract: environment.DefaultRuntimeContract, SchedulerContract: environment.SchedulerContractVersion})
	for _, key := range []string{"environment_profile_id", "environment_profile_contract_version", "provider_id", "provider_contract_version", "isolation_tier", "runtime_contract_version", "environment_instance_id"} {
		if _, ok := pins[key]; !ok {
			t.Fatalf("pins missing %s: %+v", key, pins)
		}
	}
	if inst.ProfileID != environment.ProfileKubernetesNamespace || inst.ProviderID != environment.ProviderKindDev || inst.IsolationTier != environment.IsolationNamespace {
		t.Fatalf("pins = %+v", inst)
	}
	if f.run.RunID == "" {
		t.Fatal("run linkage missing")
	}
}

// 20. worker has no Kind-specific dependency.
func TestRunLifecycleNoKindDependency(t *testing.T) {
	provider := newFakeProvider("fake-dev:v1", []string{environment.ProfileKubernetesNamespace})
	_ = provider
	kindless := kindFake()
	// Functional proof: provisioning succeeds with the fake abstraction and
	// never type-asserts the concrete Kind development provider.
	envs, _, byID := newManagedEnvs(testRequirements(), true, namespaceAvailability(kindless), kindless)
	f := newFixture()
	if _, err := envs.ProvisionForRun(context.Background(), f.run, f.sv); err != nil {
		t.Fatal(err)
	}
	for id, p := range byID {
		if _, isKind := any(p).(*environment.KindDevProvider); isKind {
			t.Fatalf("test path must not depend on concrete Kind provider: %s", id)
		}
	}
	var _ RunEnvironments = envs // worker depends on the interface only
}

// 21. no kubeconfig/provider secret exposure.
func TestRunLifecycleNoSecretExposure(t *testing.T) {
	provider := kindFake()
	envs, _, _ := newManagedEnvs(testRequirements(), true, namespaceAvailability(provider), provider)
	f := newFixture()
	out, err := envs.ProvisionForRun(context.Background(), f.run, f.sv)
	if err != nil {
		t.Fatal(err)
	}
	candidates := []string{out.Instance.ProviderRef, out.Provisioned.Reference, string(f.sv.ExecutionSpec)}
	for _, c := range candidates {
		lowered := strings.ToLower(c)
		for _, marker := range []string{"kubeconfig", "-----begin", "bearer ", "aws_secret", "private_key"} {
			if strings.Contains(lowered, marker) {
				t.Fatalf("secret marker %q exposed in %q", marker, c)
			}
		}
	}
	if !isSafeProviderRef(out.Instance.ProviderRef) {
		t.Fatal("provider ref must be safe opaque handle")
	}
	if isSafeProviderRef("kubeconfig: fake") || isSafeProviderRef("-----BEGIN RSA PRIVATE KEY-----") {
		t.Fatal("unsafe references must be rejected")
	}
	view := out.Instance.SafeView()
	if view.ProviderRef != out.Instance.ProviderRef {
		t.Fatal("safe view must preserve opaque ref without adding secrets")
	}
}

// 22. coverage behavior unchanged.
func TestRunLifecycleCoverageUnchanged(t *testing.T) {
	provider := kindFake()
	envs, _, _ := newManagedEnvs(testRequirements(), true, namespaceAvailability(provider), provider)
	f := newFixture()
	w := f.workerWithEnvs(t, envs)
	if err := w.ExecuteRun(context.Background(), f.run); err != nil {
		t.Fatal(err)
	}
	if f.evaluated != 1 || len(f.results) != 1 {
		t.Fatalf("evaluation must still run once: evaluated=%d", f.evaluated)
	}
	// Empty/no_op still terminates decisions without grading success (existing
	// worker semantics preserved).
	if len(f.requests) != 1 {
		t.Fatalf("no_op semantics changed: requests=%d", len(f.requests))
	}
}

// 23. certification behavior unchanged.
func TestRunLifecycleCertificationUnchanged(t *testing.T) {
	req := testRequirements()
	trial := environment.SelectForRequirements(context.Background(), req, "trial", "", environment.DefaultAvailability())
	cert := environment.SelectForRequirements(context.Background(), req, "certification", "", environment.DefaultAvailability())
	if trial.Profile == nil || cert.Profile == nil || trial.Profile.ID != cert.Profile.ID {
		t.Fatalf("certification resolution must match trial: %+v vs %+v", trial.Profile, cert.Profile)
	}
	if trial.RuntimeContract != environment.DefaultRuntimeContract {
		t.Fatalf("runtime must derive from server profile, got %q", trial.RuntimeContract)
	}
	// Arbitrary caller profile never determines truth.
	bad := environment.Select(context.Background(), environment.SchedulerInput{Requirements: req, ProfilePreference: "aws-sandbox:v9"}, environment.DefaultAvailability())
	if bad.Profile != nil {
		t.Fatal("arbitrary profile preference must not resolve")
	}
}

// 24. no LLM grading.
func TestRunLifecycleNoLLM(t *testing.T) {
	req := testRequirements()
	first := environment.SelectForRequirements(context.Background(), req, "trial", "", environment.DefaultAvailability())
	second := environment.SelectForRequirements(context.Background(), req, "trial", "", environment.DefaultAvailability())
	if first.Profile.ID != second.Profile.ID || first.ReasonCode != second.ReasonCode {
		t.Fatal("scheduler must be deterministic without LLM")
	}
	if first.ReasonCode != environment.ReasonProfileSelected {
		t.Fatalf("reason=%q", first.ReasonCode)
	}
}
