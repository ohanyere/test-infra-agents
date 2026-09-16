package scenariocertification

import (
	"testing"

	"github.com/evalplane/evalplane/backend/internal/environment"
)

// 22. Certification uses server-owned profile: resolution derives placement
// from stored requirements (legacy default without a pool), never caller text.
func TestResolveEnvironmentServerSelection(t *testing.T) {
	svc := &Service{}
	decision, requirements, err := svc.resolveEnvironment(t.Context(), "11111111-1111-4111-8111-111111111111", "")
	if err != nil {
		t.Fatalf("server selection must resolve legacy requirements: %v", err)
	}
	if decision.Profile == nil || decision.Provider == nil {
		t.Fatalf("server selection must yield profile and provider")
	}
	if decision.Profile.ID != environment.ProfileKubernetesNamespace {
		t.Fatalf("legacy requirements must resolve to kubernetes.namespace:v1, got %s", decision.Profile.ID)
	}
	if decision.Provider.ID != environment.ProviderKindDev {
		t.Fatalf("V1 resolution must select kind-dev, got %s", decision.Provider.ID)
	}
	if requirements.MinimumIsolation != environment.IsolationNamespace {
		t.Fatalf("legacy requirements must claim namespace only")
	}
}

// 23. Arbitrary certification profile rejected: unknown, planned, and
// unsatisfiable preferences all fail instead of inventing placement.
func TestResolveEnvironmentRejectsArbitraryProfile(t *testing.T) {
	svc := &Service{}
	for _, preference := range []string{
		"aws-sandbox:v9",
		"custom-profile:v1",
		environment.ProfileGitOpsArgoCD,        // planned, not available
		environment.ProfileKubernetesDedicated, // planned, not available
	} {
		if _, _, err := svc.resolveEnvironment(t.Context(), "11111111-1111-4111-8111-111111111111", preference); err == nil {
			t.Fatalf("preference %q must be rejected", preference)
		}
	}
}

// A known active profile satisfying requirements is accepted as preference.
func TestResolveEnvironmentAcceptsSatisfyingPreference(t *testing.T) {
	svc := &Service{}
	decision, _, err := svc.resolveEnvironment(t.Context(), "11111111-1111-4111-8111-111111111111", environment.ProfileKubernetesNamespace)
	if err != nil {
		t.Fatalf("satisfying preference must resolve: %v", err)
	}
	if decision.Profile == nil || decision.Profile.ID != environment.ProfileKubernetesNamespace {
		t.Fatalf("preference must select kubernetes.namespace:v1")
	}
}

// 24. Arbitrary runtime contract rejected: only server-owned contracts pass.
func TestArbitraryRuntimeContractRejected(t *testing.T) {
	if environment.IsAllowedRuntimeContract("custom-runtime:v9") {
		t.Fatalf("arbitrary runtime contracts must be rejected")
	}
	if !environment.IsAllowedRuntimeContract(DefaultRuntimeContractVersion) {
		t.Fatalf("default runtime contract must stay allowed")
	}
}

// 25. Certification pins provider/profile/isolation: fresh pins carry all
// five environment contracts and stay eligible under matching requirements.
func TestCertificationPinsEnvironmentContracts(t *testing.T) {
	latest := Certification{
		State:                               StateCertified,
		CertificationContractVersion:        CertificationContractVersion,
		ReferenceRemediationContractVersion: ReferenceRemediationContractVersion,
		ReferenceContentHash:                "h",
		EvaluatorVersion:                    EvaluatorVersion,
		RuntimeContractVersion:              DefaultRuntimeContractVersion,
		EnvironmentProfile:                  environment.ProfileKubernetesNamespace,
		ProviderID:                          environment.ProviderKindDev,
		ProviderContractVersion:             environment.ProviderContractVersion,
		IsolationTier:                       environment.IsolationNamespace,
		EnvironmentProfileContractVersion:   environment.ProfileCatalogVersion,
		EnvironmentComponentContractVersion: environment.ComponentCatalogVersion,
		CriteriaVersion:                     "c",
	}
	reasons := staleReasonsFor(latest, "c", "h", nil, environment.LegacyDefaultRequirements())
	if len(reasons) != 0 {
		t.Fatalf("fully pinned fresh certification must not be stale: %v", reasons)
	}
}

// 26. Certification stale when required environment contract changes:
// dedicated-class requirements invalidate a namespace pin.
func TestCertificationStaleOnIsolationChange(t *testing.T) {
	latest := Certification{
		State:                               StateCertified,
		CertificationContractVersion:        CertificationContractVersion,
		ReferenceRemediationContractVersion: ReferenceRemediationContractVersion,
		ReferenceContentHash:                "h",
		EvaluatorVersion:                    EvaluatorVersion,
		RuntimeContractVersion:              DefaultRuntimeContractVersion,
		EnvironmentProfile:                  environment.ProfileKubernetesNamespace,
		ProviderID:                          environment.ProviderKindDev,
		ProviderContractVersion:             environment.ProviderContractVersion,
		IsolationTier:                       environment.IsolationNamespace,
		EnvironmentProfileContractVersion:   environment.ProfileCatalogVersion,
		EnvironmentComponentContractVersion: environment.ComponentCatalogVersion,
		CriteriaVersion:                     "c",
	}
	dedicated := environment.Requirements{
		RequiredComponents: []string{"kubernetes", "prometheus"},
		MinimumIsolation:   environment.IsolationDedicatedCluster,
		RuntimeContract:    environment.DefaultRuntimeContract,
		ContractVersion:    environment.RequirementsContractVersion,
	}
	reasons := staleReasonsFor(latest, "c", "h", nil, dedicated)
	if len(reasons) == 0 {
		t.Fatalf("namespace certification must go stale when dedicated isolation is required")
	}
}

// Provider contract drift also requires re-certification.
func TestCertificationStaleOnProviderContractChange(t *testing.T) {
	latest := Certification{
		State:                               StateCertified,
		CertificationContractVersion:        CertificationContractVersion,
		ReferenceRemediationContractVersion: ReferenceRemediationContractVersion,
		ReferenceContentHash:                "h",
		EvaluatorVersion:                    EvaluatorVersion,
		RuntimeContractVersion:              DefaultRuntimeContractVersion,
		EnvironmentProfile:                  environment.ProfileKubernetesNamespace,
		ProviderID:                          environment.ProviderKindDev,
		ProviderContractVersion:             "environment-provider-contract:v0",
		IsolationTier:                       environment.IsolationNamespace,
		EnvironmentProfileContractVersion:   environment.ProfileCatalogVersion,
		EnvironmentComponentContractVersion: environment.ComponentCatalogVersion,
		CriteriaVersion:                     "c",
	}
	reasons := staleReasonsFor(latest, "c", "h", nil, environment.LegacyDefaultRequirements())
	if len(reasons) == 0 {
		t.Fatalf("provider contract drift must mark certification stale")
	}
}
