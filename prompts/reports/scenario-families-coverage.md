# Completion Report — Scenario Families & Capability Coverage

Task: `prompts/tasks/scenario-families-coverage.md` (implemented exactly; this report does not overwrite the task file).

## 1. Files changed

Backend (new):
- `backend/internal/coverage/contract.go` — contract versions + test states + missing-requirement codes.
- `backend/internal/coverage/families.go` — V1 task-family catalog (16 stable dotted IDs).
- `backend/internal/coverage/casetype.go` — case-type model + validation.
- `backend/internal/coverage/requirements.go` — server-owned V1 coverage requirement catalog (8 capabilities).
- `backend/internal/coverage/assess.go` — deterministic assessment engine (`Assess`/`AssessMany`).
- `backend/internal/coverage/repository.go` — assignments, trial join, persisted assessments.
- `backend/internal/coverage/service.go` — assignment + compute/persist/list workflows with RBAC.
- `backend/internal/coverage/assess_test.go` — 14 engine tests.
- `backend/internal/httpapi/coverage.go` — coverage handlers.
- `backend/internal/httpapi/coverage_test.go` — 8 handler tests.
- `backend/migrations/000016_scenario_families_coverage.up.sql` / `.down.sql`.

Backend (edited):
- `backend/internal/agent/model.go` — `CapabilityClaim`: `Tested`/`Qualified` em-dash strings replaced by machine-readable `test_status` + `qualification_status` (null in V1).
- `backend/internal/agent/repository.go` — claims return `test_status="untested"`, `qualification_status=null`.
- `backend/internal/scenario/model.go` — `Version` exposes `task_family_id`, `case_type`, `task_family_contract_version` (empty when unassigned).
- `backend/internal/scenario/repository.go` — selects LEFT JOIN `scenario_version_task_family`.
- `backend/internal/httpapi/capability_catalog.go` — claims response drops em-dash fields; per-claim `test_status` enriched from latest persisted coverage.
- `backend/internal/httpapi/router.go` — `WithCoverageService` + 7 new routes.
- `backend/internal/httpapi/capability_catalog_test.go` — asserts no em-dash, valid `test_status`, null qualification.
- `backend/cmd/api/main.go` — wires `coverage.Service` (claim adapter + execution-membership adapter).
- `backend/README.md` — endpoint list synced.

Frontend (edited):
- `lib/api.ts` — machine-readable claim/coverage types.
- `app/agents/[id]/agent-detail.tsx` — real CLAIMED + UNTESTED/PARTIAL/TESTED per claim, Qualified always "—", restrained per-family coverage detail with missing requirements.

## 2. Migration/schema

`000016_scenario_families_coverage`:
- `task_families` (PK `family_id`, seeded with all 16 V1 families, contract CHECK).
- `scenario_version_task_family` (PK `scenario_version_id` → `scenario_versions`, FK to `task_families`, case-type CHECK, contract CHECK) + `scenario_version_task_family_evidence_freeze` trigger: reassignment after evidence in `evaluation_batches`/`suite_execution_batches` raises `23514`.
- `capability_coverage_assessments` (exact scope: workspace/agent/agent_version/capability/suite_execution + 3 contract versions; unique scope constraint for idempotent race-safe creation; `families`/`missing_requirements` JSONB; `scenario_version_ids`/`run_ids` arrays).

## 3. Task Family model

Independent infrastructure-situation classes with stable dotted IDs; backend keeps saying Scenario where the domain already does. Customer terms Evaluation Task / Task Family.

## 4. Task Family contract/version

`task-family-contract:v1`, pinned on every family row, assignment, and assessment. Catalog code + migration seed agree.

## 5. Case-type model

`positive | negative | decoy | adversarial | edge`, explicit per ScenarioVersion, validated server-side and DB CHECK. Never inferred from names, no LLM. Contrast helper (`negative/decoy/adversarial`) for requirement groups.

## 6. Capability coverage contract

`capability-coverage-contract:v1`, pinned on requirements and assessments. Existing `infrastructure-capability-catalog:v1` untouched.

## 7. Initial capability→family requirements

8 conservative V1 requirements (2 distinct families × 2 valid trials each, plus case-type rules; remediation requires a contrast group). All other 16 capabilities have no V1 requirement → `partial` + `coverage_requirement_unavailable`, never TESTED.

## 8. Existing ScenarioVersion mappings

No scenarios auto-mapped. Only explicitly assigned versions count; unassigned rows are excluded in the trial join. `traffic_spike` runtimes are genuinely assignable to `kubernetes.capacity.traffic_spike` (documented, operator-assigned); node-failure-style futures cannot count (unknown families rejected, unassigned ignored).

## 9. Valid-trial rules

Only `valid` trials count. `invalid_platform` (platform classification, runtime/telemetry failures on terminal runs) and `pending` (incomplete) never count. Agent `fail`/`inconclusive`/`stopped` with a valid evaluation count — coverage asks "did we validly test?", not "did it pass?".

## 10. Family breadth vs trial depth

Breadth = distinct required families each meeting per-family trial minimums. Ten trials of one family yields `covered_family_count=1` → PARTIAL. Repetition establishes reliability only.

## 11. Hidden-variant treatment

`distinct_variant_count` (opaque `variant_id` only) reported per assessment and per family; variants never create families. Hidden configs never selected or exposed.

## 12. Persisted/reproducible coverage assessment

Persisted rows pin AgentVersion, capability, catalog + family + coverage contracts, SuiteExecution, scenario/run ID sets, counts, families, and missing codes. Unique scope constraint + `ON CONFLICT DO NOTHING` + read-back = idempotent, retry-safe, race-safe. Reads never fabricate TESTED.

## 13. UNTESTED/PARTIAL/TESTED semantics

`untested` = no valid evidence; `partial` = some evidence or no published requirement, gaps listed; `tested` = all family/trial/case requirements met by valid evidence. No `qualified`/`not_qualified` anywhere in the engine (JSON-asserted in tests).

## 14. Claims API cleanup

No em dash in domain state (asserted). Unassessed claims: `test_status="untested"`; `qualification_status=null` always in V1. Top-level `tested`/`qualified` placeholders removed.

## 15. API/RBAC

- `GET /v1/task-family-catalog`, `GET /v1/capability-coverage-requirements` (authenticated reads).
- `POST|GET .../scenarios/{id}/versions/{vid}/task-family` (write = owner/admin/member; read = any member incl. viewer; unknown family/case rejected; post-evidence change → conflict).
- `GET .../agents/{aid}/versions/{vid}/capability-coverage` (read; latest persisted per claim, each with its own execution scope — no silent run merging).
- `GET .../suite-executions/{eid}/agents/{avid}/capability-coverage` (read; live deterministic compute, never persists).
- `POST .../suite-executions/{eid}/agents/{avid}/capability-coverage/assessments` (write; idempotent persist).
- No endpoint sets `tested` directly (PUT-tested probe test). Hidden variant values never exposed.

## 16. Agent detail coverage UX

Per claim: CLAIMED + UNTESTED/PARTIAL/TESTED from live coverage; Qualified always "—" (display-only, allowed). Copy states TESTED = contract satisfied by valid evidence, not success/qualification.

## 17. Missing-requirement UX

Restrained text: `covered/required` families, valid-trial count, per-required-family ✓/— lines with trials + exercised case types, `Missing: <family or code>`. No cards, charts, stars, or percentages.

## 18. Backend focused tests

- `internal/coverage`: 14 tests (IDs unique, case validation, contracts, untested/partial/tested, failure-counts, invalid-platform exclusion, platform-only→untested, breadth, variant depth, contrast enforcement, unavailable-requirement, no-qualification-in-output).
- `internal/httpapi`: full package green, incl. new catalog/requirements/version/execution coverage reads, viewer read vs write, no-manual-tested-endpoint, assignment validation, updated claims test.
- `internal/competency`: green. `go vet` clean on coverage/httpapi/agent/scenario/competency. `gofmt` clean on all touched files (only pre-existing `incidentsource` files remain unformatted; untouched).

## 19. Frontend TypeScript/build

`npx tsc --noEmit` clean. One `npm run build` succeeded.

## 20. Confirmation TESTED never implies QUALIFIED

Engine has no qualification field (JSON-asserted); claims return `qualification_status=null`; frontend hardcodes Qualified "—". Not derived.

## 21. Confirmation platform-invalid evidence never satisfies coverage

Engine drops `invalid_platform` trials; platform-only scope → `untested` + `platform_invalid_only`; mixed scope → at best `partial`. Tested in engine unit tests.

## 22. Confirmation no LLM grading

Assessment is a pure function of explicit assignments + trial validity. No LLM imports, calls, or classification paths in `internal/coverage`.

## 23. Deferred work

Per task §26, not implemented: automatic Qualification Suite construction, reference remediation/solutions, Scenario certification, new scenario corpus / K8s runtime scenarios, real Argo CD, AWS/Terraform/GCP/PagerDuty/Datadog, pass@k analytics, trajectory explorer, billing/Stripe, CI release gates, LLM grading, frontend-wide redesign.
