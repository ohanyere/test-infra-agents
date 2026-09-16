package coverage

import (
	"fmt"
	"sort"
	"strings"
)

// TaskFamily is one independent class of infrastructure situation.
// Customer-facing name: Evaluation Task / Task Family. Backend code may keep
// saying Scenario where the surrounding domain already does.
//
// Stable dotted IDs. V1 is intentionally small: families without runnable
// scenarios today are still listed so requirements stay honest (coverage for
// them remains PARTIAL or UNTESTED instead of weakening rules).
type TaskFamily struct {
	ID              string `json:"id"`
	Domain          string `json:"domain"`
	Label           string `json:"label"`
	Description     string `json:"description"`
	ContractVersion string `json:"contract_version"`
}

var taskFamilies = []TaskFamily{
	{ID: "kubernetes.workload.baseline", Domain: "Kubernetes", Label: "Workload baseline", Description: "Independent workload health situations requiring diagnosis or bounded remediation.", ContractVersion: TaskFamilyContractVersion},
	{ID: "kubernetes.capacity.traffic_spike", Domain: "Kubernetes", Label: "Capacity traffic spike", Description: "Sudden traffic saturation requiring capacity diagnosis or scaling remediation. Runnable today via the traffic_spike runtime.", ContractVersion: TaskFamilyContractVersion},
	{ID: "kubernetes.capacity.resource_pressure", Domain: "Kubernetes", Label: "Capacity resource pressure", Description: "Independent capacity pressure situation distinct from a traffic spike (e.g. resource exhaustion with different signature).", ContractVersion: TaskFamilyContractVersion},
	{ID: "kubernetes.deployment.recovery", Domain: "Kubernetes", Label: "Deployment recovery", Description: "Failed or stalled deployment recovery situations.", ContractVersion: TaskFamilyContractVersion},
	{ID: "kubernetes.networking.dependency", Domain: "Kubernetes", Label: "Networking and dependency", Description: "Service networking symptoms and cross-service dependency faults.", ContractVersion: TaskFamilyContractVersion},
	{ID: "observability.metrics.diagnosis", Domain: "Observability", Label: "Metrics diagnosis", Description: "Incident diagnosis from request, error, and latency telemetry.", ContractVersion: TaskFamilyContractVersion},
	{ID: "observability.telemetry.missing_data", Domain: "Observability", Label: "Missing telemetry reasoning", Description: "Situations with absent telemetry where the agent must reason explicitly instead of guessing.", ContractVersion: TaskFamilyContractVersion},
	{ID: "observability.recovery.verification", Domain: "Observability", Label: "Recovery verification", Description: "Verify recovery through observation before stopping.", ContractVersion: TaskFamilyContractVersion},
	{ID: "gitops.drift.deployment", Domain: "GitOps", Label: "Drift deployment", Description: "Desired-versus-live drift in deployment state.", ContractVersion: TaskFamilyContractVersion},
	{ID: "gitops.desired_state.remediation", Domain: "GitOps", Label: "Desired state remediation", Description: "Desired-state mismatch remediation within declared scope.", ContractVersion: TaskFamilyContractVersion},
	{ID: "gitops.argocd.recovery", Domain: "GitOps", Label: "Argo CD recovery", Description: "Argo CD application health and sync fault recovery.", ContractVersion: TaskFamilyContractVersion},
	{ID: "delivery.rollout.recovery", Domain: "Delivery", Label: "Rollout recovery", Description: "Stalled or failing rollout recovery situations.", ContractVersion: TaskFamilyContractVersion},
	{ID: "delivery.progressive.error_regression", Domain: "Delivery", Label: "Progressive delivery error regression", Description: "Progressive-delivery faults visible in error telemetry.", ContractVersion: TaskFamilyContractVersion},
	{ID: "safety.boundary.adherence", Domain: "Safety", Label: "Boundary adherence", Description: "Situations exercising declared operational scope boundaries.", ContractVersion: TaskFamilyContractVersion},
	{ID: "safety.protected_resource.avoidance", Domain: "Safety", Label: "Protected-resource avoidance", Description: "Situations tempting action on protected resources.", ContractVersion: TaskFamilyContractVersion},
	{ID: "safety.stop_after_recovery", Domain: "Safety", Label: "Stop after recovery", Description: "Post-recovery situations where the agent must stop changing state.", ContractVersion: TaskFamilyContractVersion},
}

// ListTaskFamilies returns the V1 catalog in stable ID order.
func ListTaskFamilies() []TaskFamily {
	out := append([]TaskFamily(nil), taskFamilies...)
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out
}

// GetTaskFamily returns the catalog entry for id.
func GetTaskFamily(id string) (TaskFamily, bool) {
	for _, item := range taskFamilies {
		if item.ID == id {
			return item, true
		}
	}
	return TaskFamily{}, false
}

// ValidateTaskFamilyID rejects blank and unknown family IDs.
func ValidateTaskFamilyID(id string) (string, error) {
	trimmed := strings.TrimSpace(id)
	if trimmed == "" {
		return "", fmt.Errorf("task family must not be blank")
	}
	if _, ok := GetTaskFamily(trimmed); !ok {
		return "", fmt.Errorf("unknown task family %q: select from the server task-family catalog", trimmed)
	}
	return trimmed, nil
}
