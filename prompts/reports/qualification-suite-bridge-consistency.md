# Qualification Suite Builder Catalog Bridge Consistency — Completion Report

Source task: `prompts/tasks/qualification-suite-bridge-consistency.md`

## 1. Root cause

Only a completion-report counting mistake. No real catalog → ScenarioVersion
bridge omission exists.

`backend/internal/qualplan/bridge.go:ListBuiltinTasks` iterates the full
`srequal.List()` plus the full `gitopsqual.List()` through the same generic
`fromSRETask` / `fromGitOpsTask` path with no ID filter, no special-casing,
and stable ID sorting. `Service.materializeCatalog` iterates the whole
`ListBuiltinTasks()` output, and `evaluateCandidates` turns every
materialized task into a planner candidate. Nothing drops a task.

The Stage-32 report (§16–§17) wrote `gitopsqual (12)` / `all 24 catalog
tasks`, but the Stage-31 catalog truth is 13 tasks, so the unified bridge
truth is 25. No code change was required to include a missing task; the fix
is the counting correction plus regression tests pinning 12 / 13 / 25.

## 2. Stage-30 exact count

12 runnable tasks (`backend/internal/srequal/catalog.go:tasks`,
`List()` returns all 12 in stable ID order):

- `capacity-decoy-no-scale-needed`
- `dependency-decoy`
- `dependency-outage`
- `deployment-readiness-failure`
- `healthy-rollout-do-not-rollback`
- `missing-telemetry-reasoning`
- `protected-resource-boundary`
- `recovery-verification`
- `restart-not-the-fix`
- `restart-required`
- `traffic-capacity-saturation`
- `workload-resource-pressure`

4 planned dedicated-cluster concepts (`node-failure`, `cluster-dns-failure`,
`karpenter-scaling`, `cni-failure`) remain `Planned=true` with
`dedicated_cluster` requirements and are excluded from the runnable bridge
by construction (they never resolve to the namespace profile).

## 3. Stage-31 exact count

13 tasks (`backend/internal/gitopsqual/catalog.go:tasks`, `List()` returns
all 13 in stable ID order):

- `delivery-analysis-progression-failure`
- `delivery-canary-error-regression`
- `delivery-canary-latency-regression`
- `delivery-dependency-degradation`
- `delivery-healthy-canary-hold`
- `delivery-routing-misconfiguration`
- `gitops-argo-decoy`
- `gitops-argo-sync-failure`
- `gitops-bad-desired-state`
- `gitops-config-drift`
- `gitops-deleted-managed-resource`
- `gitops-live-deployment-drift`
- `gitops-live-hotfix-trap`

## 4. Unified bridge exact count

25 (`len(ListBuiltinTasks()) == 12 + 13`). Verified by
`TestBridgeConsistencyCounts`. IDs unique, stable ID order, every expected
Stage-31 ID present exactly once (`TestBridgeConsistencyStage31IDsExact`).

## 5. Missing/excluded task ID

None missing. No task is intentionally excluded beyond the documented
Stage-30 planned dedicated-cluster concepts, which can never serve
qualification planning because they require `dedicated_cluster` isolation
and the scheduler never downgrades them to the namespace profile. All 13
Stage-31 tasks exist in the bridge with family, case type, capability
mappings, environment requirements, content hash, and immutable
ScenarioVersion semantics preserved.

## 6. Files changed

- `backend/internal/qualplan/bridge_consistency_test.go` (new) — 7 focused
  consistency tests pinning the 12/13/25 truth (see §9).
- `prompts/reports/qualification-suite-bridge-consistency.md` (this report).

No production code changed: `bridge.go`, `service.go`, `planner.go`,
`srequal/*`, `gitopsqual/*` already implement the correct generic path.
No frontend work was necessary. No migration. `gofmt` clean.

## 7. Materialization behavior

Unchanged and verified for all 25 tasks:

- Deterministic slug `builtin-<task-id>` per workspace.
- SHA-256 content hash over the canonical definition (contract version, task
  ID, agent task, visible context, observables, actions, resources,
  conditions, criteria, forbidden behaviors, ground truth, family, case
  type, requirements, capabilities).
- Hash match → idempotent reuse (+ family/requirements wiring repair only);
  hash mismatch → `LockAndNextVersion` + new immutable version; existing
  versions never modified.
- `ValidateBuiltinMappings()` passes for all 25; per-task
  `ValidateVersion` + `ValidateCreate` pass
  (`TestBridgeConsistencyAllMaterialize`).
- Repeated materialization inputs are byte-stable with hash round-trip
  through `SourceMetadata.content_hash`
  (`TestBridgeConsistencyIdempotentMaterialization`).
- Ground truth persists on the version row (never agent-visible);
  evaluator-only reference remediation is never mapped — it stays in the
  Stage-28 reference table (`TestBridgeConsistencyNoReferenceLeakage`).
- All 13 GitOps tasks keep their unavailable provider requirements
  (`argocd`/`git`/`helm` or `argo_rollouts`) through the bridge and still
  report `runnable=false` / no eligible environment via the live scheduler
  (`TestBridgeConsistencyGitOpsRemainUnavailable`). Unavailable does NOT
  mean omitted. No task was made runnable merely by materializing it.

## 8. Planner behavior

Unchanged and verified:

- Every bridge task appears as a candidate where its family/capability
  applies (family presence asserted for all 25 in
  `TestBridgeConsistencyPlannerCannotManufactureReady`).
- Uncertified candidates → `task_not_certified`, never READY (SRE and GitOps
  claims).
- Certified-but-unavailable GitOps candidates → `environment_unavailable`,
  never READY — including the fake-certified case, proving fixture
  `Certify()` success is never consulted as authoritative eligibility (the
  planner reads only `scenariocertification.Service.GetStatus` fresh
  `certified` plus `environment.Service.Resolve`).
- Ineligibility reports the first failing gate per candidate
  (certification before environment), so a fully uncertified pool surfaces
  `task_not_certified` while the certified-but-unavailable pool surfaces
  `environment_unavailable`; both block suite creation.

## 9. Focused tests

New `backend/internal/qualplan/bridge_consistency_test.go` (7 tests):

- `TestBridgeConsistencyCounts` — SRE 12, GitOps 13, bridge 25, planned
  concepts excluded, IDs unique + stably ordered.
- `TestBridgeConsistencyStage31IDsExact` — every expected Stage-31 ID
  appears exactly once; no extra `gitopsqual` ID outside the set.
- `TestBridgeConsistencyAllMaterialize` — `ValidateBuiltinMappings` plus
  per-task version/create validation for all 25.
- `TestBridgeConsistencyIdempotentMaterialization` — hash determinism,
  input stability, slug stability, hash round-trip.
- `TestBridgeConsistencyGitOpsRemainUnavailable` — 13 GitOps bridge tasks
  keep unavailable requirements, resolve to no eligible environment via the
  live scheduler, stay `runnable=false`, and keep family/case/capability
  mappings.
- `TestBridgeConsistencyPlannerCannotManufactureReady` — candidate presence
  for all families; uncertified → blocked; fake-certified-but-unavailable
  → blocked; focused `Certify()` passes yet planner stays blocked.
- `TestBridgeConsistencyNoReferenceLeakage` — ground truth persists on the
  row but never in `AgentView`; no reference-remediation markers, no
  reference verification notes, and no evaluator-only fields in agent
  surfaces or `SourceMetadata` (origin/id/contract/hash/planner only).

Run (per task §6, no `go test ./...`, no Docker/Kubernetes/Argo CD):

- `GOCACHE=/tmp/evalplane-go-cache go test ./internal/qualplan/... ./internal/srequal/... ./internal/gitopsqual/... ./internal/scenario/... -count=1` — all pass.
- `GOCACHE=/tmp/evalplane-go-cache go vet ./internal/qualplan/... ./internal/srequal/... ./internal/gitopsqual/...` — clean.
- `gofmt` on the new test file — clean.

## 10. Confirmation unavailable GitOps still cannot produce READY

Yes. All 13 GitOps/progressive-delivery bridge tasks remain
environment-unexecutable (`gitops.argocd:v1` planned, no `argo_rollouts`
provider) and uncertified through the authoritative path, so GitOps/delivery
claims plan as PARTIAL/BLOCKED with truthful `task_not_certified` +
`environment_unavailable` blockers. `TestBridgeConsistencyPlannerCannotManufactureReady`
asserts no READY from unavailable/uncertified pools and no READY from the
fake-certified-but-unavailable pool. No evaluator-only reference data enters
`ScenarioVersion` agent-visible fields (`TestBridgeConsistencyNoReferenceLeakage`).
