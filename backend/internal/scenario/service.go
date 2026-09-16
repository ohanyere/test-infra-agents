package scenario

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
func (s *Service) ListPublic(ctx context.Context, f Filters) ([]Scenario, error) {
	f, e := ValidateFilters(f, true)
	if e != nil {
		return nil, e
	}
	return s.repo.ListPublic(ctx, s.pool, f)
}
func (s *Service) GetPublic(ctx context.Context, slug string) (Scenario, error) {
	return s.repo.GetPublic(ctx, s.pool, slug)
}
func (s *Service) ListWorkspace(ctx context.Context, p auth.Principal, w string, f Filters) ([]Scenario, error) {
	if _, e := s.role(ctx, p, w); e != nil {
		return nil, e
	}
	f, e := ValidateFilters(f, false)
	if e != nil {
		return nil, e
	}
	return s.repo.ListWorkspace(ctx, s.pool, w, f)
}
func (s *Service) GetWorkspace(ctx context.Context, p auth.Principal, w, id string) (Scenario, error) {
	if _, e := s.role(ctx, p, w); e != nil {
		return Scenario{}, e
	}
	return s.repo.GetWorkspace(ctx, s.pool, w, id)
}
func (s *Service) Create(ctx context.Context, p auth.Principal, w string, in CreateInput) (out Scenario, err error) {
	if err = s.write(ctx, p, w); err != nil {
		return
	}
	if in, err = ValidateCreate(in); err != nil {
		return
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
	if out, err = s.repo.Create(ctx, tx, w, in); err != nil {
		return
	}
	v, e := s.repo.CreateVersion(ctx, tx, out.ID, 1, in.InitialVersion)
	if e != nil {
		err = e
		return
	}
	v.Criteria, e = s.repo.CreateCriteria(ctx, tx, v.ID, in.InitialVersion.Criteria)
	if e != nil {
		err = e
		return
	}
	out.LatestVersion = &v
	err = tx.Commit(ctx)
	return
}
func (s *Service) Update(ctx context.Context, p auth.Principal, w, id string, in UpdateInput) (Scenario, error) {
	if e := s.write(ctx, p, w); e != nil {
		return Scenario{}, e
	}
	in, e := ValidateUpdate(in)
	if e != nil {
		return Scenario{}, e
	}
	return s.repo.Update(ctx, s.pool, w, id, in)
}
func (s *Service) CreateVersion(ctx context.Context, p auth.Principal, w, id string, in VersionInput) (out Version, err error) {
	if err = s.write(ctx, p, w); err != nil {
		return
	}
	if in, err = ValidateVersion(in); err != nil {
		return
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
	number, status, e := s.repo.LockAndNextVersion(ctx, tx, w, id)
	if e != nil {
		err = e
		return
	}
	if status == "archived" {
		err = identity.ErrConflict
		return
	}
	if out, e = s.repo.CreateVersion(ctx, tx, id, number, in); e != nil {
		err = e
		return
	}
	if out.Criteria, e = s.repo.CreateCriteria(ctx, tx, out.ID, in.Criteria); e != nil {
		err = e
		return
	}
	err = tx.Commit(ctx)
	return
}
func (s *Service) ListVersions(ctx context.Context, p auth.Principal, w, id string, page identity.Pagination) ([]Version, error) {
	if _, e := s.role(ctx, p, w); e != nil {
		return nil, e
	}
	if _, e := s.repo.GetWorkspace(ctx, s.pool, w, id); e != nil {
		return nil, e
	}
	return s.repo.ListVersions(ctx, s.pool, w, id, page)
}
func (s *Service) GetVersion(ctx context.Context, p auth.Principal, w, id, versionID string) (Version, error) {
	if _, e := s.role(ctx, p, w); e != nil {
		return Version{}, e
	}
	return s.repo.GetVersion(ctx, s.pool, w, id, versionID)
}
func (s *Service) role(ctx context.Context, p auth.Principal, w string) (string, error) {
	role, e := s.roles.GetRole(ctx, s.pool, w, p.UserID)
	if errors.Is(e, identity.ErrNotFound) {
		return "", auth.ErrForbidden
	}
	return role, e
}
func (s *Service) write(ctx context.Context, p auth.Principal, w string) error {
	role, e := s.role(ctx, p, w)
	if e != nil {
		return e
	}
	if role == "viewer" {
		return auth.ErrForbidden
	}
	return nil
}
