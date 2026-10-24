# Infrastructure Agent Verification Runtime v1

## 1–3. Repository state

- Branch: `infrastructure-agent-verification`
- Starting HEAD: `47e8f25` (`feat(frontend): reset product around agent change verification`)
- Initial worktree contained only the expected frontend visual refinement:
  `app/layout.tsx`, `app/page.tsx`, `app/hero-scale.css`, and
  `app/verification-proof.css`. Those changes were preserved.
- No credential, `.env`, generated-secret, or unexplained backend change was
  present.

## 4. Files changed

Runtime and persistence:

- `backend/migrations/000022_verification_runtime_v1.{up,down}.sql`
- `backend/internal/verification/{model,derive,risk,repository,service}.go`
- `backend/internal/verification/derive_test.go`
- `backend/internal/executionworker/{dependencies,orchestrator,composition}.go`
- `backend/internal/executionworker/worker_test.go`
- `backend/internal/agentgateway/model.go`
- `backend/internal/observation/provider.go`
- `backend/internal/evaluationbatch/{model,validation,repository,service}.go`
- `backend/internal/evaluationbatch/{validation,integration}_test.go`
- `backend/internal/httpapi/{router,verifications,evaluation_batches}.go`
- `backend/internal/migrationtest/migrations_test.go`
- `backend/cmd/api/main.go`
- `backend/README.md`

Frontend wiring:

- `lib/api.ts`
- `app/verifications/verification-list.tsx`
- `app/runs/[id]/trial-detail.tsx`

Expected pre-existing visual work retained in the final diff:

- `app/layout.tsx`
- `app/page.tsx`
- `app/hero-scale.css`
- `app/verification-proof.css`

This report is also new.

## 5. Homepage background

The global public background token is overridden from warm `#f7f7f5` to
almost-white `#fcfcfb` in `hero-scale.css`. Layout, type, navigation, black
buttons, orange accent, copy, and white evidence surfaces are unchanged.

## 6. Migration/schema

Migration `000022_verification_runtime_v1` is additive and reversible.

`verification_results` is one immutable authoritative projection per Run. It
pins workspace, Run, AgentVersion, ScenarioVersion, optional aggregate
EnvironmentInstance and CompositionPlan/hash, verifier/snapshot contracts,
baseline and final snapshots, intent, action inspection, infrastructure diff,
outcome, side effects, stability, forward risks, validity, overall status,
source context, evidence bounds, and creation time. Historical runs are not
backfilled. An update trigger prevents mutation.

`evaluation_batches` gains bounded typed source metadata and an optional
workspace-scoped idempotency key with a partial unique index. Repeating the
same key returns the original batch and does not create duplicate runs.

A disposable native PostgreSQL 16 cluster migrated cleanly to version 22 with
`dirty=false`, exercised down-one/up-again through the migration test, and
confirmed the result table, immutable trigger, and CI idempotency index.

## 7–9. VerificationResult and status semantics

Contracts are `infrastructure-verifier:v1` and `kubernetes-snapshot:v1`.
Overall status is server-derived and cannot be submitted by a customer.

- `VERIFIED`: valid evidence, passed outcome, no blocking side effect, stable,
  and no risk finding.
- `FAILED`: failed outcome, blocking safety effect, or unstable result.
- `WARNING`: passed/stable result with non-blocking side effect or supported
  forward risk.
- `INCONCLUSIVE`: invalid task/platform, absent required snapshot/telemetry, or
  an agent execution that ended before outcome verification.

Run lifecycle remains separate. Agent execution failure is represented as a
valid-but-inconclusive verification when platform evidence remains sound;
platform and malformed-task failures use `invalid_platform` and
`invalid_task` respectively.

## 10. Baseline

After environment preparation and before condition start, the production
worker captures `verification_baseline_captured`. The snapshot is scoped to
owned Kubernetes Deployments, Pods, HPAs, and the permitted Prometheus metric
set. Deployment image, replica state, resource version, resource requests and
limits are structured fields. Arbitrary event text and unknown metric keys are
removed from persisted verification snapshots.

ReplicaSets, Services, and ConfigMaps are not part of the v1 safe snapshot.
Kubernetes Secrets and environment-variable contents are never queried.

## 11. Intent

Intent contains the exact ScenarioVersion agent task plus its deterministic
criteria (`key`, metric, operator, target). It describes the required end state
and does not encode reference remediation or a required trajectory. A task
without a machine-verifiable assertion becomes `invalid_task`/`INCONCLUSIVE`.

## 12–13. Action inspection, actual diff, and attribution

The projection reuses `agent_actions` and retains requested, authorized,
denied, executed, and failed distinctions with safe request/result payloads.
It independently compares baseline and final workload state for image, desired
replicas, ready replicas, availability, creation, and deletion.

An exact executed-action target match gives `HIGH` deterministic attribution
and `EXPECTED` classification. A changed resource with no matching executor
record is `UNATTRIBUTED` with `UNKNOWN` attribution; it is not blamed on the
agent. The current v1 does not infer probabilistic causality.

## 14. Outcome

Outcome consumes the existing deterministic evaluator. `pass`, `fail`, and
`inconclusive` map to `PASSED`, `FAILED`, and `INCONCLUSIVE`. Infrastructure
state and Prometheus criteria remain authoritative; HTTP success or agent text
is never treated as outcome proof.

## 15. Side effects

V1 carries deterministic evaluator safety findings and reports independently
observed unattributed scoped changes. Blocking safety findings fail the
verification; non-blocking findings warn. Protected-resource action rules
remain enforced by the existing authorization/evaluator path.

Universal dependency discovery, broad neighbor-health inference, and GitOps
drift detection are not implemented. Dependency degradation is represented
only when the ScenarioVersion has a supported deterministic criterion.

## 16. Stability

After the scenario workload completes, the worker samples the privileged
observer for a bounded 15-second window at 5-second intervals. Defaults are
explicit in worker configuration. Tests use injectable clock/wait seams and a
micro-window rather than waiting in wall-clock minutes.

Any scoped workload below desired readiness/availability is `UNSTABLE`.
Unavailable required telemetry or absent samples is `INCONCLUSIVE`; otherwise
the result is `STABLE`. The final snapshot follows the stability window.

## 17–18. Forward Risk

`Analyzer` is a deterministic versioned interface. V1 implements only
`kubernetes-hpa-consistency:v1`: when observed final Deployment replicas exceed
the observed HPA maximum, it emits a HIGH controller-reconciliation finding
with exact affected resources, snapshot evidence, trigger, and HIGH evidence
strength. Without HPA evidence or a provable inconsistency it returns zero
findings. No generic prediction, GitOps claim, or fabricated LOW finding is
produced.

## 19. Platform/task validity

Structured worker failure classifications remain authoritative. Platform
failure never becomes `FAILED`; malformed/unsupported task evidence never
becomes agent failure. Missing baseline, final state, stability samples, or
required telemetry fails closed to `INCONCLUSIVE`.

## 20. API additions

Workspace-authorized read endpoints:

- `GET /v1/workspaces/{workspaceID}/runs/{runID}/verification`
- `GET .../verification/actions`
- `GET .../verification/snapshots`
- `GET .../verification/forward-risks`

They expose safe projections from the same immutable row. No endpoint accepts
an overall status. Existing evidence/evaluation/trajectory APIs remain intact.

## 21. Frontend wiring

`/verifications` loads authoritative results for terminal runs. A terminal run
without a v1 result is labeled `LEGACY`, not guessed from run status.
`/verifications/[id]` renders real Intent, Baseline, requested/authorized/
executed action counts, actual diffs and attribution, Outcome, Side Effects,
Stability, Forward Risk, and the existing evidence timeline. Honest legacy
unavailable states remain for historical rows.

## 22. Version comparison

Existing regression/reliability computation remains evaluator-based and was
not rewritten. VerificationResult counts have not yet been added to the
comparison response; the exact AgentVersion pins are retained. This is an
explicitly unsupported customer comparison enhancement, not fabricated UI.

## 23–24. CI contract and GitHub Action

Evaluation-batch creation now accepts typed `source_metadata` (`provider`,
`repository`, hexadecimal `commit_sha`, `pull_request`, `workflow`) and a
workspace-scoped `idempotency_key`. Values are bounded; unknown JSON fields
still fail closed. Metadata is context, never verification evidence.

A GitHub Action was not implemented. Current external auth is an HttpOnly
browser session; there is no safe scoped platform-token contract for CI. A
working Action would require inventing unsafe authentication assumptions.
Backend idempotency/source context is ready, but CI initiation remains deferred
until scoped API-token authentication exists.

## 25. Security/redaction

- Snapshots are constructed from typed Kubernetes projections, not raw objects.
- Secret resources and environment-variable values are never queried.
- Arbitrary Kubernetes event messages are excluded from VerificationResult.
- Only an allowlist of telemetry keys crosses into snapshots.
- Action request persistence continues to use the executor's safe proposal.
- Provider refs, kubeconfigs, tokens, credentials, hidden variants, ground
  truth, and reference remediation have no VerificationResult field.
- Source metadata is bounded and typed and is explicitly non-authoritative.

## 26–27. Tests and results

Passed:

```text
GOMAXPROCS=2 GOCACHE=/tmp/evalplane-go-cache go test -count=1 -p 1 \
  ./internal/verification ./internal/evaluationbatch ./internal/executionworker \
  ./internal/observation ./internal/httpapi ./internal/evaluator \
  ./internal/evidence ./internal/trajectory ./internal/environment \
  ./internal/actionexec ./internal/mcpserver ./internal/reliability \
  ./internal/agentregression ./internal/qualification

GOMAXPROCS=2 GOCACHE=/tmp/evalplane-go-cache go vet \
  ./internal/verification ./internal/evaluationbatch ./internal/executionworker \
  ./internal/observation ./internal/httpapi ./cmd/api

TEST_DATABASE_URL=<credential-free disposable local PostgreSQL URL> \
  go test -count=1 -p 1 ./internal/migrationtest/... ./internal/evaluationbatch/...

npm run typecheck
npm run check:routes
npm run build
git diff --check
```

Domain tests cover all four statuses, baseline redaction, requested/authorized/
denied/executed/failed actions, before/after diff, exact and unknown
attribution, stable/unstable/missing telemetry, task/platform/agent validity,
evidence-supported HPA risk, and zero risk without support. Worker tests cover
baseline → stability → final snapshot → evaluator → verification ordering.
Database-backed batch tests cover idempotent CI requests and cross-workspace
batch/run denial.

The frontend build passed with pre-existing Autoprefixer `end` compatibility
warnings in scenario/workspace CSS files not changed by this phase.

## 28. Real runtime proof

No Docker daemon, Kubernetes context, or reachable cluster was available.
Therefore no real agent + Kubernetes + Prometheus + stability + cleanup run was
performed, and this work is not E2E-PROVEN. No runtime evidence was fabricated.

## 29. Unsupported/unproven behavior

- Live Kubernetes/Prometheus execution of the new verification path.
- ReplicaSet, Service, safe ConfigMap, node-pressure, and resource-utilization
  snapshot/analyzer support.
- Universal dependency discovery or broad neighboring-service attribution.
- Git/Argo/GitOps reconciliation evidence.
- Verification-aware version-comparison counters.
- CI platform-token authentication and a GitHub Action.
- Physically isolated verifier credentials/process; v1 is logically privileged
  inside the trusted worker.
- CompositionPlan creation remains Stage 39A infrastructure and is only pinned
  when already available; this phase did not begin heterogeneous composition.

## 30. Maturity

- Verification contracts and deterministic derivation: **FOCUSED-VERIFIED**
- Persistence and migration 22: **FOCUSED-VERIFIED**
- Worker lifecycle integration: **FOCUSED-VERIFIED**
- Kubernetes/Prometheus live runtime: **IMPLEMENTED**, not E2E-PROVEN
- Frontend authoritative-result wiring: **FOCUSED-VERIFIED**
- CI source/idempotency contract: **FOCUSED-VERIFIED**
- GitHub Action: **DESIGNED**
- Total vertical: **FOCUSED-VERIFIED**

No PRODUCT-READY or E2E-PROVEN claim is made.

## 31–32. Diff/status

The exact final `git diff --stat` and `git status --short` are recorded in the
completion response because this report itself changes those values.

## Final product acceptance

1. **YES** — a typed scoped baseline is captured before scenario start.
2. **YES** — exact task objective and deterministic assertions are preserved.
3. **YES** — safe requested action data is inspectable.
4. **YES** — authorization decisions remain distinct.
5. **YES** — executed, denied, and failed executor states remain distinct.
6. **YES** — baseline/final workload state produces concrete field diffs.
7. **YES, conservatively** — exact executed-target matches are HIGH; unmatched
   changes are UNKNOWN/UNATTRIBUTED.
8. **YES** — deterministic evaluator evidence produces outcome state.
9. **YES within v1 scope** — safety and unattributed scoped changes are shown;
   universal dependency discovery is not claimed.
10. **YES** — bounded repeated observations derive stability.
11. **YES within v1 scope** — provable HPA reconciliation risk is supported;
    absent evidence produces no finding.
12. **YES** — the server derives VERIFIED/FAILED/WARNING/INCONCLUSIVE.
13. **YES** — the workspace-authorized result and focused evidence APIs expose
    the evidence and contract pins behind each conclusion.
14. **NO for unattended CI today** — source context and idempotency are ready,
    but scoped non-browser API-token authentication and the GitHub Action are
    still missing.
