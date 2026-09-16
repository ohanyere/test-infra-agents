# COMPLETION REPORT — GitOps & Progressive Delivery Qualification

Source task: `prompts/tasks/gitops-progressive-delivery-qualification.md`
(Note: the requested filename `propmt/tasks/gitops-progressive-qualification.md`
does not exist; the implemented spec is
`prompts/tasks/gitops-progressive-delivery-qualification.md`.)

## 1. Files changed

Backend (new package `backend/internal/gitopsqual/`):
- `backend/internal/gitopsqual/doc.go` — package contract (no second
  runtime/grader/API, no LLM, truthfulness rules).
- `backend/internal/gitopsqual/sot.go` — source-of-truth model
  (`git` / `gitops_controller` / `live_kubernetes` owners, symptom vs
  root-cause layers, `LiveOnlyInsufficient()`).
- `backend/internal/gitopsqual/fixtures.go` — deterministic in-memory
  Git / Argo CD / Rollout fixtures (isolated per evaluation, exact
  revision, deterministic reset, no credentials).
- `backend/internal/gitopsqual/catalog.go` — 13-task catalog + bounded
  hidden-variant schema + validation.
- `backend/internal/gitopsqual/grading.go` — scenario projection,
  reference inputs, fake-certification fixtures, scheduler-derived
  availability, agent-safe `SafeView`, leakage sentinels.
- `backend/internal/gitopsqual/catalog_test.go` — 30+ focused tests
  mirroring spec §34.

Backend (edits):
- `backend/internal/environment/requirements.go` —
  `RequirementsForTaskFamily` now maps `delivery.progressive.*` to
  `[argo_rollouts, kubernetes, prometheus]`/namespace (honest: no active
  profile provides `argo_rollouts`).
- `backend/internal/httpapi/qualification_tasks.go` — `GET
  /v1/qualification-tasks` and `GET /v1/qualification-tasks/{taskID}` now
  aggregate `srequal` + `gitopsqual` through one shared response shape
  (no second GitOps task API).
- `backend/internal/httpapi/qualification_tasks_test.go` — updated to the
  new truth (12 runnable SRE + 13 unavailable GitOps/delivery, leakage
  checks, GitOps get-by-id test).

Frontend:
- `app/scenarios/scenarios-data.ts` — `GitOps` domain, 13 planned tasks
  with `Requires GitOps Environment — Not yet available`, `Drift /
  Desired state / Argo CD / Progressive delivery` families, 13 detail
  entries.
- `app/scenarios/scenario-directory.tsx` — filters extended for the new
  families (restrained row layout unchanged).

Docs:
- `backend/README.md` — NOT updated (it already omits several domains;
  route truth remains `backend/internal/httpapi/router.go`). Flagged as
  deferred work below.

## 2. Catalog architecture

New package `backend/internal/gitopsqual` mirrors the proven `srequal`
pattern without duplicating systems: task definitions + reusable fixture
primitives + deterministic grading builders over the existing
`scenario.Version`, `coverage` family/case, `environment` scheduler,
`deterministic-evaluator:v1`, and `scenario-certification-contract:v1`
stacks. Execution, grading, certification, and coverage engines are
untouched; the HTTP layer aggregates both catalogs into the single
existing endpoint.

## 3. GitOps task list (7)

| ID | Case | Family |
|----|------|--------|
| `gitops-live-deployment-drift` | positive | `gitops.drift.deployment` |
| `gitops-config-drift` | positive | `gitops.desired_state.remediation` |
| `gitops-deleted-managed-resource` | positive | `gitops.drift.deployment` |
| `gitops-bad-desired-state` | positive/cross-layer | `gitops.desired_state.remediation` |
| `gitops-live-hotfix-trap` | negative | `gitops.desired_state.remediation` |
| `gitops-argo-decoy` | decoy/cross-layer | `gitops.argocd.recovery` |
| `gitops-argo-sync-failure` | positive | `gitops.argocd.recovery` |

## 4. Progressive-delivery task list (6)

| ID | Case | Family |
|----|------|--------|
| `delivery-canary-error-regression` | positive | `delivery.progressive.error_regression` |
| `delivery-canary-latency-regression` | positive | `delivery.rollout.recovery` |
| `delivery-healthy-canary-hold` | negative | `delivery.progressive.error_regression` |
| `delivery-analysis-progression-failure` | edge | `delivery.rollout.recovery` |
| `delivery-routing-misconfiguration` | positive/cross-layer | `delivery.rollout.recovery` |
| `delivery-dependency-degradation` | decoy/cross-layer | `delivery.progressive.error_regression` |

Total: 13 tasks (spec range 10–14). Positive 8 / negative 2 / decoy 2 /
edge 1 across multiple independent families (no repeated-incident
versions).

## 5. Task families / case types

Reused Stage-27 families only — no new family invented:
`gitops.drift.deployment`, `gitops.desired_state.remediation`,
`gitops.argocd.recovery`, `delivery.rollout.recovery`,
`delivery.progressive.error_regression`. Case types use the existing
`positive / negative / decoy / edge` vocabulary (`TestFamiliesReused`).

## 6. Source-of-truth model

`sot.go`: `DesiredStateOwner ∈ {git, gitops_controller,
live_kubernetes}` plus evaluator-only `SymptomLayer` / `RootCauseLayer`
from a closed layer vocabulary (git, gitops_controller, kubernetes,
delivery, routing, dependency, telemetry, telemetry_platform, safety,
prometheus_symptom). `LiveOnlyInsufficient()` is true whenever Git or the
controller owns desired state. Owner/layers are evaluator metadata: the
agent learns ownership only through symptom-level visible context, never
through ground-truth remediation instructions (`TestNoLeakage` asserts
`SourceOfTruth` JSON never appears agent-side).

## 7. Environment requirements

- GitOps tasks: `[git, argocd, kubernetes, prometheus, helm]` /
  namespace — exactly `gitops.argocd:v1` composition (profile stays
  `planned`).
- Progressive tasks: `[kubernetes, prometheus, argo_rollouts]` /
  namespace — `argo_rollouts` is `planned` and in no profile.
- Minimum isolation is namespace throughout; no dedicated-cluster
  requirement invented. Kind alone is never presented as a GitOps
  environment.

## 8. Git provider / runtime

`GitFixture`: isolated per evaluation (`eval/<id>/desired-state`, path
traversal rejected), exact content-hash revision pinning, deterministic
`Reset()` to the initial revision, mediated `InspectDesired()` /
`InspectHistory()` projections, controlled `PatchDesired()` /
`RevertDesired()` typed methods, in-memory live-divergence tracking with
`LiveDivergence()` detection. Never touches the Evalplane source repo,
the developer machine, or any real cluster; no credentials anywhere.
`TestGitLiveDivergenceDetectable` + `TestFixtureDeterminism` cover
revision advance/revert/reset semantics.

## 9. Argo CD provider / runtime

`ArgoFixture`: environment-owned application only (name constrained to a
plain token), bound to an isolated `GitFixture`, deterministic
health/sync projection (`Inspect()`), `InjectDrift()` setup, `Synchronize()`
that blocks while live divergence persists (correct-control-plane path),
`MarkReconciled()` for repaired-source flows, deterministic `Reset()`.
No admin token exposure (no tokens exist), no cluster-admin. Real
development Argo CD runtime was NOT implemented: per spec §22, the
deterministic interface/fake coverage ships while the profile stays
`planned` — truth over availability.

## 10. Argo Rollouts integration

No rollout controller model was duplicated (none exists in the repo).
`RolloutFixture` is the bounded abstraction for qualification: canary
phase vocabulary (`Progressing/Paused/Aborted/Healthy/Degraded`),
deterministic candidate/stable telemetry with evaluator-owned thresholds
(error 5%, latency 500 ms), permitted `Pause/Resume/Abort/Rollback`
scoped to the owned rollout with recorded action history, deterministic
`Reset()`. No production-grade delivery platform installed.

## 11. Deterministic grading

All criteria use supported evaluator metrics only (`availability_pct`,
`error_rate_pct`, `p95_latency_ms`, `pod_ready_count`,
`deployment_available_replicas`, `agent_action_count`) with measurable
`gt/gte/lt/lte/eq/neq` operators. Source-of-truth convergence is graded
as: desired correct + reconciliation healthy + live correct + recovery
verified — operationalized as converged-good passes vs diverged/live-only
fails on the same criteria (`TestSourceOfTruthConvergence`,
`TestBadDesiredStateAuthoritative`, `TestLiveHotfixInsufficient`).
Cross-layer measurement records evaluator-only root-cause layers (§12).
No LLM anywhere (`TestNoLLMGrading`).

## 12. Cross-layer reasoning measurement

4 cross-layer tasks (`gitops-bad-desired-state`,
`gitops-argo-decoy`, `delivery-routing-misconfiguration`,
`delivery-dependency-degradation`) model symptom ≠ root-cause
(Prometheus symptom → Kubernetes → bad Git desired state; canary
symptom → healthy candidate → dependency outage). Root-cause layers are
evaluator-only and never exposed (`TestCrossLayerModel`).

## 13. Reference remediations

Every task carries evaluator-only `reference-remediation:v1` input
(validated by `ValidateReferenceInput`). GitOps references remediate the
authoritative source via `set_state` with `control_plane: git |
gitops_controller` params (never live-only); progressive references show
valid abort/rollback recovery or healthy-canary restraint (`no_op`).
References prove solvability; they are not the required trajectory.

## 14. Certification results

All 13 tasks pass deterministic fake certification
(`StateCertified`, `TestReferencesPassCertification`):
`criteria_validation → initial_state → reference_solvability →
grader_positive → grader_negative → safety/telemetry/repeatability/
reset/variant/leakage` all green. Status is honestly reported as
"Requires GitOps Environment — Not yet available. Focused certification
validation passed; live-runtime certification pending." No task is
labeled production-certified.

## 15. Hidden variants

Bounded schema (`ValidateVariantParams`): GitOps (`drifted_field`,
`desired/live_revision`, `resource_kind/name`, `sync_condition`,
`app_health`) and delivery (`candidate_error_rate_pct`,
`candidate_latency_ms`, `traffic_percent`, `stable/candidate_healthy`,
`dependency_health`, `analysis_state`, `routing_state`). Unknown keys and
out-of-range values fail; negative variants must keep candidate error <
5% and latency < 500 ms so they remain logically negative under all
variants (`TestNegativeVariantsRemainNegative`).

## 16. Safety boundaries

Protected resources inaccessible (no protected fixture exists; allowed
resources are owned-only); Git mutation constrained to the
evaluation-owned path; Argo ops to the owned app; rollout ops to the
owned rollout; no raw credentials; no cluster-admin. Unauthorized
mutation remains structured safety evidence via the existing
denied-action evaluator path (`BuildSafetyInput`, safety gate in every
fixture). `TestBoundaries` + `TestNoCredentialsOrAdmin` enforce this.

## 17. Coverage impact

Coverage contracts and requirement thresholds unchanged
(`TestCoverageRequirementsIntact`: all `min_distinct ≥ 2`,
`min_trials ≥ 2`; contract versions intact). New tasks exercise
`gitops.*` / `delivery.*` families so future evidence can honestly
advance those capabilities toward TESTED; nothing was weakened to
manufacture TESTED.

## 18. Availability truth

Every `gitopsqual` task reports `runnable=false`,
`availability=not_available` (`no_eligible_environment` /
`profile_not_active` / `missing_required_component`) derived from the
live scheduler — no hardcoded flag, no fake Argo CD capability.
`gitops.argocd:v1` remains `planned` (asserted in test). The API and
frontend mirror this truth.

## 19. API

Reused `GET /v1/qualification-tasks` (+ `/{taskID}`); no second GitOps
task API. Safe metadata extended per spec §31 (domain, family, case
type, capabilities, environment requirements, availability, honest
certification note); solution data excluded (leakage-tested at catalog
and HTTP layers).

## 20. Frontend UX

Evaluation Tasks directory gains `GitOps` (Drift, Desired state, Argo
CD) and extended `Delivery` (Rollouts, Progressive delivery) groups with
restrained row layout. All 13 tasks show `Requires GitOps Environment —
Not yet available` from backend-architecture truth (planned status),
with detail pages explaining observables/expected/forbidden behavior.
`/` teaser and `/environments` page needed no change (already describe
GitOps as planned).

## 21. Focused backend tests

`GOCACHE=/tmp/evalplane-go-cache go test` — all pass:
`gitopsqual`, `srequal` (Stage-30 regression), `scenario`, `evaluator`,
`scenariocertification`, `coverage`, `environment`, `httpapi`.
`go vet` clean on touched packages. `gofmt` clean. No `go test ./...`,
no Docker/Kind/live Argo CD per spec §35.

## 22. Stage-30 regression result

`internal/srequal` fully green (all 30+ catalog tests); the 12 runnable
namespace tasks still resolve to `kubernetes.namespace:v1`; planned
concepts still never resolve. `httpapi` updated expectations pass.

## 23. TypeScript / build

`npx tsc --noEmit` — exit 0. `npm run build` — success; all 27 static
task paths prerendered (`/scenarios/[id]` shows `[+24 more paths]`).

## 24. Confirmation: live-only mutation insufficient where Git owns state

`TestLiveHotfixInsufficient` + `TestBadDesiredStateAuthoritative` +
`TestSourceOfTruthConvergence`: unresolved/diverged inputs evaluate to
`fail`; only converged (desired + reconciliation + live + recovery)
states pass. Observable state/actions graded, never private reasoning.

## 25. Confirmation: healthy canary not rewarded for rollback

`TestHealthyCanaryRestraint`: 5-action spam on healthy-canary metrics
evaluates to `fail`; reference is `inspect + no_op`.

## 26. Confirmation: unavailable runtime not falsely advertised

`TestUnavailableProviderNotRunnable`: all 13 tasks `runnable=false`
with scheduler reasons; API + frontend show "Requires GitOps
Environment — Not yet available"; `gitops.argocd:v1` asserted
non-executable.

## 27. Confirmation: no evaluator/reference/root-cause leakage

`TestNoLeakage` (AgentView, gateway-shaped, MCP-shaped payloads +
`SafeView` + HTTP bodies): no ground truth, reference JSON,
source-of-truth JSON, hidden variants, sentinels, or decoy/negative
markers agent-side.

## 28. Confirmation: no raw credentials / cluster-admin

`TestNoCredentialsOrAdmin` scans task text, observables, mediated ops,
actions, and all agent payloads for credential/admin markers — clean.
Mediated operations are typed names only (`request-argocd-sync`,
`abort-rollout`, …); fixtures hold no tokens.

## 29. Confirmation: no LLM grading

`TestNoLLMGrading`: repeated `Certify()` bit-identical; no
llm/openai/anthropic/gpt markers in versions or payloads. Grader is
`deterministic-evaluator:v1` throughout.

## 30. Real-runtime limitations

- No live Git server, Argo CD instance, or Rollouts controller is
  contacted; fixtures are in-memory deterministic doubles.
- `gitops.argocd:v1` stays `planned`; tasks are structurally +
  fake-certified but not runnable and not production-certified.
- Mediated operations are vocabulary + fixture methods, not a live
  gateway/MCP tool surface (no new agent tools were added to
  `actionexec`/`mcpserver`).

## 31. Deferred work

- Real development GitOps provider + live-runtime certification (the
  explicit Stage-31 path this stage keeps open).
- `backend/README.md` endpoint list sync (pre-existing drift: it omits
  agents/scenarios/batches/suites/regressions/qualification/evidence/
  execution routes beyond identity + scenario-drafts).
- Progressive-delivery coverage requirements publication (families exist;
  no new `coverageRequirements` entries were added to avoid changing
  breadth rules mid-stage).
- Live MCP/HTTP gateway surfacing of mediated GitOps operations once a
  real provider exists.
