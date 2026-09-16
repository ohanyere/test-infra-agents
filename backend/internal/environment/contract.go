// Package environment owns the composable execution-environment model.
//
// Architecture:
//
//	Environment Components → Environment Profiles → Environment Providers
//	→ Environment Scheduler → Provisioned Environment (EnvironmentInstance)
//
// This stage is the architecture/model/scheduling layer. It does NOT
// implement AWS, production EKS, vCluster installation, Terraform, or any
// cloud control plane. Future providers (shared-eks, vcluster-eks,
// dedicated-eks, aws-sandbox) can register against the same provider
// abstraction without rewriting the evaluation core.
//
// No LLM is used anywhere in this package. All matching is deterministic.
package environment

const (
	// ComponentCatalogVersion pins the server-owned component catalog.
	ComponentCatalogVersion = "environment-component-catalog:v1"

	// ProfileCatalogVersion pins the server-owned profile catalog.
	ProfileCatalogVersion = "environment-profile-catalog:v1"

	// ProviderContractVersion pins the provider interface contract.
	ProviderContractVersion = "environment-provider-contract:v1"

	// SchedulerContractVersion pins scheduler matching semantics.
	SchedulerContractVersion = "environment-scheduler-contract:v1"
)

// Isolation tiers. Ordering is namespace < virtual_cluster <
// dedicated_cluster. A stronger profile may satisfy a weaker minimum; a
// weaker profile can never satisfy a stronger requirement.
const (
	IsolationNamespace        = "namespace"
	IsolationVirtualCluster   = "virtual_cluster"
	IsolationDedicatedCluster = "dedicated_cluster"
)

// IsolationSemantics documents what each tier guarantees.
var IsolationSemantics = map[string]string{
	IsolationNamespace:        "shared control plane, workload isolation through namespace/RBAC/network/quota",
	IsolationVirtualCluster:   "isolated logical Kubernetes control plane on shared physical infrastructure",
	IsolationDedicatedCluster: "independent Kubernetes cluster lifecycle",
}

// IsolationRank returns the deterministic fidelity rank. Unknown tiers
// return -1 and never match.
func IsolationRank(tier string) int {
	switch tier {
	case IsolationNamespace:
		return 0
	case IsolationVirtualCluster:
		return 1
	case IsolationDedicatedCluster:
		return 2
	default:
		return -1
	}
}

// IsValidIsolation reports whether tier is a known isolation tier.
func IsValidIsolation(tier string) bool { return IsolationRank(tier) >= 0 }

// SatisfiesIsolation reports whether profileIsolation meets (is at least as
// strong as) minimum.
func SatisfiesIsolation(profileIsolation, minimum string) bool {
	profile, required := IsolationRank(profileIsolation), IsolationRank(minimum)
	if profile < 0 || required < 0 {
		return false
	}
	return profile >= required
}

// Environment instance states. Machine-readable only; never display
// punctuation. destroyed is terminal and can never return to ready.
const (
	StateProvisioning = "provisioning"
	StateReady        = "ready"
	StateResetting    = "resetting"
	StateDestroying   = "destroying"
	StateDestroyed    = "destroyed"
	StateFailed       = "failed"
)

// ValidStateTransition reports whether moving from → to is allowed.
// Destroy is idempotent (destroyed → destroying → destroyed stays valid so
// repeated destroy calls succeed). Nothing may leave destroyed except
// through the destroy path.
func ValidStateTransition(from, to string) bool {
	switch from {
	case StateProvisioning:
		switch to {
		case StateReady, StateFailed, StateDestroying, StateDestroyed:
			return true
		}
	case StateReady:
		switch to {
		case StateResetting, StateDestroying, StateDestroyed, StateFailed:
			return true
		}
	case StateResetting:
		switch to {
		case StateReady, StateFailed, StateDestroying, StateDestroyed:
			return true
		}
	case StateDestroying:
		switch to {
		case StateDestroyed, StateFailed:
			return true
		}
	case StateDestroyed:
		// Idempotent repeated destroy only.
		switch to {
		case StateDestroying, StateDestroyed:
			return true
		}
	case StateFailed:
		switch to {
		case StateDestroying, StateDestroyed:
			return true
		}
	}
	return false
}

// IsStoredState reports whether value is a persisted instance state.
func IsStoredState(value string) bool {
	switch value {
	case StateProvisioning, StateReady, StateResetting, StateDestroying, StateDestroyed, StateFailed:
		return true
	default:
		return false
	}
}

// Scheduler reason codes. Stable, machine-readable; never UI prose.
const (
	ReasonProfileSelected         = "profile_selected"
	ReasonMissingComponent        = "missing_required_component"
	ReasonInsufficientIsolation   = "insufficient_isolation"
	ReasonProfileNotActive        = "profile_not_active"
	ReasonProviderUnavailable     = "provider_unavailable"
	ReasonRuntimeUnsupported      = "runtime_contract_unsupported"
	ReasonNoEligibleEnvironment   = "no_eligible_environment"
	ReasonPreferenceUnsatisfiable = "preference_unsatisfiable"
)

// Owner types for EnvironmentInstance lifecycle.
const (
	OwnerRun           = "run"
	OwnerCertification = "certification"
)

// IsValidOwnerType reports whether owner type may own an environment.
func IsValidOwnerType(owner string) bool {
	switch owner {
	case OwnerRun, OwnerCertification:
		return true
	default:
		return false
	}
}
