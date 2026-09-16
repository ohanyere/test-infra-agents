# Completion report — Reliability & eval science (Stage-33)

Source task: `prompts/tasks/reliability-eval-science.md` (read completely; this
report does not overwrite it). Evalplane now reports reproducible reliability
at task, family, capability, suite-execution, and agent-version scopes,
distinguishing "succeeded at least once" from "succeeds consistently".

## 1. Files changed

Backend (new package `backend/internal/reliability/`):

- `contract.go` — package rules, `reliability-metrics-contract:v1`,
  `reliability-policy:v1`, supported k set, validity/outcome/sufficiency/
  saturation/capability-state constants, `RoundPercent` (single display-edge
  rounding point, one decimal place).
- `policy.go` — versioned `Policy` + `DefaultPolicy()`; deterministic
  `AssessSufficiency` (insufficient / provisional / sufficient).
- `trial.go` — server-built `Trial` evidence row; `IsCountedValid`,
  `IsSuccess`, `IsAgentFailure`, `OutcomeClass` (reuses deterministic outcome
  vocabulary, no new taxonomy).
- `metrics.go` — `Summarize` (empirical), `KMetrics` (finite-sample pass@k /
  pass^k), `ConsecutiveSuccess` (max + latest, stable persisted order),
  `DescribeDiversity` (distinct variants, coverage ratio, dominant share).
- `task.go` — `TaskReliability` + `BuildTaskReliability` with full contract
  pins, outcome distribution, failure breakdown.
- `family.go` — `BuildFamilyReliabilities`; breadth (distinct
  ScenarioVersions) and repetition (valid trials) stay separate;
  `single_scenario` breadth note when retries dominate.
- `capability.go` — `BuildCapabilityReliabilities` over Stage-27/32
  coverage requirements; required/covered families, pooled required-family
  summary, contrast-case reliability; states are
  `insufficient_evidence | limited_evidence | measured` (never qualified).
- `saturation.go` — `BuildSaturation` (fresh / developing / saturated) with
  machine-readable reasons; deterministic; never pass-rate-based.
- `comparability.go` — `PinsForTrials` + `CheckComparability` with stable
  `not_comparable` reason codes.
- `compare.go` — `Compare`: pass-rate / pass@k / pass^k / valid-sample /
  platform-invalid / per-family deltas, gated on comparability.
- `report.go` — `ExecutionReport`, `AgentReport`, `ScenarioReport`,
  `ComparisonReport`, shared `ScopedSummary` (empirical + k + sufficiency +
  `consistency_k3` convenience pointer).
- `repository.go` — workspace-scoped SQL reusing the exact platform-invalid
  classification already used by `evaluationsuite`/`coverage`; selects opaque
  `variant_id` only (never hidden configs, remediations, or ground truth).
- `service.go` — `ScenarioReliability`, `ExecutionReliability` (optional
  baseline/candidate comparison), `AgentVersionReliability`; viewer-readable,
  writer role not required; claims resolved via `ClaimReader` (no import
  cycle).
- `reliability_test.go` — focused tests incl. spec math vectors (see §28).

Backend (wiring, no refactors):

- `backend/internal/httpapi/reliability.go` (new) — three GET handlers;
  request bodies never accepted; comparison query params must arrive as a
  pair; UUIDs validated.
- `backend/internal/httpapi/router.go` — `ReliabilityService` config,
  `WithReliabilityService`, three routes (see §23).
- `backend/internal/httpapi/reliability_test.go` (new) — auth, viewer read,
  UUID rejection, comparison shape, workspace isolation.
- `backend/cmd/api/main.go` — reliability service construction with a
  `reliabilityClaimAdapter` (mirrors the coverage adapter); router option
  added.
- `backend/README.md` — three new endpoint lines.

Frontend:

- `lib/api.ts` — `Reliability*` types + `AgentReliabilityReport`.
- `app/agents/[id]/agent-detail.tsx` — per-version Reliability section (see
  §24) + per-execution trial-evidence list (see §25).
- `app/scenarios/[id]/page.tsx` — restrained Fresh/Developing/Saturated
  explainer (see §26).

No changes to: evaluator grading, execution engine, planner, qualification
policy verdicts, coverage contracts, agent-regression comparison, migrations.

## 2. Migration/schema

None. Per §26 ("prefer deterministic recomputation when cheap"), all
readings are recomputed from exact persisted trial evidence
(runs/evaluations/findings/variant assignments/family assignments) on every
call. There are no mutable "current score" rows. Historical reproducibility
comes from immutable trial evidence plus the contract/policy versions pinned
on every report (§5, §22 below).

## 3. Reliability contract/version

`reliability-metrics-contract:v1` (`reliability/contract.go`), pinned as
`reliability_contract_version` on every task/family/capability/saturation/
comparison payload. Formula changes require a new contract version; V1
estimators are frozen and unit-tested.

## 4. Reliability policy/version

`reliability-policy:v1` (`reliability/policy.go`), pinned as
`reliability_policy` (full threshold object) on execution/agent/scenario
reports. Conservative V1 defaults: min 3 / target 5 valid trials per
ScenarioVersion; min 6 / target 10 per Task Family; min 2 distinct variants;
min 2 distinct ScenarioVersions per family; min 1 contrast trial reported.
Reliability thresholds are separate from coverage breadth requirements.

## 5. Valid-trial definition

Exactly one counted-valid deterministic evaluation exists
(`validity=valid`). Excluded from every denominator: `invalid_platform`
(provisioning failure, platform-class run events, `runtime_failure` /
`telemetry_unavailable` findings on terminal runs, abandoned platform
execution) and `pending` (no evaluation yet). Classification SQL is byte-for-
byte the same predicate already used by `evaluationsuite` aggregation and
`coverage` assessment. Unknown validity values are treated as pending
(never counted).

## 6. Pass-rate calculation

`passed / (passed + failed)` as 0–100, nil when no decisive valid outcomes
exist. Inconclusive/stopped trials are valid evidence but neither success
nor failure, so they are excluded from this denominator — matching existing
suite aggregation semantics. Documented on `Empirical.PassRate`.

## 7. pass@k definition/formula

"Can the agent succeed at least once in k attempts?" Finite-sample
unbiased hypergeometric estimator over n valid trials with c passes, each
valid trial counted as success (pass) or non-success:

`pass@k = 1 - C(n-c, k) / C(n, k)` for n >= k, else unavailable.

Computed multiplicatively (`1 - prod(n-c-i)/(n-i)`) so no integer
combinatorics can overflow. The naive `1-(1-pass_rate)^k` independence
assumption is never used. Never called "consistency".

## 8. pass^k definition/formula

"Do all k attempts succeed?" (consistency), finite-sample:

`pass^k = C(c, k) / C(n, k)` for n >= k, else unavailable.

Computed as `prod(c-i)/(n-i)`. `pass_rate^k` is never used (pinned out by
`TestPassAtKAndPassToTheKDiffer`). Displayed more prominently than pass@k
everywhere.

## 9. Supported k values

Server-owned bounded set k = 1, 2, 3, 5 (`SupportedK`). A metric is emitted
only when n >= k; otherwise `available=false` with null estimates, rendered
as "insufficient valid trials" (never 0%).

## 10. Sample sufficiency

Deterministic states from the versioned policy: `insufficient` (below
minimum valid count), `provisional` (minimum met, consistency target not
reached), `sufficient` (target reached). Task scope uses scenario
thresholds; family/capability/execution/agent scopes use family thresholds.
No statistical significance is claimed.

## 11. Task-level reliability

`TaskReliability`: valid trials, passes, failures, platform-invalid count,
pass rate, pass@k, pass^k (supported k), consecutive signals, hidden-variant
count + distinct hidden variants exercised, sample sufficiency, outcome
distribution, failure breakdown. Pins: ScenarioVersion, AgentVersion,
runtime contract, environment refs observed, evaluator contracts observed,
reliability contract + policy, distinct execution count. Cross-contract
merging is rejected by comparability, not silently averaged.

## 12. Family-level reliability

`FamilyReliability` aggregates only compatible valid evidence from
ScenarioVersions with an explicit assignment to the same Task Family
(unassigned versions never contribute). Reports distinct ScenarioVersions
(with sorted ID list), distinct variants, valid trials, pass rate,
consistency (same supported-k estimates), case-type distribution,
sufficiency, and a `single_scenario` breadth note when all valid evidence
comes from one task.

## 13. Capability-level reliability

Uses `coverage.GetRequirement` (Stage-27/32 capability-to-family mappings):
required families, covered families (same minimum rule as the coverage
engine), per-family reliability (required first, observed extras after),
pooled required-family summary, valid trial count, contrast-case trials +
contrast pass rate, and `reliability_state`. States are evidence-depth only;
QUALIFIED is never emitted here.

## 14. Hidden-variant diversity

`Diversity`: distinct opaque variant IDs exercised, variant-trial count,
eligible-variant denominator + coverage ratio only when legitimate
(execution scope with configured variants; otherwise omitted, never
fabricated), and `dominant_variant_share` (peak variant share of valid
trials). Hidden configs, reference remediations, and ground truth are never
selected; only the opaque `variant_id`/`variant_seed` already surfaced by
suite results is used.

## 15. Variant dominance

`dominant_variant_share` (0–1) is the concentration signal: ~0.9+ means
success evidence is dominated by one hidden variant and reliability must not
appear broad. Exposed as evidence for future QualificationPolicy use; it
never fails anything in this package (tested at 0.4 and 0.9).

## 16. Platform-invalid handling

Excluded from every agent denominator; counted separately as
`platform_invalid_trials` plus `platform_invalid_rate =
invalid/(valid+invalid)` terminal trials. Always rendered next to (never
inside) the agent pass rate, e.g. agent reliability 92% with 18%
platform-invalid trials surfacing noisy infrastructure.

## 17. Task saturation

`Saturation{state, reasons, executions, distinct_agent_versions,
distinct_variants, outcome_distribution, recent_outcome_stability,
family_alternatives}`. Deterministic rules: fresh below 3 valid trials;
saturated at ≥10 valid trials with ≥2 variants, ≥2 executions, and a stable
trailing-5 outcome window; developing otherwise (reasons name the gap).
Saturation is repetition-based, never pass-rate-based: 10 stable failures
saturate (tested); 2 passes stay fresh (tested). Never retires tasks; it is
catalog-maintenance evidence plus a family-alternatives pointer.

## 18. Comparison/regression integration

`reliability.Compare` extends agent-version comparison with reliability
deltas (pass-rate, pass@k/pass^k per k, valid-sample, platform-invalid,
per-family) over the same exact compatible evidence, exposed via the
execution endpoint's optional `baseline_agent_version_id` +
`candidate_agent_version_id` pair. The existing `agentregression`
classification is untouched (existing tests green); reliability deltas are
additive evidence, and no improvement is claimed for incompatible samples.

## 19. Comparability rules

`CheckComparability` requires: same scope + exact ScenarioVersion set,
same observed evaluator contracts, same runtime contract, same reliability
contract, same environment refs, same trial distribution. Violations yield
structured `not_comparable_reasons` (`scenario_scope_mismatch`,
`family_scope_mismatch`, `evaluator_contract_mismatch`,
`runtime_contract_mismatch`, `environment_fidelity_mismatch`,
`reliability_contract_mismatch`, `trial_distribution_mismatch`). Deltas are
withheld entirely when incomparable.

## 20. QualificationPolicy integration/defer decision

DEFERRED (explicit §24 allowance). Rationale: `Definition` is column-mapped
in `qualification_policy_versions`; adding reliability rule types
(`pass^3 >= threshold`, minimum family reliability, max platform-invalid
ratio) would need a migration plus verdict-semantics review, risking the
"existing qualification policy behavior not weakened" gate. Reliability
evidence is fully persisted/reported via the new APIs for a later
versioned-rule adoption; no qualification thresholds were hardcoded in the
reliability package.

## 21. Planner relationship

Unchanged. Stage-32 planner trial counts, QualificationPlan immutability,
and suite-construction semantics are untouched; no existing SuiteVersion
trial counts were modified. Reliability policy may later inform recommended
repeat counts through a versioned integration.

## 22. Persistence/reproducibility

Recompute-on-read from immutable trial evidence; no new tables, no mutable
scores. Every payload pins `reliability-metrics-contract:v1` (+ policy
object where applicable), plus observed evaluator/runtime/environment pins,
so any historical reading reproduces exactly from the same evidence under
the same contract version.

## 23. API

Authenticated workspace-scoped GET-only endpoints (any member incl. viewer;
no score submission possible):

- `GET /v1/workspaces/{workspaceID}/agents/{agentID}/versions/{versionID}/reliability`
- `GET /v1/workspaces/{workspaceID}/suite-executions/{executionID}/reliability`
  (`+?baseline_agent_version_id=&candidate_agent_version_id=` adds
  `{report, comparison}`)
- `GET /v1/workspaces/{workspaceID}/scenarios/{scenarioID}/versions/{versionID}/reliability`
  (`+?agent_version_id=` filters to one agent)

Unknown agent/execution/scenario returns 404 (workspace isolation via
`workspace_id` predicates + ownership checks); lone comparison params and
bad UUIDs return 400. Backend README endpoint list updated.

## 24. Frontend AgentVersion UX

`app/agents/[id]/agent-detail.tsx` per-version Reliability section: overall
valid trials / pass rate / consistency (pass^3) / sufficiency /
platform-invalid; per-capability rows (valid trials, pass rate, consistency
pass^3 shown before at-least-once pass@3, family breadth covered/required,
contrast trials + rate, platform-invalid, evidence-depth state); no radar
charts, no global score.

## 25. Frontend suite/trial UX

Per-execution "trial evidence" list inside the agent reliability section:
valid vs platform-invalid trials, pass rate, pass@k, pass^k, sufficiency,
family/variant breadth per suite execution; unavailable k renders
"insufficient valid trials", never 0%.

## 26. Task saturation UX

`app/scenarios/[id]/page.tsx` gained a restrained Task Saturation section
(Fresh / Developing / Saturated with one-line explanations + the
repetition-not-difficulty note). Catalog pages carry no live variant data;
workspace task saturation (counts, breadth, stability) is served by the
scenario reliability API without hidden variant contents.

## 27. Security/leakage

APIs expose aggregates, family/capability IDs, counts, and contract pins
only. Hidden variant configs, reference remediations, ground truth,
evaluator internals, provider secrets, kubeconfigs, and agent credentials
are never selected (repository selects `variant_id`/`variant_seed` only) and
never serialized. Workspace isolation enforced at SQL + service layers and
covered by tests. No mutation surface for scores exists.

## 28. Mathematical test vectors

`backend/internal/reliability/reliability_test.go`: n=5/c=3/k=2 →
pass@2=0.9, pass^2=0.3; n=5/c=5/k=3 → 1/1; n=5/c=0/k=3 → 0/0; mixed n=4/c=2
→ pass@2=5/6 > pass^2=1/6; naive `pass_rate^k`=0.36 pinned != finite-sample
0.3. Exact rational checks use 1e-9 tolerance; no float formatting in
assertions.

## 29. Focused backend tests

New: `internal/reliability` (19 tests: contract pin, zero/single/mixed
samples, platform exclusion, agent-failure inclusion, both finite-sample
formulas, k-unavailability, all-pass/all-fail, pass@k>pass^k, determinism,
sufficiency transitions, breadth-vs-retries, diversity without leakage,
dominance, consecutive, capability families, contrast preservation,
saturation determinism + not-pass-rate-based, comparability, deltas,
platform-rate separation, no-LLM) and `internal/httpapi`
(`reliability_test.go`: auth, viewer read + contract pin, param pairing,
UUID rejection, isolation, comparison shape). Regression: evaluationsuite,
coverage, qualification, agentregression, httpapi, qualplan, evidence,
evaluator, executionworker, failuretaxonomy — all green. `gofmt` clean;
`go vet` clean on reliability/httpapi. Integration tests requiring
`TEST_DATABASE_URL` and Docker/Kind/K8s were not run (§37 exclusion).

## 30. TypeScript/build

`npx tsc --noEmit` clean; one production `npm run build` green (incl. new
saturation section and agent reliability fetch). `next lint` skipped: no
eslint config is committed and first run prompts for guided setup.

## 31. Confirmation pass@k != pass^k

Confirmed by construction (separate estimators, separate JSON fields
`pass_at_k`/`pass_to_the_k`, separate test vectors) and by UI labeling:
"Consistency (pass^k)" primary, "At-least-once (pass@k)" secondary. The
spec's example holds: pass@5=100% with pass^5=40% reads as "can usually
succeed if retried, but is not consistently reliable".

## 32. Confirmation invalid-platform excluded from agent denominator

Confirmed: validity predicate excludes platform-invalid/pending from
`ValidTrials`; `FailedTrials` counts only valid `fail` outcomes;
`PlatformInvalidTrials` + `PlatformInvalidRate` are separate fields; tests
assert a valid pass + platform-invalid pair yields 100% agent rate with 50%
platform-invalid rate.

## 33. Confirmation retries do not fake family breadth

Confirmed: family aggregation keys breadth on distinct ScenarioVersion IDs;
20 retries of one scenario yield `distinct_scenario_versions=1` with
`breadth_note=single_scenario` while still counting 20 valid trials;
capability coverage requires distinct families via coverage requirements
(two-family capability with one-family evidence stays below measured).

## 34. Confirmation hidden variants do not leak

Confirmed: SQL selects opaque IDs only; reports carry counts, ratios, and
dominant share — never configs, remediations, or ground truth. UI states
counts ("4 distinct hidden variants exercised" shape) with no values.

## 35. Confirmation no LLM calculation/judging

Confirmed: pure functions of persisted evidence; no LLM imports, no grading
changes, deterministic byte-identical output on repeat inputs
(`reflect.DeepEqual` test). Deterministic evaluator grading untouched.

## 36. Real-runtime limitations

- Reliability SQL was unit-tested (pure builders) and handler-tested (fake
  service); repository queries are untested against a live database (no
  `TEST_DATABASE_URL` in this environment) — run
  `TEST_DATABASE_URL=... go test ./internal/reliability/` (no-op today,
  integration coverage is future work) plus a manual cookie-jar pass over
  the three endpoints before relying on numbers in review.
- Environment fidelity pins use opaque run `environment_ref`s; provider/
  profile semantic comparison defers to certification pins.
- Saturation family-alternatives counts evidenced same-family siblings only.

## 37. Deferred work

- QualificationPolicy versioned reliability rules (pass^3 thresholds, family
  minimums, platform-invalid caps) — evidence ready, verdict work deferred.
- Planner repeat-count integration (Stage-32 semantics preserved as-is).
- Reliability snapshot table if recomputation ever becomes a performance
  bottleneck (currently cheap aggregations; contract pins already carried).
- Bayesian models, confidence intervals, ML scoring, LLM judging: explicitly
  out of scope, not implemented.
