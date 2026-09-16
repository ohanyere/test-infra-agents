package failuretaxonomy

import (
	"context"
	"errors"
	"fmt"
	"github.com/evalplane/evalplane/backend/internal/auth"
	"github.com/evalplane/evalplane/backend/internal/identity"
	"github.com/jackc/pgx/v5/pgxpool"
)

type roleRepository interface {
	GetRole(context.Context, identity.DBTX, string, string) (string, error)
}
type occurrenceStore interface {
	List(context.Context, DBTX, string, string) ([]Occurrence, error)
}
type Service struct {
	pool  *pgxpool.Pool
	repo  occurrenceStore
	roles roleRepository
}

func NewService(pool *pgxpool.Pool, repo occurrenceStore, roles roleRepository) *Service {
	return &Service{pool: pool, repo: repo, roles: roles}
}
func (s *Service) ForExecution(ctx context.Context, p auth.Principal, workspaceID, executionID string) (Results, error) {
	if identity.ValidateUUID(workspaceID) != nil || identity.ValidateUUID(executionID) != nil {
		return Results{}, fmt.Errorf("%w: identifiers must be valid UUIDs", identity.ErrValidation)
	}
	_, err := s.roles.GetRole(ctx, s.pool, workspaceID, p.UserID)
	if errors.Is(err, identity.ErrNotFound) {
		return Results{}, auth.ErrForbidden
	}
	if err != nil {
		return Results{}, err
	}
	rows, err := s.repo.List(ctx, s.pool, workspaceID, executionID)
	if err != nil {
		return Results{}, err
	}
	return Aggregate(executionID, rows), nil
}
