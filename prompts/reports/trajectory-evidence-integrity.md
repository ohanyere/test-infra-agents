# Completion report — Stage 34 Trajectory & Evidence Integrity Patch

## 1. Root causes

**Problem 1 — worker recovery conflated with service recovery.**
`backend/internal/trajectory/project.go` treated any `event_type == "recovery"`
as service-recovery detection (`detectRecovery`), and treated both
`recovery_verified` and `worker_recovery_completed` as verification
(`detectRecoveryVerified`, `projectRunEvent`). Since the worker emits all five
lifecycle codes (`worker_lease_expired`, `run_abandoned`,
`worker_recovery_started`, `worker_recovery_completed`,
`worker_recovery_cleanup_failed`) with `event_type = "recovery"`
(`backend/internal/executionworker/recovery.go:73`), any recovered lease set
`recovery_detected_at`, `recovery_verified_at`, `time_to_recovery`,
`recovery_verified=true`, and split actions into before/after — purely from
platform lifecycle, with no application evidence. Projection also rendered
`worker_recovery_completed` as `EventRecoveryVerified` with actor `evaluator`.

**Problem 2 — suite summary silent omission.**
`Service.SuiteSummary` (`backend/internal/trajectory/repository.go`) did
`continue` on any `LoadTrial` error, then set `TotalTrials = len(trials)`.
A suite with `expected=10, loaded=9` was indistinguishable from a complete
`10/10` suite; the client could not determine completeness, and ownership
violations were quietly dropped.

## 2. Worker-recovery semantics

Preserved, not removed (Stage-18 evidence intact). Worker lifecycle codes —
`worker_lease_expired`, `run_abandoned`, `worker_recovery_started`,
`worker_recovery_completed`, `worker_recovery_cleanup_failed`, plus any future
`worker_*` code — now project as:

- `event_type = platform_invalid`, `phase = evaluation`,
  `actor = platform`, `status = invalid`, `source_type = platform`,
  `view = evaluator`, summary `"Worker recovery: <code>"` with mandatory
  `evidence_refs`.

This keeps the crash/lease/reclaim truthfully visible as platform/execution
evidence, reuses the existing platform-validity classification (payload
`classification=platform` already drives `trialValidity`), and invents no new
failure rule. Worker recovery never sets service-recovery fields and never
implies agent failure or platform-invalid by itself beyond the pre-existing
classification.

## 3. Application-recovery semantics

Only explicit persisted evidence whose semantics mean task/service recovery
counts. Closed exact-match vocabulary (no substring, no generic-type match):

- detected: `recovery_detected`, `recovery_criteria_satisfied`,
  `recovery_verified` (verified implies detection for earliest-time).
- verified: `recovery_verified` only.

Detection reads the explicit payload `code` field only — never free-form
`summary` strings containing "recovery", never `event_type == "recovery"`
alone, never `worker_*`. The `evaluations.recovery_duration_ms` fallback is
retained because it is evaluator evidence (`evaluation.Found &&
RecoveryDurationMS != nil`); with no event and no evaluator duration, all
recovery fields stay null/false (no invention).

## 4. Recovery detection rule

`detectRecovery`: iterate events, extract payload `code` only, keep earliest
`OccurredAt` where `IsApplicationRecoveryDetectedCode(code)` is true.
Otherwise fall back to evaluator `recovery_duration_ms` offset from
`run.CreatedAt`. Else nil.

## 5. Recovery verification rule

`detectRecoveryVerified(events, detected)`: earliest event at/after `detected`
whose payload `code == "recovery_verified"` (exact). `worker_recovery_completed`
and generic recovery strings never verify.

## 6. Actions-after-recovery behavior

Unchanged shape, corrected input: `splitActionsByRecovery` runs only when
application `recoveryDetectedAt != nil`. Worker-only case yields
`recovery_detected_at=null`, `recovery_verified_at=null`,
`time_to_recovery=null`, `recovery_verified=false`,
`actions_before_recovery=len(actions)`, `actions_after_recovery=0`. No action
is labeled post-service-recovery because a lease was reclaimed.

## 7. Suite-summary integrity behavior

`SuiteTrajectorySummary` gains additive fields (all JSON-omitted when empty
except counts/complete):

- `complete: bool`, `expected_trials`, `loaded_trials`,
  `missing_trials: string[]` (workspace-scoped run IDs),
  `issues: [{code, run_id?, detail?}]`, `total_trials` now equals expected
  (previously equaled loaded).

`summarizeSuiteTrials` (pure, injected loader/existence closures) assembles the
summary: success → project trial row; `ErrConflict` → fail request immediately;
`ErrNotFound` → check `RunExists`: exists globally but not in workspace →
fail with `ErrConflict: trial ownership mismatch`; truly absent → record
`missing_trials` + `issues[{code: trial_unavailable}]` and continue (partial
allowed); any other error → fail request. `complete = len(missing)==0`;
complete summaries omit `missing_trials`/`issues` (nil).

## 8. Partial-summary behavior

Allowed only for actually missing/deleted/unavailable rows. Returns
`complete=false` with explicit `expected_trials`/`loaded_trials`/`total_trials`,
`missing_trials` IDs, and `issues` reasons. Never fabricates an outcome row
for a missing trial.

## 9. Ownership/integrity behavior

Referenced trial that belongs to another workspace or violates invariants
(e.g. `action belongs to another run` conflict, or `RunExists=true` but
workspace-scoped `LoadTrial=NotFound`) fails the whole request with existing
`ErrConflict` semantics — never quietly omitted, never leaked (safe conflict
code only, no cross-workspace identifiers in `missing_trials`/`issues`, which
contain only workspace-scoped IDs from `ListSuiteTrials`).

## 10. Frontend behavior

`lib/api.ts`: `SuiteTrajectorySummary` extended with `complete`,
`expected_trials`, `loaded_trials`, `missing_trials?`, `issues?` (plus
`SuiteTrajectoryIssue` type). `app/suite-executions/[id]/suite-view.tsx`
renders a restrained `role="alert"` panel when `complete === false`:
"Trajectory summary incomplete — {loaded} of {expected} trials could be
loaded." plus "Missing trials are not counted as pass, fail, or
platform-invalid. Their outcomes were not fabricated." Only loaded `trials`
are rendered; no missing outcome is invented or graded.

## 11. Files changed

- `backend/internal/trajectory/project.go` — closed recovery vocabulary
  (`IsApplicationRecoveryDetectedCode`,
  `IsApplicationRecoveryVerifiedCode`, `IsWorkerRecoveryCode`); recovery-case
  projection split (application → recovery events, else platform-invalid with
  actor platform); `detectRecovery`/`detectRecoveryVerified` closed exact
  payload-code matching, worker/generic exclusion.
- `backend/internal/trajectory/model.go` — `SuiteIssue` type;
  `SuiteTrajectorySummary` integrity fields.
- `backend/internal/trajectory/repository.go` — `RunExists`; `SuiteSummary`
  rewritten around testable `summarizeSuiteTrials` (conflict fails, NotFound +
  exists → conflict fails, NotFound + absent → explicit partial, other → fail).
- `backend/internal/trajectory/integrity_test.go` — new, 14 focused tests
  (see §12).
- `lib/api.ts` — suite summary integrity types.
- `app/suite-executions/[id]/suite-view.tsx` — incomplete-summary warning,
  no fabricated outcomes.

## 12. Focused tests

New `backend/internal/trajectory/integrity_test.go`:

1. `TestWorkerRecoveryAloneSetsNoDetectedAt`
2. `TestWorkerRecoveryAloneSetsNoVerifiedAt`
3. `TestWorkerRecoveryAloneCreatesNoTTR`
4. `TestWorkerRecoveryAloneCreatesNoPostRecoveryActions`
5. `TestWorkerRecoveryRemainsVisibleAsPlatformEvidence`
6. `TestExplicitApplicationRecoverySetsDetected`
7. `TestExplicitVerificationSetsVerified`
8. `TestApplicationRecoveryLaterActionCountsPostRecovery`
9. `TestGenericRecoveryStringDoesNotVerify`
10. `TestCompleteSuiteSummaryReportsExpectedEqualsLoaded`
11. `TestMissingTrialNeverSilentlyOmitted`
12. `TestPartialSummaryReportsCompleteFalseAndCounts`
13. `TestOwnershipMismatchFailsInsteadOfSkipping`
14. `TestMissingTrialHasNoFabricatedOutcome` (backend half of "frontend never
    invents an outcome"; frontend half proven by code — only loaded trials
    rendered — plus `tsc`/`build` below)
15. Stage-34 requested-vs-executed: existing
    `TestRequestedVsExecutedDistinction` still passes (full trajectory suite
    green).
16. Stage-33 reliability: `reliability` package green (see §13).
17. No evaluator-only leakage:
    `TestWorkerRecoveryProjectionLeaksNoEvaluatorOnly` plus existing
    `TestProvenanceAndHiddenLeakage` green.
18. No LLM logic: `TestProjectionDeterministicNoLLM` (pure deterministic
    double-project equality) plus zero LLM calls in projector.

## 13. TypeScript/build if applicable

- `go test ./internal/trajectory/... -count=1` → ok.
- `go test ./internal/executionworker/...` → ok.
- `go test ./internal/httpapi/... -run Trajectory` → 7/7 pass.
- `go test ./internal/evaluationsuite/...` → ok.
- `go test ./internal/reliability/...` → ok (Stage-33 green).
- `go vet ./internal/trajectory/...` → clean.
- `npx tsc --noEmit` → clean.
- `npm run build` → succeeds (`/runs/[id]/trajectory`, `/suite-executions/[id]`
  present).

Full `go test ./...`, Docker, Kind, Kubernetes, Argo CD not run per task.

## 14. Confirmation worker recovery cannot imply service recovery

Proven by tests 1–4 and 9: `worker_recovery_completed` alone leaves
`recovery_detected_at`, `recovery_verified_at`, `time_to_recovery` nil,
`recovery_verified=false`, `actions_after_recovery=0`; closed-code matching
rejects `worker_*` and generic recovery strings in both detection and
verification; projection emits `platform_invalid`/actor `platform`, never
`recovery_detected`/`recovery_verified`.

## 15. Confirmation missing trials cannot be silently omitted

Proven by tests 10–12: complete summary asserts
`expected==loaded` with `complete=true` and empty missing/issues; any
`ErrNotFound` (truly absent) yields `complete=false`,
`expected≠loaded`, explicit `missing_trials` + `issues`; `expected=10` can
never render indistinguishably from `loaded=9`.

## 16. Confirmation requested != executed remains intact

`TestRequestedVsExecutedDistinction` green in the full trajectory run;
no action-lifecycle code touched.

## 17. Confirmation no evaluator-only leakage

`TestProvenanceAndHiddenLeakage` green; new worker-projection leakage scan
green; repository still selects only opaque `variant_id` (never
`hidden_config`); redaction centralized server-side; workspace predicates on
every query; `missing_trials`/`issues` carry only workspace-scoped IDs.

## 18. Confirmation no LLM logic

Projector remains pure deterministic (`Project` + `summarizeSuiteTrials`:
no I/O, no network, no LLM); `TestProjectionDeterministicNoLLM` passes;
no new endpoints call any model; frontend explanation composes from structured
summary only.

## 19. Stage-34 correction — no prefix-based platform-invalid inference

Root cause: §2 stated that all known worker recovery codes "plus any future
`worker_*` code" project as `event_type=platform_invalid`, `status=invalid`.
The recovery-case fallback in `projectRunEvent` did exactly that (any
non-application recovery → platform-invalid), and `IsWorkerRecoveryCode`
returned true for any `worker_*` prefix. That infers platform-invalid from
the prefix alone; persisted execution/platform classification was not
authoritative.

Classification rule after patch: only platform-classified persisted evidence
projects as platform-invalid, per current validity semantics —
`payload.classification == "platform"` with non-empty code other than
`cleanup_completed` (mirrors `trialValidity`). `IsWorkerRecoveryCode` is now
exact known Stage-18 codes only; `isWorkerLifecycleCode` (exact + `worker_`
prefix) is display-only and never drives failure classification,
service-recovery fields, or validity.

Known worker recovery behavior: with persisted `classification=platform`
(as `executionworker/recovery.go` emits), known codes still project as
`platform_invalid` / `invalid` / actor `platform` with
`"Worker recovery: <code>"` and mandatory `evidence_refs`, and
`trialValidity` still yields `invalid_platform`. Without platform
classification they project as safe worker lifecycle instead.

Unknown future worker event behavior: `worker_*` without platform
classification projects as safe lifecycle evidence —
`cleanup_started` / `cleanup` phase / actor `platform` / `info` status /
`agent_visible` view with `"Worker lifecycle: <code>"`. It manufactures no
`platform_invalid`, no `invalid` status, no agent failure
(`agent_action_failed`), and no service recovery (`recovery_detected_at`,
`recovery_verified_at`, `time_to_recovery`, `actions_after_recovery`
all stay null/0/false). Same gate applied in `projectSystemEvent`; its
default branch was already safe for future codes.

Files changed:

- `backend/internal/trajectory/project.go` — `IsWorkerRecoveryCode` exact
  only; added display-only `isWorkerLifecycleCode` and authoritative
  `isPlatformClassifiedEvent`; recovery-case fallback gated on
  classification (platform-invalid vs. safe `cleanup_started` worker
  lifecycle); `projectSystemEvent` worker codes split from generic
  platform failures and gated the same way, actor `platform` for
  platform-classified worker events.
- `backend/internal/trajectory/integrity_test.go` — 6 new focused tests
  (known+platform → platform-invalid; unknown without classification →
  no platform-invalid but still visible; unknown → no service recovery;
  validity driven by classification; requested-vs-executed green with
  unknown worker; unknown-worker leakage scan).
- Service-recovery rules (`detectRecovery`/`detectRecoveryVerified`,
  closed application vocabulary) and `trialValidity` untouched.

Focused test result:

- `go test ./internal/trajectory/... -count=1` → ok (all existing
  Stage-34 tests 1–18 green plus 6 new correction tests).
- `go vet ./internal/trajectory/...` → clean.
- Full `go test ./...`, Docker, Kubernetes not run per task.

## 20. Final correction — unknown worker lifecycle view is evaluator, not agent_visible

Root cause: §19 fixed platform-invalid inference (classification-gated) but
left the safe fallback visible as `agent_visible`:
`projectRunEvent` recovery-case safe `worker_*` branch emitted
`cleanup_started` / `agent_visible`, and `projectSystemEvent` safe worker
branches (known codes without classification + default unknown `worker_*`)
fell through to `viewFor(cleanup_started)` = `agent_visible`. Actor was
correct (`platform`), but view claimed the agent saw internal
Evalplane/execution lifecycle without persisted evidence the agent received
that event. Contract semantics (`trajectory-evidence-explorer.md` §56):
`agent_visible` = observations, requests, executions, tool results;
`evaluator` = findings, safety, recovery, platform-invalid, evaluation
completion. Worker lifecycle belongs to the latter side.

Final view semantics: no new view vocabulary. Existing closed views
(`agent_visible`, `evaluator`) reused. Any non-invalid worker lifecycle event
— known Stage-18 code without `classification=platform`, or unknown/future
`worker_*` without platform classification — keeps `actor=platform` but uses
`view=evaluator`. Only persisted evidence that the agent received an event
may label it `agent_visible`; none exists for worker lifecycle.

Unknown worker lifecycle behavior (both `recovery` and `system`
persisted types, code `worker_future_reclaim_v2` tested): stays visible as
safe platform lifecycle evidence — `cleanup_started` / `cleanup` phase /
`actor=platform` / `info` status / `view=evaluator` /
`"Worker lifecycle: <code>"` with mandatory `evidence_refs`. It manufactures
no `platform_invalid`, no `invalid` status, no `agent_action_failed`, no
service recovery (`recovery_detected_at`, `recovery_verified_at`,
`time_to_recovery`, `actions_after_recovery` all null/0/false), no validity
change, no redaction change.

Known classified worker behavior: unchanged. With persisted
`classification=platform`, known codes still project as `platform_invalid` /
`invalid` / `actor=platform` / `view=evaluator` with
`"Worker recovery: <code>"`, and `trialValidity` still yields
`invalid_platform`. Without platform classification (including
`classification=ok` or absent), known codes now project as safe lifecycle on
the evaluator side — same as unknown — never `agent_visible`, never invalid.
Service-recovery rules, requested-vs-executed, redaction, service-recovery
timestamps, and classification logic untouched.

Files changed:

- `backend/internal/trajectory/project.go` — recovery-case safe `worker_*`
  branch `View: agent_visible` → `evaluator` with correction comment;
  `projectSystemEvent` adds explicit `viewOverride` (default
  `viewFor(eventType)`, `evaluator` for both safe worker branches: known
  codes without classification and default unknown `worker_*`); no change to
  event_type/phase/actor/status/classification/validity/recovery logic.
- `backend/internal/trajectory/integrity_test.go` — 2 new focused tests:
  `TestUnknownWorkerLifecycleUsesEvaluatorView` (recovery+system unknown
  worker: visible, actor platform, view evaluator, not agent_visible, not
  platform_invalid, no service recovery, provenance + leakage scan);
  `TestKnownWorkerViewBehaviorPreservedByClassification` (known+platform →
  still platform-invalid/evaluator; known without classification → safe
  lifecycle/evaluator, never invalid, validity not manufactured).

Focused test result:

- `go test ./internal/trajectory/... -count=1` → ok (all prior Stage-34 +
  §19 tests green plus 2 new view-correction tests).
- `go vet ./internal/trajectory/...` → clean.
- Full `go test ./...`, Docker, Kubernetes not run per task.
