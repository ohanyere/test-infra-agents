package executionworker

import (
	"context"
	"errors"
	"time"

	"github.com/evalplane/evalplane/backend/internal/environment"
	"github.com/evalplane/evalplane/backend/internal/identity"
	"github.com/jackc/pgx/v5/pgxpool"
)

// postgresInstanceStore persists EnvironmentInstance rows per existing
// repository conventions. Workspace scoping is mandatory; no cross-workspace
// reuse is possible through these methods.
type postgresInstanceStore struct {
	pool *pgxpool.Pool
	repo *environment.Repository
}

func (s *postgresInstanceStore) insertProvisioning(ctx context.Context, workspaceID, ownerID, _ string, decision environment.SchedulerDecision) (environment.EnvironmentInstance, error) {
	if decision.Profile == nil || decision.Provider == nil {
		return environment.EnvironmentInstance{}, errors.New("no eligible environment")
	}
	lease := time.Now().UTC().Add(leaseTTLForRun())
	return s.repo.InsertInstance(ctx, s.pool, environment.EnvironmentInstance{
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
	})
}

func (s *postgresInstanceStore) getByOwner(ctx context.Context, workspaceID, runID string) (environment.EnvironmentInstance, error) {
	out, err := s.repo.GetByOwner(ctx, s.pool, workspaceID, environment.OwnerRun, runID)
	if err != nil {
		if errors.Is(err, identity.ErrNotFound) {
			return environment.EnvironmentInstance{}, ErrNoEnvironment
		}
		return environment.EnvironmentInstance{}, err
	}
	return out, nil
}

func (s *postgresInstanceStore) markReady(ctx context.Context, workspaceID, instanceID, ref string) (environment.EnvironmentInstance, error) {
	return s.repo.TransitionInstance(ctx, s.pool, workspaceID, instanceID, environment.StateReady, ref)
}

func (s *postgresInstanceStore) markFailed(ctx context.Context, workspaceID, instanceID string) (environment.EnvironmentInstance, error) {
	out, err := s.repo.TransitionInstance(ctx, s.pool, workspaceID, instanceID, environment.StateFailed, "")
	if err != nil {
		// If the row already moved beyond failed (e.g. destroying), keep it
		// discoverable for the sweeper and let the original failure propagate.
		current, getErr := s.repo.GetInstance(ctx, s.pool, workspaceID, instanceID)
		if getErr != nil {
			return environment.EnvironmentInstance{}, err
		}
		return current, nil
	}
	return out, nil
}

func (s *postgresInstanceStore) markDestroyed(ctx context.Context, workspaceID, instanceID string) (environment.EnvironmentInstance, error) {
	return s.repo.TransitionInstance(ctx, s.pool, workspaceID, instanceID, environment.StateDestroyed, "")
}

func (s *postgresInstanceStore) markDestroying(ctx context.Context, workspaceID, instanceID string) (environment.EnvironmentInstance, error) {
	current, err := s.repo.GetInstance(ctx, s.pool, workspaceID, instanceID)
	if err != nil {
		return environment.EnvironmentInstance{}, err
	}
	if current.Status == environment.StateDestroyed {
		return current, nil
	}
	if !environment.ValidStateTransition(current.Status, environment.StateDestroying) {
		return current, nil
	}
	return s.repo.TransitionInstance(ctx, s.pool, workspaceID, instanceID, environment.StateDestroying, "")
}

// NewPostgresRunEnvironments wires the production-shaped run path: exact
// ScenarioVersion requirements → server-owned scheduler → run-owned
// EnvironmentInstance → provider provision → ready. Providers arrive via the
// abstraction; pass the Kind development provider here for local runs and
// future production providers without changing the worker.
func NewPostgresRunEnvironments(pool *pgxpool.Pool, repo *environment.Repository, availability environment.ProviderAvailability, providers map[string]environment.Provider) RunEnvironments {
	if availability == nil {
		availability = environment.DefaultAvailability()
	}
	registry := map[string]environment.Provider{}
	for id, p := range providers {
		if p != nil {
			registry[id] = p
		}
	}
	store := &postgresInstanceStore{pool: pool, repo: repo}
	return &managedRunEnvironments{
		requirements: func(ctx context.Context, versionID string) (environment.Requirements, bool, error) {
			if versionID == "" || pool == nil || repo == nil {
				return environment.LegacyDefaultRequirements(), false, nil
			}
			row, err := repo.GetRequirements(ctx, pool, versionID)
			if err != nil {
				if errors.Is(err, identity.ErrNotFound) {
					return environment.LegacyDefaultRequirements(), false, nil
				}
				return environment.Requirements{}, false, err
			}
			return row.Requirements(), true, nil
		},
		availability: availability,
		instances:    store,
		providers: func(id string) (environment.Provider, bool) {
			p, ok := registry[id]
			return p, ok
		},
	}
}
