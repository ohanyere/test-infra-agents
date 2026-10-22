package verification

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
type Service struct {
	pool      *pgxpool.Pool
	repo      *Repository
	roles     roleRepository
	now       func() time.Time
	analyzers []Analyzer
}

func NewService(pool *pgxpool.Pool, repo *Repository, roles roleRepository) *Service {
	return &Service{pool: pool, repo: repo, roles: roles, now: time.Now, analyzers: []Analyzer{HPAConsistencyAnalyzer{}, HPACapacityHeadroomAnalyzer{}, GitOpsReconciliationAnalyzer{}}}
}
func (s *Service) DeriveRun(ctx context.Context, workspaceID, runID string) (Result, error) {
	in, err := s.repo.LoadInput(ctx, s.pool, workspaceID, runID)
	if err != nil {
		return Result{}, err
	}
	in.Now = s.now().UTC()
	return s.repo.Insert(ctx, s.pool, Derive(in, s.analyzers...))
}
func (s *Service) Get(ctx context.Context, p auth.Principal, workspaceID, runID string) (Result, error) {
	if _, err := s.roles.GetRole(ctx, s.pool, workspaceID, p.UserID); err != nil {
		if errors.Is(err, identity.ErrNotFound) {
			return Result{}, auth.ErrForbidden
		}
		return Result{}, err
	}
	return s.repo.Get(ctx, s.pool, workspaceID, runID)
}

// GetInternal is for server-owned background processing after the caller has
// resolved the workspace and run from trusted database records.
func (s *Service) GetInternal(ctx context.Context, workspaceID, runID string) (Result, error) {
	return s.repo.Get(ctx, s.pool, workspaceID, runID)
}
