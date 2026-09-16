package environment

import (
	"sort"
	"strings"
)

// Component statuses.
const (
	ComponentActive       = "active"
	ComponentExperimental = "experimental"
	ComponentPlanned      = "planned"
)

// Component is one reusable infrastructure capability required by an
// evaluation environment. Server-owned; agents never author this catalog.
type Component struct {
	ID              string `json:"id"`
	DisplayName     string `json:"display_name"`
	Description     string `json:"description"`
	ContractVersion string `json:"contract_version"`
	Status          string `json:"status"`
}

// componentCatalog is the small V1 server-owned catalog. AWS/Terraform/GCP
// exist only as future extension points and are NOT active components.
var componentCatalog = []Component{
	{ID: "kubernetes", DisplayName: "Kubernetes", Description: "Kubernetes workload orchestration for evaluation workloads.", ContractVersion: ComponentCatalogVersion, Status: ComponentActive},
	{ID: "prometheus", DisplayName: "Prometheus", Description: "Metrics telemetry backing request, error, and latency observables.", ContractVersion: ComponentCatalogVersion, Status: ComponentActive},
	{ID: "git", DisplayName: "Git", Description: "Version-controlled desired state for GitOps evaluation tasks.", ContractVersion: ComponentCatalogVersion, Status: ComponentActive},
	{ID: "argocd", DisplayName: "Argo CD", Description: "GitOps continuous delivery for drift and sync evaluation tasks.", ContractVersion: ComponentCatalogVersion, Status: ComponentExperimental},
	{ID: "helm", DisplayName: "Helm", Description: "Package management for GitOps delivery evaluation tasks.", ContractVersion: ComponentCatalogVersion, Status: ComponentExperimental},
	{ID: "argo_rollouts", DisplayName: "Argo Rollouts", Description: "Progressive delivery for rollout evaluation tasks where supported.", ContractVersion: ComponentCatalogVersion, Status: ComponentPlanned},
}

// ListComponents returns the V1 component catalog in stable ID order.
func ListComponents() []Component {
	out := append([]Component(nil), componentCatalog...)
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out
}

// GetComponent returns the catalog entry for id.
func GetComponent(id string) (Component, bool) {
	for _, item := range componentCatalog {
		if item.ID == id {
			return item, true
		}
	}
	return Component{}, false
}

// ValidateComponentID rejects blank and unknown component IDs.
func ValidateComponentID(id string) (string, error) {
	trimmed := strings.TrimSpace(id)
	if trimmed == "" {
		return "", errValidation("environment component must not be blank")
	}
	if _, ok := GetComponent(trimmed); !ok {
		return "", errValidation("unknown environment component " + quote(trimmed) + ": select from the server component catalog")
	}
	return trimmed, nil
}

// Profile statuses.
const (
	ProfileActive       = "active"
	ProfileExperimental = "experimental"
	ProfilePlanned      = "planned"
)

// Profile is a versioned server-owned composition of components plus an
// isolation/fidelity tier. Historical runs/certifications pin the exact
// profile ID + contract version used.
type Profile struct {
	ID                 string   `json:"id"`
	DisplayName        string   `json:"display_name"`
	Description        string   `json:"description"`
	ContractVersion    string   `json:"contract_version"`
	Components         []string `json:"components"`
	Isolation          string   `json:"isolation_tier"`
	Status             string   `json:"status"`
	Priority           int      `json:"priority_weight"`
	RuntimeContract    string   `json:"runtime_contract_version"`
	ProviderConstraint string   `json:"provider_constraint,omitempty"`
}

// Profile IDs (versioned).
const (
	ProfileKubernetesNamespace      = "kubernetes.namespace:v1"
	ProfileKubernetesVirtualCluster = "kubernetes.virtual_cluster:v1"
	ProfileKubernetesDedicated      = "kubernetes.dedicated:v1"
	ProfileGitOpsArgoCD             = "gitops.argocd:v1"
)

// DefaultRuntimeContract is the runtime contract served by the V1 development
// provider. Derived from the provider/profile, never caller text.
const DefaultRuntimeContract = "kubernetes-traffic-spike:v1"

// profileCatalog is the small V1 server-owned catalog. Truthful V1 status:
// only kubernetes.namespace is active (current development runtime).
// Everything else is planned/experimental until a real provider exists.
var profileCatalog = []Profile{
	{
		ID:              ProfileKubernetesNamespace,
		DisplayName:     "Kubernetes Namespace",
		Description:     "Ordinary application/workload evaluations inside an existing/shared Kubernetes cluster.",
		ContractVersion: ProfileCatalogVersion,
		Components:      []string{"kubernetes", "prometheus"},
		Isolation:       IsolationNamespace,
		Status:          ProfileActive,
		Priority:        10,
		RuntimeContract: DefaultRuntimeContract,
	},
	{
		ID:              ProfileKubernetesVirtualCluster,
		DisplayName:     "Kubernetes Virtual Cluster",
		Description:     "Tasks requiring stronger Kubernetes control-plane isolation without a whole independent cloud cluster.",
		ContractVersion: ProfileCatalogVersion,
		Components:      []string{"kubernetes", "prometheus"},
		Isolation:       IsolationVirtualCluster,
		Status:          ProfilePlanned,
		Priority:        20,
		RuntimeContract: DefaultRuntimeContract,
	},
	{
		ID:              ProfileKubernetesDedicated,
		DisplayName:     "Dedicated Kubernetes",
		Description:     "Node/cluster/cloud-controller/CNI/Karpenter/CoreDNS-class scenarios requiring an independent cluster lifecycle.",
		ContractVersion: ProfileCatalogVersion,
		Components:      []string{"kubernetes", "prometheus"},
		Isolation:       IsolationDedicatedCluster,
		Status:          ProfilePlanned,
		Priority:        30,
		RuntimeContract: DefaultRuntimeContract,
	},
	{
		ID:              ProfileGitOpsArgoCD,
		DisplayName:     "GitOps with Argo CD",
		Description:     "GitOps drift and sync evaluations. Planned/experimental until a real Argo CD provider exists; never claimed available.",
		ContractVersion: ProfileCatalogVersion,
		Components:      []string{"git", "argocd", "kubernetes", "prometheus", "helm"},
		Isolation:       IsolationNamespace,
		Status:          ProfilePlanned,
		Priority:        40,
		RuntimeContract: DefaultRuntimeContract,
	},
}

// ListProfiles returns the V1 profile catalog in deterministic priority order
// (least-fidelity adequate first), then ID order.
func ListProfiles() []Profile {
	out := append([]Profile(nil), profileCatalog...)
	sort.Slice(out, func(i, j int) bool {
		if out[i].Priority != out[j].Priority {
			return out[i].Priority < out[j].Priority
		}
		return out[i].ID < out[j].ID
	})
	return out
}

// GetProfile returns the catalog entry for id.
func GetProfile(id string) (Profile, bool) {
	for _, item := range profileCatalog {
		if item.ID == id {
			return item, true
		}
	}
	return Profile{}, false
}

// HasComponent reports whether the profile composition includes component.
func (p Profile) HasComponent(component string) bool {
	for _, c := range p.Components {
		if c == component {
			return true
		}
	}
	return false
}

// Executable reports whether the profile may be selected for normal
// execution (active only; planned/experimental never auto-selected).
func (p Profile) Executable() bool { return p.Status == ProfileActive }

// AllowedRuntimeContracts is the server-owned allowlist for certification and
// run execution. Caller text outside this set is rejected.
func AllowedRuntimeContracts() []string { return []string{DefaultRuntimeContract} }

// IsAllowedRuntimeContract reports whether runtime is server-known.
func IsAllowedRuntimeContract(runtime string) bool {
	for _, allowed := range AllowedRuntimeContracts() {
		if runtime == allowed {
			return true
		}
	}
	return false
}

func quote(value string) string { return "\"" + value + "\"" }
