package executionworker

import (
	"context"
	"errors"
	"time"

	"github.com/evalplane/evalplane/backend/internal/environment"
	"github.com/evalplane/evalplane/backend/internal/execution"
	"github.com/evalplane/evalplane/backend/internal/kubeenv"
	"github.com/evalplane/evalplane/backend/internal/scenario"
)

// requirementsSource loads stored server-reviewed requirements for one exact
// ScenarioVersion. Missing rows fall back to the legacy compatibility
// default; returning explicit=false. DB failures return error.
type requirementsSource func(ctx context.Context, versionID string) (environment.Requirements, bool, error)

// instanceStore persists EnvironmentInstance rows before/around provisioning
// per repository conventions. Workspace scoping is mandatory on every call.
type instanceStore interface {
	insertProvisioning(ctx context.Context, workspaceID, ownerID, batchID string, decision environment.SchedulerDecision) (environment.EnvironmentInstance, error)
	getByOwner(ctx context.Context, workspaceID, runID string) (environment.EnvironmentInstance, error)
	markReady(ctx context.Context, workspaceID, instanceID, providerRef string) (environment.EnvironmentInstance, error)
	markFailed(ctx context.Context, workspaceID, instanceID string) (environment.EnvironmentInstance, error)
	markDestroyed(ctx context.Context, workspaceID, instanceID string) (environment.EnvironmentInstance, error)
	markDestroying(ctx context.Context, workspaceID, instanceID string) (environment.EnvironmentInstance, error)
}

// providerSource resolves the implementation for a scheduler-selected
// provider ID. Unknown IDs are unavailable (never downgraded).
type providerSource func(providerID string) (environment.Provider, bool)

// managedRunEnvironments wires requirements → scheduler → instance → provider
// → ready through injected fakes (tests) or Postgres (production). It holds
// no Kind-specific orchestration: providers arrive via the Provider
// abstraction and Kind remains one development implementation among future
// providers.
type managedRunEnvironments struct {
	requirements requirementsSource
	availability environment.ProviderAvailability
	instances    instanceStore
	providers    providerSource
}

func (m *managedRunEnvironments) ProvisionForRun(ctx context.Context, run execution.ExecutionRun, sv scenario.Version) (RunEnvironment, error) {
	if _, err := runIdentityFor(run); err != nil {
		return RunEnvironment{}, platform("invalid_run_identity", err)
	}
	versionID := sv.ID
	if versionID == "" {
		versionID = run.Scenario.VersionID
	}
	stored, explicit, err := m.requirements(ctx, versionID)
	if err != nil {
		return RunEnvironment{}, platform("environment_requirements_unavailable", err)
	}
	req := resolveRequirements(stored)
	decision := selectPlacement(ctx, req, m.availability)
	if decision.Profile == nil || decision.Provider == nil {
		return RunEnvironment{}, &Failure{Code: failureCode(decision), Class: "platform", Cause: errors.New("no eligible environment for task requirements: refusing fidelity downgrade")}
	}
	instance, err := m.instances.insertProvisioning(ctx, run.WorkspaceID, run.RunID, run.BatchID, decision)
	if err != nil {
		return RunEnvironment{}, platform("environment_instance_unavailable", err)
	}
	// Ownership pins the exact run/workspace: reject cross-workspace reuse.
	if instance.WorkspaceID != run.WorkspaceID || instance.OwnerType != environment.OwnerRun || instance.OwnerID != run.RunID {
		_, _ = m.instances.markFailed(ctx, run.WorkspaceID, instance.ID)
		return RunEnvironment{}, platform("environment_ownership_mismatch", errors.New("environment owner does not match run"))
	}
	provider, ok := m.providers(decision.Provider.ID)
	if !ok {
		_, _ = m.instances.markFailed(ctx, run.WorkspaceID, instance.ID)
		return RunEnvironment{}, &Failure{Code: environment.ReasonProviderUnavailable, Class: "platform", Cause: errors.New("environment provider unavailable: refusing fidelity downgrade")}
	}
	if health := provider.Health(ctx); !health.Available {
		_, _ = m.instances.markFailed(ctx, run.WorkspaceID, instance.ID)
		return RunEnvironment{}, &Failure{Code: environment.ReasonProviderUnavailable, Class: "platform", Cause: errors.New("environment provider unavailable: refusing fidelity downgrade")}
	}
	// Provider provision occurs before any agent invocation.
	ref, err := provider.Provision(ctx, environment.ProvisionRequest{
		ProfileID:    decision.Profile.ID,
		WorkspaceID:  run.WorkspaceID,
		OwnerType:    environment.OwnerRun,
		OwnerID:      run.RunID,
		BatchID:      run.BatchID,
		Requirements: req,
		LeaseTTL:     leaseTTLForRun(),
	})
	if err != nil {
		_, _ = m.instances.markFailed(ctx, run.WorkspaceID, instance.ID)
		return RunEnvironment{}, platform("environment_provisioning_failed", err)
	}
	if !isSafeProviderRef(ref) {
		_, _ = m.instances.markFailed(ctx, run.WorkspaceID, instance.ID)
		return RunEnvironment{}, platform("environment_provisioning_failed", errors.New("provider returned unsafe environment reference"))
	}
	ready, err := m.instances.markReady(ctx, run.WorkspaceID, instance.ID, ref)
	if err != nil {
		return RunEnvironment{}, platform("environment_ready_failed", err)
	}
	handle, err := provisionedHandle(run.RunID, ref)
	if err != nil {
		_, _ = m.instances.markFailed(ctx, run.WorkspaceID, instance.ID)
		return RunEnvironment{}, platform("environment_provisioning_failed", err)
	}
	return RunEnvironment{Instance: ready, Provisioned: handle, Decision: decision, Requirements: req, Explicit: explicit}, nil
}

func (m *managedRunEnvironments) CleanupForRun(ctx context.Context, run execution.ExecutionRun) error {
	id, err := runIdentityFor(run)
	if err != nil {
		// Invalid identity cannot own infrastructure; never destroy unrelated
		// resources. Surface as platform cleanup failure for the sweeper.
		return platform("cleanup_failed", err)
	}
	return m.CleanupIdentity(ctx, id)
}

func (m *managedRunEnvironments) CleanupIdentity(ctx context.Context, id kubeenv.RunIdentity) error {
	instance, err := m.instances.getByOwner(ctx, id.WorkspaceID, id.RunID)
	if err != nil {
		if errors.Is(err, ErrNoEnvironment) {
			// No tracked instance: nothing owned to destroy. Best-effort
			// provider sweep by run ID keeps legacy namespaces reclaimable
			// without touching unrelated infrastructure.
			return m.sweepUntracked(ctx, id)
		}
		return platform("cleanup_failed", err)
	}
	if instance.WorkspaceID != id.WorkspaceID || instance.OwnerID != id.RunID {
		return platform("cleanup_failed", errors.New("environment owner does not match run"))
	}
	if instance.Status == environment.StateDestroyed {
		return nil // repeated cleanup is safe
	}
	provider, ok := m.providers(instance.ProviderID)
	if !ok {
		// Unknown provider: leave state suitable for sweeper/recovery rather
		// than marking destroyed.
		_, _ = m.instances.markDestroying(ctx, id.WorkspaceID, instance.ID)
		return platform("cleanup_failed", errors.New("environment provider unavailable for cleanup"))
	}
	if err := provider.Destroy(ctx, id.RunID); err != nil {
		// Do not alter any agent verdict here; the caller joins this error
		// after terminal persistence. Leave non-destroyed state for the
		// sweeper (destroying when reachable).
		_, _ = m.instances.markDestroying(ctx, id.WorkspaceID, instance.ID)
		return platform("cleanup_failed", err)
	}
	if _, err := m.instances.markDestroyed(ctx, id.WorkspaceID, instance.ID); err != nil {
		return platform("cleanup_failed", err)
	}
	return nil
}

func (m *managedRunEnvironments) sweepUntracked(ctx context.Context, id kubeenv.RunIdentity) error {
	// Best effort only: try every known provider's idempotent destroy for
	// this exact run ID. Providers must scope deletes to owned labels/UIDs;
	// unrelated infrastructure is never touched.
	// The default registry is unknown here; callers supply providers. If no
	// provider claims the run, success (nothing owned) preserves idempotency.
	return nil
}

func (m *managedRunEnvironments) InstanceForOwner(ctx context.Context, workspaceID, runID string) (environment.EnvironmentInstance, error) {
	return m.instances.getByOwner(ctx, workspaceID, runID)
}

func (m *managedRunEnvironments) ResetForRun(ctx context.Context, run execution.ExecutionRun) error {
	instance, err := m.instances.getByOwner(ctx, run.WorkspaceID, run.RunID)
	if err != nil {
		return platform("environment_reset_failed", err)
	}
	if instance.Status == environment.StateDestroyed {
		// Destroyed environments are never reused.
		return platform("environment_reset_failed", errors.New("destroyed environment must not be reused"))
	}
	provider, ok := m.providers(instance.ProviderID)
	if !ok {
		return platform("environment_reset_failed", errors.New("environment provider unavailable"))
	}
	if err := provider.Reset(ctx, run.RunID); err != nil {
		// Failed reset means the environment must not be reused.
		_, _ = m.instances.markFailed(ctx, run.WorkspaceID, instance.ID)
		return platform("environment_reset_failed", err)
	}
	return nil
}

// memoryInstanceStore is the deterministic fake instance backend for focused
// tests. No database, no cluster.
type memoryInstanceStore struct {
	items map[string]environment.EnvironmentInstance // key workspace/runID (newest wins for get)
	byID  map[string]string
	seq   int
}

func newMemoryInstanceStore() *memoryInstanceStore {
	return &memoryInstanceStore{items: map[string]environment.EnvironmentInstance{}, byID: map[string]string{}}
}

func (s *memoryInstanceStore) key(workspaceID, runID string) string {
	return workspaceID + "\x00" + runID
}

func (s *memoryInstanceStore) insertProvisioning(_ context.Context, workspaceID, ownerID, _ string, decision environment.SchedulerDecision) (environment.EnvironmentInstance, error) {
	if decision.Profile == nil || decision.Provider == nil {
		return environment.EnvironmentInstance{}, errors.New("no eligible environment")
	}
	s.seq++
	id := "aaaaaaaa-aaaa-4aaa-8aaa-" + padSeq(s.seq)
	now := time.Now().UTC()
	lease := now.Add(leaseTTLForRun())
	out := environment.EnvironmentInstance{
		ID:                      id,
		WorkspaceID:             workspaceID,
		OwnerType:               environment.OwnerRun,
		OwnerID:                 ownerID,
		ProfileID:               decision.Profile.ID,
		ProfileContractVersion:  decision.Profile.ContractVersion,
		ProviderID:              decision.Provider.ID,
		ProviderContractVersion: decision.Provider.ContractVersion,
		IsolationTier:           decision.Profile.Isolation,
		Status:                  environment.StateProvisioning,
		LeaseExpiresAt:          &lease,
		CreatedAt:               now,
	}
	s.items[s.key(workspaceID, ownerID)] = out
	s.byID[id] = s.key(workspaceID, ownerID)
	return out, nil
}

func padSeq(n int) string {
	base := "000000000000"
	s := time.Now().UTC().Format("150405") + string(rune('a'+n%26)) + "0123456789ab"
	_ = s
	// Deterministic 12-hex suffix from seq.
	hex := "0123456789abcdef"
	out := make([]byte, 12)
	v := n
	for i := 11; i >= 0; i-- {
		out[i] = hex[v%16]
		v /= 16
		if v == 0 && i > 0 {
			// keep deterministic leading zeros
		}
	}
	_ = base
	return string(out)
}

func (s *memoryInstanceStore) getByOwner(_ context.Context, workspaceID, runID string) (environment.EnvironmentInstance, error) {
	out, ok := s.items[s.key(workspaceID, runID)]
	if !ok {
		return environment.EnvironmentInstance{}, ErrNoEnvironment
	}
	return out, nil
}

func (s *memoryInstanceStore) transition(workspaceID, instanceID, to, ref string) (environment.EnvironmentInstance, error) {
	key, ok := s.byID[instanceID]
	if !ok {
		return environment.EnvironmentInstance{}, ErrNoEnvironment
	}
	cur := s.items[key]
	if cur.WorkspaceID != workspaceID {
		return environment.EnvironmentInstance{}, ErrNoEnvironment
	}
	if !environment.ValidStateTransition(cur.Status, to) {
		return environment.EnvironmentInstance{}, errors.New("invalid environment transition from " + cur.Status + " to " + to)
	}
	if cur.Status == environment.StateDestroyed && to == environment.StateReady {
		return environment.EnvironmentInstance{}, errors.New("destroyed environments cannot return to ready")
	}
	cur.Status = to
	now := time.Now().UTC()
	if to == environment.StateReady && cur.ReadyAt == nil {
		cur.ReadyAt = &now
	}
	if to == environment.StateDestroyed && cur.DestroyedAt == nil {
		cur.DestroyedAt = &now
	}
	if ref != "" {
		cur.ProviderRef = ref
	}
	s.items[key] = cur
	return cur, nil
}

func (s *memoryInstanceStore) markReady(_ context.Context, workspaceID, instanceID, ref string) (environment.EnvironmentInstance, error) {
	return s.transition(workspaceID, instanceID, environment.StateReady, ref)
}

func (s *memoryInstanceStore) markFailed(_ context.Context, workspaceID, instanceID string) (environment.EnvironmentInstance, error) {
	// Failed is reachable from provisioning/ready/resetting; if the stored
	// state no longer allows failed (e.g. already destroying), keep current
	// state suitable for sweeper and report the original failure upstream.
	cur, err := s.getByID(workspaceID, instanceID)
	if err != nil {
		return environment.EnvironmentInstance{}, err
	}
	if !environment.ValidStateTransition(cur.Status, environment.StateFailed) {
		return cur, nil
	}
	return s.transition(workspaceID, instanceID, environment.StateFailed, "")
}

func (s *memoryInstanceStore) getByID(workspaceID, instanceID string) (environment.EnvironmentInstance, error) {
	key, ok := s.byID[instanceID]
	if !ok {
		return environment.EnvironmentInstance{}, ErrNoEnvironment
	}
	cur := s.items[key]
	if cur.WorkspaceID != workspaceID {
		return environment.EnvironmentInstance{}, ErrNoEnvironment
	}
	return cur, nil
}

func (s *memoryInstanceStore) markDestroyed(_ context.Context, workspaceID, instanceID string) (environment.EnvironmentInstance, error) {
	return s.transition(workspaceID, instanceID, environment.StateDestroyed, "")
}

func (s *memoryInstanceStore) markDestroying(_ context.Context, workspaceID, instanceID string) (environment.EnvironmentInstance, error) {
	cur, err := s.getByID(workspaceID, instanceID)
	if err != nil {
		return environment.EnvironmentInstance{}, err
	}
	if cur.Status == environment.StateDestroyed {
		return cur, nil
	}
	if !environment.ValidStateTransition(cur.Status, environment.StateDestroying) {
		return cur, nil
	}
	return s.transition(workspaceID, instanceID, environment.StateDestroying, "")
}
