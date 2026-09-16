# Qualification Suite Builder — Completion Report

## 1. Files changed

Backend (new package `backend/internal/qualplan/`):
- `contract.go` — `qualification-suite-planner:v1`, plan states
  (`ready|partial|blocked`), 12 machine-readable blocker codes, contract pins.
- `model.go` — `Plan`, `CapabilityPlan`, `SelectedTask` (exact ScenarioVersion
  IDs, trial counts, sort order, content hash, certification ID, environment
  requirements), `Blocker`, `SuiteBuild`.
- `planner.go` — pure deterministic planner: coverage requirement → required
  families/case groups → eligible candidates → minimal sufficient selection
  with trial-count derivation. No I/O, no LLM, stable sorted output.
- `bridge.go` — built-in catalog (`srequal` + `gitopsqual`) → immutable
  persisted `ScenarioVersion` materialization: deterministic slug
  (`builtin-<task-id>`), SHA-256 content hash, idempotent reuse, new version
  on definition change, evaluator-only reference remediation never mapped.
- `service.go` — orchestration: claim-subset validation, bridge
  materialization (phase 1, write tx), candidate evaluation against Stage-28
  certification truth + Stage-29 scheduler truth (phase 2, read-only), plan
  persistence, READY-only suite creation through existing `EvaluationSuite`.
- `repository.go` — plan persistence, slug lookup, suite-build mapping,
  policy-availability check. Plans immutable (trigger); suite links are
  separate rows, never UPDATEs.
- `planner_test.go` (18 tests), `bridge_test.go` (5 tests),
  `service_test.go` (4 tests: subset rules, suite-entry dedupe, pins).

Backend (modified):
- `internal/coverage/requirements_v2.go` (+ `requirements_v2_test.go`) —
  `capability-coverage-contract:v2`: 10 conservative GitOps/delivery
  requirements; V1 map untouched.
- `migrations/000019_qualification_plans.{up,down}.sql` — `qualification_plans`
  (+ immutability trigger) and `qualification_plan_suites` mapping table.
- `internal/httpapi/qualification_plans.go` (+
  `qualification_plans_test.go`) — three endpoints; `DisallowUnknownFields`
  rejects `state`/task IDs/`certified`/coverage or qualification results.
- `internal/httpapi/router.go` — route wiring + `WithQualificationPlanService`.
- `backend/cmd/api/main.go` — service construction with existing collaborators.
- `backend/README.md` — endpoint list extended (3 routes).

Frontend:
- `lib/api.ts` — `QualificationBlocker`, `QualificationSelectedTask`,
  `QualificationCapabilityPlan`, `QualificationPlan`, `QualificationSuiteBuild`.
- `app/agents/[id]/agent-detail.tsx` — per-version Qualification section:
  claimed capabilities → readiness → required coverage → blockers → suite;
  "Build qualification plan", READY-gated "Create Qualification Suite", no
  auto-launch, never shows QUALIFIED.

## 2. Migration/schema

`000019_qualification_plans`: `qualification_plans` (workspace/agent/version
FKs, capability IDs, six contract pins, `ready|partial|blocked` state, plan
JSONB, immutability trigger) + `qualification_plan_suites` (one suite link
per plan). Down migration drops both plus trigger/function. Next number used
was 000019 (max was 000018).

## 3. Planner contract/version

`qualification-suite-planner:v1`, pinned on every plan and every generated
suite version (`purpose=qualification` metadata). Historical suites stay
interpretable if logic changes later.

## 4. QualificationPlan model/states

Persisted immutable `Plan`: READY (all requested claims satisfiable),
PARTIAL (some), BLOCKED (none, or zero claims via `no_capability_claims`).
READY means a suite can be constructed — never that the agent passed it.

## 5. Coverage contract v1/v2 behavior

V1 (`capability-coverage-contract:v1`, 8 requirements) is byte-identical:
`GetRequirement`/`ListRequirements` untouched, existing assessment rows never
rewritten (test: `TestV1RequirementsUnchangedByV2`). V2 view
(`GetRequirementV2`/`ListRequirementsV2`, 18 entries) serves V1 entries with
their original V1 contract plus 10 new V2 entries.

## 6. GitOps/delivery coverage requirements

All 10 candidates from §5 published, each demanding 2 independent families,
2 valid trials per family, positive cases, and a negative/decoy/adversarial
contrast group for remediation-class capabilities. No requirement weakened
for convenience.

## 7. Claim selection

Empty subset → all declared competency claims. Non-empty subset must be
within the version's claims (`selectCapabilities`); unknown or unclaimed IDs
return validation errors. Zero claims → structured BLOCKED plan, nothing
fabricated from tool permissions.

## 8. Task eligibility

Candidates pass only when: family/case matches the requirement, exact
ScenarioVersion is fresh-certified, scheduler finds an eligible active
profile/provider, runtime contract is allow-listed, agent declares required
actions, and the context is runnable. Workspace-authored versions with
explicit family assignments go through identical gates.

## 9. Authoritative certification enforcement

`scenariocertification.Service.GetStatus` per candidate (fresh `certified`
only; `stale` → `task_certification_stale`). Catalog "focused certification
validation passed" notes are never consulted. Structurally valid but
uncertified tasks yield `task_not_certified`.

## 10. Environment eligibility

`environment.Service.Resolve` per candidate (`purpose=trial`). Planned
`gitops.argocd:v1`, missing `argo_rollouts` provider, and dedicated-only
tasks all resolve to no eligible environment → `environment_unavailable`.
Scheduler never downgrades fidelity (no Kind-for-dedicated substitution).

## 11. Runtime/tool compatibility

Claim-level check via `competency.MissingRequirements` plus per-task check of
the task's gateway action vocabulary against declared runtime capabilities.
Missing permissions yield `agent_runtime_capability_incompatible` or
`runtime_contract_unsupported`; nothing is ever auto-granted.

## 12. Blocker taxonomy

All 12 codes from §9 implemented (`IsBlockerCode`), each with
capability/family context, operator-facing detail, and a `blocks_suite`
flag. `qualification_policy_unavailable` is informational only.

## 13. Deterministic selection algorithm

Per capability: lowest-ID eligible task per required family, then greedy
lowest-ID additions closing scope-wide case-type gaps (mirrors
`coverage.Assess` scope semantics). Tie-breaks: family → built-in task ID →
exact ScenarioVersion ID. Same inputs → same tasks/order/trials (test).

## 14. Family breadth/contrast handling

`MinDistinctFamilies` enforced; one family never masquerades as another
(exact family identity). One task may serve several capabilities when its
metadata genuinely names them (suite entries dedupe with max trials).
Contrast groups force negative/decoy/adversarial inclusion (test).

## 15. Trial-count derivation

`distributeTrials`: each family's minimum split across its selected tasks
(≥1 each, remainder to first). Totals always equal the requirement minimum
(2 per family). No frontend invention, no pass@k (Stage 33 scope untouched).

## 16. Built-in task → ScenarioVersion bridge

`ListBuiltinTasks` unifies `srequal` (12 runnable; planned dedicated
concepts excluded) and `gitopsqual` (12). `ToVersionInput`/`ToCreateInput`
map every field the scenario validator requires (category from domain,
parsed durations, criteria with sort order, `traffic_spike` execution spec
only for traffic-spike tasks, `unimplemented` otherwise). Ground truth
persists on the row (never agent-visible); reference remediation is never
mapped — it stays in the Stage-28 reference table.

## 17. Materialization immutability/idempotency

Slug `builtin-<task-id>` per workspace (unique constraint + conflict retry
for concurrent builders). Hash match → reuse (+ repair missing
family/requirements wiring only); hash mismatch → `LockAndNextVersion` +
new version; existing versions never modified. `ValidateBuiltinMappings`
test proves all 24 catalog tasks materialize under current validation.

## 18. Suite creation

`BuildSuite`: rejects non-READY plans (validation error) and double builds
(conflict); dedupes shared tasks; creates existing `EvaluationSuite` +
`SuiteVersion` with pinned entries; records the mapping row. Never calls
`StartSuiteExecution`.

## 19. SuiteVersion pins/metadata

Entries pin exact ScenarioVersion IDs, trial counts, stable sort order.
Metadata pins purpose, planner/coverage/family/catalog/certification/
evaluator/environment-profile contracts, capability IDs, plan ID, agent
version ID. Generated suites flow through existing `SuiteExecution`,
comparative reports, and regression comparisons unchanged.

## 20. Separation of plan/build/execute

Three separate operations (POST plan → POST suite → existing execution
endpoints). The frontend never auto-launches trials after suite creation.

## 21. Comparison/regression compatibility

No new engine: generated versions are ordinary `SuiteVersion` rows, so
`StartSuiteExecution`, `GetSuiteResults`, agent-regression comparisons, and
qualification assessments consume them directly.

## 22. Current Kubernetes readiness truth

`srequal` namespace tasks resolve environmentally, but with no persisted
Stage-28 certification the planner reports `task_not_certified` (BLOCKED or
PARTIAL) rather than READY. Fake fixture grading is never consulted.

## 23. Current GitOps/delivery readiness truth

`gitops.argocd:v1` planned, no `argo_rollouts` provider, no live
certification → GitOps/delivery claims plan as PARTIAL/BLOCKED with truthful
`environment_unavailable` + `task_not_certified` blockers. No READY is
manufactured.

## 24. API/RBAC

- `POST /v1/workspaces/{ws}/agents/{agent}/versions/{ver}/qualification-plans`
  (owner/admin/member; 201)
- `GET /v1/workspaces/{ws}/qualification-plans/{plan}` (any member; 200)
- `POST /v1/workspaces/{ws}/qualification-plans/{plan}/suite`
  (owner/admin/member; 201; READY only)
- Viewer build attempts → 403 (service `requireWrite`; httpapi test).
- Unknown/server-derived fields → 400 via `DisallowUnknownFields`.

## 25. Agent-detail qualification UX

Per-version Qualification block shows claimed capabilities, readiness
(READY/PARTIAL/BLOCKED), per-capability family/trial coverage, exact
blockers, and the generated suite ID when built — e.g. a capacity claim
renders "1 / 2 task families · Authoritative task certification missing →
BLOCKED". Restrained list markup, no gauges/scores, never claims QUALIFIED.

## 26. Frontend plan/build flow

"Build qualification plan" → state display → (READY only) "Create
Qualification Suite" → suite ID + "trials have not been launched" note.
Blocked plans explain exactly why via blocker codes/details.

## 27. Security/leakage

Responses carry only safe metadata (IDs, family/case, trial counts, pins,
blocker reasons). Ground truth, reference remediation, hidden variants,
credentials, kubeconfigs, and sentinel values never leave the server
(httpapi test asserts absence; `AgentView`/gateway surfaces untouched).
All reads/writes workspace-scoped; eligibility checks run through
workspace-authorized services.

## 28. Focused backend tests

- `qualplan`: 22 tests (contract pin, zero-claims, minimal sufficiency,
  determinism, contrast, uncertified/stale/env/runtime blockers, dedicated
  no-downgrade, unavailable requirement, partial, never-qualified,
  multi-capability sharing, subset rules, entry dedupe, reproducibility
  pins, bridge mapping/hash/slug/origin).
- `coverage`: +3 V2 tests (V1 frozen at 8, V2 publishes 10 with breadth/
  trials/contrast, combined view preserves V1 entries).
- `httpapi`: 6 endpoint tests (create, unknown-field rejection ×5,
  caller-task-ID rejection, blocked-suite failure, viewer 403, plan read,
  no-qualified/no-leakage assertions).
- Regression: `competency`, `srequal`, `gitopsqual`,
  `scenariocertification`, `environment`, `evaluationsuite`,
  `qualification`, `agent`, `scenario`, `agentregression` — all pass
  (`-count=1`).

## 29. TypeScript/build

`npx tsc --noEmit` clean; `npm run build` production build succeeds;
`gofmt` clean; `go vet` clean on touched packages; `go build ./...` clean.

## 30. Confirmation: planner never means QUALIFIED

The planner vocabulary contains only `ready|partial|blocked`. Qualification
verdicts come exclusively from Stage-22 policy assessments over executed
evidence. Tests assert no `qualified` string in planner outputs and no
qualified-state constant exists in the package.

## 31. Confirmation: unavailable GitOps cannot produce READY

`gitops.argocd:v1` is planned and no provider supports `argo_rollouts`,
so scheduler truth yields `environment_unavailable` for every GitOps/
delivery candidate; combined with absent live certification, READY is
unreachable for those claims today (tests: `TestUnavailableEnvironmentBlocks`,
`TestPartialWhenSomeCapabilitiesCovered`).

## 32. Confirmation: fake certification does not count authoritative

The planner reads only `scenariocertification.Service.GetStatus`
(`eligible_authoritative_evidence` ⇔ fresh `certified`). Fixture builders in
`srequal`/`gitopsqual` grading paths and `SafeView.CertificationNote` text
are never inputs to eligibility.

## 33. Confirmation: no LLM task selection/grading

No LLM import, client, prompt, or call exists in `qualplan`, `coverage`
(V1+V2), or the bridge. Selection is a pure sorted greedy function;
grading remains the deterministic evaluator.

## 34. Deferred work

- DB-backed integration tests for `qualplan.Service` (build/get/build-suite
  round-trip, idempotent materialization under concurrency, workspace
  isolation) — pure logic is fully tested; service paths need
  `TEST_DATABASE_URL` scratch runs per repo convention.
- Reference-remediation authoring + live certification for materialized
  versions (normal Stage-28 path; bridge deliberately stops before it).
- Multi-agent union/intersection planning (V1 plans one version; generated
  suites already execute against many via existing comparison).
- Trial-count tuning when real evidence distributions are known.

## 35. Real-runtime limitations

- No live cluster was used: Kind/Argo CD/Prometheus/Docker were not run
  (§35). Materialized GitOps/delivery tasks are therefore honestly
  unrunnable until real providers exist.
- Migration 000019 was syntax/style-reviewed against repo conventions but
  not applied to a live database here (integration harness
  `migrationtest` covers it when `TEST_DATABASE_URL` is set).
- Only `kubernetes.namespace:v1` on `kind-dev:v1` is an active
  profile/provider, so current READY reachability is limited to certified
  namespace tasks — of which none are certified yet, hence BLOCKED-by-
  default is the expected live behavior until Stage-28 certifications land.
