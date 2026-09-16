package kubeenv

import "errors"

var (
	ErrEnvironmentNotFound  = errors.New("environment not found")
	ErrEnvironmentConflict  = errors.New("environment conflict")
	ErrEnvironmentOwnership = errors.New("environment ownership mismatch")
)

const (
	ManagedLabelKey     = "evalplane.dev/managed"
	RunIDLabelKey       = "evalplane.dev/run-id"
	BatchIDLabelKey     = "evalplane.dev/batch-id"
	WorkspaceIDLabelKey = "evalplane.dev/workspace-id"
	ManagedByLabelKey   = "app.kubernetes.io/managed-by"

	quotaName      = "evalplane-quota"
	limitRangeName = "evalplane-defaults"
)

type RunIdentity struct {
	RunID       string
	BatchID     string
	WorkspaceID string
}

type Environment struct {
	RunID     string `json:"run_id"`
	Namespace string `json:"namespace"`
	Reference string `json:"reference"`
}

type BoundaryConfig struct {
	Pods                 string
	RequestsCPU          string
	LimitsCPU            string
	RequestsMemory       string
	LimitsMemory         string
	DefaultCPU           string
	DefaultMemory        string
	DefaultRequestCPU    string
	DefaultRequestMemory string
}

func DefaultBoundaryConfig() BoundaryConfig {
	return BoundaryConfig{
		Pods:                 "20",
		RequestsCPU:          "4",
		LimitsCPU:            "8",
		RequestsMemory:       "8Gi",
		LimitsMemory:         "16Gi",
		DefaultCPU:           "1",
		DefaultMemory:        "1Gi",
		DefaultRequestCPU:    "100m",
		DefaultRequestMemory: "128Mi",
	}
}
