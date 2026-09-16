package execution

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

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

func (s *Service) StartBatch(ctx context.Context, principal auth.Principal, workspaceID, batchID string) (out BatchState, err error) {
	if err = validateIDs(workspaceID, batchID); err != nil {
		return out, err
	}
	if err = s.requireWrite(ctx, principal, workspaceID); err != nil {
		return out, err
	}
	tx, err := s.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return out, err
	}
	defer rollbackOnError(ctx, tx, &err)
	var runCount int
	out, runCount, err = s.repo.LockBatch(ctx, tx, workspaceID, batchID)
	if err != nil {
		return out, err
	}
	if out.Status != "draft" {
		return out, fmt.Errorf("%w: evaluation batch can only be started from draft", identity.ErrConflict)
	}
	if runCount == 0 {
		return out, fmt.Errorf("%w: evaluation batch must contain at least one run", identity.ErrConflict)
	}
	out, err = s.repo.QueueBatch(ctx, tx, batchID)
	if err != nil {
		return out, err
	}
	err = tx.Commit(ctx)
	return out, err
}

func (s *Service) StopBatch(ctx context.Context, principal auth.Principal, workspaceID, batchID string) (out BatchState, err error) {
	if err = validateIDs(workspaceID, batchID); err != nil {
		return out, err
	}
	if err = s.requireWrite(ctx, principal, workspaceID); err != nil {
		return out, err
	}
	tx, err := s.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return out, err
	}
	defer rollbackOnError(ctx, tx, &err)
	out, _, err = s.repo.LockBatch(ctx, tx, workspaceID, batchID)
	if err != nil {
		return out, err
	}
	// A retry may need to finish queued-run reconciliation after the durable
	// batch stop committed but the subsequent update failed.
	if out.Status == "stopped" {
		err = tx.Commit(ctx)
		if err == nil {
			err = s.repo.StopQueuedBatch(ctx, s.pool, batchID)
		}
		return out, err
	}
	if out.Status != "draft" && out.Status != "queued" && out.Status != "running" {
		return out, fmt.Errorf("%w: evaluation batch cannot be stopped from %s", identity.ErrConflict, out.Status)
	}
	out, err = s.repo.StopBatch(ctx, tx, batchID)
	if err != nil {
		return out, err
	}
	err = tx.Commit(ctx)
	if err == nil {
		err = s.repo.StopQueuedBatch(ctx, s.pool, batchID)
	}
	return out, err
}

// ClaimNextRun atomically reserves one queued run for trusted worker execution.
func (s *Service) ClaimNextRun(ctx context.Context) (out ExecutionRun, err error) {
	return s.ClaimNextRunWithLease(ctx, "legacy-claim", time.Minute)
}

// ClaimNextRunWithLease atomically assigns a bounded worker lease while retaining
// the concurrency-safe Stage 9 queued-run claim.
func (s *Service) ClaimNextRunWithLease(ctx context.Context, workerID string, leaseDuration time.Duration) (out ExecutionRun, err error) {
	if err = validateLease(workerID, leaseDuration); err != nil {
		return out, err
	}
	tx, err := s.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return out, err
	}
	defer rollbackOnError(ctx, tx, &err)
	out, err = s.repo.LockNextQueuedRun(ctx, tx)
	if err != nil {
		return out, err
	}
	if err = s.repo.ClaimRunWithLease(ctx, tx, out.RunID, workerID, leaseDuration.Milliseconds()); err != nil {
		return out, err
	}
	if err = s.repo.StartBatch(ctx, tx, out.BatchID); err != nil {
		return out, err
	}
	err = tx.Commit(ctx)
	return out, err
}

// RenewRunLease uses database time and fails closed when ownership or expiry no
// longer matches. An expired owner cannot revive its lease after takeover.
func (s *Service) RenewRunLease(ctx context.Context, workspaceID, runID, workerID string, leaseDuration time.Duration) (bool, error) {
	if err := validateWorkerRun(workspaceID, runID, workerID); err != nil {
		return false, err
	}
	if leaseDuration <= 0 {
		return false, fmt.Errorf("%w: lease duration must be positive", identity.ErrValidation)
	}
	return s.repo.RenewRunLease(ctx, s.pool, workspaceID, runID, workerID, leaseDuration.Milliseconds())
}

func (s *Service) VerifyRunLease(ctx context.Context, workspaceID, runID, workerID string) (bool, error) {
	if err := validateWorkerRun(workspaceID, runID, workerID); err != nil {
		return false, err
	}
	return s.repo.VerifyRunLease(ctx, s.pool, workspaceID, runID, workerID)
}

// AcquireAbandonedRun locks one genuinely stale active run and changes ownership
// in the same transaction. Competing recovery workers use SKIP LOCKED.
func (s *Service) AcquireAbandonedRun(ctx context.Context, workerID string, leaseDuration, staleGrace time.Duration) (out AbandonedRun, err error) {
	if err = validateLease(workerID, leaseDuration); err != nil {
		return out, err
	}
	if staleGrace < 0 {
		return out, fmt.Errorf("%w: stale grace must not be negative", identity.ErrValidation)
	}
	tx, err := s.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return out, err
	}
	defer rollbackOnError(ctx, tx, &err)
	out, err = s.repo.LockNextAbandonedRun(ctx, tx, staleGrace.Milliseconds())
	if err != nil {
		return out, err
	}
	if err = s.repo.TakeOverRunLease(ctx, tx, out.RunID, workerID, leaseDuration.Milliseconds(), staleGrace.Milliseconds()); err != nil {
		return out, err
	}
	err = tx.Commit(ctx)
	return out, err
}

func (s *Service) MarkRunRunning(ctx context.Context, runID, environmentRef string) (err error) {
	if err = identity.ValidateUUID(runID); err != nil {
		return err
	}
	if environmentRef, err = ValidateEnvironmentRef(environmentRef); err != nil {
		return err
	}
	tx, err := s.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return err
	}
	defer rollbackOnError(ctx, tx, &err)
	status, _, err := s.repo.LockRun(ctx, tx, runID)
	if err != nil {
		return err
	}
	if err = ValidateRunTransition(status, "running"); err != nil {
		return err
	}
	if err = s.repo.MarkRunRunning(ctx, tx, runID, environmentRef); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

func (s *Service) MarkOwnedRunRunning(ctx context.Context, runID, workerID, environmentRef string) (err error) {
	if err = validateWorkerID(workerID); err != nil {
		return err
	}
	if err = identity.ValidateUUID(runID); err != nil {
		return err
	}
	if environmentRef, err = ValidateEnvironmentRef(environmentRef); err != nil {
		return err
	}
	return s.repo.MarkOwnedRunRunning(ctx, s.pool, runID, workerID, environmentRef)
}

func (s *Service) CompleteRun(ctx context.Context, runID string) error {
	return s.markTerminal(ctx, runID, "completed")
}

func (s *Service) FailRun(ctx context.Context, runID string) error {
	return s.markTerminal(ctx, runID, "failed")
}

func (s *Service) StopRun(ctx context.Context, runID string) error {
	return s.markTerminal(ctx, runID, "stopped")
}

func (s *Service) CompleteOwnedRun(ctx context.Context, runID, workerID string) error {
	return s.markOwnedTerminal(ctx, runID, workerID, "completed")
}

func (s *Service) FailOwnedRun(ctx context.Context, runID, workerID string) error {
	return s.markOwnedTerminal(ctx, runID, workerID, "failed")
}

func (s *Service) StopOwnedRun(ctx context.Context, runID, workerID string) error {
	return s.markOwnedTerminal(ctx, runID, workerID, "stopped")
}

func (s *Service) markOwnedTerminal(ctx context.Context, runID, workerID, target string) (err error) {
	if err = validateWorkerID(workerID); err != nil {
		return err
	}
	if err = identity.ValidateUUID(runID); err != nil {
		return err
	}
	tx, err := s.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return err
	}
	defer rollbackOnError(ctx, tx, &err)
	status, batchID, err := s.repo.LockOwnedRun(ctx, tx, runID, workerID)
	if err != nil {
		return err
	}
	if status == target {
		return tx.Commit(ctx)
	}
	if err = ValidateRunTransition(status, target); err != nil {
		return err
	}
	if err = s.repo.LockBatchByID(ctx, tx, batchID); err != nil {
		return err
	}
	var batchStopped bool
	if err = tx.QueryRow(ctx, `SELECT status='stopped' FROM evaluation_batches WHERE id=$1`, batchID).Scan(&batchStopped); err != nil {
		return err
	}
	if batchStopped {
		target = "stopped"
	}
	if err = s.repo.MarkOwnedRunTerminal(ctx, tx, runID, workerID, target); err != nil {
		return err
	}
	if err = s.repo.ReconcileBatch(ctx, tx, batchID); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

func (s *Service) markTerminal(ctx context.Context, runID, target string) (err error) {
	if err = identity.ValidateUUID(runID); err != nil {
		return err
	}
	tx, err := s.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return err
	}
	defer rollbackOnError(ctx, tx, &err)
	status, batchID, err := s.repo.LockRun(ctx, tx, runID)
	if err != nil {
		return err
	}
	if status == target {
		return tx.Commit(ctx)
	}
	if err = ValidateRunTransition(status, target); err != nil {
		return err
	}
	if err = s.repo.LockBatchByID(ctx, tx, batchID); err != nil {
		return err
	}
	var batchStopped bool
	if err = tx.QueryRow(ctx, `SELECT status='stopped' FROM evaluation_batches WHERE id=$1`, batchID).Scan(&batchStopped); err != nil {
		return err
	}
	if batchStopped {
		target = "stopped"
	}
	if err = s.repo.MarkRunTerminal(ctx, tx, runID, target); err != nil {
		return err
	}
	if err = s.repo.ReconcileBatch(ctx, tx, batchID); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

func (s *Service) requireWrite(ctx context.Context, principal auth.Principal, workspaceID string) error {
	role, err := s.roles.GetRole(ctx, s.pool, workspaceID, principal.UserID)
	if errors.Is(err, identity.ErrNotFound) {
		return auth.ErrForbidden
	}
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

func validateIDs(workspaceID, batchID string) error {
	if identity.ValidateUUID(workspaceID) != nil {
		return fmt.Errorf("%w: workspaceID must be a valid UUID", identity.ErrValidation)
	}
	if identity.ValidateUUID(batchID) != nil {
		return fmt.Errorf("%w: batchID must be a valid UUID", identity.ErrValidation)
	}
	return nil
}

func validateWorkerID(workerID string) error {
	workerID = strings.TrimSpace(workerID)
	if workerID == "" || len(workerID) > 200 {
		return fmt.Errorf("%w: worker_id is required and must be at most 200 characters", identity.ErrValidation)
	}
	return nil
}

func validateLease(workerID string, leaseDuration time.Duration) error {
	if err := validateWorkerID(workerID); err != nil {
		return err
	}
	if leaseDuration <= 0 || leaseDuration > 24*time.Hour {
		return fmt.Errorf("%w: lease duration must be positive and at most 24 hours", identity.ErrValidation)
	}
	return nil
}

func validateWorkerRun(workspaceID, runID, workerID string) error {
	if identity.ValidateUUID(workspaceID) != nil || identity.ValidateUUID(runID) != nil {
		return fmt.Errorf("%w: workspace_id and run_id must be valid UUIDs", identity.ErrValidation)
	}
	return validateWorkerID(workerID)
}

func rollbackOnError(ctx context.Context, tx pgx.Tx, err *error) {
	if *err != nil {
		_ = tx.Rollback(ctx)
	}
}
