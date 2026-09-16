package evidence

import (
	"context"
	"errors"
	"fmt"

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

// AppendRunEvent is intended for trusted executor code, not browser-facing HTTP.
func (s *Service) AppendRunEvent(ctx context.Context, input AppendRunEventInput) (out RunEvent, err error) {
	input, err = ValidateRunEvent(input)
	if err != nil {
		return out, err
	}
	tx, err := s.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return out, err
	}
	defer func() {
		if err != nil {
			_ = tx.Rollback(ctx)
		}
	}()
	if err = s.repo.LockRun(ctx, tx, input.RunID); err != nil {
		return out, err
	}
	var sequence int64
	if sequence, err = s.repo.NextEventSequence(ctx, tx, input.RunID); err != nil {
		return out, err
	}
	if out, err = s.repo.InsertRunEvent(ctx, tx, input, sequence); err != nil {
		return out, err
	}
	err = tx.Commit(ctx)
	return out, err
}

// AppendRunEventOnce serializes on the run and makes stable recovery codes
// retry-safe without adding a parallel event store.
func (s *Service) AppendRunEventOnce(ctx context.Context, input AppendRunEventInput) (out RunEvent, err error) {
	input, err = ValidateRunEvent(input)
	if err != nil {
		return out, err
	}
	tx, err := s.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return out, err
	}
	defer func() {
		if err != nil {
			_ = tx.Rollback(ctx)
		}
	}()
	if err = s.repo.LockRun(ctx, tx, input.RunID); err != nil {
		return out, err
	}
	var found bool
	if out, found, err = s.repo.FindRunEventBySummary(ctx, tx, input.RunID, input.Summary); err != nil || found {
		if err == nil {
			err = tx.Commit(ctx)
		}
		return out, err
	}
	var sequence int64
	if sequence, err = s.repo.NextEventSequence(ctx, tx, input.RunID); err != nil {
		return out, err
	}
	if out, err = s.repo.InsertRunEvent(ctx, tx, input, sequence); err != nil {
		return out, err
	}
	err = tx.Commit(ctx)
	return out, err
}

// RecordAgentAction is intended for trusted executor code. Payloads must be redacted by its caller.
func (s *Service) RecordAgentAction(ctx context.Context, input RecordAgentActionInput) (AgentAction, error) {
	input, err := ValidateAgentAction(input)
	if err != nil {
		return AgentAction{}, err
	}
	if input.RunEventID != nil {
		belongs, err := s.repo.EventBelongsToRun(ctx, s.pool, input.RunID, *input.RunEventID)
		if err != nil {
			return AgentAction{}, err
		}
		if !belongs {
			return AgentAction{}, invalid("run_event_id must belong to the same run")
		}
	}
	return s.repo.InsertAgentAction(ctx, s.pool, input)
}

// CreateEvaluation persists output produced by a trusted deterministic evaluator.
func (s *Service) CreateEvaluation(ctx context.Context, input CreateEvaluationInput) (out Evaluation, err error) {
	input, err = ValidateEvaluation(input)
	if err != nil {
		return out, err
	}
	tx, err := s.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return out, err
	}
	defer func() {
		if err != nil {
			_ = tx.Rollback(ctx)
		}
	}()
	status, scenarioVersionID, err := s.repo.LockRunForEvaluation(ctx, tx, input.RunID)
	if err != nil {
		return out, err
	}
	if (input.Result == "stopped" && status != "stopped") || (input.Result != "stopped" && status != "completed") {
		return out, fmt.Errorf("%w: run status does not permit this evaluation result", identity.ErrConflict)
	}
	criterionIDs, err := s.repo.ListCriterionIDs(ctx, tx, scenarioVersionID)
	if err != nil {
		return out, err
	}
	if err = validateCriterionSet(input.Result, criterionIDs, input.Criteria); err != nil {
		return out, err
	}
	if out, err = s.repo.InsertEvaluation(ctx, tx, input); err != nil {
		return out, err
	}
	for _, criterion := range input.Criteria {
		if err = s.repo.InsertCriterionResult(ctx, tx, out.ID, criterion); err != nil {
			return out, err
		}
	}
	for _, finding := range input.Findings {
		if err = s.repo.InsertFinding(ctx, tx, out.ID, finding); err != nil {
			return out, err
		}
	}
	if out.Criteria, err = s.repo.ListEvaluationCriteria(ctx, tx, out.ID); err != nil {
		return out, err
	}
	if out.Findings, err = s.repo.ListEvaluationFindings(ctx, tx, out.ID); err != nil {
		return out, err
	}
	err = tx.Commit(ctx)
	return out, err
}

func (s *Service) ListRunEvents(ctx context.Context, principal auth.Principal, workspaceID, runID string, filters EventFilters) ([]RunEvent, error) {
	if err := s.requireRead(ctx, principal, workspaceID); err != nil {
		return nil, err
	}
	filters, err := ValidateEventFilters(filters)
	if err != nil {
		return nil, err
	}
	if err = s.repo.EnsureRunWorkspace(ctx, s.pool, workspaceID, runID); err != nil {
		return nil, err
	}
	return s.repo.ListRunEvents(ctx, s.pool, workspaceID, runID, filters)
}

func (s *Service) ListAgentActions(ctx context.Context, principal auth.Principal, workspaceID, runID string, filters ActionFilters) ([]AgentAction, error) {
	if err := s.requireRead(ctx, principal, workspaceID); err != nil {
		return nil, err
	}
	filters, err := ValidateActionFilters(filters)
	if err != nil {
		return nil, err
	}
	if err = s.repo.EnsureRunWorkspace(ctx, s.pool, workspaceID, runID); err != nil {
		return nil, err
	}
	return s.repo.ListAgentActions(ctx, s.pool, workspaceID, runID, filters)
}

func (s *Service) GetEvaluation(ctx context.Context, principal auth.Principal, workspaceID, runID string) (Evaluation, error) {
	if err := s.requireRead(ctx, principal, workspaceID); err != nil {
		return Evaluation{}, err
	}
	out, err := s.repo.GetEvaluation(ctx, s.pool, workspaceID, runID)
	if err != nil {
		return out, err
	}
	if out.Criteria, err = s.repo.ListEvaluationCriteria(ctx, s.pool, out.ID); err != nil {
		return out, err
	}
	if out.Findings, err = s.repo.ListEvaluationFindings(ctx, s.pool, out.ID); err != nil {
		return out, err
	}
	return out, nil
}

func (s *Service) requireRead(ctx context.Context, principal auth.Principal, workspaceID string) error {
	_, err := s.roles.GetRole(ctx, s.pool, workspaceID, principal.UserID)
	if errors.Is(err, identity.ErrNotFound) {
		return auth.ErrForbidden
	}
	return err
}

func validateCriterionSet(result string, expectedIDs []string, provided []CriterionResultInput) error {
	expected := make(map[string]struct{}, len(expectedIDs))
	for _, id := range expectedIDs {
		expected[id] = struct{}{}
	}
	for _, criterion := range provided {
		if _, ok := expected[criterion.ScenarioCriterionID]; !ok {
			return invalid("criterion does not belong to the run's scenario version")
		}
	}
	if result == "pass" || result == "fail" {
		if len(provided) != len(expectedIDs) {
			return invalid("pass and fail evaluations require the exact scenario criterion set")
		}
	}
	return nil
}
