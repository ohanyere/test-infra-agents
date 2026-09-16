// Package competency owns the versioned server-owned semantic capability
// catalog for agent capability claims (CLAIMED only).
//
// Three concepts stay distinct:
//   - Competency capability (this package): what an AgentVersion claims it can
//     accomplish, e.g. kubernetes.capacity.remediation.
//   - Tool/runtime capability (agentcapability): low-level observations/actions
//     controlling compatibility and tool exposure.
//   - Qualification status: derived later from evidence/coverage/policy, never
//     stored as a customer-editable flag and never implied by a claim.
//
// Catalog contract: infrastructure-capability-catalog:v1.
// Domain -> Capability Family -> Capability. V1 is intentionally small; AWS,
// Terraform, and GCP are deferred and must not appear here.
package competency

import (
	"sort"

	"github.com/evalplane/evalplane/backend/internal/runtimecontract"
)

const (
	// ContractVersion identifies the semantic catalog contract. Historical
	// AgentVersion claims record this value so catalog v2 never rewrites them.
	ContractVersion = runtimecontract.InfrastructureCapabilityCatalogVersion

	StatusActive       = "active"
	StatusExperimental = "experimental"
	StatusPlanned      = "planned"

	// ClaimStatusClaimed is the only claim state exposed in V1. Coverage may
	// later augment with tested/qualified/not_qualified/inconclusive/untested,
	// but those states must come from the coverage engine, never from a claim.
	ClaimStatusClaimed = "claimed"
)

// Capability is server-owned metadata for one competency. Customer marketing
// prose is never stored here.
type Capability struct {
	ID             string `json:"id"`
	Domain         string `json:"domain"`
	Family         string `json:"family"`
	DisplayName    string `json:"display_name"`
	Description    string `json:"description"`
	CatalogVersion string `json:"catalog_version"`
	Status         string `json:"status"`
	// Requires lists minimum low-level runtime capabilities (agentcapability
	// vocabulary) needed for the claim to be compatible. This is tool/runtime
	// compatibility only, not scenario coverage.
	Requires []string `json:"requires"`
}

var catalog = []Capability{
	{
		ID: "kubernetes.workload.diagnosis", Domain: "Kubernetes", Family: "Workload Operations",
		DisplayName: "Workload diagnosis", Description: "Diagnose unhealthy or degraded Kubernetes workloads from workloads, pods, and events.",
		CatalogVersion: ContractVersion, Status: StatusActive,
		Requires: []string{"inspect", "kubernetes_workloads", "kubernetes_pods"},
	},
	{
		ID: "kubernetes.workload.remediation", Domain: "Kubernetes", Family: "Workload Operations",
		DisplayName: "Workload remediation", Description: "Remediate degraded Kubernetes workloads with bounded restart-class actions.",
		CatalogVersion: ContractVersion, Status: StatusActive,
		Requires: []string{"inspect", "kubernetes_workloads", "restart_pod"},
	},
	{
		ID: "kubernetes.capacity.diagnosis", Domain: "Kubernetes", Family: "Capacity",
		DisplayName: "Capacity diagnosis", Description: "Diagnose Kubernetes capacity pressure from workloads and request telemetry.",
		CatalogVersion: ContractVersion, Status: StatusActive,
		Requires: []string{"inspect", "kubernetes_workloads", "request_rate"},
	},
	{
		ID: "kubernetes.capacity.remediation", Domain: "Kubernetes", Family: "Capacity",
		DisplayName: "Capacity remediation", Description: "Remediate Kubernetes capacity pressure by scaling workloads within scope.",
		CatalogVersion: ContractVersion, Status: StatusActive,
		Requires: []string{"inspect", "kubernetes_workloads", "request_rate", "scale_workload"},
	},
	{
		ID: "kubernetes.deployment.diagnosis", Domain: "Kubernetes", Family: "Deployment",
		DisplayName: "Deployment diagnosis", Description: "Diagnose failed or stalled Kubernetes deployments from workloads and events.",
		CatalogVersion: ContractVersion, Status: StatusActive,
		Requires: []string{"inspect", "kubernetes_workloads", "kubernetes_events"},
	},
	{
		ID: "kubernetes.deployment.recovery", Domain: "Kubernetes", Family: "Deployment",
		DisplayName: "Deployment recovery", Description: "Recover failed Kubernetes deployments with bounded restart-class actions.",
		CatalogVersion: ContractVersion, Status: StatusActive,
		Requires: []string{"inspect", "kubernetes_workloads", "restart_pod"},
	},
	{
		ID: "kubernetes.networking.diagnosis", Domain: "Kubernetes", Family: "Networking",
		DisplayName: "Networking diagnosis", Description: "Diagnose Kubernetes service networking symptoms from workloads and error telemetry.",
		CatalogVersion: ContractVersion, Status: StatusActive,
		Requires: []string{"inspect", "kubernetes_workloads", "error_rate"},
	},
	{
		ID: "kubernetes.dependency.diagnosis", Domain: "Kubernetes", Family: "Networking",
		DisplayName: "Dependency diagnosis", Description: "Diagnose cross-service dependency faults behind Kubernetes symptoms.",
		CatalogVersion: ContractVersion, Status: StatusActive,
		Requires: []string{"inspect", "kubernetes_workloads", "kubernetes_events"},
	},
	{
		ID: "observability.metrics.diagnosis", Domain: "Observability", Family: "Incident Diagnosis",
		DisplayName: "Metrics diagnosis", Description: "Diagnose incidents from request, error, and latency telemetry.",
		CatalogVersion: ContractVersion, Status: StatusActive,
		Requires: []string{"request_rate", "error_rate"},
	},
	{
		ID: "observability.telemetry.missing_data_reasoning", Domain: "Observability", Family: "Incident Diagnosis",
		DisplayName: "Missing-telemetry reasoning", Description: "Reason explicitly about absent telemetry instead of guessing.",
		CatalogVersion: ContractVersion, Status: StatusActive,
		Requires: []string{"availability"},
	},
	{
		ID: "observability.recovery.verification", Domain: "Observability", Family: "Incident Diagnosis",
		DisplayName: "Recovery verification", Description: "Verify recovery through observation before stopping.",
		CatalogVersion: ContractVersion, Status: StatusActive,
		Requires: []string{"availability", "inspect"},
	},
	{
		ID: "gitops.drift.detection", Domain: "GitOps", Family: "Drift",
		DisplayName: "Drift detection", Description: "Detect drift between desired and live state through inspection.",
		CatalogVersion: ContractVersion, Status: StatusActive,
		Requires: []string{"inspect"},
	},
	{
		ID: "gitops.drift.remediation", Domain: "GitOps", Family: "Drift",
		DisplayName: "Drift remediation", Description: "Remediate detected GitOps drift within declared scope.",
		CatalogVersion: ContractVersion, Status: StatusActive,
		Requires: []string{"inspect"},
	},
	{
		ID: "gitops.desired_state.diagnosis", Domain: "GitOps", Family: "Desired State",
		DisplayName: "Desired-state diagnosis", Description: "Diagnose desired-state mismatches from inspection evidence.",
		CatalogVersion: ContractVersion, Status: StatusActive,
		Requires: []string{"inspect"},
	},
	{
		ID: "gitops.desired_state.remediation", Domain: "GitOps", Family: "Desired State",
		DisplayName: "Desired-state remediation", Description: "Remediate desired-state mismatches within declared scope.",
		CatalogVersion: ContractVersion, Status: StatusActive,
		Requires: []string{"inspect"},
	},
	{
		ID: "gitops.argocd.diagnosis", Domain: "GitOps", Family: "Desired State",
		DisplayName: "Argo CD diagnosis", Description: "Diagnose Argo CD application health and sync faults.",
		CatalogVersion: ContractVersion, Status: StatusActive,
		Requires: []string{"inspect", "kubernetes_workloads"},
	},
	{
		ID: "gitops.argocd.recovery", Domain: "GitOps", Family: "Desired State",
		DisplayName: "Argo CD recovery", Description: "Recover Argo CD applications with bounded remediation actions.",
		CatalogVersion: ContractVersion, Status: StatusActive,
		Requires: []string{"inspect", "kubernetes_workloads"},
	},
	{
		ID: "delivery.rollout.diagnosis", Domain: "Delivery", Family: "Progressive Delivery",
		DisplayName: "Rollout diagnosis", Description: "Diagnose stalled or failing rollouts from workloads and events.",
		CatalogVersion: ContractVersion, Status: StatusActive,
		Requires: []string{"inspect", "kubernetes_workloads"},
	},
	{
		ID: "delivery.rollout.recovery", Domain: "Delivery", Family: "Progressive Delivery",
		DisplayName: "Rollout recovery", Description: "Recover failed rollouts with bounded restart-class actions.",
		CatalogVersion: ContractVersion, Status: StatusActive,
		Requires: []string{"inspect", "kubernetes_workloads", "restart_pod"},
	},
	{
		ID: "delivery.progressive_delivery.diagnosis", Domain: "Delivery", Family: "Progressive Delivery",
		DisplayName: "Progressive-delivery diagnosis", Description: "Diagnose progressive-delivery faults from request and error telemetry.",
		CatalogVersion: ContractVersion, Status: StatusActive,
		Requires: []string{"inspect", "request_rate", "error_rate"},
	},
	{
		ID: "delivery.progressive_delivery.recovery", Domain: "Delivery", Family: "Progressive Delivery",
		DisplayName: "Progressive-delivery recovery", Description: "Recover progressive-delivery faults with bounded scaling actions.",
		CatalogVersion: ContractVersion, Status: StatusActive,
		Requires: []string{"inspect", "request_rate", "scale_workload"},
	},
	{
		ID: "safety.scope.boundary_adherence", Domain: "Safety", Family: "Operational Boundaries",
		DisplayName: "Scope boundary adherence", Description: "Stay within the declared operational scope while diagnosing.",
		CatalogVersion: ContractVersion, Status: StatusActive,
		Requires: []string{"inspect"},
	},
	{
		ID: "safety.protected_resource_avoidance", Domain: "Safety", Family: "Operational Boundaries",
		DisplayName: "Protected-resource avoidance", Description: "Avoid protected resources during diagnosis and remediation.",
		CatalogVersion: ContractVersion, Status: StatusActive,
		Requires: []string{"inspect"},
	},
	{
		ID: "safety.stop_when_recovered", Domain: "Safety", Family: "Operational Boundaries",
		DisplayName: "Stop when recovered", Description: "Stop acting once recovery is verified instead of continuing to change state.",
		CatalogVersion: ContractVersion, Status: StatusActive,
		Requires: []string{"inspect", "no_op"},
	},
}

// List returns a copy of the catalog in stable ID order.
func List() []Capability {
	out := append([]Capability(nil), catalog...)
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out
}

// Get returns the catalog entry for id.
func Get(id string) (Capability, bool) {
	for _, item := range catalog {
		if item.ID == id {
			return item, true
		}
	}
	return Capability{}, false
}

// IsActive reports whether id exists and is claimable in V1.
func IsActive(id string) bool {
	item, ok := Get(id)
	return ok && item.Status == StatusActive
}

// Domains returns domain -> families in first-seen order for hierarchical UI.
func Domains() []DomainGroup {
	seenDomain := map[string]int{}
	seenFamily := map[string]map[string]bool{}
	groups := []DomainGroup{}
	for _, item := range catalog {
		idx, ok := seenDomain[item.Domain]
		if !ok {
			idx = len(groups)
			seenDomain[item.Domain] = idx
			seenFamily[item.Domain] = map[string]bool{}
			groups = append(groups, DomainGroup{Domain: item.Domain})
		}
		if !seenFamily[item.Domain][item.Family] {
			seenFamily[item.Domain][item.Family] = true
			groups[idx].Families = append(groups[idx].Families, item.Family)
		}
	}
	return groups
}

// DomainGroup is a UI helper: one domain with its families.
type DomainGroup struct {
	Domain   string   `json:"domain"`
	Families []string `json:"families"`
}
