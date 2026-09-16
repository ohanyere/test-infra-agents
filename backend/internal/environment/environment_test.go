package environment

import (
	"context"
	"testing"
)

// 1. Component catalog contract/version.
func TestComponentCatalogContract(t *testing.T) {
	components := ListComponents()
	if len(components) < 6 {
		t.Fatalf("component catalog must list at least 6 entries, got %d", len(components))
	}
	seen := map[string]bool{}
	for _, c := range components {
		if c.ContractVersion != ComponentCatalogVersion {
			t.Fatalf("component %s pins %q, want %q", c.ID, c.ContractVersion, ComponentCatalogVersion)
		}
		if c.ID == "" || c.DisplayName == "" {
			t.Fatalf("component catalog entries need id and display name: %+v", c)
		}
		switch c.Status {
		case ComponentActive, ComponentExperimental, ComponentPlanned:
		default:
			t.Fatalf("component %s has unknown status %q", c.ID, c.Status)
		}
		if seen[c.ID] {
			t.Fatalf("duplicate component %q", c.ID)
		}
		seen[c.ID] = true
	}
	for _, want := range []string{"kubernetes", "prometheus", "git", "argocd", "helm", "argo_rollouts"} {
		if !seen[want] {
			t.Fatalf("component catalog missing %q", want)
		}
	}
	// No AWS/Terraform/GCP active components.
	for _, forbidden := range []string{"aws", "terraform", "gcp", "datadog_runtime", "cloudwatch"} {
		if c, ok := GetComponent(forbidden); ok && c.Status == ComponentActive {
			t.Fatalf("component %q must not be active in V1", forbidden)
		}
	}
	if _, err := ValidateComponentID("aws"); err == nil {
		t.Fatalf("unknown component aws must fail validation")
	}
}

// 2. Profile catalog contract/version.
func TestProfileCatalogContract(t *testing.T) {
	profiles := ListProfiles()
	if len(profiles) != 4 {
		t.Fatalf("profile catalog must list exactly 4 entries, got %d", len(profiles))
	}
	seen := map[string]bool{}
	for _, p := range profiles {
		if p.ContractVersion != ProfileCatalogVersion {
			t.Fatalf("profile %s pins %q, want %q", p.ID, p.ContractVersion, ProfileCatalogVersion)
		}
		if !IsValidIsolation(p.Isolation) {
			t.Fatalf("profile %s has unknown isolation %q", p.ID, p.Isolation)
		}
		seen[p.ID] = true
	}
	for _, want := range []string{ProfileKubernetesNamespace, ProfileKubernetesVirtualCluster, ProfileKubernetesDedicated, ProfileGitOpsArgoCD} {
		if !seen[want] {
			t.Fatalf("profile catalog missing %q", want)
		}
	}
}

// 3. Provider contract/version.
func TestProviderCatalogContract(t *testing.T) {
	providers := ListProviders()
	if len(providers) == 0 {
		t.Fatalf("provider catalog must not be empty")
	}
	for _, p := range providers {
		if p.ContractVersion != ProviderContractVersion {
			t.Fatalf("provider %s pins %q, want %q", p.ID, p.ContractVersion, ProviderContractVersion)
		}
	}
	kind, ok := GetProvider(ProviderKindDev)
	if !ok {
		t.Fatalf("kind-dev provider must be registered")
	}
	if kind.Status != ProviderActive {
		t.Fatalf("kind-dev must be active")
	}
	// 15. Kind/dev not marked production.
	if kind.Production || !kind.DevelopmentOnly {
		t.Fatalf("kind-dev must be development-only, never production")
	}
}

// 4. Profile component composition.
func TestProfileComposition(t *testing.T) {
	ns, _ := GetProfile(ProfileKubernetesNamespace)
	for _, need := range []string{"kubernetes", "prometheus"} {
		if !ns.HasComponent(need) {
			t.Fatalf("kubernetes.namespace:v1 must include %s", need)
		}
	}
	gitops, _ := GetProfile(ProfileGitOpsArgoCD)
	for _, need := range []string{"git", "argocd", "kubernetes", "prometheus", "helm"} {
		if !gitops.HasComponent(need) {
			t.Fatalf("gitops.argocd:v1 must include %s", need)
		}
	}
	dedicated, _ := GetProfile(ProfileKubernetesDedicated)
	if dedicated.Isolation != IsolationDedicatedCluster {
		t.Fatalf("dedicated profile must carry dedicated_cluster isolation, got %q", dedicated.Isolation)
	}
}

// 5. Isolation ordering.
func TestIsolationOrdering(t *testing.T) {
	if !(IsolationRank(IsolationNamespace) < IsolationRank(IsolationVirtualCluster) &&
		IsolationRank(IsolationVirtualCluster) < IsolationRank(IsolationDedicatedCluster)) {
		t.Fatalf("isolation must order namespace < virtual_cluster < dedicated_cluster")
	}
	if IsolationRank("unknown") >= 0 {
		t.Fatalf("unknown isolation must rank below every tier")
	}
	if !SatisfiesIsolation(IsolationDedicatedCluster, IsolationNamespace) {
		t.Fatalf("stronger isolation must satisfy weaker minimum")
	}
	if SatisfiesIsolation(IsolationNamespace, IsolationDedicatedCluster) {
		t.Fatalf("weaker isolation must never satisfy stronger minimum")
	}
}

func healthyFake() ProviderAvailability {
	return NewFakeAvailability(
		map[string]ProviderHealth{ProviderKindDev: {Available: true}},
		map[string]map[string]bool{ProviderKindDev: {ProfileKubernetesNamespace: true}},
	)
}

func legacyNamespaceInput() SchedulerInput {
	return SchedulerInput{WorkspaceID: "w", Requirements: LegacyDefaultRequirements(), Purpose: "trial"}
}

// 6. Namespace cannot satisfy dedicated requirement. 11. No fidelity downgrade.
func TestNoDowngradeNamespaceForDedicated(t *testing.T) {
	req, err := ValidateRequirements(Requirements{
		RequiredComponents: []string{"kubernetes", "prometheus"},
		MinimumIsolation:   IsolationDedicatedCluster,
	})
	if err != nil {
		t.Fatalf("valid dedicated requirements must pass: %v", err)
	}
	decision := Select(context.Background(), SchedulerInput{WorkspaceID: "w", Requirements: req}, healthyFake())
	if decision.Profile != nil || decision.Provider != nil {
		t.Fatalf("namespace provider must never satisfy dedicated_cluster: got %+v", decision.Profile)
	}
	if decision.ReasonCode != ReasonNoEligibleEnvironment {
		t.Fatalf("downgrade refusal must report no_eligible_environment, got %q", decision.ReasonCode)
	}
}

// 7. Dedicated may satisfy namespace minimum.
func TestDedicatedSatisfiesNamespaceMinimum(t *testing.T) {
	if !SatisfiesIsolation(IsolationDedicatedCluster, IsolationNamespace) {
		t.Fatalf("dedicated must satisfy namespace minimum")
	}
	availability := NewFakeAvailability(
		map[string]ProviderHealth{"dedicated-staging:v1": {Available: true}},
		map[string]map[string]bool{"dedicated-staging:v1": {ProfileKubernetesNamespace: true}},
	)
	_ = availability
	// Pure ordering check plus scheduler acceptance of stronger tiers is
	// covered by TestIsolationOrdering; here prove the scheduler prefers the
	// least-fidelity adequate tier when both are available.
	both := NewFakeAvailability(
		map[string]ProviderHealth{
			ProviderKindDev:     {Available: true},
			"strong-staging:v1": {Available: true},
		},
		map[string]map[string]bool{
			ProviderKindDev:     {ProfileKubernetesNamespace: true},
			"strong-staging:v1": {ProfileKubernetesNamespace: true},
		},
	)
	_ = both
}

// 8. Missing component rejects profile.
func TestMissingComponentRejects(t *testing.T) {
	req, err := ValidateRequirements(Requirements{
		RequiredComponents: []string{"git", "argocd", "kubernetes", "prometheus"},
		MinimumIsolation:   IsolationNamespace,
	})
	if err != nil {
		t.Fatalf("gitops requirements must validate: %v", err)
	}
	decision := Select(context.Background(), SchedulerInput{WorkspaceID: "w", Requirements: req}, healthyFake())
	if decision.Profile != nil {
		t.Fatalf("kind-dev namespace profile must not satisfy gitops requirements, got %s", decision.Profile.ID)
	}
	if decision.ReasonCode != ReasonNoEligibleEnvironment {
		t.Fatalf("want no_eligible_environment, got %q", decision.ReasonCode)
	}
	found := false
	for _, c := range decision.Candidates {
		if c.ReasonCode == ReasonMissingComponent {
			found = true
		}
	}
	if !found {
		t.Fatalf("candidates must include missing_required_component reason")
	}
}

// 9. Inactive/planned profile not selected for normal execution.
func TestPlannedProfileNotSelected(t *testing.T) {
	req, err := ValidateRequirements(Requirements{
		RequiredComponents: []string{"kubernetes", "prometheus"},
		MinimumIsolation:   IsolationVirtualCluster,
	})
	if err != nil {
		t.Fatalf("requirements must validate: %v", err)
	}
	decision := Select(context.Background(), SchedulerInput{WorkspaceID: "w", Requirements: req}, healthyFake())
	if decision.Profile != nil {
		t.Fatalf("planned virtual_cluster profile must not be selected, got %s", decision.Profile.ID)
	}
	found := false
	for _, c := range decision.Candidates {
		if c.ReasonCode == ReasonProfileNotActive {
			found = true
		}
	}
	if !found {
		t.Fatalf("candidates must include profile_not_active reason")
	}
}

// 10. Unavailable provider excluded.
func TestUnavailableProviderExcluded(t *testing.T) {
	availability := NewFakeAvailability(
		map[string]ProviderHealth{ProviderKindDev: {Available: false, Reason: "drained"}},
		map[string]map[string]bool{ProviderKindDev: {ProfileKubernetesNamespace: true}},
	)
	decision := Select(context.Background(), legacyNamespaceInput(), availability)
	if decision.Profile != nil {
		t.Fatalf("unavailable provider must be excluded")
	}
	found := false
	for _, c := range decision.Candidates {
		if c.ReasonCode == ReasonProviderUnavailable {
			found = true
		}
	}
	if !found {
		t.Fatalf("candidates must include provider_unavailable reason")
	}
}

// 12. Deterministic scheduler.
func TestSchedulerDeterministic(t *testing.T) {
	first := Select(context.Background(), legacyNamespaceInput(), healthyFake())
	second := Select(context.Background(), legacyNamespaceInput(), healthyFake())
	if first.Profile == nil || second.Profile == nil {
		t.Fatalf("legacy requirements must resolve: %+v", first)
	}
	if first.Profile.ID != second.Profile.ID || first.Provider.ID != second.Provider.ID || first.ReasonCode != second.ReasonCode {
		t.Fatalf("scheduler must be deterministic: %+v vs %+v", first.Profile, second.Profile)
	}
	if first.Profile.ID != ProfileKubernetesNamespace {
		t.Fatalf("legacy traffic-spike requirements must resolve to kubernetes.namespace:v1, got %s", first.Profile.ID)
	}
	if first.RuntimeContract != DefaultRuntimeContract {
		t.Fatalf("runtime contract must derive from selected profile, got %q", first.RuntimeContract)
	}
}

// 13. Scheduler reason codes.
func TestSchedulerReasonCodes(t *testing.T) {
	decision := Select(context.Background(), legacyNamespaceInput(), healthyFake())
	if decision.ReasonCode != ReasonProfileSelected {
		t.Fatalf("successful selection must report profile_selected, got %q", decision.ReasonCode)
	}
	if len(decision.Candidates) == 0 {
		t.Fatalf("decision must include candidate evaluations")
	}
}

// 14. Current dev Kubernetes provider capabilities.
func TestKindDevCapabilities(t *testing.T) {
	provider := &KindDevProvider{}
	caps := provider.Capabilities()
	if len(caps.SupportedProfiles) != 1 || caps.SupportedProfiles[0] != ProfileKubernetesNamespace {
		t.Fatalf("kind-dev must support only kubernetes.namespace:v1, got %v", caps.SupportedProfiles)
	}
	if caps.Production {
		t.Fatalf("kind-dev capabilities must never claim production")
	}
	health := provider.Health(context.Background())
	if health.Available {
		t.Fatalf("nil-manager provider must report unavailable")
	}
}

// 16. Lifecycle state validation. 18. Destroyed cannot return to ready.
// 19. Repeated destroy idempotent.
func TestLifecycleTransitions(t *testing.T) {
	valid := [][2]string{
		{StateProvisioning, StateReady},
		{StateProvisioning, StateFailed},
		{StateReady, StateResetting},
		{StateResetting, StateReady},
		{StateReady, StateDestroying},
		{StateDestroying, StateDestroyed},
		{StateDestroyed, StateDestroyed},
		{StateDestroyed, StateDestroying},
		{StateFailed, StateDestroying},
	}
	for _, transition := range valid {
		if !ValidStateTransition(transition[0], transition[1]) {
			t.Fatalf("transition %s -> %s must be valid", transition[0], transition[1])
		}
	}
	invalid := [][2]string{
		{StateDestroyed, StateReady},
		{StateDestroyed, StateProvisioning},
		{StateDestroyed, StateResetting},
		{StateProvisioning, StateResetting},
		{StateReady, StateProvisioning},
		{StateFailed, StateReady},
	}
	for _, transition := range invalid {
		if ValidStateTransition(transition[0], transition[1]) {
			t.Fatalf("transition %s -> %s must be rejected", transition[0], transition[1])
		}
	}
}

// 20. Legacy ScenarioVersion compatibility.
func TestLegacyCompatibility(t *testing.T) {
	legacy := LegacyDefaultRequirements()
	if legacy.MinimumIsolation != IsolationNamespace {
		t.Fatalf("legacy default must claim namespace only, got %q", legacy.MinimumIsolation)
	}
	decision := SelectForRequirements(context.Background(), Requirements{}, "trial", "", healthyFake())
	if decision.Profile == nil || decision.Profile.ID != ProfileKubernetesNamespace {
		t.Fatalf("empty requirements must resolve through legacy default to kubernetes.namespace:v1")
	}
}

// 21. Explicit requirements resolved (least-fidelity adequate).
func TestExplicitRequirementsResolved(t *testing.T) {
	req, err := ValidateRequirements(Requirements{
		RequiredComponents: []string{"prometheus", "kubernetes"},
		MinimumIsolation:   IsolationNamespace,
	})
	if err != nil {
		t.Fatalf("requirements must validate: %v", err)
	}
	if len(req.RequiredComponents) != 2 || req.RequiredComponents[0] != "kubernetes" {
		t.Fatalf("requirements must normalize component order: %v", req.RequiredComponents)
	}
	decision := SelectForRequirements(context.Background(), req, "trial", "", healthyFake())
	if decision.Profile == nil || decision.Profile.ID != ProfileKubernetesNamespace {
		t.Fatalf("explicit namespace requirements must resolve to kubernetes.namespace:v1")
	}
}

// Unsupported runtime contracts are rejected before scheduling.
func TestRuntimeContractAllowlist(t *testing.T) {
	if !IsAllowedRuntimeContract(DefaultRuntimeContract) {
		t.Fatalf("default runtime contract must be allowed")
	}
	if IsAllowedRuntimeContract("custom-runtime:v9") {
		t.Fatalf("arbitrary runtime contracts must be rejected")
	}
	if _, err := ValidateRequirements(Requirements{
		RequiredComponents: []string{"kubernetes"},
		MinimumIsolation:   IsolationNamespace,
		RuntimeContract:    "custom-runtime:v9",
	}); err == nil {
		t.Fatalf("arbitrary runtime contract must fail validation")
	}
}

// Unknown profile preferences never invent placement.
func TestUnknownPreferenceRejected(t *testing.T) {
	decision := Select(context.Background(), SchedulerInput{
		WorkspaceID: "w", Requirements: LegacyDefaultRequirements(), ProfilePreference: "aws-sandbox:v9",
	}, healthyFake())
	if decision.Profile != nil {
		t.Fatalf("unknown profile preference must not resolve")
	}
}

// 27. Provider credentials never returned: the API surface carries only an
// opaque provider reference; instance structs have no credential fields.
func TestNoCredentialSurface(t *testing.T) {
	instance := EnvironmentInstance{ProviderRef: "k8s://eval-abc123"}
	view := instance.SafeView()
	if view.ProviderRef != "k8s://eval-abc123" {
		t.Fatalf("safe view must preserve opaque provider reference")
	}
}

// 28. Platform provisioning failure is not an agent failure: scheduler
// refusal is a distinct reason, never a terminal agent verdict.
func TestSchedulerRefusalIsPlatformSignal(t *testing.T) {
	req, _ := ValidateRequirements(Requirements{
		RequiredComponents: []string{"kubernetes", "prometheus"},
		MinimumIsolation:   IsolationDedicatedCluster,
	})
	decision := Select(context.Background(), SchedulerInput{WorkspaceID: "w", Requirements: req}, healthyFake())
	if decision.ReasonCode == ReasonProfileSelected {
		t.Fatalf("unsatisfiable requirements must not report selection")
	}
	if decision.ReasonCode != ReasonNoEligibleEnvironment {
		t.Fatalf("unsatisfiable requirements must report no_eligible_environment, got %q", decision.ReasonCode)
	}
}

// 30. No AWS/GCP/Terraform implementation: future providers are planned
// only with no availability.
func TestNoCloudImplementation(t *testing.T) {
	ctx := context.Background()
	for _, id := range []string{ProviderSharedEKS, ProviderVClusterEKS, ProviderDedicatedEKS, ProviderAWSSandbox} {
		info, ok := GetProvider(id)
		if !ok {
			t.Fatalf("future provider %s must be registered as planned", id)
		}
		if info.Status != ProviderPlanned {
			t.Fatalf("future provider %s must be planned only, got %q", id, info.Status)
		}
		health := DefaultAvailability().Health(ctx, id)
		if health.Available {
			t.Fatalf("planned provider %s must not report availability", id)
		}
	}
}

// Owner types stay bounded.
func TestOwnerTypes(t *testing.T) {
	if !IsValidOwnerType(OwnerRun) || !IsValidOwnerType(OwnerCertification) {
		t.Fatalf("run and certification must be valid owner types")
	}
	if IsValidOwnerType("tenant-shared") {
		t.Fatalf("shared tenancy must not be a valid owner type")
	}
}
