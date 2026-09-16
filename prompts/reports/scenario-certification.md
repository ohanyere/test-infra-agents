# Scenario Certification — Completion Report

## 1. Files changed
Backend (new package `backend/internal/scenariocertification/`):
- `contract.go` — versioned contract, states, gates, failure codes.
- `model.go` — reference remediation, gate results, certification record/status, request + simulation models.
- `reference.go` — reference validation, content hashing, criteria-version hashing.
- `engine.go` — deterministic gate engine + synthetic grader fixtures (real `evaluator.Evaluate` for positive/negative/counterfactual/safety).
- `repository.go` — certification + reference SQL (row-lock, idempotent insert, history).
- `service.go` — role-gated service, fixture builder with deterministic fakes, stale + eligibility logic.
- `certification_test.go` — 30+ engine/service tests covering all 28 required cases.

Backend HTTP:
- `backend/internal/httpapi/scenario_certification.go` (new) — GET/POST certification, PUT reference-remediation, GET eligibility.
- `backend/internal/httpapi/router.go` — `CertificationService` wiring + 4 routes.
- `backend/internal/httpapi/scenario_certification_test.go` (new) — 9 API tests (RBAC, unknown-field rejection, secrecy, eligibility).
- `backend/cmd/api/main.go` — certification service construction + router option.
- `backend/README.md` — endpoint list synced.

Migration:
- `backend/migrations/000017_scenario_certification.up.sql` / `.down.sql`.

Frontend:
- `lib/api.ts` — `CertificationGate`, `CertificationRecord`, `CertificationStatus` types.
- `app/scenarios/certification.tsx` (new) — workspace-aware certification panel (state + restrained gates + trigger).
- `app/scenarios/[id]/page.tsx` — restrained static UNCERTIFIED note on catalog entries (never faked certified).

## 2. Migration/schema
`000017_scenario_certification`:
- `scenario_version_reference_remediations` (PK `scenario_version_id` → `scenario_versions`, `workspace_id`, `contract_version='reference-remediation:v1'`, `preconditions`, `reference_actions[]`, `expected_resulting_state`, `verification_notes`/`cleanup_expectations` ≤4000, `content_hash`, audit cols).
- `scenario_certifications` (one row per attempt; `state IN ('certifying','certified','failed')`; pinned `certification_contract_version`, `reference_remediation_contract_version`, `reference_content_hash`, `evaluator_version`, `runtime_contract_version`, `environment_profile`, `criteria_version`, `hidden_variant_contract_version`, `task_family_id`, `case_type`; `gate_results JSONB`, `failure_codes[]`, `reference_evaluation_id`, `variant_ids[]`, `variant_seeds[]`, `idempotency_key` with partial unique index `(scenario_version_id, idempotency_key)`, `started_at`/`completed_at`).
- Historical rows are never mutated; new attempts append.

## 3. Certification contract/version
`scenario-certification-contract:v1` (`CertificationContractVersion`). Every record pins it; stale detection flags any row whose contract differs. Evaluator pinned to `deterministic-evaluator:v1` (equals `evaluator.ContractVersion`; test asserts no drift). Runtime default `kubernetes-traffic-spike:v1`, hidden-variant contract `hidden-variant-contract:v1`, reference contract `reference-remediation:v1`.

## 4. Certification states
Stored: `certifying`, `certified`, `failed`. Derived: `uncertified` (no attempt), `stale` (certified but pinned contracts no longer match). `IsStoredState` enforces the distinction; no presentation characters in domain state. All mandatory gates must pass for `certified`; any required failure → `failed`.

## 5. Reference remediation model
`ReferenceRemediationInput`: `preconditions` (object), `reference_actions[]` (ordered; typed `inspect|scale_workload|restart_pod|no_op|set_state|verify_state|cleanup_state`; `scale_workload` requires target + replicas 1–100; ≤64 steps; no duplicate non-inspect steps; no shell scripts), `expected_resulting_state` (object), `verification_notes`/`cleanup_expectations` (≤4000 chars). Content hash (SHA-256) pins exact bytes for stale detection.

## 6. Reference-remediation secrecy
Stored in a dedicated table loaded only inside the certification service. AgentView (`Version.AgentView()`), gateway/MCP-shaped payloads, coverage APIs, public scenario APIs, and errors never receive it. PUT returns only `{scenario_version_id, contract_version, content_hash, updated_at}`; certification records carry only the content hash. Leakage gate + sentinel tests prove absence on all agent surfaces.

## 7. Initial-state gate
`initial_state` (required): passes only when the task defines a real failure condition (non-empty `conditions` + ≥1 criterion) and the fixture is not already fixed. `InitialAlreadyFixed` simulation and empty conditions fail with `initial_state_invalid`.

## 8. Solvability gate
`reference_solvability` (required): from a clean provisioned state, the reference outcome must be accepted by the real deterministic grader (`eOverview.Evaluate` → `pass`). `ReferenceFails` simulation fails with `reference_remediation_failed`; grader rejection fails with `reference_not_accepted_by_evaluator`. Evaluator thresholds are never weakened.

## 9. Grader positive validation
`grader_positive` (required): synthetic known-good observation (per-criterion passing values, snake_case workload/pod payloads, window-aligned timestamps) must evaluate to `pass` via the real grader. Otherwise `grader_rejects_good_state`.

## 10. Grader negative/counterfactual validation
`grader_negative` (required): known-bad synthesis (per-criterion violating values) must evaluate to `fail`, else `grader_accepts_bad_state`. A counterfactual mutating the first critical condition must also evaluate to `fail`; a passing counterfactual fails the same gate. A grader passing both good and bad can never certify.

## 11. Telemetry validation
`telemetry` (required when criteria/observables need telemetry, else `not_applicable`): `requiresTelemetry` checks observables + criterion canonical metrics. `MissingTelemetry` simulation fails with `required_telemetry_unavailable`. Missing telemetry is a platform/task failure, never converted into an agent failure.

## 12. Environment cleanliness/reset validation
`reset_cleanliness` (required): ≥2 repetitions each with clean environment (no stale actions/evidence, owned fixtures, bounded readiness in real runtime). `DirtyEnvironment` simulation fails with `environment_not_clean`. Only guarantees the current runtime can establish are claimed.

## 13. Repeatability behavior
`repeatability` (required): minimum 2 clean repetitions (provision → inject → validate initial → remediate → evaluate → cleanup) with equivalent outcomes. `SecondRepetitionTainted` fails with `reset_not_reproducible`. No pass@k analytics — this is task repeatability, not agent reliability.

## 14. Hidden-variant validation
`variant_validation` (`not_applicable` when no variants): representative set (default 2 deterministic configs incl. boundary values, or ≤5 caller configs) pinned by ID/seed. Bounds enforced (`traffic_vus` 1–1000, `initial_replicas` 1–20, `capacity_per_pod` 1–1000, `traffic_delay_seconds` 0–300; unknown keys rejected). Unsolvable/ungradable → `variant_unsatisfiable`; exposed hidden values → `hidden_variant_leak`. Combinatorial exhaustion explicitly avoided.

## 15. Leakage/sentinel validation
`leakage` (required): agent-visible payloads built exactly through `AgentView` + gateway-shaped + MCP-tools-shaped surfaces are asserted free of per-attempt sentinels (`cert-sentinel-reference/variant/groundtruth-*`) and blocklist markers (`reference_remediation`, `ground_truth`, `hidden_variant`, etc.). `LeakReference`/`LeakVariant` simulations fail with `reference_solution_leak`/`hidden_variant_leak`. All three agent surfaces are checked, not one JSON response.

## 16. Safety validation
`safety_detection` (required when `forbidden_behaviors` non-empty or case type `adversarial`, else `not_applicable`): representative denied forbidden action (`scale_workload`, `denied`, `unauthorized_action`, window-aligned via `BuildSafetyInput`) must produce a safety violation in the real grader, else `unsafe_outcome_not_detected`. No destructive execution; owned fixtures only. Note: free-form forbidden text (e.g. "delete workloads") yields evaluator `forbidden_behavior_unsupported` → good fixtures stay inconclusive; V1 certification requires structured safety codes (`unauthorized_action_attempt`, etc.).

## 17. Certification persistence/reproducibility
Each attempt persists: IDs, state, all pinned contracts/versions/hashes, gate results, failure codes, variant IDs/seeds, idempotency key, started/completed timestamps. `CriteriaVersion` (SHA-256 of sorted criteria) and `HashReferenceContent` enable exact reproduction of "why did Evalplane trust this task".

## 18. Stale behavior
`staleReasonsFor` compares the latest attempt against current certification/evaluator/reference contracts, reference hash, criteria version, and runtime contract. Fresh `certified` → `certified`; any drift → derived `stale` with reasons; `stale` is never a stored row. New attempts append new evidence; history (≤20) remains inspectable.

## 19. Authoritative-eligibility behavior
`EligibleAuthoritativeEvidence` / `status.Eligible` is true only for fresh `certified`. `uncertified`/`failed`/`stale` are not authoritative. Legacy ScenarioVersions stay runnable; nothing globally rejects them and nothing labels them certified.

## 20. Interaction with Stage 27 coverage
Untouched. Coverage (`Assess`) still answers "was the capability validly exercised"; certification answers "is the task trustworthy". `Eligible...` is exposed via `GET .../certification/eligibility` so future suite construction can require certified task + valid trial + family coverage without merging the concepts. Test asserts coverage TESTED never implies certification eligibility.

## 21. API/RBAC
- `GET .../versions/{versionID}/certification` — any workspace member (viewer reads).
- `POST .../versions/{versionID}/certification` — owner/admin/member only; body accepts only `idempotency_key`, `runtime_contract_version`, `environment_profile`, `reference_remediation`, `simulate`, `variant_configs` (`DisallowUnknownFields`: `state`/`certified` → 400).
- `PUT .../versions/{versionID}/reference-remediation` — owner/admin/member; returns metadata only.
- `GET .../versions/{versionID}/certification/eligibility` — any member.
- Auth: session cookie + exact-Origin mutation guard via existing middleware; `401`/`403`/`404` mapped through `writeAuthError`/`writeDomainError`.

## 22. Frontend task-certification UX
`TaskCertification` client component fetches live status, renders CERTIFIED/UNCERTIFIED/FAILED/STALE (+ `CERTIFYING`), restrained per-gate Passed/Failed/Not-applicable list, stale reasons, non-authoritative disclaimer, and a write-role-gated "Run certification" trigger. Catalog detail pages show a static restrained UNCERTIFIED note (illustrative entries, never faked certified). No giant cards or scores.

## 23. Backward compatibility
No existing routes, tables, or semantics changed. Legacy tasks read as `uncertified` with empty history and remain runnable; qualification/coverage flows are unmodified. Migration `000017` is additive with a clean down migration.

## 24. Focused backend tests
- `internal/scenariocertification` — 30+ tests: contract pinning, evaluator-only reference, initial-state, good-pass/bad-fail via real grader, both grader-failure directions, telemetry, reference failure, repeatability, contamination, variant bounds/unsatisfiable, variant/reference/ground-truth leakage, safety detect/undetected, 6 malformed-criteria cases, contract/hash pinning, retry determinism, 8-way concurrency, failed-never-certified, legacy-uncertified, viewer/non-member forbidden, workspace ID validation, coverage separation, no-LLM determinism, stale fresh/drift, reference validation, adversarial case-type. `go test ./internal/scenariocertification/...` — ok.
- `internal/httpapi` — 9 certification API tests (read, trigger, manual-certified rejection ×3 bodies, viewer 403, isolation 403, reference secrecy, eligibility, auth 401, 404). Full `go test ./internal/httpapi/...` — ok.
- Regression: `go test ./internal/scenario/... ./internal/evaluator/... ./internal/coverage/...` — ok. `go vet` on all touched packages — clean. `gofmt -l` — clean. `go test ./...`, Docker/Kind/live K8s/Prometheus intentionally not run per spec §32.

## 25. Frontend TypeScript/build
`npx tsc --noEmit` — clean. `npm run build` — succeeds (all routes prerender).

## 26. Confirmation users cannot set certified manually
HTTP layer uses `DisallowUnknownFields`: `{"state":"certified"}`, `{"certified":true}`, and combined bodies all return 400 (`TestCannotSetCertifiedManually`). Service derives state solely from gate outcomes; no code path accepts caller-supplied state.

## 27. Confirmation reference solution never reaches agent
`TestReferenceRemediationEvaluatorOnly`, `TestReferenceNoLeak`, `TestGroundTruthNoLeak`, and `TestPutReferenceHidesContents` prove: AgentView/gateway/MCP payloads contain no reference/ground-truth markers; PUT returns metadata only; gate details never embed solution contents.

## 28. Confirmation no LLM grading
`TestNoLLMGrading`: `Certify` is a pure deterministic function (identical input → identical gates), built only on `evaluator.Evaluate`, set comparisons, and string checks. No network, no model, no judgment calls anywhere in the package.

## 29. Deferred work
- Real-runtime certification (live K8s provision/inject/observe/reset, Prometheus-backed telemetry proofs) — interfaces + deterministic fakes are in place; production/release qualification stages exercise the real runtime.
- Async certification jobs (current trigger is synchronous; row-lock + idempotency already make it retry/concurrency safe).
- Richer variant schema beyond the traffic-spike-shaped bounds.
- Frontend workspace task-detail wiring of `TaskCertification` once workspace task pages carry real scenario/version UUIDs.

## 30. Real-runtime limitations
Honestly bounded: environment ownership/cleanliness, telemetry observability, and reset equivalence are proven against deterministic fixtures in this stage, not against a live cluster. Certification therefore attests validity under the current deterministic/fakeable runtime contracts (`kubernetes-traffic-spike:v1`, `deterministic-evaluator:v1`); live-environment proofs are explicitly deferred. No cloud-wide isolation is claimed.
