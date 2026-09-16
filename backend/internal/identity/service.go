package identity

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

type Service struct {
	pool       *pgxpool.Pool
	repository *Repository
}

func NewService(pool *pgxpool.Pool, repository *Repository) *Service {
	return &Service{pool: pool, repository: repository}
}

func (s *Service) CreateOrganization(ctx context.Context, in OrganizationInput) (Organization, error) {
	var err error
	in, err = ValidateOrganization(in)
	if err != nil {
		return Organization{}, err
	}
	return s.repository.CreateOrganization(ctx, s.pool, in)
}
func (s *Service) GetOrganization(ctx context.Context, id string) (Organization, error) {
	return s.repository.GetOrganization(ctx, s.pool, id)
}
func (s *Service) ListOrganizations(ctx context.Context, p Pagination) ([]Organization, error) {
	return s.repository.ListOrganizations(ctx, s.pool, p)
}

func (s *Service) CreateWorkspace(ctx context.Context, organizationID string, in WorkspaceInput) (Workspace, error) {
	var err error
	in, err = ValidateWorkspace(in)
	if err != nil {
		return Workspace{}, err
	}
	if _, err = s.repository.GetOrganization(ctx, s.pool, organizationID); err != nil {
		return Workspace{}, err
	}
	return s.repository.CreateWorkspace(ctx, s.pool, organizationID, in)
}
func (s *Service) GetWorkspace(ctx context.Context, id string) (Workspace, error) {
	return s.repository.GetWorkspace(ctx, s.pool, id)
}
func (s *Service) UpdateWorkspaceName(ctx context.Context, id, name string) (Workspace, error) {
	name = strings.TrimSpace(name)
	if err := validateName("workspace name", name); err != nil {
		return Workspace{}, err
	}
	return s.repository.UpdateWorkspaceName(ctx, s.pool, id, name)
}
func (s *Service) ListWorkspaces(ctx context.Context, organizationID string, p Pagination) ([]Workspace, error) {
	if _, err := s.repository.GetOrganization(ctx, s.pool, organizationID); err != nil {
		return nil, err
	}
	return s.repository.ListWorkspaces(ctx, s.pool, organizationID, p)
}

func (s *Service) CreateUser(ctx context.Context, in UserInput) (User, error) {
	var err error
	in, err = ValidateUser(in)
	if err != nil {
		return User{}, err
	}
	return s.repository.CreateUser(ctx, s.pool, in)
}
func (s *Service) GetUser(ctx context.Context, id string) (User, error) {
	return s.repository.GetUser(ctx, s.pool, id)
}

func (s *Service) CreateMembership(ctx context.Context, workspaceID, userID, role string) (Membership, error) {
	if err := ValidateRole(role); err != nil {
		return Membership{}, err
	}
	if _, err := s.repository.GetWorkspace(ctx, s.pool, workspaceID); err != nil {
		return Membership{}, err
	}
	if _, err := s.repository.GetUser(ctx, s.pool, userID); err != nil {
		return Membership{}, err
	}
	return s.repository.CreateMembership(ctx, s.pool, workspaceID, userID, role)
}
func (s *Service) ListMembers(ctx context.Context, workspaceID string, p Pagination) ([]Membership, error) {
	if _, err := s.repository.GetWorkspace(ctx, s.pool, workspaceID); err != nil {
		return nil, err
	}
	return s.repository.ListMembers(ctx, s.pool, workspaceID, p)
}
func (s *Service) UpdateMembership(ctx context.Context, workspaceID, userID, role string) (Membership, error) {
	if err := ValidateRole(role); err != nil {
		return Membership{}, err
	}
	return s.repository.UpdateMembership(ctx, s.pool, workspaceID, userID, role)
}
func (s *Service) DeleteMembership(ctx context.Context, workspaceID, userID string) error {
	return s.repository.DeleteMembership(ctx, s.pool, workspaceID, userID)
}

func (s *Service) Bootstrap(ctx context.Context, in BootstrapInput) (result BootstrapResult, err error) {
	in.Organization, err = ValidateOrganization(in.Organization)
	if err != nil {
		return result, err
	}
	in.Workspace, err = ValidateWorkspace(in.Workspace)
	if err != nil {
		return result, err
	}
	in.User, err = ValidateUser(in.User)
	if err != nil {
		return result, err
	}
	tx, err := s.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return result, fmt.Errorf("begin bootstrap transaction: %w", err)
	}
	defer func() {
		if err != nil {
			_ = tx.Rollback(ctx)
		}
	}()
	result.Organization, err = s.repository.CreateOrganization(ctx, tx, in.Organization)
	if err != nil {
		return result, err
	}
	result.Workspace, err = s.repository.CreateWorkspace(ctx, tx, result.Organization.ID, in.Workspace)
	if err != nil {
		return result, err
	}
	result.User, err = s.repository.CreateUser(ctx, tx, in.User)
	if err != nil {
		return result, err
	}
	result.Membership, err = s.repository.CreateMembership(ctx, tx, result.Workspace.ID, result.User.ID, "owner")
	if err != nil {
		return result, err
	}
	if err = tx.Commit(ctx); err != nil {
		if errors.Is(err, pgx.ErrTxClosed) {
			return result, err
		}
		return result, fmt.Errorf("commit bootstrap transaction: %w", err)
	}
	return result, nil
}
