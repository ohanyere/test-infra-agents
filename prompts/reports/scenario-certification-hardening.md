# Scenario Certification Hardening — Completion Report

Small security/integrity patch. No redesign, no new migration, no new
architecture, no environment profiles, no K8s work, no corpus, no billing,
no LLM grading.

## 1. Files changed

Backend:
- `backend/internal/scenariocertification/model.go` — `CertifyRequest`
  shrunk to the minimal public trigger; `Simulation` kept and explicitly
  marked internal-only (never part of the HTTP DTO).
- `backend/internal/scenariocertification/service.go` — removed inline
  reference upsert, caller variant-config limit, and `Simulate` handling;
  `buildFixture` is now server-owned; simulation moved to internal
  `buildFixtureWithSimulation`; added `serverSelectedVariants` reusing the
  existing deterministic `buildVariants`; fixed a pre-existing inverted
  known-bad flag (see §7).
- `backend/internal/httpapi/scenario_certification.go` — public
  `certifyHTTPRequest` shrunk to 3 fields; mapping forwards only allowed
  fields (`DisallowUnknownFields` rejects everything else with 400).
- `backend/README.md` — endpoint list updated to the hardened trigger shape.
- `backend/internal/scenariocertification/certification_test.go` — 3 new
  server-owned behavior tests.
- `backend/internal/httpapi/scenario_certification_test.go` — 4 new
  rejection/shape tests; existing manual-certified test extended.

Frontend: untouched (trigger sends `{}`, still valid; response types unchanged).
Migration: none.

## 2. Public certification request shape

`POST /v1/workspaces/{ws}/scenarios/{s}/versions/{v}/certification`
accepts only:

- `idempotency_key` (optional, ≤200 chars, retry-safe)
- `environment_profile` (optional, defaults `default`, ≤200 chars)
- `runtime_contract_version` (optional, defaults
  `kubernetes-traffic-spike:v1`, ≤200 chars — the one extra field genuinely
  required by the existing pinned-contract architecture)

Rejected with 400 via `DisallowUnknownFields`: `state`, `certified`,
`simulate`, `variant_configs`, `reference_remediation`, `gate_results`,
`failure_codes`, `evaluator_version`.

## 3. Simulation isolation

`Simulation` stays in `model.go` as an internal-only failure-injection model
for deterministic fakes. It is referenced only by `buildVariants` (variant
leak/bounds/unsatisfiable flags) and the internal
`buildFixtureWithSimulation` helper. The production path
(`Service.buildFixture`) always uses zero-value simulation: clean provisioned
state, no injected failures. Engine tests construct `Fixture` directly
(good/bad/dirty/leakage/unsatisfiable/grader paths) with no HTTP exposure.

## 4. Server-owned variant selection

`serverSelectedVariants(versionID)` deterministically returns the 2
representative configs (including boundary values) with pinned IDs
(`cert-variant-<shorthash>-<i>`) and seeds (1000, 1001) via the existing
`buildVariants` implementation. Production `Certify` always uses it; the
persisted record pins `variant_ids`/`variant_seeds`. Caller configs never
reach the engine. Internal helpers (`buildVariants` with explicit configs)
remain for focused unit tests.

## 5. Stored reference-remediation behavior

`Service.Certify` no longer upserts inline references. It loads the
evaluator-only reference via `repo.GetReference` for the exact
`ScenarioVersion` (authored through `PUT .../reference-remediation`,
write roles only) and pins `reference_remediation_contract_version`
(`reference-remediation:v1`) plus `reference_content_hash`
(`HashReferenceContent`). Missing stored reference returns validation
"reference remediation is required before certification…". Responses carry
only the content hash, never contents (unchanged `PUT` metadata-only
behavior, leakage gate unchanged).

## 6. Rejection tests

`backend/internal/httpapi/scenario_certification_test.go`:
- `TestSimulateRejectedByHTTP` — `{"simulate":…}` ×3 → 400.
- `TestVariantConfigsRejectedByHTTP` — `{"variant_configs":…}` ×3 → 400.
- `TestInlineReferenceRemediationRejectedByHTTP` — inline object and
  explicit-null field → 400.
- `TestCannotSetCertifiedManually` (extended) — `state`, `certified`,
  plus `gate_results`, `failure_codes`, `evaluator_version` → 400.
- `TestMinimalTriggerShapeAccepted` — full minimal shape → 201 with correct
  server-side mapping.
- Pre-existing: viewer 403, isolation 403, reference secrecy, eligibility,
  auth 401, 404 — all still pass.

## 7. Engine regression tests

`go test ./internal/scenariocertification/...` — ok (all 30+ pre-existing
engine/service tests plus 3 new ones):
- `TestServerSelectedVariantsPinned` — deterministic IDs/seeds, bounds-valid,
  server configs independent of caller configs.
- `TestStoredReferenceLoadedServerSide` — fixture carries stored
  preconditions/actions/expected state; hash pins content and changes with it.
- `TestProductionFixtureIgnoresCallerControls` — production fixture equals
  zero-simulation server-selected build, clean envs, certifies happy path.

Pre-existing bug fixed along the way: `buildFixtureWithSimulation`
constructed the known-bad input with `!badPassing`, i.e. *passing* values on
the happy path, so any service-built certification would always fail
`grader_negative` (`grader_accepts_bad_state`). Fixed to `badPassing`
(happy → failing values → grader correctly rejects). Engine `happyFixture`
tests were unaffected (they build inputs directly); the new production-path
test pins the correct behavior.

`go test ./internal/httpapi/...` — ok (full package).
`go vet` on both packages — clean. `gofmt -l` — clean.
`go test ./...`, Docker/Kind/Kubernetes — not run per spec §7.

## 8. TypeScript result

Not affected: no API response type changed, no request type is modeled in
`lib/api.ts`, and `app/scenarios/certification.tsx` posts `{}` (still valid).
`npx tsc --noEmit` not run per spec §7 (frontend only if API types affected).

## 9. Confirmation caller cannot influence authoritative certification gates

- Failure-injection model: `simulate` is not in the HTTP DTO (400) and never
  reaches `Service.Certify`; production fixture is zero-simulation.
- Variant set: `variant_configs` is not in the HTTP DTO (400); variants come
  only from `serverSelectedVariants` and are pinned per attempt.
- Reference: inline `reference_remediation` is not in the HTTP DTO (400);
  certification loads the stored evaluator-only reference server-side and
  pins its content hash; authoring stays on the dedicated write-gated PUT.
- State/gates: `state`/`certified`/`gate_results`/`failure_codes`/
  `evaluator_version` are not in the HTTP DTO (400); state derives solely
  from deterministic gate outcomes under
  `scenario-certification-contract:v1` via the real deterministic grader.
- RBAC/isolation unchanged: trigger owner/admin/member, reads any member,
  reference evaluator-only, no contents in responses, no LLM anywhere.
