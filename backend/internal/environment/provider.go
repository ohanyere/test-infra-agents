package environment

import (
	"context"
	"sort"
	"time"
)

// Provider statuses.
const (
	ProviderActive  = "active"
	ProviderPlanned = "planned"
)

// ProviderInfo is the server-owned provider catalog entry. Health and
// availability are real and observed; planned providers never report fake
// health.
type ProviderInfo struct {
	ID                string   `json:"id"`
	DisplayName       string   `json:"display_name"`
	Description       string   `json:"description"`
	ContractVersion   string   `json:"contract_version"`
	Status            string   `json:"status"`
	SupportedProfiles []string `json:"supported_profiles"`
	Production        bool     `json:"production"`
	DevelopmentOnly   bool     `json:"development_only"`
}

// Provider IDs.
const (
	ProviderKindDev      = "kind-dev:v1"
	ProviderSharedEKS    = "shared-eks:v1"
	ProviderVClusterEKS  = "vcluster-eks:v1"
	ProviderDedicatedEKS = "dedicated-eks:v1"
	ProviderAWSSandbox   = "aws-sandbox:v1"
)

// providerCatalog lists the development provider as available and future
// production providers as planned only (no fake health/availability).
var providerCatalog = []ProviderInfo{
	{
		ID:                ProviderKindDev,
		DisplayName:       "Kind development",
		Description:       "Local Kind development runtime. Development/focused testing only; not production qualification infrastructure.",
		ContractVersion:   ProviderContractVersion,
		Status:            ProviderActive,
		SupportedProfiles: []string{ProfileKubernetesNamespace},
		Production:        false,
		DevelopmentOnly:   true,
	},
	{ID: ProviderSharedEKS, DisplayName: "Shared EKS", Description: "Future shared production cluster provider.", ContractVersion: ProviderContractVersion, Status: ProviderPlanned, SupportedProfiles: []string{ProfileKubernetesNamespace}},
	{ID: ProviderVClusterEKS, DisplayName: "Virtual cluster EKS", Description: "Future virtual-cluster production provider.", ContractVersion: ProviderContractVersion, Status: ProviderPlanned, SupportedProfiles: []string{ProfileKubernetesVirtualCluster}},
	{ID: ProviderDedicatedEKS, DisplayName: "Dedicated EKS", Description: "Future dedicated-cluster production provider.", ContractVersion: ProviderContractVersion, Status: ProviderPlanned, SupportedProfiles: []string{ProfileKubernetesDedicated}},
	{ID: ProviderAWSSandbox, DisplayName: "AWS sandbox", Description: "Future cloud sandbox provider.", ContractVersion: ProviderContractVersion, Status: ProviderPlanned},
}

// ListProviders returns the provider catalog in stable ID order.
func ListProviders() []ProviderInfo {
	out := append([]ProviderInfo(nil), providerCatalog...)
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out
}

// GetProvider returns the catalog entry for id.
func GetProvider(id string) (ProviderInfo, bool) {
	for _, item := range providerCatalog {
		if item.ID == id {
			return item, true
		}
	}
	return ProviderInfo{}, false
}

// ProvisionRequest is what the core execution engine asks for: profile,
// workspace, run/certification ownership, task requirements, and
// lease/lifecycle metadata. Cloud-specific details (Terraform resource
// names, AWS account IDs, Kind internals) are never part of this request;
// providers may return safe opaque metadata only.
type ProvisionRequest struct {
	ProfileID   string
	WorkspaceID string
	OwnerType   string
	// OwnerID is the run ID for run owners or the certification attempt ID
	// for certification owners.
	OwnerID string
	// BatchID carries run ownership for namespaced providers (part of the
	// run identity labels). Empty for certification owners.
	BatchID      string
	Requirements Requirements
	LeaseTTL     time.Duration
}

// ProviderCapabilities describes what a provider can do.
type ProviderCapabilities struct {
	SupportedProfiles []string `json:"supported_profiles"`
	RuntimeContracts  []string `json:"runtime_contracts"`
	Production        bool     `json:"production"`
}

// ProviderHealth is observed liveness, never fabricated for planned providers.
type ProviderHealth struct {
	Available bool   `json:"available"`
	Reason    string `json:"reason,omitempty"`
}

// Provider is the implementation capable of provisioning a profile. Names
// adapt to the existing architecture: Provision/Get/Reset/Destroy plus
// Capabilities/Health. Providers must not expose cloud internals to core.
type Provider interface {
	Info() ProviderInfo
	Capabilities() ProviderCapabilities
	Health(ctx context.Context) ProviderHealth
	Provision(ctx context.Context, req ProvisionRequest) (opaqueRef string, err error)
	Get(ctx context.Context, environmentID string) (opaqueRef string, err error)
	Reset(ctx context.Context, environmentID string) error
	Destroy(ctx context.Context, environmentID string) error
}

// EnvironmentInstance is the canonical provisioned environment. Provider
// references are safe opaque handles only: never kubeconfigs,
// service-account tokens, cloud credentials, or provider secrets.
type EnvironmentInstance struct {
	ID                      string     `json:"id"`
	WorkspaceID             string     `json:"workspace_id"`
	OwnerType               string     `json:"owner_type"`
	OwnerID                 string     `json:"owner_id"`
	ProfileID               string     `json:"profile_id"`
	ProfileContractVersion  string     `json:"profile_contract_version"`
	ProviderID              string     `json:"provider_id"`
	ProviderContractVersion string     `json:"provider_contract_version"`
	IsolationTier           string     `json:"isolation_tier"`
	Status                  string     `json:"status"`
	LeaseExpiresAt          *time.Time `json:"lease_expires_at,omitempty"`
	CreatedAt               time.Time  `json:"created_at"`
	ReadyAt                 *time.Time `json:"ready_at,omitempty"`
	DestroyedAt             *time.Time `json:"destroyed_at,omitempty"`
	// ProviderRef is a safe opaque handle (e.g. namespace reference).
	ProviderRef string `json:"provider_ref"`
}

// SafeView returns the API-safe projection: opaque provider reference only,
// never credentials.
func (e EnvironmentInstance) SafeView() EnvironmentInstance { return e }
