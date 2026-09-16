package executionworker

import (
	"context"
	"errors"
	"github.com/evalplane/evalplane/backend/internal/execution"
	"sync"
	"time"
)

// Run uses a fixed number of claim slots. Every claim attempt is paced, including
// empty queues and failures. A failed run never kills the polling process.
func (w *Worker) Run(ctx context.Context) error {
	// Recover before accepting new work, then sweep conservatively on a fixed cadence.
	w.recoverSweep(ctx)
	var recovery sync.WaitGroup
	recovery.Add(1)
	go func() {
		defer recovery.Done()
		ticker := time.NewTicker(w.config.RecoveryInterval)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				w.recoverSweep(ctx)
			}
		}
	}()
	var workers sync.WaitGroup
	for i := 0; i < w.config.MaxConcurrentRuns; i++ {
		workers.Add(1)
		go func() {
			defer workers.Done()
			for ctx.Err() == nil {
				run, err := w.deps.Lifecycle.ClaimNextRunWithLease(ctx, w.config.WorkerID, w.config.LeaseDuration)
				if err == nil {
					err = w.ExecuteRun(ctx, run)
				}
				if err != nil && !errors.Is(err, execution.ErrNoRunAvailable) && ctx.Err() == nil && w.deps.OnError != nil {
					w.deps.OnError(run.RunID, errors.New("worker operation failed; inspect persisted lifecycle codes"))
				}
				if wait(ctx, w.config.PollInterval) != nil {
					return
				}
			}
		}()
	}
	workers.Wait()
	recovery.Wait()
	return nil
}

func (w *Worker) recoverSweep(ctx context.Context) {
	for i := 0; i < w.config.MaxConcurrentRuns && ctx.Err() == nil; i++ {
		runID, err := w.RecoverOne(ctx)
		if errors.Is(err, execution.ErrNoRunAvailable) {
			return
		}
		if err != nil {
			if w.deps.OnError != nil {
				w.deps.OnError(runID, errors.New("worker recovery failed; inspect persisted recovery codes"))
			}
			return
		}
	}
}
func wait(ctx context.Context, d time.Duration) error {
	timer := time.NewTimer(d)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}
