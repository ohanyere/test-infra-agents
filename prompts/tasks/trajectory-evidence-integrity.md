EVALPLANE — STAGE 34 TRAJECTORY & EVIDENCE INTEGRITY PATCH

Read AGENTS.md first.

Read:

- prompts/reports/trajectory-evidence-explorer.md
- backend/internal/trajectory
- backend/internal/executionworker recovery/lease code
- run-event vocabulary
- evaluator recovery evidence
- suite execution repositories
- trajectory HTTP handlers/tests

This is a narrow Stage-34 correctness patch.

Do not redesign the trajectory explorer.

==================================================
PROBLEM 1 — WORKER RECOVERY != SERVICE RECOVERY
==================================================

Inspect the actual implementation.

The Stage-34 report states that:

recovery_verified_at

may be derived from:

recovery_verified
or
worker_recovery_completed

These concepts must not be conflated.

Worker/execution recovery means Evalplane recovered processing after:

- worker crash
- stale lease
- abandoned execution
- worker restart/reclaim

Application/service recovery means deterministic task recovery criteria became
satisfied.

Only application/service/evaluator recovery evidence may populate:

- recovery_detected_at
- recovery_verified_at
- time_to_recovery
- recovery_verified
- actions_before_recovery
- actions_after_recovery

A worker lifecycle recovery event must never by itself satisfy those fields.

==================================================
1. RECOVERY DOMAINS
==================================================

Preserve worker recovery as trajectory evidence if useful.

Example:

actor = platform
event = worker/execution recovery completed

But do not project it as:

recovery_detected
recovery_verified

unless independent application recovery evidence exists.

Do not remove legitimate Stage-18 worker recovery evidence.

Separate the semantics.

==================================================
2. RECOVERY DETECTION
==================================================

Application recovery may come only from explicit persisted evidence whose
semantics mean task/service recovery, for example:

- evaluator recovery criteria became satisfied
- explicit application recovery event
- explicit recovery_verified evidence

Use the existing actual vocabulary.

Do not infer service recovery from generic strings containing "recovery".

Avoid substring classification.

Prefer closed event/code matching.

==================================================
3. POST-RECOVERY ACTIONS
==================================================

actions_before_recovery / actions_after_recovery must use application recovery
time only.

If only worker recovery exists:

recovery_detected_at = null
recovery_verified_at = null
time_to_recovery = null
recovery_verified = false

No action should be labeled post-service-recovery merely because a worker lease
was recovered.

==================================================
4. PLATFORM SEMANTICS
==================================================

Worker recovery does not automatically mean:

agent failure
service recovery
platform-invalid

Use the existing execution/platform validity semantics.

A worker crash may be represented truthfully according to the existing
platform classification, but this patch must not invent a new failure rule.

==================================================
PROBLEM 2 — SUITE SUMMARY SILENT OMISSION
==================================================

Inspect:

GET
/v1/workspaces/{workspaceID}/suite-executions/{executionID}/trajectory-summary

The completion report says trial rows that fail to load are skipped.

Do not silently omit trials.

The client must be able to determine whether the suite trajectory summary is
complete.

==================================================
5. SUMMARY INTEGRITY
==================================================

Prefer additive integrity fields such as:

complete
expected_trials
loaded_trials
missing_trials

and, where safe:

issues: [
  {
    code,
    trial/run reference if authorized and safe
  }
]

Use repository naming conventions.

Do not expose cross-workspace identifiers.

==================================================
6. OWNERSHIP / CORRUPTION
==================================================

If a referenced trial belongs to another workspace or violates ownership
invariants:

do not quietly omit it.

Treat that as an integrity/security failure.

Prefer failing the request with the existing safe internal/conflict/not-found
semantics rather than returning a misleading partial report.

For an actually missing/deleted/unavailable row where partial rendering is
allowed:

return complete=false with explicit counts/reasons.

Never make:

expected_trials=10

appear indistinguishable from:

loaded_trials=9.

==================================================
7. FRONTEND
==================================================

If suite-summary integrity fields are introduced:

show a restrained warning when:

complete=false

Example:

"Trajectory summary incomplete — 9 of 10 trials could be loaded."

Do not fabricate the missing outcome.

Do not treat the missing trial as pass/fail/platform-invalid.

==================================================
8. TESTS
==================================================

Add focused tests proving:

1. worker_recovery_completed alone does not set recovery_detected_at
2. worker_recovery_completed alone does not set recovery_verified_at
3. worker_recovery_completed alone does not create time_to_recovery
4. worker_recovery_completed alone does not create actions_after_recovery
5. worker recovery remains visible as platform/execution evidence where
   appropriate
6. explicit application recovery still sets recovery_detected_at
7. explicit recovery verification still sets recovery_verified_at
8. application recovery + later action correctly counts post-recovery action
9. no substring-based generic "recovery" event accidentally verifies service
   recovery
10. complete suite summary reports expected == loaded
11. missing trial can never be silently omitted
12. partial summary, if supported, explicitly reports complete=false and counts
13. ownership/integrity mismatch does not leak or silently skip
14. frontend never invents an outcome for missing trial
15. Stage-34 requested-vs-executed behavior remains green
16. Stage-33 reliability regression remains green
17. no evaluator-only leakage
18. no LLM logic

==================================================
9. VERIFICATION
==================================================

Run focused only:

go test ./internal/trajectory/... -count=1

plus directly affected:

internal/evaluationsuite
internal/executionworker
internal/httpapi

and Stage-33 reliability if needed.

Do not run:

go test ./...
Docker
Kind
Kubernetes
Argo CD

If frontend types change:

npx tsc --noEmit

and one npm run build.

==================================================
COMPLETION REPORT
==================================================

Report:

1. root causes
2. worker-recovery semantics
3. application-recovery semantics
4. recovery detection rule
5. recovery verification rule
6. actions-after-recovery behavior
7. suite-summary integrity behavior
8. partial-summary behavior
9. ownership/integrity behavior
10. frontend behavior
11. files changed
12. focused tests
13. TypeScript/build if applicable
14. confirmation worker recovery cannot imply service recovery
15. confirmation missing trials cannot be silently omitted
16. confirmation requested != executed remains intact
17. confirmation no evaluator-only leakage
18. confirmation no LLM logic

Write to:

prompts/reports/trajectory-evidence-integrity.md

DO NOT overwrite this task file.
