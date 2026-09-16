package executionworker

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"github.com/evalplane/evalplane/backend/internal/execution"
	"github.com/evalplane/evalplane/backend/internal/kubeenv"
)

// RecoverOne atomically acquires one stale lease, cleans only that run's owned
// environment, terminalizes it as a platform interruption, and evaluates it.
// It never invokes the agent or replays infrastructure setup.
func (w *Worker) RecoverOne(parent context.Context) (string, error) {
	abandoned, err := w.deps.Lifecycle.AcquireAbandonedRun(parent, w.config.WorkerID, w.config.LeaseDuration, w.config.RecoveryGrace)
	if err != nil {
		return "", err
	}
	run := abandoned.ExecutionRun
	ctx, cancel := context.WithCancelCause(parent)
	defer cancel(nil)
	heartbeatCtx, stopHeartbeat := context.WithCancel(ctx)
	heartbeatDone := w.startHeartbeat(heartbeatCtx, cancel, run)
	defer func() { stopHeartbeat(); <-heartbeatDone }()

	if err = w.recoveryEventOnce(ctx, run.RunID, "worker_lease_expired", map[string]any{"classification": "platform", "previous_status": abandoned.PreviousStatus}); err != nil {
		return run.RunID, err
	}
	if err = w.recoveryEventOnce(ctx, run.RunID, "run_abandoned", map[string]any{"classification": "platform", "previous_status": abandoned.PreviousStatus}); err != nil {
		return run.RunID, err
	}
	if err = w.recoveryEventOnce(ctx, run.RunID, "worker_recovery_started", map[string]any{"classification": "platform"}); err != nil {
		return run.RunID, err
	}

	id := kubeenv.RunIdentity{RunID: run.RunID, BatchID: run.BatchID, WorkspaceID: run.WorkspaceID}
	cleanupCtx, cleanupCancel := context.WithTimeout(ctx, w.config.CleanupTimeout)
	cleanupErr := w.cleanupRuntime(cleanupCtx, id)
	cleanupCancel()
	if cleanupErr != nil {
		err = errors.Join(err, platform("worker_recovery_cleanup_failed", cleanupErr))
		err = errors.Join(err, w.recoveryEventOnce(ctx, run.RunID, "worker_recovery_cleanup_failed", map[string]any{"classification": "platform"}))
	}
	if cause := context.Cause(ctx); cause != nil {
		return run.RunID, errors.Join(err, cause)
	}
	if eventErr := w.recoveryEventOnce(ctx, run.RunID, "worker_recovery_completed", map[string]any{"classification": "platform"}); eventErr != nil {
		return run.RunID, errors.Join(err, eventErr)
	}
	// Terminalization intentionally clears the lease. Stop renewal first so that
	// this expected ownership release is not mistaken for lease loss.
	stopHeartbeat()
	<-heartbeatDone
	final, done := context.WithTimeout(context.Background(), w.config.FinalizeTimeout)
	defer done()
	if terminalErr := w.deps.Lifecycle.FailOwnedRun(final, run.RunID, w.config.WorkerID); terminalErr != nil {
		return run.RunID, errors.Join(err, terminalErr)
	}
	return run.RunID, errors.Join(err, w.evaluate(run), w.settleBilling(run))
}

func (w *Worker) recoveryEventOnce(ctx context.Context, runID, code string, fields map[string]any) error {
	if fields == nil {
		fields = map[string]any{}
	}
	fields["code"] = code
	raw, err := marshalFields(fields)
	if err != nil {
		return err
	}
	_, err = w.deps.Evidence.AppendRunEventOnce(ctx, evidenceEvent(runID, "recovery", code, raw))
	return err
}

func (w *Worker) startHeartbeat(ctx context.Context, cancel context.CancelCauseFunc, run execution.ExecutionRun) <-chan struct{} {
	done := make(chan struct{})
	go func() {
		defer close(done)
		ticker := time.NewTicker(w.config.HeartbeatInterval)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				owned, err := w.deps.Lifecycle.RenewRunLease(ctx, run.WorkspaceID, run.RunID, w.config.WorkerID, w.config.LeaseDuration)
				if err != nil {
					cancel(platform("worker_heartbeat_failed", err))
					return
				}
				if !owned {
					cancel(ErrLeaseLost)
					return
				}
			}
		}
	}()
	return done
}

func marshalFields(fields map[string]any) (json.RawMessage, error) {
	return json.Marshal(fields)
}
