package environment

import (
	"context"

	"github.com/evalplane/evalplane/backend/internal/kubeenv"
)

// KindDevProvider adapts the existing Kubernetes environment manager as the
// first development provider. Kind/local Kubernetes is a DEVELOPMENT
// provider: it supports kubernetes.namespace:v1 for development/focused
// testing and is never production qualification infrastructure.
type KindDevProvider struct {
	manager *kubeenv.Manager
}

// NewKindDevProvider wraps an existing kubeenv manager.
func NewKindDevProvider(manager *kubeenv.Manager) (*KindDevProvider, error) {
	if manager == nil {
		return nil, errValidation("Kubernetes environment manager is required")
	}
	return &KindDevProvider{manager: manager}, nil
}

// NewKindDevProviderInfo returns the static catalog identity without a live
// manager (for catalog/health-unknown paths).
func NewKindDevProviderInfo() ProviderInfo {
	info, _ := GetProvider(ProviderKindDev)
	return info
}

func (p *KindDevProvider) Info() ProviderInfo { return NewKindDevProviderInfo() }

func (p *KindDevProvider) Capabilities() ProviderCapabilities {
	return ProviderCapabilities{
		SupportedProfiles: []string{ProfileKubernetesNamespace},
		RuntimeContracts:  []string{DefaultRuntimeContract},
		Production:        false,
	}
}

// Health reports real observed health: available when the manager exists.
// It never claims production qualification capability.
func (p *KindDevProvider) Health(_ context.Context) ProviderHealth {
	if p == nil || p.manager == nil {
		return ProviderHealth{Available: false, Reason: "development provider not configured"}
	}
	return ProviderHealth{Available: true, Reason: "development provider configured; not production qualification infrastructure"}
}

// Provision creates one namespaced environment for a run owner. Opaque
// reference returned is the safe namespace handle (k8s://...), never a
// kubeconfig or credential.
func (p *KindDevProvider) Provision(ctx context.Context, req ProvisionRequest) (string, error) {
	if req.OwnerType != OwnerRun {
		return "", errValidation("kind-dev provider supports run-owned environments only")
	}
	if req.ProfileID != "" && req.ProfileID != ProfileKubernetesNamespace {
		return "", errValidation("kind-dev provider supports kubernetes.namespace:v1 only")
	}
	identity, err := kubeenv.ValidateRunIdentity(kubeenv.RunIdentity{
		RunID:       req.OwnerID,
		BatchID:     req.BatchID,
		WorkspaceID: req.WorkspaceID,
	})
	if err != nil {
		return "", err
	}
	env, err := p.manager.CreateEnvironment(ctx, identity)
	if err != nil {
		return "", err
	}
	return env.Reference, nil
}

// Get returns the safe opaque reference for a run-owned environment.
func (p *KindDevProvider) Get(ctx context.Context, environmentID string) (string, error) {
	env, err := p.manager.GetEnvironment(ctx, environmentID)
	if err != nil {
		return "", err
	}
	return env.Reference, nil
}

// Reset is a no-op handle validation for V1: namespace reset happens through
// the run cleanup path. Kept explicit so lifecycle callers have a stable
// entry point.
func (p *KindDevProvider) Reset(ctx context.Context, environmentID string) error {
	_, err := p.manager.GetEnvironment(ctx, environmentID)
	return err
}

// Destroy deletes the owned namespace. Idempotent: missing namespaces
// succeed so repeated destroy never fails.
func (p *KindDevProvider) Destroy(ctx context.Context, environmentID string) error {
	return p.manager.DeleteEnvironment(ctx, environmentID)
}
