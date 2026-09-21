package executionworker

import (
	"context"
	"encoding/json"
	"errors"
	"github.com/evalplane/evalplane/backend/internal/execution"
	"github.com/evalplane/evalplane/backend/internal/kubeenv"
	"github.com/evalplane/evalplane/backend/internal/observation"
	"github.com/evalplane/evalplane/backend/internal/scenario"
	"time"
)

// ExecuteRun accepts only an atomically claimed provisioning run. Terminal retries
// perform evaluation and cleanup only; no infrastructure or actions are replayed.
func (w *Worker) ExecuteRun(parent context.Context, run execution.ExecutionRun) (err error) {
	ctx, deadlineCancel := context.WithTimeout(parent, w.config.RunTimeout)
	defer deadlineCancel()
	ctx, cancel := context.WithCancelCause(ctx)
	defer cancel(nil)
	id := kubeenv.RunIdentity{RunID: run.RunID, BatchID: run.BatchID, WorkspaceID: run.WorkspaceID}
	loadCtx, loadCancel := context.WithTimeout(context.WithoutCancel(parent), w.config.FinalizeTimeout)
	status, stopped, err := w.deps.Lifecycle.RunStatus(loadCtx, run.WorkspaceID, run.RunID)
	loadCancel()
	if err != nil {
		return platform("run_load_failed", err)
	}
	if status == "completed" || status == "failed" || status == "stopped" {
		defer func() { err = errors.Join(err, w.cleanup(id)) }()
		err = w.evaluate(run)
		return errors.Join(err, w.settleBilling(run))
	}
	if status != "provisioning" {
		return errors.New("run must be claimed before execution; active runs cannot be replayed")
	}
	var env observation.EnvironmentContext
	var provisionAttempted, ready bool
	// Independent bounded finalization survives caller cancellation. Cleanup gets a
	// separate budget so evaluation outages cannot consume its deadline.
	defer func() {
		cause := context.Cause(ctx)
		if errors.Is(err, ErrLeaseLost) || errors.Is(cause, ErrLeaseLost) {
			// Recovery owns finalization. A former owner must not race cleanup,
			// terminal transitions, evaluation, or further evidence writes.
			return
		}
		// Register cleanup first so a finalization panic cannot bypass it.
		defer func() {
			if provisionAttempted {
				err = errors.Join(err, w.cleanup(id))
			}
		}()
		if p := recover(); p != nil {
			err = platform("runtime_panic", errors.New("runtime dependency panicked"))
		}
		cause = context.Cause(ctx)
		if cause != nil {
			err = errors.Join(err, cause)
		}
		final, done := context.WithTimeout(context.Background(), w.config.FinalizeTimeout)
		defer done()
		if err != nil {
			code, class := "runtime_failure", "platform"
			var f *Failure
			if errors.As(err, &f) {
				code, class = f.Code, f.Class
			}
			if errors.Is(err, ErrStopped) || errors.Is(cause, context.Canceled) {
				code, class = "run_stopped", "control"
			} else if errors.Is(cause, context.DeadlineExceeded) {
				code, class = "run_timeout", "platform"
			}
			err = errors.Join(err, w.event(final, run.RunID, "system", code, map[string]any{"classification": class}))
		}
		if ready && ctx.Err() == nil {
			w.stage("final_snapshot", "started")
			if e := w.observe(ctx, env, "final_observation_collected"); e != nil {
				err = errors.Join(err, platform("final_observation_failed", e))
				err = errors.Join(err, w.event(final, run.RunID, "system", "runtime_failure", map[string]any{"classification": "platform", "reason_code": "final_observation_failed"}))
				w.stage("final_snapshot", "failed")
			} else {
				w.stage("final_snapshot", "completed")
			}
		} else {
			err = errors.Join(err, w.event(final, run.RunID, "system", "final_observation_unavailable", nil))
		}
		// Recheck the durable stop flag to close the final loop/transition race.
		current, stop, e := w.deps.Lifecycle.RunStatus(final, run.WorkspaceID, run.RunID)
		if e != nil {
			err = errors.Join(err, e)
		}
		var terminalErr error
		w.stage("persistence", "started")
		switch {
		case e != nil:
			terminalErr = e
		case current == "stopped":
		case stop || errors.Is(cause, ErrStopped) || errors.Is(cause, context.Canceled):
			terminalErr = w.deps.Lifecycle.StopOwnedRun(final, run.RunID, w.config.WorkerID)
		case err != nil:
			terminalErr = w.deps.Lifecycle.FailOwnedRun(final, run.RunID, w.config.WorkerID)
		default:
			terminalErr = w.deps.Lifecycle.CompleteOwnedRun(final, run.RunID, w.config.WorkerID)
		}
		err = errors.Join(err, terminalErr)
		if terminalErr == nil {
			err = errors.Join(err, w.evaluate(run))
			err = errors.Join(err, w.settleBilling(run))
		}
		if terminalErr != nil || err != nil {
			w.stage("persistence", "failed")
		} else {
			w.stage("persistence", "completed")
		}
	}()
	heartbeatCtx, stopHeartbeat := context.WithCancel(ctx)
	heartbeatDone := w.startHeartbeat(heartbeatCtx, cancel, run)
	defer func() { stopHeartbeat(); <-heartbeatDone }()
	if stopped {
		cancel(ErrStopped)
		return ErrStopped
	}
	watchDone := make(chan struct{})
	watchCtx, watchCancel := context.WithCancel(ctx)
	go func() {
		defer close(watchDone)
		for {
			if wait(watchCtx, w.config.StopPollInterval) != nil {
				return
			}
			_, stop, e := w.deps.Lifecycle.RunStatus(watchCtx, run.WorkspaceID, run.RunID)
			if watchCtx.Err() != nil {
				return
			}
			if e != nil {
				cancel(platform("stop_check_failed", e))
				return
			}
			if stop {
				cancel(ErrStopped)
				return
			}
		}
	}()
	// Stop and join watcher before finalization starts; its cancel is not a run stop.
	defer func() { watchCancel(); <-watchDone }()
	sv, av, err := w.deps.Lifecycle.LoadVersions(ctx, run.WorkspaceID, run.RunID)
	if err != nil {
		return platform("version_load_failed", err)
	}
	if sv.ID != run.Scenario.VersionID || av.ID != run.Agent.VersionID {
		return platform("version_mismatch", errors.New("claimed versions differ from persisted versions"))
	}
	if av.InterfaceKind != "http" {
		return platform("agent_interface_unsupported", ErrUnsupported)
	}
	if _, err = executionProfile(sv); err != nil {
		return platform("scenario_unsupported", err)
	}
	if _, err = parseRuntimeVariant(run.HiddenVariant); err != nil {
		return platform("scenario_variant_unsupported", err)
	}
	if err = w.event(ctx, run.RunID, "system", "run_claimed", nil); err != nil {
		return err
	}
	if err = w.event(ctx, run.RunID, "system", "environment_provisioning_started", nil); err != nil {
		return err
	}
	w.stage("environment_setup", "started")
	provisionAttempted = true
	environment, runEnvPins, err := w.provisionEnvironment(ctx, run, sv, id)
	if err != nil {
		return err
	}
	env = observation.EnvironmentContext{Identity: id, Environment: environment}
	if err = w.event(ctx, run.RunID, "system", "environment_ready", runEnvPins); err != nil {
		return err
	}
	if err = w.deps.Runtime.Prepare(ctx, env, sv, run.HiddenVariant); err != nil {
		return platform("scenario_prepare_failed", err)
	}
	w.stage("environment_setup", "completed")
	ready = true
	if w.deps.Verification != nil {
		w.stage("baseline_capture", "started")
		if err = w.observe(ctx, env, "verification_baseline_captured"); err != nil {
			return platform("baseline_snapshot_failed", err)
		}
		w.stage("baseline_capture", "completed")
	}
	w.stage("telemetry_start", "started")
	if err = w.deps.Runtime.Start(ctx, env, sv, run.HiddenVariant); err != nil {
		return platform("scenario_start_failed", err)
	}
	w.stage("telemetry_start", "completed")
	if err = w.event(ctx, run.RunID, "scenario", "scenario_started", nil); err != nil {
		return err
	}
	if err = w.deps.Lifecycle.MarkOwnedRunRunning(ctx, run.RunID, w.config.WorkerID, environment.Reference); err != nil {
		return err
	}
	w.stage("agent_execution", "started")
	if err = w.agentLoop(ctx, run, env, sv.AgentView(), av); err != nil {
		return err
	}
	w.stage("agent_execution", "completed")
	w.stage("traffic_completion", "started")
	if err = w.deps.Runtime.Finish(ctx, env); err != nil {
		return platform("scenario_finish_failed", err)
	}
	w.stage("traffic_completion", "completed")
	w.stage("stability_observation", "started")
	err = w.observeStability(ctx, env)
	if err == nil {
		w.stage("stability_observation", "completed")
	}
	return err
}

func (w *Worker) stage(name, status string) {
	if w.deps.OnStage != nil {
		w.deps.OnStage(name, status)
	}
}

func (w *Worker) settleBilling(run execution.ExecutionRun) error {
	if w.deps.Billing == nil {
		return nil
	}
	ctx, cancel := context.WithTimeout(context.Background(), w.config.FinalizeTimeout)
	defer cancel()
	return w.deps.Billing.SettlePersistedRun(ctx, run.WorkspaceID, run.RunID)
}

func (w *Worker) event(ctx context.Context, runID, kind, code string, fields map[string]any) error {
	if fields == nil {
		fields = map[string]any{}
	}
	fields["code"] = code
	raw, err := json.Marshal(fields)
	if err != nil {
		return err
	}
	_, err = w.deps.Evidence.AppendRunEvent(ctx, evidenceEvent(runID, kind, code, raw))
	return err
}
func (w *Worker) evaluate(run execution.ExecutionRun) (err error) {
	defer func() {
		if recover() != nil {
			err = platform("evaluation_persistence_failed", errors.New("evaluator dependency panicked"))
		}
	}()
	ctx, cancel := context.WithTimeout(context.Background(), w.config.FinalizeTimeout)
	defer cancel()
	if _, err := w.deps.Evaluator.EvaluateRun(ctx, run.WorkspaceID, run.RunID); err != nil {
		record, done := context.WithTimeout(context.Background(), w.config.FinalizeTimeout)
		defer done()
		return errors.Join(platform("evaluation_persistence_failed", err), w.event(record, run.RunID, "system", "evaluation_persistence_failed", map[string]any{"classification": "platform"}))
	}
	if err := w.event(ctx, run.RunID, "evaluation", "evaluation_completed", nil); err != nil {
		return err
	}
	if w.deps.Verification != nil {
		w.stage("gitops_consistency_and_forward_risk_derivation", "started")
		if err := w.deps.Verification.DeriveRun(ctx, run.WorkspaceID, run.RunID); err != nil {
			return platform("verification_persistence_failed", err)
		}
		w.stage("gitops_consistency_and_forward_risk_derivation", "completed")
		return w.event(ctx, run.RunID, "evaluation", "verification_completed", nil)
	}
	return nil
}

func (w *Worker) observeStability(ctx context.Context, env observation.EnvironmentContext) error {
	if w.deps.Verification == nil {
		return nil
	}
	now := w.deps.Now
	if now == nil {
		now = time.Now
	}
	started := now()
	for {
		if err := w.observe(ctx, env, "stability_observation_collected"); err != nil {
			return platform("stability_observation_failed", err)
		}
		remaining := w.config.StabilityWindow - now().Sub(started)
		if remaining <= 0 {
			return nil
		}
		delay := w.config.StabilityInterval
		if remaining < delay {
			delay = remaining
		}
		waiter := w.deps.Wait
		if waiter == nil {
			waiter = wait
		}
		if err := waiter(ctx, delay); err != nil {
			return err
		}
	}
}
func (w *Worker) cleanup(id kubeenv.RunIdentity) (err error) {
	w.stage("cleanup", "started")
	defer func() {
		if err != nil {
			w.stage("cleanup", "failed")
		} else {
			w.stage("cleanup", "completed")
		}
	}()
	ctx, cancel := context.WithTimeout(context.Background(), w.config.CleanupTimeout)
	defer cancel()
	startErr := w.event(ctx, id.RunID, "system", "cleanup_started", nil)
	cleanupErr := w.cleanupRuntime(ctx, id)
	code := "cleanup_completed"
	if cleanupErr != nil {
		code = "cleanup_failed"
	}
	// A timed out cleanup still gets a short independent chance to record failure.
	record, done := context.WithTimeout(context.Background(), w.config.FinalizeTimeout)
	defer done()
	return errors.Join(startErr, platform("cleanup_failed", cleanupErr), w.event(record, id.RunID, "system", code, map[string]any{"classification": "platform"}))
}

// provisionEnvironment wires the production-shaped run start path through the
// environment scheduler/provider model:
//
//	Run → exact ScenarioVersion → stored requirements → server-owned
//	scheduler → profile/provider → EnvironmentInstance → provider provision
//	→ ready → existing execution.
//
// The worker never provisions Kubernetes directly while bypassing the
// environment service when Environments is set. Legacy ScenarioVersions
// without explicit requirements resolve through LegacyDefaultRequirements
// (kubernetes.namespace:v1 on kind-dev:v1, never production isolation).
// Scheduler or provider failures are platform/environment failures: the
// agent is never invoked and no misleading agent evaluation evidence is
// produced. Pins (profile/provider/isolation/runtime/instance IDs) are
// returned for safe reproducibility evidence; provider credentials and raw
// kubeconfigs are never exposed.
func (w *Worker) provisionEnvironment(ctx context.Context, run execution.ExecutionRun, sv scenario.Version, id kubeenv.RunIdentity) (kubeenv.Environment, map[string]any, error) {
	if w.deps.Environments == nil {
		environment, err := w.deps.Runtime.Provision(ctx, id)
		if err != nil {
			return kubeenv.Environment{}, nil, platform("environment_provisioning_failed", err)
		}
		return environment, nil, nil
	}
	runEnv, err := w.deps.Environments.ProvisionForRun(ctx, run, sv)
	if err != nil {
		var f *Failure
		if errors.As(err, &f) {
			return kubeenv.Environment{}, nil, err
		}
		return kubeenv.Environment{}, nil, platform("environment_provisioning_failed", err)
	}
	return runEnv.Provisioned, runEnv.Pins(), nil
}

func (w *Worker) cleanupRuntime(ctx context.Context, id kubeenv.RunIdentity) (err error) {
	defer func() {
		if recover() != nil {
			err = platform("cleanup_failed", errors.New("cleanup dependency panicked"))
		}
	}()
	var revokeErr error
	if w.deps.MCP != nil {
		revokeErr = w.deps.MCP.RevokeRun(ctx, id.RunID)
	}
	// Provider-owned environment destruction runs before the legacy runtime
	// cleanup. Both are idempotent; repeated cleanup is safe. A cleanup
	// failure never alters the agent verdict (callers join it after terminal
	// persistence) and leaves state suitable for sweeper/recovery.
	envErr := w.cleanupEnvironments(ctx, id)
	return errors.Join(revokeErr, envErr, w.deps.Runtime.Cleanup(ctx, id))
}

// cleanupEnvironments destroys the run-owned EnvironmentInstance idempotently.
// Nil Environments preserves legacy behavior.
func (w *Worker) cleanupEnvironments(ctx context.Context, id kubeenv.RunIdentity) error {
	if w.deps.Environments == nil {
		return nil
	}
	if err := w.deps.Environments.CleanupIdentity(ctx, id); err != nil {
		var f *Failure
		if errors.As(err, &f) {
			return err
		}
		return platform("cleanup_failed", err)
	}
	return nil
}
