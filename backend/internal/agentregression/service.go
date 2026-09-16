package agentregression

import (
	"context"
	"errors"
	"fmt"

	"github.com/evalplane/evalplane/backend/internal/auth"
	"github.com/evalplane/evalplane/backend/internal/evaluationsuite"
	"github.com/evalplane/evalplane/backend/internal/identity"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

type store interface {
	Create(context.Context, DBTX, string, string, string, CreateInput) (Comparison, error)
	Get(context.Context, DBTX, string, string) (Comparison, error)
	ListTrials(context.Context, DBTX, string, string) ([]TrialRow, error)
	ListCriteria(context.Context, DBTX, string, string) ([]CriterionRow, error)
}
type roleRepository interface {
	GetRole(context.Context, identity.DBTX, string, string) (string, error)
}
type suiteStarter interface {
	StartSuiteExecutionWithRecorder(context.Context, auth.Principal, string, evaluationsuite.StartExecutionInput, evaluationsuite.ExecutionRecorder) (evaluationsuite.Execution, error)
}

type Service struct {
	pool   *pgxpool.Pool
	repo   store
	roles  roleRepository
	suites suiteStarter
}

func NewService(pool *pgxpool.Pool, repo store, roles roleRepository, suites suiteStarter) *Service {
	return &Service{pool: pool, repo: repo, roles: roles, suites: suites}
}

func (s *Service) Create(ctx context.Context, principal auth.Principal, workspaceID string, in CreateInput) (Comparison, error) {
	if err := validate(workspaceID, in); err != nil {
		return Comparison{}, err
	}
	if err := s.requireWrite(ctx, principal, workspaceID); err != nil {
		return Comparison{}, err
	}
	var comparison Comparison
	execution, err := s.suites.StartSuiteExecutionWithRecorder(ctx, principal, workspaceID, evaluationsuite.StartExecutionInput{SuiteVersionID: in.SuiteVersionID, AgentVersionIDs: []string{in.BaselineAgentVersionID, in.CandidateAgentVersionID}}, func(ctx context.Context, tx pgx.Tx, execution evaluationsuite.Execution) error {
		var createErr error
		comparison, createErr = s.repo.Create(ctx, tx, workspaceID, principal.UserID, execution.ID, in)
		return createErr
	})
	if err != nil {
		return Comparison{}, err
	}
	if comparison.SuiteExecutionID != execution.ID {
		return Comparison{}, identity.ErrConflict
	}
	return comparison, nil
}
func (s *Service) Get(ctx context.Context, principal auth.Principal, workspaceID, comparisonID string) (Comparison, error) {
	if err := validateIDs(workspaceID, comparisonID); err != nil {
		return Comparison{}, err
	}
	if _, err := s.role(ctx, principal, workspaceID); err != nil {
		return Comparison{}, err
	}
	return s.repo.Get(ctx, s.pool, workspaceID, comparisonID)
}
func (s *Service) Results(ctx context.Context, principal auth.Principal, workspaceID, comparisonID string) (Results, error) {
	if err := validateIDs(workspaceID, comparisonID); err != nil {
		return Results{}, err
	}
	if _, err := s.role(ctx, principal, workspaceID); err != nil {
		return Results{}, err
	}
	comparison, err := s.repo.Get(ctx, s.pool, workspaceID, comparisonID)
	if err != nil {
		return Results{}, err
	}
	trials, err := s.repo.ListTrials(ctx, s.pool, workspaceID, comparisonID)
	if err != nil {
		return Results{}, err
	}
	criteria, err := s.repo.ListCriteria(ctx, s.pool, workspaceID, comparisonID)
	if err != nil {
		return Results{}, err
	}
	return Compare(comparison, trials, criteria), nil
}
func (s *Service) role(ctx context.Context, p auth.Principal, workspaceID string) (string, error) {
	role, err := s.roles.GetRole(ctx, s.pool, workspaceID, p.UserID)
	if errors.Is(err, identity.ErrNotFound) {
		return "", auth.ErrForbidden
	}
	return role, err
}
func (s *Service) requireWrite(ctx context.Context, p auth.Principal, workspaceID string) error {
	role, err := s.role(ctx, p, workspaceID)
	if err != nil {
		return err
	}
	switch role {
	case "owner", "admin", "member":
		return nil
	}
	return auth.ErrForbidden
}
func validate(workspaceID string, in CreateInput) error {
	if err := validateIDs(workspaceID, in.SuiteVersionID, in.BaselineAgentVersionID, in.CandidateAgentVersionID); err != nil {
		return err
	}
	if in.BaselineAgentVersionID == in.CandidateAgentVersionID {
		return fmt.Errorf("%w: baseline and candidate agent versions must differ", identity.ErrValidation)
	}
	return nil
}
func validateIDs(ids ...string) error {
	for _, id := range ids {
		if identity.ValidateUUID(id) != nil {
			return fmt.Errorf("%w: identifiers must be valid UUIDs", identity.ErrValidation)
		}
	}
	return nil
}
