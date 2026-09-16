package environment

import (
	"context"
	"errors"
	"time"

	"github.com/evalplane/evalplane/backend/internal/auth"
	"github.com/evalplane/evalplane/backend/internal/identity"
	"github.com/jackc/pgx/v5/pgxpool"
)

type roleRepository interface {
	GetRole(context.Context, identity.DBTX, string, string) (string, error)
}

// Service owns server-reviewed environment requirements, the deterministic
// scheduler, and workspace-scoped EnvironmentInstance lifecycle. Reads allow
// viewer; mutations require owner/admin/member.
type Service struct {
	pool         *pgxpool.Pool
	repo         *Repository
	roles        roleRepository
	availability ProviderAvailability
}

// NewService creates an environment Service.
func NewService(pool *pgxpool.Pool, repo *Repository, roles roleRepository, availability ProviderAvailability) *Service {
	if availability == nil {
		availability = DefaultAvailability()
	}
	return &Service{pool: pool, repo: repo, roles: roles, availability: availability}
}

// Components returns the server-owned component catalog.
func (s *Service) Components() []Component { return ListComponents() }

// Profiles returns the server-owned profile catalog.
func (s *Service) Profiles() []Profile { return ListProfiles() }

// Providers returns the server-owned provider catalog. Planned providers
// carry no fake health; callers check Health separately.
func (s *Service) Providers() []ProviderInfo { return ListProviders() }

// ProviderHealth returns observed availability for one provider.
func (s *Service) ProviderHealth(ctx context.Context, providerID string) (ProviderHealth, error) {
	if _, ok := GetProvider(providerID); !ok {
		return ProviderHealth{}, identity.ErrNotFound
	}
	return s.availability.Health(ctx, providerID), nil
}

// SetRequirements stores server-reviewed requirements for one exact
// ScenarioVersion. Write-capable roles only. Requirements are evaluator
// metadata: they carry no ground truth, reference remediation, hidden
// variant values, or grader internals by construction.
func (s *Service) SetRequirements(ctx context.Context, principal auth.Principal, workspaceID, scenarioID, versionID string, in Requirements) (Requirements, error) {
	if err := validateVersionIDs(workspaceID, scenarioID, versionID); err != nil {
		return Requirements{}, err
	}
	if err := s.requireWrite(ctx, principal, workspaceID); err != nil {
		return Requirements{}, err
	}
	normalized, err := ValidateRequirements(in)
	if err != nil {
		return Requirements{}, err
	}
	owner, err := s.scenarioWorkspace(ctx, scenarioID, versionID)
	if err != nil {
		return Requirements{}, err
	}
	if owner != workspaceID {
		return Requirements{}, auth.ErrForbidden
	}
	return s.repo.UpsertRequirements(ctx, s.pool, workspaceID, versionID, normalized)
}

// GetRequirements loads requirements for one ScenarioVersion, falling back
// to the legacy compatibility default when none are stored. Any workspace
// member (including viewer) may read.
func (s *Service) GetRequirements(ctx context.Context, principal auth.Principal, workspaceID, scenarioID, versionID string) (Requirements, bool, error) {
	if err := validateVersionIDs(workspaceID, scenarioID, versionID); err != nil {
		return Requirements{}, false, err
	}
	if _, err := s.role(ctx, principal, workspaceID); err != nil {
		return Requirements{}, false, err
	}
	owner, err := s.scenarioWorkspace(ctx, scenarioID, versionID)
	if err != nil {
		return Requirements{}, false, err
	}
	if owner != workspaceID {
		return Requirements{}, false, auth.ErrForbidden
	}
	row, err := s.repo.GetRequirements(ctx, s.pool, versionID)
	if err != nil {
		if errors.Is(err, identity.ErrNotFound) {
			return LegacyDefaultRequirements(), false, nil
		}
		return Requirements{}, false, err
	}
	return row.Requirements(), true, nil
}

// ResolveForCertification is the service-internal scheduler entry used by
// certification and run execution: requirements come from the stored
// server-reviewed row (or the legacy default), never from caller text.
// Preference must be a known server-owned active profile satisfying
// requirements; unknown or unsatisfiable preferences yield no eligible
// environment rather than a downgrade.
func (s *Service) ResolveForCertification(ctx context.Context, versionID, preference string) (SchedulerDecision, Requirements, error) {
	requirements, err := s.requirementsForVersion(ctx, versionID)
	if err != nil {
		return SchedulerDecision{}, Requirements{}, err
	}
	if preference != "" {
		profile, ok := GetProfile(preference)
		if !ok {
			return SchedulerDecision{}, Requirements{}, validation("unknown environment profile " + quote(preference) + ": select a server-owned profile")
		}
		if !profile.Executable() {
			return SchedulerDecision{}, Requirements{}, validation("environment profile " + quote(preference) + " is not available for execution")
		}
		for _, need := range requirements.RequiredComponents {
			if !profile.HasComponent(need) {
				return SchedulerDecision{}, Requirements{}, validation("environment profile " + quote(preference) + " does not satisfy task requirements")
			}
		}
		if !SatisfiesIsolation(profile.Isolation, requirements.MinimumIsolation) {
			return SchedulerDecision{}, Requirements{}, validation("environment profile " + quote(preference) + " does not satisfy minimum isolation")
		}
	}
	decision := Select(ctx, SchedulerInput{
		Requirements:      requirements,
		Purpose:           "certification",
		ProfilePreference: preference,
	}, s.availability)
	return decision, requirements, nil
}

// SelectForRequirements is the pure deterministic entry for run execution:
// stored requirements in, placement out. Legacy versions without explicit
// requirements resolve through the compatibility default (namespace only,
// never stronger isolation).
func SelectForRequirements(ctx context.Context, req Requirements, purpose, preference string, availability ProviderAvailability) SchedulerDecision {
	if len(req.RequiredComponents) == 0 {
		req = LegacyDefaultRequirements()
	}
	if availability == nil {
		availability = DefaultAvailability()
	}
	return Select(ctx, SchedulerInput{
		Requirements:      req,
		Purpose:           purpose,
		ProfilePreference: preference,
	}, availability)
}

// ResolveRequest selects placement for one ScenarioVersion reference.
// Authoritative execution passes a ScenarioVersion; arbitrary caller
// requirements are never accepted here.
type ResolveRequest struct {
	ScenarioID        string
	ScenarioVersionID string
	Purpose           string
	ProfilePreference string
}

// Resolve runs the deterministic scheduler for one ScenarioVersion.
func (s *Service) Resolve(ctx context.Context, principal auth.Principal, workspaceID string, req ResolveRequest) (SchedulerDecision, Requirements, error) {
	if identity.ValidateUUID(workspaceID) != nil {
		return SchedulerDecision{}, Requirements{}, validation("workspace_id must be a valid UUID")
	}
	if identity.ValidateUUID(req.ScenarioID) != nil || identity.ValidateUUID(req.ScenarioVersionID) != nil {
		return SchedulerDecision{}, Requirements{}, validation("scenario_id and scenario_version_id must be valid UUIDs")
	}
	if _, err := s.role(ctx, principal, workspaceID); err != nil {
		return SchedulerDecision{}, Requirements{}, err
	}
	owner, err := s.scenarioWorkspace(ctx, req.ScenarioID, req.ScenarioVersionID)
	if err != nil {
		return SchedulerDecision{}, Requirements{}, err
	}
	if owner != workspaceID {
		return SchedulerDecision{}, Requirements{}, auth.ErrForbidden
	}
	requirements, err := s.requirementsForVersion(ctx, req.ScenarioVersionID)
	if err != nil {
		return SchedulerDecision{}, Requirements{}, err
	}
	purpose := req.Purpose
	if purpose == "" {
		purpose = "trial"
	}
	decision := Select(ctx, SchedulerInput{
		WorkspaceID:       workspaceID,
		Requirements:      requirements,
		Purpose:           purpose,
		ProfilePreference: req.ProfilePreference,
	}, s.availability)
	return decision, requirements, nil
}

// ProvisionForOwner resolves placement and records one provisioning
// EnvironmentInstance. Every environment belongs to a workspace and a
// specific execution owner; instances are never reused across unrelated
// tenants. Namespace shared-cluster execution shares the physical provider
// but never the instance ownership boundary.
func (s *Service) ProvisionForOwner(ctx context.Context, principal auth.Principal, workspaceID, ownerType, ownerID, batchID string, decision SchedulerDecision) (EnvironmentInstance, error) {
	if err := s.requireWrite(ctx, principal, workspaceID); err != nil {
		return EnvironmentInstance{}, err
	}
	if !IsValidOwnerType(ownerType) {
		return EnvironmentInstance{}, validation("owner_type must be run or certification")
	}
	if identity.ValidateUUID(workspaceID) != nil || identity.ValidateUUID(ownerID) != nil {
		return EnvironmentInstance{}, validation("workspace_id and owner_id must be valid UUIDs")
	}
	if decision.Profile == nil || decision.Provider == nil {
		return EnvironmentInstance{}, validation("no eligible environment: resolve placement before provisioning")
	}
	profile, provider := *decision.Profile, *decision.Provider
	lease := time.Now().UTC().Add(2 * time.Hour)
	_ = batchID
	return s.repo.InsertInstance(ctx, s.pool, EnvironmentInstance{
		WorkspaceID:             workspaceID,
		OwnerType:               ownerType,
		OwnerID:                 ownerID,
		ProfileID:               profile.ID,
		ProfileContractVersion:  profile.ContractVersion,
		ProviderID:              provider.ID,
		ProviderContractVersion: provider.ContractVersion,
		IsolationTier:           profile.Isolation,
		Status:                  StateProvisioning,
		LeaseExpiresAt:          &lease,
	})
}

// GetInstance loads one instance scoped by workspace.
func (s *Service) GetInstance(ctx context.Context, principal auth.Principal, workspaceID, environmentID string) (EnvironmentInstance, error) {
	if _, err := s.role(ctx, principal, workspaceID); err != nil {
		return EnvironmentInstance{}, err
	}
	if identity.ValidateUUID(workspaceID) != nil || identity.ValidateUUID(environmentID) != nil {
		return EnvironmentInstance{}, validation("workspace_id and environment_id must be valid UUIDs")
	}
	return s.repo.GetInstance(ctx, s.pool, workspaceID, environmentID)
}

// ListInstances lists workspace instances newest-first.
func (s *Service) ListInstances(ctx context.Context, principal auth.Principal, workspaceID string, limit int) ([]EnvironmentInstance, error) {
	if _, err := s.role(ctx, principal, workspaceID); err != nil {
		return nil, err
	}
	if identity.ValidateUUID(workspaceID) != nil {
		return nil, validation("workspace_id must be a valid UUID")
	}
	return s.repo.ListInstances(ctx, s.pool, workspaceID, limit)
}

// TransitionInstance moves one instance forward. Write-capable roles only.
// Destroy is idempotent; destroyed never returns to ready.
func (s *Service) TransitionInstance(ctx context.Context, principal auth.Principal, workspaceID, environmentID, to string) (EnvironmentInstance, error) {
	if err := s.requireWrite(ctx, principal, workspaceID); err != nil {
		return EnvironmentInstance{}, err
	}
	if !IsStoredState(to) {
		return EnvironmentInstance{}, validation("unknown environment state")
	}
	return s.repo.TransitionInstance(ctx, s.pool, workspaceID, environmentID, to, "")
}

func (s *Service) requirementsForVersion(ctx context.Context, versionID string) (Requirements, error) {
	row, err := s.repo.GetRequirements(ctx, s.pool, versionID)
	if err != nil {
		if errors.Is(err, identity.ErrNotFound) {
			return LegacyDefaultRequirements(), nil
		}
		return Requirements{}, err
	}
	return row.Requirements(), nil
}

func (s *Service) scenarioWorkspace(ctx context.Context, scenarioID, versionID string) (string, error) {
	var workspaceID *string
	err := s.pool.QueryRow(ctx, `SELECT s.owner_workspace_id FROM scenario_versions sv JOIN scenarios s ON s.id=sv.scenario_id WHERE sv.id=$1 AND sv.scenario_id=$2`, versionID, scenarioID).Scan(&workspaceID)
	if err != nil {
		return "", identity.ErrNotFound
	}
	if workspaceID == nil {
		return "", identity.ErrNotFound
	}
	return *workspaceID, nil
}

func (s *Service) role(ctx context.Context, principal auth.Principal, workspaceID string) (string, error) {
	role, err := s.roles.GetRole(ctx, s.pool, workspaceID, principal.UserID)
	if errors.Is(err, identity.ErrNotFound) {
		return "", auth.ErrForbidden
	}
	return role, err
}

func (s *Service) requireWrite(ctx context.Context, principal auth.Principal, workspaceID string) error {
	role, err := s.role(ctx, principal, workspaceID)
	if err != nil {
		return err
	}
	switch role {
	case "owner", "admin", "member":
		return nil
	default:
		return auth.ErrForbidden
	}
}

func validateVersionIDs(workspaceID, scenarioID, versionID string) error {
	if identity.ValidateUUID(workspaceID) != nil {
		return validation("workspace_id must be a valid UUID")
	}
	if identity.ValidateUUID(scenarioID) != nil {
		return validation("scenario_id must be a valid UUID")
	}
	if identity.ValidateUUID(versionID) != nil {
		return validation("scenario_version_id must be a valid UUID")
	}
	return nil
}

func validation(message string) error { return validationError{message: message} }
