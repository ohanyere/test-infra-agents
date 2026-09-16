package agent

import (
	"context"
	"errors"

	"github.com/evalplane/evalplane/backend/internal/auth"
	"github.com/evalplane/evalplane/backend/internal/identity"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

type roleRepository interface {
	GetRole(context.Context, identity.DBTX, string, string) (string, error)
}

type Service struct {
	pool  *pgxpool.Pool
	repo  *Repository
	roles roleRepository
}

func NewService(pool *pgxpool.Pool, repo *Repository, roles roleRepository) *Service {
	return &Service{pool: pool, repo: repo, roles: roles}
}

func (s *Service) ListPublic(ctx context.Context, filters Filters) ([]Agent, error) {
	filters, err := ValidateFilters(filters, true)
	if err != nil {
		return nil, err
	}
	return s.repo.ListPublic(ctx, s.pool, filters)
}
func (s *Service) GetPublic(ctx context.Context, slug string) (Agent, error) {
	return s.repo.GetPublicBySlug(ctx, s.pool, slug)
}

func (s *Service) ListWorkspace(ctx context.Context, p auth.Principal, workspaceID string, filters Filters) ([]Agent, error) {
	if _, err := s.workspaceRole(ctx, p, workspaceID); err != nil {
		return nil, err
	}
	filters, err := ValidateFilters(filters, false)
	if err != nil {
		return nil, err
	}
	return s.repo.ListWorkspace(ctx, s.pool, workspaceID, filters)
}
func (s *Service) GetWorkspace(ctx context.Context, p auth.Principal, workspaceID, agentID string) (Agent, error) {
	if _, err := s.workspaceRole(ctx, p, workspaceID); err != nil {
		return Agent{}, err
	}
	return s.repo.GetWorkspaceAgent(ctx, s.pool, workspaceID, agentID)
}

func (s *Service) Create(ctx context.Context, p auth.Principal, workspaceID string, input CreateInput) (result Agent, err error) {
	if err = s.requireWrite(ctx, p, workspaceID); err != nil {
		return result, err
	}
	if input, err = ValidateCreate(input); err != nil {
		return result, err
	}
	tx, err := s.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return result, err
	}
	defer func() {
		if err != nil {
			_ = tx.Rollback(ctx)
		}
	}()
	if result, err = s.repo.Create(ctx, tx, workspaceID, input); err != nil {
		return result, err
	}
	version, err := s.repo.CreateVersion(ctx, tx, result.ID, input.InitialVersion)
	if err != nil {
		return Agent{}, err
	}
	result.LatestVersion = &version
	err = tx.Commit(ctx)
	return result, err
}

func (s *Service) Update(ctx context.Context, p auth.Principal, workspaceID, agentID string, input UpdateInput) (Agent, error) {
	if err := s.requireWrite(ctx, p, workspaceID); err != nil {
		return Agent{}, err
	}
	input, err := ValidateUpdate(input)
	if err != nil {
		return Agent{}, err
	}
	return s.repo.Update(ctx, s.pool, workspaceID, agentID, input)
}

func (s *Service) CreateVersion(ctx context.Context, p auth.Principal, workspaceID, agentID string, input VersionInput) (Version, error) {
	if err := s.requireWrite(ctx, p, workspaceID); err != nil {
		return Version{}, err
	}
	input, err := ValidateVersion(input)
	if err != nil {
		return Version{}, err
	}
	value, err := s.repo.GetWorkspaceAgent(ctx, s.pool, workspaceID, agentID)
	if err != nil {
		return Version{}, err
	}
	if value.Status == "archived" {
		return Version{}, identity.ErrConflict
	}
	return s.repo.CreateVersion(ctx, s.pool, agentID, input)
}

func (s *Service) ListVersions(ctx context.Context, p auth.Principal, workspaceID, agentID string, page identity.Pagination) ([]Version, error) {
	if _, err := s.workspaceRole(ctx, p, workspaceID); err != nil {
		return nil, err
	}
	if _, err := s.repo.GetWorkspaceAgent(ctx, s.pool, workspaceID, agentID); err != nil {
		return nil, err
	}
	return s.repo.ListVersions(ctx, s.pool, workspaceID, agentID, page)
}
func (s *Service) GetVersion(ctx context.Context, p auth.Principal, workspaceID, agentID, versionID string) (Version, error) {
	if _, err := s.workspaceRole(ctx, p, workspaceID); err != nil {
		return Version{}, err
	}
	return s.repo.GetVersion(ctx, s.pool, workspaceID, agentID, versionID)
}

func (s *Service) ListVersionClaims(ctx context.Context, p auth.Principal, workspaceID, agentID, versionID string) ([]CapabilityClaim, error) {
	if _, err := s.workspaceRole(ctx, p, workspaceID); err != nil {
		return nil, err
	}
	return s.repo.ListVersionClaims(ctx, s.pool, workspaceID, agentID, versionID)
}

func (s *Service) workspaceRole(ctx context.Context, p auth.Principal, workspaceID string) (string, error) {
	role, err := s.roles.GetRole(ctx, s.pool, workspaceID, p.UserID)
	if errors.Is(err, identity.ErrNotFound) {
		return "", auth.ErrForbidden
	}
	return role, err
}
func (s *Service) requireWrite(ctx context.Context, p auth.Principal, workspaceID string) error {
	role, err := s.workspaceRole(ctx, p, workspaceID)
	if err != nil {
		return err
	}
	if role == "viewer" {
		return auth.ErrForbidden
	}
	return nil
}
