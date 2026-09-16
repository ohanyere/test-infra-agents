# Completion report — Environment Run-Lifecycle Wiring

## 1. files changed

New:

- `backend/internal/executionworker/runenv.go` — `RunEnvironment`, `RunEnvironments` interface, pins, safe-ref validation, requirement/scheduler helpers. Worker depends on this abstraction, never Kind specifics.
- `backend/internal/executionworker/runenv_managed.go` — `managedRunEnvironments` (requirements → scheduler → instance → provider → ready), `instanceStore` interface, deterministic `memoryInstanceStore` fake backend.
- `backend/internal/executionworker/runenv_postgres.go` — `postgresInstanceStore` + `NewPostgresRunEnvironments` production wiring (exact ScenarioVersion requirements, server-owned scheduler, run-owned instance, provider registry).
- `backend/internal/executionworker/runenv_test.go` — 24 focused fake-provider tests (no live Kubernetes).

Modified:

- `backend/internal/environment/repository.go` — added `GetByOwner` / `ListByOwner` (workspace + owner_type + owner_id, newest-first) so cancelled/abandoned/crashed runs remain discoverable and cleanup-capable. No migration, no schema change.
- `backend/internal/executionworker/dependencies.go` — added optional `Environments RunEnvironments` dependency (nil preserves legacy direct provision for backward compatibility; production always sets it).
- `backend/internal/executionworker/orchestrator.go` — run-start now goes through `provisionEnvironment` (scheduler/provider/instance → ready → existing Prepare/Start/agentLoop); `cleanupRuntime` now also destroys the owned instance idempotently; added `cleanupEnvironments` helper. Agent invocation still only after ready; HTTP/MCP mediation unchanged.
- `backend/internal/executionworker/composition.go` — `NewPostgresWorker*` now builds `kubeenv.Manager` → `KindDevProvider` → `NewPostgresRunEnvironments` and sets `Dependencies.Environments`. Kind remains merely the development provider implementation.
- `backend/internal/executionworker/recovery.go` — unchanged code, but `RecoverOne` now cleans environments via `cleanupRuntime` → `cleanupEnvironments` (same Stage-18 lease/recovery framework, no second framework; never invokes agent or replays setup).

## 2. worker integration point

`Worker.provisionEnvironment` (`orchestrator.go`) + `Worker.cleanupRuntime`/`cleanupEnvironments` + `RecoverOne` (via `cleanupRuntime`). `ExecuteRun` flow:

```
Run → LoadVersions (exact ScenarioVersion) → stored requirements
→ scheduler → instance (provisioning) → provider.Provision → instance (ready)
→ provisionedHandle → environment_ready (with pins) → Prepare/Start
→ MarkOwnedRunRunning(environment.Reference) → agentLoop → Finish
→ terminal → evaluate → cleanup (provider Destroy + mark destroyed + runtime cleanup)
```

When `Environments == nil`, legacy direct `Runtime.Provision` is used (existing unit tests preserved). Production composition always sets it.

## 3. requirement resolution

`PostgresRunEnvironments` loads `scenario_version_environment_requirements` via `environment.Repository.GetRequirements` for the exact `sv.ID` (fallback `run.Scenario.VersionID`); `ErrNotFound` → `LegacyDefaultRequirements()`, `explicit=false`. DB errors → `platform/environment_requirements_unavailable`. `managedRunEnvironments` normalizes empty input through `resolveRequirements` (legacy default). Tests 1–2 prove explicit vs legacy paths.

## 4. scheduler usage

`selectPlacement` → `environment.SelectForRequirements(ctx, req, "trial", "", availability)` with `DefaultAvailability` in prod, deterministic fakes in tests. Priorities unchanged (components → isolation → provider active/supporting/healthy → least-fidelity adequate). Scheduler refusal (`Profile==nil`) returns `Failure{Class:"platform", Code:no_eligible_environment}` — never downgraded, never agent failure.

## 5. EnvironmentInstance creation/ownership

`insertProvisioning` persists before provider provision per repository conventions (`InsertInstance` with `owner_type=run`, `owner_id=runID`, `workspace_id=run workspace`, profile/provider IDs + contracts + isolation, `status=provisioning`, 2h lease, empty `provider_ref`). Post-insert guard rejects cross-workspace mismatch (`markFailed` + `platform/environment_ownership_mismatch`). `GetByOwner`/`ListByOwner` scope every lookup by workspace. No cross-workspace reuse (test 17).

## 6. provisioning lifecycle

Success: provider `Provision` (before any agent call) → `isSafeProviderRef` check → `markReady` (`provisioning→ready`, sets `ready_at` + opaque `provider_ref`) → `provisionedHandle` (namespace derived via `kubeenv.NamespaceForRun`, reference = opaque provider ref). Run retains linkage via existing `runs.environment_ref` (`MarkOwnedRunRunning` with opaque reference) + instance `owner_id=runID`. No kubeconfig/credentials stored or returned (`provider_ref` opaque only).

Failure: scheduler refusal, unknown/unhealthy provider, `Provision` error, or unsafe ref → `markFailed` (where reachable) → `Failure{Class:"platform"}` (`no_eligible_environment` / `provider_unavailable` / `environment_provisioning_failed`). Agent never invoked; evaluation (if reached via terminal path) is platform `inconclusive`, never agent pass/fail.

## 7. agent-invocation ordering

`provisionEnvironment` must return success before `Prepare`/`Start`/`agentLoop`. Tests 5, 7, 8 assert `provider.provisioned==1` before `gateway==1` on success and `gateway==0` on scheduler/provision failure. Existing HTTP/MCP mediation untouched.

## 8. platform-failure behavior

All environment failures use `Failure{Class:"platform"}` with codes `no_eligible_environment`, `provider_unavailable`, `environment_provisioning_failed`, `environment_requirements_unavailable`, `environment_instance_unavailable`, `environment_ready_failed`, `cleanup_failed`. Safe evidence: deferred finalization emits system events with `classification:platform` + code; provider internals never persisted (test 9 checks no leak). `invalid_platform` semantics preserved via platform class + inconclusive evaluation.

## 9. no-downgrade behavior

`Select` never trades fidelity for availability. Dedicated requirement + namespace-only provider → `no_eligible_environment`, never namespace fallback (tests 7, 10, 11, both worker-level and pure-scheduler). Unavailable provider excluded with `provider_unavailable`, never converted to agent failure.

## 10. cleanup behavior

`CleanupIdentity`: lookup by owner → if `destroyed`, nil (idempotent); else resolve provider by `instance.ProviderID` → `provider.Destroy(runID)` → `markDestroyed`. Unknown provider → `markDestroying` + `platform/cleanup_failed` (sweeper-recoverable). Destroy error → `markDestroying` (when reachable) + `platform/cleanup_failed`, never alters agent verdict (terminal already persisted; test 14 asserts `status==completed` + `cleanup_failed` event + non-destroyed state). Both provider destroy and legacy `Runtime.Cleanup` run (both idempotent; kind deletes are UID-preconditioned/NotFound-safe). Repeated cleanup safe (test 13). Untracked runs (no instance) → no-op sweep (legacy namespaces remain reclaimable without touching unrelated infra). Never destroys unrelated infrastructure: deletes scoped to exact run identity/labels.

## 11. cancellation/abandonment recovery

Same Stage-18 lease/recovery framework reused: `ExecuteRun` deferred cleanup covers cancelled runs (stop flag → `stopped` → still destroys instance; test 15). `RecoverOne` calls `cleanupRuntime` → `cleanupEnvironments`, so abandoned/stale runs destroy owned instances without invoking agent or replaying setup (test 16: pre-provisioned instance discoverable via `InstanceForOwner`, destroyed after recovery, `gateway==0`). No second recovery framework introduced.

## 12. reproducibility linkage

No migration. Run row keeps `environment_ref` (opaque); instance row carries pins; evidence carries safe pins via `environment_ready` event (`RunEnvironment.Pins()` / `pinsForInstance`): `environment_profile_id`, `environment_profile_contract_version`, `provider_id`, `provider_contract_version`, `isolation_tier`, `runtime_contract_version`, `scheduler_contract_version`, `environment_instance_id` (test 19). Historical rows untouched.

## 13. legacy compatibility

Empty requirements → `LegacyDefaultRequirements()` → `kubernetes.namespace:v1` on `kind-dev:v1`, namespace isolation only, never production claim (tests 2, 4). Current Kubernetes behavior continues through adapted development provider.

## 14. worker/provider abstraction

`RunEnvironments` interface + `Provider` abstraction only. `runenv*.go` and provisioning path never reference `*KindDevProvider` concrete type, `kubeenv.Manager`, `client-go`, `scenariorunner`, or Terraform/AWS IDs (only `kubeenv.RunIdentity`/`Environment` value types + `environment.Provider` interface). Production wires Kind as one entry in `map[string]environment.Provider`. `ResetForRun` exists for lifecycle completeness (failed reset → `markFailed` + platform error; destroyed never reused) but normal path performs no pooling/warm reuse.

## 15. focused tests

`backend/internal/executionworker/runenv_test.go` (fake providers, no cluster) covers all 24 required items: 1 resolves requirements, 2 legacy default, 3 instance creation, 4 ownership, 5 provision-before-agent, 6 ready marking, 7 scheduler-failure blocks agent, 8 provision-failure blocks agent, 9 provision-failure is platform, 10 unavailable never agent failure, 11 no downgrade, 12 terminal triggers cleanup, 13 repeated cleanup idempotent, 14 cleanup failure preserves verdict, 15 cancelled cleanup, 16 abandoned recoverable, 17 workspace isolation, 18 destroyed never reused, 19 pins/reproducibility, 20 no Kind dependency (functional + no concrete-type assertion), 21 no secret exposure, 22 coverage unchanged (evaluation + no_op semantics), 23 certification unchanged (trial vs certification parity + arbitrary profile rejection), 24 no LLM (determinism). Existing `worker_test.go`/`recovery_test.go`/`variant_test.go` preserved (nil-Environments fallback).

## 16. confirmation no Kind-specific worker dependency

`managedRunEnvironments` works entirely with `environment.Provider` fakes; new tests provision/cleanup/recover without `kubeenv.Manager`, cluster, or `KindDevProvider` type assertion (test 20 asserts non-concrete). `composition.go` is the only place constructing `KindDevProvider`, injected as `environment.Provider`.

## 17. confirmation no provider credential/kubeconfig exposure

`isSafeProviderRef` rejects `kubeconfig`, `-----begin`, `bearer/basic`, `aws_secret`, `private_key`, newlines, >2000 chars. `ProviderRef`/`environment_ref`/events carry `k8s://...`/`fake://...` opaque handles only. No credential columns added; `SafeView` returns opaque ref only (test 21).

## 18. confirmation environment failures never become agent failures

Scheduler refusal, provision failure, unavailable provider all return `Failure{Class:"platform"}`; agent `Invoke` count stays 0; evaluator never records agent pass on these paths (tests 7–11). Cleanup failures join after terminal persistence and never rewrite the verdict (test 14).

## 19. confirmation no live cloud/Kubernetes work

No EKS/vCluster/dedicated/AWS/Terraform/Crossplane/Argo runtime, pooling, billing, pass@k, trajectory, LLM grading, or frontend changes. Focused runs only: `internal/environment`, `internal/executionworker` (GOMAXPROCS=2), plus `internal/scenariocertification`, `internal/coverage`, `internal/kubeenv` as unchanged-behavior checks. No `go test ./...`, Docker, Kind, live cluster, or Prometheus.

## 20. deferred real-runtime validation

Needs live Postgres (`TEST_DATABASE_URL`) for `migrationtest`/real `InsertCertification` with new instance rows, and a real cluster for `KindDevProvider` provision/destroy through `PostgresRunEnvironments`. Scheduler/lifecycle covered with deterministic fakes here; sweeper for long-lived `destroying`/`failed` instances remains future work alongside planned shared/vcluster/dedicated/AWS providers.
