EVALPLANE — QUALIFICATION SUITE BUILDER CATALOG BRIDGE CONSISTENCY PATCH

Read AGENTS.md first.

Read:

- prompts/reports/kubernetes-sre-qualification-catalog.md
- prompts/reports/gitops-progressive-delivery-qualification.md
- prompts/reports/qualification-suite-builder.md
- backend/internal/srequal
- backend/internal/gitopsqual
- backend/internal/qualplan/bridge.go
- qualplan bridge/planner tests

This is a narrow Stage-32 correctness patch.

Do not redesign the qualification planner.

==================================================
PROBLEM
==================================================

Stage 30 defines 12 runnable Kubernetes/SRE built-in tasks.

Stage 31 defines 13 GitOps/progressive-delivery built-in tasks.

Therefore the built-in qualification catalog contains 25 relevant task
definitions before excluding explicitly planned cluster concepts.

The Stage-32 completion report says:

srequal = 12
gitopsqual = 12
total materialized = 24

Determine whether this is:

1. only a completion-report counting mistake
or
2. a real catalog→ScenarioVersion bridge omission.

Do not assume either.

==================================================
1. INVENTORY EXACT IDS
==================================================

Enumerate the exact built-in task IDs from:

srequal
gitopsqual

Compare them against:

qualplan.ListBuiltinTasks
materialization mappings
planner candidate discovery

Produce deterministic counts and exact missing/extra IDs.

==================================================
2. EXPECTED CATALOG TRUTH
==================================================

Preserve:

12 Stage-30 runnable SRE tasks

13 Stage-31 GitOps/progressive-delivery tasks

Do not include Stage-30 planned dedicated-cluster concepts as runnable
qualification candidates.

All 13 Stage-31 tasks may remain environmentally unavailable, but they must
still exist in the bridge so the planner can produce truthful blockers.

Unavailable does NOT mean omitted.

==================================================
3. FIX IF REAL
==================================================

If one Stage-31 task is missing:

include it through the same generic bridge path.

Do not special-case its ID unless the catalog architecture genuinely requires
it.

Preserve:

family
case type
capability mappings
environment requirements
content hash
immutable ScenarioVersion semantics
evaluator-only reference separation

Do not make it runnable merely by materializing it.

==================================================
4. PLANNER TRUTH
==================================================

The formerly omitted task, if any, must:

- appear as a candidate where its family/capability applies
- still fail authoritative eligibility when uncertified
- still fail environment eligibility when its provider/profile is unavailable
- never cause fake READY state

==================================================
5. TESTS
==================================================

Add focused tests asserting:

- Stage-30 runnable built-ins count = 12
- Stage-31 built-ins count = 13
- unified bridge count = 25
- IDs are unique
- every expected Stage-31 ID appears exactly once
- every unified built-in task materializes successfully
- repeated materialization remains idempotent
- unavailable GitOps tasks remain unavailable after materialization
- planner still cannot manufacture READY from unavailable/fake-certified tasks
- no evaluator-only reference data enters ScenarioVersion agent-visible fields

If repository truth shows that 24 is intentionally correct for a documented
reason, do not force 25. Explain exactly which task is intentionally excluded
and why it can never serve qualification planning.

==================================================
6. VERIFICATION
==================================================

Run focused:

internal/qualplan/...
internal/srequal/...
internal/gitopsqual/...

and directly relevant scenario tests.

Do not run go test ./...
Do not start Docker/Kubernetes/Argo CD.

gofmt touched Go files.

No frontend work unless necessary.

==================================================
COMPLETION REPORT
==================================================

Report:

1. root cause
2. Stage-30 exact count
3. Stage-31 exact count
4. unified bridge exact count
5. missing/excluded task ID if applicable
6. files changed
7. materialization behavior
8. planner behavior
9. focused tests
10. confirmation unavailable GitOps still cannot produce READY

Write to:

prompts/reports/qualification-suite-bridge-consistency.md

Do not overwrite this task file.
