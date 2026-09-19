package evaluationbatch

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

func NewService(p *pgxpool.Pool, r *Repository, roles roleRepository) *Service {
	return &Service{pool: p, repo: r, roles: roles}
}
func (s *Service) Create(ctx context.Context, p auth.Principal, w string, in CreateInput) (out Batch, err error) {
	if err = s.requireWrite(ctx, p, w); err != nil {
		return
	}
	if in, err = ValidateCreate(in); err != nil {
		return
	}
	if in.IdempotencyKey != "" {
		if existing, found, findErr := s.repo.FindByIdempotencyKey(ctx, s.pool, w, in.IdempotencyKey); findErr != nil {
			err = findErr
			return
		} else if found {
			return existing, nil
		}
	}
	tx, e := s.pool.BeginTx(ctx, pgx.TxOptions{})
	if e != nil {
		err = e
		return
	}
	defer func() {
		if err != nil {
			_ = tx.Rollback(ctx)
		}
	}()
	scenario, e := s.repo.LoadUsableScenario(ctx, tx, w, in.ScenarioVersionID)
	if e != nil {
		err = e
		return
	}
	agents, e := s.repo.LoadUsableAgents(ctx, tx, w, in.AgentVersionIDs)
	if e != nil {
		err = e
		return
	}
	out, e = s.repo.CreateBatch(ctx, tx, w, p.UserID, in)
	if e != nil {
		err = e
		return
	}
	if e = s.repo.AddAgentsAndRuns(ctx, tx, out.ID, in.AgentVersionIDs, in.RunsPerAgent); e != nil {
		err = e
		return
	}
	out.Scenario = scenario
	out.Agents = agents
	out.AgentCount = len(agents)
	out.RunCount = len(agents) * in.RunsPerAgent
	err = tx.Commit(ctx)
	return
}
func (s *Service) List(ctx context.Context, p auth.Principal, w string, f Filters) ([]Batch, error) {
	if _, e := s.role(ctx, p, w); e != nil {
		return nil, e
	}
	f, e := ValidateFilters(f)
	if e != nil {
		return nil, e
	}
	return s.repo.ListBatches(ctx, s.pool, w, f)
}
func (s *Service) Get(ctx context.Context, p auth.Principal, w, id string) (Batch, error) {
	if _, e := s.role(ctx, p, w); e != nil {
		return Batch{}, e
	}
	return s.repo.GetBatch(ctx, s.pool, w, id)
}
func (s *Service) ListRuns(ctx context.Context, p auth.Principal, w, batch string, f Filters) ([]Run, error) {
	if _, e := s.role(ctx, p, w); e != nil {
		return nil, e
	}
	f, e := ValidateRunFilters(f)
	if e != nil {
		return nil, e
	}
	if _, e = s.repo.GetBatch(ctx, s.pool, w, batch); e != nil {
		return nil, e
	}
	return s.repo.ListRuns(ctx, s.pool, w, batch, f)
}
func (s *Service) GetRun(ctx context.Context, p auth.Principal, w, id string) (Run, error) {
	if _, e := s.role(ctx, p, w); e != nil {
		return Run{}, e
	}
	return s.repo.GetRun(ctx, s.pool, w, id)
}
func (s *Service) role(ctx context.Context, p auth.Principal, w string) (string, error) {
	role, e := s.roles.GetRole(ctx, s.pool, w, p.UserID)
	if errors.Is(e, identity.ErrNotFound) {
		return "", auth.ErrForbidden
	}
	return role, e
}
func (s *Service) requireWrite(ctx context.Context, p auth.Principal, w string) error {
	role, e := s.role(ctx, p, w)
	if e != nil {
		return e
	}
	switch role {
	case "owner", "admin", "member":
		return nil
	default:
		return auth.ErrForbidden
	}
}
