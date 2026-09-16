package evaluationsuite

import (
	"context"
	"errors"

	"github.com/evalplane/evalplane/backend/internal/auth"
	"github.com/evalplane/evalplane/backend/internal/evaluationbatch"
	"github.com/evalplane/evalplane/backend/internal/identity"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

type roleRepository interface {
	GetRole(context.Context, identity.DBTX, string, string) (string, error)
}

type Service struct {
	pool    *pgxpool.Pool
	repo    *Repository
	roles   roleRepository
	batches *evaluationbatch.Repository
	billing ExecutionBillingGate
}

// ExecutionBillingGate is deliberately narrow: qualification planning and
// suite authoring never use it, while starting actual trials must pass both
// the preflight commercial check and transactional reservation.
type ExecutionBillingGate interface {
	PrepareExecution(context.Context, auth.Principal, string) error
	ReserveSuiteExecution(context.Context, pgx.Tx, string, string, string) error
}

func (s *Service) SetExecutionBillingGate(gate ExecutionBillingGate) { s.billing = gate }

func NewService(pool *pgxpool.Pool, repo *Repository, roles roleRepository, batches *evaluationbatch.Repository) *Service {
	return &Service{pool: pool, repo: repo, roles: roles, batches: batches}
}

func (s *Service) CreateSuite(ctx context.Context, principal auth.Principal, workspaceID string, in CreateSuiteInput) (Suite, error) {
	if err := validateID(workspaceID, "workspace_id"); err != nil {
		return Suite{}, err
	}
	if err := s.requireWrite(ctx, principal, workspaceID); err != nil {
		return Suite{}, err
	}
	in, err := ValidateCreateSuite(in)
	if err != nil {
		return Suite{}, err
	}
	return s.repo.CreateSuite(ctx, s.pool, workspaceID, principal.UserID, in)
}

func (s *Service) CreateSuiteVersion(ctx context.Context, principal auth.Principal, workspaceID, suiteID string, in CreateVersionInput) (out Version, err error) {
	if err = validateID(workspaceID, "workspace_id"); err != nil {
		return
	}
	if err = validateID(suiteID, "suite_id"); err != nil {
		return
	}
	if err = s.requireWrite(ctx, principal, workspaceID); err != nil {
		return
	}
	if in, err = ValidateVersion(in); err != nil {
		return
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
	if _, err = s.repo.LockSuite(ctx, tx, workspaceID, suiteID); err != nil {
		return
	}
	for index := range in.Scenarios {
		ref, loadErr := s.batches.LoadUsableScenario(ctx, tx, workspaceID, in.Scenarios[index].ScenarioVersionID)
		if loadErr != nil {
			err = loadErr
			return
		}
		if len(in.Scenarios[index].HiddenVariants) > 0 {
			var supported bool
			supported, err = s.repo.ScenarioSupportsHiddenVariants(ctx, tx, in.Scenarios[index].ScenarioVersionID)
			if err != nil {
				return
			}
			if !supported {
				err = invalid("hidden variants are supported only for traffic_spike scenarios")
				return
			}
		}
		in.Scenarios[index].ScenarioID = ref.ScenarioID
		in.Scenarios[index].ScenarioName = ref.Name
		in.Scenarios[index].ScenarioVersion = ref.Version
	}
	number, nextErr := s.repo.NextVersion(ctx, tx, suiteID)
	if nextErr != nil {
		err = nextErr
		return
	}
	out, err = s.repo.InsertVersion(ctx, tx, suiteID, principal.UserID, number, in)
	if err != nil {
		return
	}
	if err = s.repo.InsertVersionScenarios(ctx, tx, out.ID, in.Scenarios); err != nil {
		return
	}
	out.Scenarios = append([]ScenarioEntry(nil), in.Scenarios...)
	err = tx.Commit(ctx)
	return
}

func (s *Service) GetSuite(ctx context.Context, principal auth.Principal, workspaceID, suiteID string) (Suite, error) {
	if err := validateID(workspaceID, "workspace_id"); err != nil {
		return Suite{}, err
	}
	if err := validateID(suiteID, "suite_id"); err != nil {
		return Suite{}, err
	}
	if _, err := s.role(ctx, principal, workspaceID); err != nil {
		return Suite{}, err
	}
	return s.repo.GetSuite(ctx, s.pool, workspaceID, suiteID)
}

func (s *Service) ListSuites(ctx context.Context, principal auth.Principal, workspaceID string, filters Filters) ([]Suite, error) {
	if err := validateID(workspaceID, "workspace_id"); err != nil {
		return nil, err
	}
	if _, err := s.role(ctx, principal, workspaceID); err != nil {
		return nil, err
	}
	if filters.Pagination.Limit == 0 {
		filters.Pagination.Limit = 20
	}
	if filters.Pagination.Limit < 1 || filters.Pagination.Limit > 100 || filters.Pagination.Offset < 0 {
		return nil, invalid("pagination is invalid")
	}
	return s.repo.ListSuites(ctx, s.pool, workspaceID, filters)
}

func (s *Service) GetSuiteVersion(ctx context.Context, principal auth.Principal, workspaceID, versionID string) (Version, error) {
	if err := validateID(workspaceID, "workspace_id"); err != nil {
		return Version{}, err
	}
	if err := validateID(versionID, "version_id"); err != nil {
		return Version{}, err
	}
	if _, err := s.role(ctx, principal, workspaceID); err != nil {
		return Version{}, err
	}
	return s.repo.GetVersion(ctx, s.pool, workspaceID, versionID)
}

func (s *Service) StartSuiteExecution(ctx context.Context, principal auth.Principal, workspaceID string, in StartExecutionInput) (out Execution, err error) {
	return s.StartSuiteExecutionWithRecorder(ctx, principal, workspaceID, in, nil)
}

// ExecutionRecorder joins a caller-owned artifact to the transaction that
// creates the execution, runs, and immutable variant assignments.
type ExecutionRecorder func(context.Context, pgx.Tx, Execution) error

func (s *Service) StartSuiteExecutionWithRecorder(ctx context.Context, principal auth.Principal, workspaceID string, in StartExecutionInput, recorder ExecutionRecorder) (out Execution, err error) {
	if err = validateID(workspaceID, "workspace_id"); err != nil {
		return
	}
	if err = s.requireWrite(ctx, principal, workspaceID); err != nil {
		return
	}
	if s.billing != nil {
		if err = s.billing.PrepareExecution(ctx, principal, workspaceID); err != nil {
			return
		}
	}
	if in, err = ValidateStart(in); err != nil {
		return
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
	version, loadErr := s.repo.GetVersion(ctx, tx, workspaceID, in.SuiteVersionID)
	if loadErr != nil {
		err = loadErr
		return
	}
	agents, loadErr := s.batches.LoadUsableAgents(ctx, tx, workspaceID, in.AgentVersionIDs)
	if loadErr != nil {
		err = loadErr
		return
	}
	if len(agents) != len(in.AgentVersionIDs) {
		err = identity.ErrNotFound
		return
	}
	out, err = s.repo.CreateExecution(ctx, tx, workspaceID, principal.UserID, version.ID)
	if err != nil {
		return
	}
	if err = s.repo.AddExecutionAgents(ctx, tx, out.ID, in.AgentVersionIDs); err != nil {
		return
	}
	for _, batchInput := range BuildBatchPlan(version, in.AgentVersionIDs) {
		batch, createErr := s.batches.CreateBatch(ctx, tx, workspaceID, principal.UserID, batchInput)
		if createErr != nil {
			err = createErr
			return
		}
		if err = s.batches.AddAgentsAndRuns(ctx, tx, batch.ID, in.AgentVersionIDs, batchInput.RunsPerAgent); err != nil {
			return
		}
		if err = s.repo.LinkBatch(ctx, tx, out.ID, batch.ID, batchInput.ScenarioVersionID); err != nil {
			return
		}
		if err = s.repo.QueueBatch(ctx, tx, workspaceID, batch.ID); err != nil {
			return
		}
		out.BatchIDs = append(out.BatchIDs, batch.ID)
	}
	runs, loadErr := s.repo.ListRunsForAssignment(ctx, tx, out.ID)
	if loadErr != nil {
		err = loadErr
		return
	}
	assignments := AssignVariants(out.ID, version.ID, version.Scenarios, runs)
	if len(assignments) != len(runs) {
		err = identity.ErrConflict
		return
	}
	if err = s.repo.InsertVariantAssignments(ctx, tx, assignments); err != nil {
		return
	}
	out.AgentVersionIDs = append([]string(nil), in.AgentVersionIDs...)
	if s.billing != nil {
		if err = s.billing.ReserveSuiteExecution(ctx, tx, workspaceID, out.ID, principal.UserID); err != nil {
			return
		}
	}
	if recorder != nil {
		if err = recorder(ctx, tx, out); err != nil {
			return
		}
	}
	err = tx.Commit(ctx)
	return
}

func (s *Service) GetSuiteExecution(ctx context.Context, principal auth.Principal, workspaceID, executionID string) (Execution, error) {
	if err := validateID(workspaceID, "workspace_id"); err != nil {
		return Execution{}, err
	}
	if err := validateID(executionID, "execution_id"); err != nil {
		return Execution{}, err
	}
	if _, err := s.role(ctx, principal, workspaceID); err != nil {
		return Execution{}, err
	}
	if err := s.repo.ReconcileExecution(ctx, s.pool, workspaceID, executionID); err != nil {
		return Execution{}, err
	}
	return s.repo.GetExecution(ctx, s.pool, workspaceID, executionID)
}

func (s *Service) GetSuiteResults(ctx context.Context, principal auth.Principal, workspaceID, executionID string) (Results, error) {
	if err := validateID(workspaceID, "workspace_id"); err != nil {
		return Results{}, err
	}
	if err := validateID(executionID, "execution_id"); err != nil {
		return Results{}, err
	}
	if _, err := s.role(ctx, principal, workspaceID); err != nil {
		return Results{}, err
	}
	if err := s.repo.ReconcileExecution(ctx, s.pool, workspaceID, executionID); err != nil {
		return Results{}, err
	}
	execution, err := s.repo.GetExecution(ctx, s.pool, workspaceID, executionID)
	if err != nil {
		return Results{}, err
	}
	rows, err := s.repo.ListTrialRows(ctx, s.pool, workspaceID, executionID)
	if err != nil {
		return Results{}, err
	}
	criteria, err := s.repo.ListCriterionObservationRows(ctx, s.pool, workspaceID, executionID)
	if err != nil {
		return Results{}, err
	}
	result := Aggregate(executionID, rows)
	result.SuiteVersionID = execution.SuiteVersionID
	result.Criteria = SummarizeCriteria(criteria)
	return result, nil
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

func validateID(value, label string) error {
	if identity.ValidateUUID(value) != nil {
		return invalid(label + " must be a valid UUID")
	}
	return nil
}
