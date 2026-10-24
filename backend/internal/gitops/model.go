package gitops

import "time"

const EvidenceContractVersion = "gitops-evidence:v1"

type WorkloadTarget struct {
	Kind, Namespace, Name, Container string
}

type DesiredState struct {
	Repository string            `json:"repository"`
	Revision   string            `json:"revision"`
	Path       string            `json:"path"`
	Resource   string            `json:"resource"`
	Container  string            `json:"container"`
	Image      string            `json:"image"`
	Replicas   *int32            `json:"replicas,omitempty"`
	Labels     map[string]string `json:"labels,omitempty"`
	CapturedAt time.Time         `json:"captured_at"`
	Reference  string            `json:"reference"`
}

type ArgoState struct {
	Application      string    `json:"application"`
	Namespace        string    `json:"namespace"`
	SyncStatus       string    `json:"sync_status"`
	HealthStatus     string    `json:"health_status,omitempty"`
	TargetRevision   string    `json:"target_revision,omitempty"`
	ObservedRevision string    `json:"observed_revision,omitempty"`
	Automated        bool      `json:"automated_reconciliation"`
	SelfHeal         bool      `json:"self_heal"`
	CapturedAt       time.Time `json:"captured_at"`
	Reference        string    `json:"reference"`
}

type Evidence struct {
	ContractVersion string        `json:"contract_version"`
	Desired         *DesiredState `json:"desired_state,omitempty"`
	Argo            *ArgoState    `json:"argo_state,omitempty"`
	Errors          []string      `json:"errors,omitempty"`
}
