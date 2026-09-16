package executionworker

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/evalplane/evalplane/backend/internal/execution"
)

func staleRun(status string) execution.AbandonedRun {
	f := newFixture()
	return execution.AbandonedRun{ExecutionRun: f.run, PreviousStatus: status}
}

func TestActiveLeaseIsNotRecovered(t *testing.T) {
	f := newFixture()
	_, err := f.worker(t, testConfig()).RecoverOne(context.Background())
	if !errors.Is(err, execution.ErrNoRunAvailable) || f.cleaned != 0 || f.evaluated != 0 {
		t.Fatalf("err=%v cleanup=%d evaluation=%d", err, f.cleaned, f.evaluated)
	}
}

func TestExpiredProvisioningAndRunningBecomePlatformInconclusive(t *testing.T) {
	for _, status := range []string{"provisioning", "running"} {
		t.Run(status, func(t *testing.T) {
			f := newFixture()
			f.abandoned = []execution.AbandonedRun{staleRun(status)}
			run, err := f.worker(t, testConfig()).RecoverOne(context.Background())
			if err != nil || run != runID {
				t.Fatalf("run=%q err=%v", run, err)
			}
			if f.status != "failed" || f.cleaned != 1 || f.evaluated != 1 || f.reconciled != 1 || f.mcpRevoked != 1 {
				t.Fatalf("status=%s cleanup=%d evaluate=%d reconcile=%d mcp_revoked=%d", f.status, f.cleaned, f.evaluated, f.reconciled, f.mcpRevoked)
			}
			if len(f.results) != 1 || f.results[0].Status != "inconclusive" {
				t.Fatalf("abandonment was graded as %+v", f.results)
			}
			for _, code := range []string{"worker_lease_expired", "run_abandoned", "worker_recovery_started", "worker_recovery_completed"} {
				if !hasEvent(f, code) {
					t.Fatalf("missing %s: %+v", code, f.events)
				}
			}
			if f.provisioned != 0 || len(f.requests) != 0 || f.executed != 0 {
				t.Fatal("recovery replayed execution")
			}
		})
	}
}

func TestCompetingRecoveryAndHardCrashRetryAreIdempotent(t *testing.T) {
	f := newFixture()
	f.abandoned = []execution.AbandonedRun{staleRun("running")}
	c1, c2 := testConfig(), testConfig()
	c1.WorkerID, c2.WorkerID = "recovery-one", "recovery-two"
	w1, w2 := f.worker(t, c1), f.worker(t, c2)
	var wg sync.WaitGroup
	results := make(chan error, 2)
	for _, worker := range []*Worker{w1, w2} {
		wg.Add(1)
		go func(w *Worker) {
			defer wg.Done()
			_, err := w.RecoverOne(context.Background())
			results <- err
		}(worker)
	}
	wg.Wait()
	close(results)
	success, empty := 0, 0
	for err := range results {
		switch {
		case err == nil:
			success++
		case errors.Is(err, execution.ErrNoRunAvailable):
			empty++
		default:
			t.Fatal(err)
		}
	}
	if success != 1 || empty != 1 || f.cleaned != 1 || f.evaluated != 1 || f.reconciled != 1 {
		t.Fatalf("success=%d empty=%d cleanup=%d evaluation=%d reconcile=%d", success, empty, f.cleaned, f.evaluated, f.reconciled)
	}
	if _, err := w1.RecoverOne(context.Background()); !errors.Is(err, execution.ErrNoRunAvailable) {
		t.Fatalf("terminal retry reacquired run: %v", err)
	}
}

func TestRecoveryCleanupFailurePreservesAbandonment(t *testing.T) {
	f := newFixture()
	f.abandoned = []execution.AbandonedRun{staleRun("running")}
	f.cleanupErr = errors.New("cleanup unavailable")
	_, err := f.worker(t, testConfig()).RecoverOne(context.Background())
	if !errors.Is(err, f.cleanupErr) || f.status != "failed" || f.evaluated != 1 || f.results[0].Status != "inconclusive" {
		t.Fatalf("err=%v status=%s result=%+v", err, f.status, f.results)
	}
	if !hasEvent(f, "worker_recovery_cleanup_failed") || !hasEvent(f, "run_abandoned") {
		t.Fatalf("events=%+v", f.events)
	}
}

func TestHeartbeatExtendsLeaseAndOwnershipLossStopsWork(t *testing.T) {
	f := newFixture()
	f.blockAt = "gateway"
	f.loseHeartbeat = true
	w := f.worker(t, testConfig())
	err := w.ExecuteRun(context.Background(), f.run)
	if !errors.Is(err, ErrLeaseLost) || f.heartbeats == 0 {
		t.Fatalf("err=%v heartbeats=%d", err, f.heartbeats)
	}
	before := f.heartbeats
	time.Sleep(3 * testConfig().HeartbeatInterval)
	if f.heartbeats != before || f.executed != 0 || f.cleaned != 0 || f.evaluated != 0 || f.status == "completed" {
		t.Fatalf("lost owner continued: heartbeats=%d trace=%v", f.heartbeats, f.trace)
	}
}

func TestHeartbeatRenewsWhileRunIsOwned(t *testing.T) {
	f := newFixture()
	f.blockAt = "gateway"
	ctx, cancel := context.WithTimeout(context.Background(), 18*time.Millisecond)
	defer cancel()
	_ = f.worker(t, testConfig()).ExecuteRun(ctx, f.run)
	if f.heartbeats < 1 {
		t.Fatal("lease was not renewed")
	}
}

func TestLeaseWorkspaceBoundary(t *testing.T) {
	f := newFixture()
	owned, err := f.VerifyRunLease(context.Background(), "99999999-9999-4999-8999-999999999999", runID, "test-worker")
	if err != nil || owned {
		t.Fatalf("cross-workspace lease accepted: owned=%v err=%v", owned, err)
	}
}

func TestGracefulShutdownDoesNotClaimNewWork(t *testing.T) {
	f := newFixture()
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := f.worker(t, testConfig()).Run(ctx); err != nil {
		t.Fatal(err)
	}
	if f.claimCount != 0 || f.provisioned != 0 {
		t.Fatalf("shutdown claimed work: claims=%d provisioned=%d", f.claimCount, f.provisioned)
	}
}
