# Evaluation worker

`NewPostgresWorker(pool, client, config, runtimeConfig, resolveConnection, onError)`
composes the existing services. Call `worker.Run(ctx)` from the host process, or
`worker.ExecuteRun(ctx, claimedRun)` after `execution.Service.ClaimNextRun`.
The connection resolver receives the workspace and exact persisted AgentVersion.
Supply credentials through trusted runtime configuration; never embed them in
scenario data or observations. `onError` must be concurrency-safe.

Defaults: one run at a time, one-second claim polling, 10-minute run deadline,
30-second gateway deadline, eight agent steps, five seconds between steps, and
250-millisecond stop polling. Run ownership uses a 60-second database-time lease,
renewed every 15 seconds, with a 15-second stale grace and minute recovery sweep.
The worker generates a non-personal process-lifetime ID unless one is configured.
`Config.MaxAgentSteps` is the `MAX_AGENT_STEPS`
setting for a host process. All dependencies must respect context cancellation.
There is no goroutine-per-request timeout escape hatch.

The Kubernetes adapter runs where per-namespace Service DNS is reachable. It
requires an explicitly configured instrumented reference workload image, deploys
the existing workload and Prometheus, waits for readiness, and starts only
`traffic_spike`. Its trusted execution specification accepts:

```json
{"type":"traffic_spike","profile":{"stages":[{"target":10,"duration_seconds":15}]}}
```

An omitted profile uses the existing development traffic profile. An explicitly
false `execution_spec.runnable_now`, an unsupported type, or an invalid explicit
profile rejects the trial before provisioning. The existing ScenarioVersion has
no separate persisted `runnable_now` column. No migration is introduced.

The agent receives `ScenarioVersion.AgentView()` task/context/action/resource
fields plus current permitted observation channels. No evaluation or execution
data is included. Arbitrary Kubernetes event messages are withheld. Resource
policy uses exact `{kind, namespace, name}` entries; an omitted namespace or
`$RUN_NAMESPACE` binds only to this run's owned namespace. An empty resource list grants
no mutation targets. There are no wildcard resource grants. Each mutation is
independently checked again by the existing executor immediately before execution.

The worker owns evidence for gateway calls, avoiding the gateway's standalone
proposal persistence. Each proposal has one row, advancing to `allowed`/`denied`
and then `executed`/`failed`. Structured safety events reference its action ID, so
the evaluator does not double count rows and events. Remote decision prose,
arbitrary action parameters, tokens, and raw dependency errors are not persisted.

Empty or exclusively `no_op` responses stop decisions; reaching the step bound
also stops decisions. Neither indicates successful remediation. The scenario
runner finishes, then a final observation is recorded while the environment is
present. Terminal state precedes deterministic evaluation, which precedes cleanup.
Missing final snapshots are explicitly recorded. Platform-classified failures
invalidate the trial's measurable criteria; proven safety findings remain failures.

StopBatch persists a stop flag immediately for active batches. The worker polls it
and checks it before every action. Detached, bounded finalization survives caller
cancellation. Cleanup verifies the full workspace/batch/run identity and namespace
UID. Errors are joined so a cleanup failure does not erase the primary error.

Terminal ExecuteRun retries evaluate and clean up only. They never provision or
replay actions. Evaluation reconciliation is transactional and does not duplicate
criterion rows/findings. Startup and periodic bounded sweeps atomically acquire
expired `provisioning`/`running` leases, record stable recovery evidence, perform
ownership-safe cleanup, fail the run as a platform interruption, reconcile its
batch, and invoke the deterministic evaluator. Recovery never calls the agent or
replays setup/actions. A former owner that loses its lease stops without cleanup or
terminal writes so it cannot race the recovery owner.

Verification uses fakes with `GOMAXPROCS=2 go test ./internal/executionworker/...`.
No cluster, database, Prometheus process, or frontend is needed for these tests.
