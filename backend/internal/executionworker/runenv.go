package executionworker

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/evalplane/evalplane/backend/internal/environment"
	"github.com/evalplane/evalplane/backend/internal/execution"
	"github.com/evalplane/evalplane/backend/internal/kubeenv"
	"github.com/evalplane/evalplane/backend/internal/scenario"
)

// RunEnvironment is the ready execution environment for one run, wired
// through the Stage-29 environment model:
//
//	ScenarioVersion requirements → scheduler → profile/provider →
//	EnvironmentInstance → provider provision → ready → existing execution.
//
// Provider references are safe opaque handles only (for example k8s://... or
// fake://...). Kubeconfigs, tokens, and cloud credentials must never appear
// here; see isSafeProviderRef.
type RunEnvironment struct {
	// Instance is the persisted EnvironmentInstance (status ready on success).
	// Owner is always owner_type=run, owner_id=run ID, workspace_id=run workspace.
	Instance environment.EnvironmentInstance
	// Provisioned is the ready runtime handle consumed by existing execution.
	Provisioned kubeenv.Environment
	// Decision is the deterministic scheduler placement (never downgraded).
	Decision environment.SchedulerDecision
	// Requirements are the stored server-reviewed requirements (or the legacy
	// compatibility default) used for this placement.
	Requirements environment.Requirements
	// Explicit reports whether the ScenarioVersion had stored requirements.
	Explicit bool
}

// Pins returns the reproducibility identity for run/evidence linkage. All
// values are safe to persist: profile/provider/contract IDs and the instance
// ID. No credentials are included.
func (e RunEnvironment) Pins() map[string]any {
	return map[string]any{
		"environment_profile_id":               e.Instance.ProfileID,
		"environment_profile_contract_version": e.Instance.ProfileContractVersion,
		"provider_id":                          e.Instance.ProviderID,
		"provider_contract_version":            e.Instance.ProviderContractVersion,
		"isolation_tier":                       e.Instance.IsolationTier,
		"runtime_contract_version":             e.Decision.RuntimeContract,
		"scheduler_contract_version":           e.Decision.SchedulerContract,
		"environment_instance_id":              e.Instance.ID,
	}
}

// RunEnvironments is the worker-facing environment abstraction. The worker
// depends on this interface (scheduler + provider + instance lifecycle), not
// on any Kind-specific implementation. Kind remains merely the current
// development provider implementation wired elsewhere.
type RunEnvironments interface {
	// ProvisionForRun resolves stored requirements for the exact
	// ScenarioVersion, runs the server-owned scheduler, persists one
	// run-owned EnvironmentInstance, provisions through the selected
	// provider, and marks the instance ready. Agent invocation must only
	// happen after this returns success.
	ProvisionForRun(ctx context.Context, run execution.ExecutionRun, sv scenario.Version) (RunEnvironment, error)
	// CleanupForRun destroys the owned environment idempotently and marks
	// the instance destroyed. Repeated calls are safe. A cleanup failure
	// never alters the agent verdict; it returns a platform-classified
	// error and leaves state suitable for sweeper/recovery.
	CleanupForRun(ctx context.Context, run execution.ExecutionRun) error
	// CleanupIdentity destroys by explicit identity (recovery/abandoned
	// path). Idempotent.
	CleanupIdentity(ctx context.Context, id kubeenv.RunIdentity) error
	// InstanceForOwner returns the newest owned instance for diagnosis and
	// recovery. ErrNoEnvironment when none exists.
	InstanceForOwner(ctx context.Context, workspaceID, runID string) (environment.EnvironmentInstance, error)
	// ResetForRun invokes provider Reset where the lifecycle requires
	// reset/reuse. There is no pooling/warm reuse: a failed reset means the
	// environment must not be reused (platform failure), and destroyed
	// environments never return to ready.
	ResetForRun(ctx context.Context, run execution.ExecutionRun) error
}

// ErrNoEnvironment reports that no owned EnvironmentInstance exists.
var ErrNoEnvironment = errors.New("no environment for run")

// isSafeProviderRef rejects anything resembling a credential. Providers must
// return safe opaque handles only (k8s://..., fake://...). Secrets are never
// persisted or emitted in evidence.
func isSafeProviderRef(ref string) bool {
	trimmed := strings.TrimSpace(ref)
	if trimmed == "" || len(trimmed) > 2000 {
		return false
	}
	lowered := strings.ToLower(trimmed)
	for _, marker := range []string{
		"kubeconfig", "-----begin", "bearer ", "basic ",
		"aws_secret", "aws_session", "client-key", "client-certificate",
		"service-account-token", "privatekey", "private_key",
	} {
		if strings.Contains(lowered, marker) {
			return false
		}
	}
	// Opaque handles are short URIs or namespace-derived references, never
	// multi-KB blobs.
	if strings.Contains(trimmed, "\n") {
		return false
	}
	return true
}

// runIdentityFor validates the full workspace/batch/run identity for one run.
// Cross-workspace reuse is rejected here: callers must pass the exact run
// workspace.
func runIdentityFor(run execution.ExecutionRun) (kubeenv.RunIdentity, error) {
	return kubeenv.ValidateRunIdentity(kubeenv.RunIdentity{
		RunID:       run.RunID,
		BatchID:     run.BatchID,
		WorkspaceID: run.WorkspaceID,
	})
}

// provisionedHandle builds the runtime handle from the opaque provider
// reference. The namespace is deterministically derived from the run ID via
// the shared kubeenv naming convention (value type only, no
// Kind-specific orchestration). The reference itself stays opaque and safe.
func provisionedHandle(runID, providerRef string) (kubeenv.Environment, error) {
	namespace, err := kubeenv.NamespaceForRun(runID)
	if err != nil {
		return kubeenv.Environment{}, err
	}
	if !isSafeProviderRef(providerRef) {
		return kubeenv.Environment{}, errors.New("provider returned unsafe environment reference")
	}
	return kubeenv.Environment{RunID: runID, Namespace: namespace, Reference: providerRef}, nil
}

// resolveRequirements normalizes empty input through the legacy compatibility
// default: kubernetes+prometheus, namespace isolation only. Legacy tasks
// continue through the truthful Kind development namespace profile and never
// silently claim production isolation.
func resolveRequirements(req environment.Requirements) environment.Requirements {
	if len(req.RequiredComponents) == 0 {
		return environment.LegacyDefaultRequirements()
	}
	return req
}

// selectPlacement runs the deterministic server-owned scheduler with no
// fidelity downgrade. A dedicated_cluster minimum with only the namespace
// provider available yields no eligible environment (platform-invalid),
// never a namespace fallback.
func selectPlacement(ctx context.Context, req environment.Requirements, availability environment.ProviderAvailability) environment.SchedulerDecision {
	if availability == nil {
		availability = environment.DefaultAvailability()
	}
	return environment.SelectForRequirements(ctx, req, "trial", "", availability)
}

// failureCode extracts a stable platform code for evidence without leaking
// provider internals.
func failureCode(decision environment.SchedulerDecision) string {
	if decision.ReasonCode != "" {
		return decision.ReasonCode
	}
	return environment.ReasonNoEligibleEnvironment
}

// pinsForInstance builds the reproducibility map from a persisted instance
// plus its scheduler decision. Used for evidence linkage without a migration:
// the run row keeps environment_ref while the instance row carries the pins.
func pinsForInstance(instance environment.EnvironmentInstance, decision environment.SchedulerDecision) map[string]any {
	runtime := decision.RuntimeContract
	if runtime == "" {
		runtime = instance.ProviderContractVersion
	}
	return map[string]any{
		"environment_profile_id":               instance.ProfileID,
		"environment_profile_contract_version": instance.ProfileContractVersion,
		"provider_id":                          instance.ProviderID,
		"provider_contract_version":            instance.ProviderContractVersion,
		"isolation_tier":                       instance.IsolationTier,
		"runtime_contract_version":             runtime,
		"scheduler_contract_version":           decision.SchedulerContract,
		"environment_instance_id":              instance.ID,
	}
}

// leaseTTLForRun bounds one run-owned environment lease.
func leaseTTLForRun() time.Duration { return 2 * time.Hour }
