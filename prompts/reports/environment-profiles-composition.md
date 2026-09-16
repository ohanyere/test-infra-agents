# Completion report — Environment Profiles & Composition

## 1. Files changed

New:

- `backend/internal/environment/contract.go` — contracts, isolation tiers/ordering, instance states/transitions, reason codes, owner types.
- `backend/internal/environment/catalog.go` — component + profile catalogs, runtime allowlist.
- `backend/internal/environment/requirements.go` — `Requirements`, validation, legacy default, family suggestions, agent-safe projection.
- `backend/internal/environment/provider.go` — provider catalog, `Provider` interface, `ProvisionRequest`, `EnvironmentInstance`.
- `backend/internal/environment/kinddev.go` — `KindDevProvider` wrapping `kubeenv.Manager`.
- `backend/internal/environment/repository.go` + `errors.go` + `service.go` — requirements/instance persistence, scheduler service.
- `backend/internal/environment/README.md` — semantics, availability truth, lifecycle.
- `backend/internal/environment/environment_test.go` — 20 focused tests.
- `backend/migrations/000018_environment_profiles.{up,down}.sql`.
- `backend/internal/httpapi/environments.go` + `environments_test.go` — 7 API tests.
- `backend/internal/scenariocertification/environment_hardening_test.go` — 6 tests.
- `app/environments/page.tsx` — Environments page.

Modified:

- `backend/internal/scenariocertification/{model,contract,service,repository}.go` — server-owned resolution, new pins, stale rules; fixed latent `created_by` placeholder off-by-one in `InsertCertification` (28 placeholders, 27 args — pre-existing, would fail on any real insert).
- `backend/internal/scenariocertification/certification_test.go` — updated `staleReasonsFor` call sites.
- `backend/internal/httpapi/router.go` — environment routes.
- `backend/cmd/api/main.go` — environment service wiring + `SetEnvironment`.
- `backend/internal/migrationtest/migrations_test.go` — version 18 + new tables.
- `backend/README.md` — endpoint list.
- `lib/api.ts` — environment types + extended `CertificationRecord`.
- `app/scenarios/[id]/page.tsx` — Execution environment section.

## 2. Migrations/schema

`000018_environment_profiles`: `scenario_version_environment_requirements` (per-version reviewed requirements), `environment_instances` (workspace+owner scoped, opaque `provider_ref TEXT`, no credential columns), and five additive nullable/defaulted pin columns on `scenario_certifications` (`provider_id`, `provider_contract_version`, `isolation_tier`, `environment_profile_contract_version`, `environment_component_contract_version`). Historical rows untouched. Down migration reverses cleanly.

## 3. Component catalog/version

`environment-component-catalog:v1`: kubernetes, prometheus, git (active); argocd, helm (experimental); argo_rollouts (planned). No AWS/Terraform/GCP active components; unknown IDs rejected.

## 4. Profile catalog/version

`environment-profile-catalog:v1` (priority-ordered): `kubernetes.namespace:v1` (active, priority 10), `kubernetes.virtual_cluster:v1` (planned, 20), `kubernetes.dedicated:v1` (planned, 30), `gitops.argocd:v1` (planned, 40). Only namespace is executable.

## 5. Isolation model

`namespace < virtual_cluster < dedicated_cluster` with documented semantics; `SatisfiesIsolation` lets stronger satisfy weaker, never the reverse. Namespace isolation is never claimed to protect cluster-scoped tasks (dedicated requirements resolve to `no_eligible_environment`).

## 6. Task environment requirements

`Requirements{required_components, minimum_isolation_tier, required_profile_capabilities, runtime_contract_version}` validated/normalized; evaluator-only by construction (no ground truth/reference/variant/grader fields). Stored per exact ScenarioVersion via `PUT .../environment`; reads fall back to the legacy default (kubernetes+prometheus/namespace).

## 7. Provider abstraction

`Provision/Get/Reset/Destroy/Capabilities/Health`. Core requests profile, workspace, run/certification ownership (+batch for namespaced runs), requirements, lease metadata — never Terraform names, AWS IDs, or Kind internals. Returns are opaque handles only.

## 8. Current Kubernetes/dev provider adaptation

`KindDevProvider` wraps the existing `kubeenv.Manager` unchanged: supports `kubernetes.namespace:v1` for run owners, `Health` reports available-but-development-only, `Destroy` delegates to idempotent namespace deletion (UID-precondition path preserved for owned deletes). Catalog marks `kind-dev:v1` active, `production=false`, `development_only=true`.

## 9. EnvironmentInstance model/lifecycle

All §13 fields present (id, workspace, owner type/id, profile+contract, provider+contract, isolation, status, lease, created/ready/destroyed, opaque `provider_ref`). States `provisioning/ready/resetting/destroying/destroyed/failed`; `ValidStateTransition` enforces forward motion, idempotent destroy, destroyed-never-ready.

## 10. Scheduler algorithm

Priorities exactly per spec: components → minimum isolation → provider active+supporting+healthy → least-fidelity adequate (priority weight, then ID). Deterministic; same input → same placement (tested).

## 11. Scheduler reason codes

`profile_selected, missing_required_component, insufficient_isolation, profile_not_active, provider_unavailable, runtime_contract_unsupported, no_eligible_environment` (+ `preference_unsatisfiable` reserved). Every candidate evaluated with its code; no free-form-only errors.

## 12. No-downgrade behavior

Dedicated requirement + namespace-only provider → `no_eligible_environment`, never namespace fallback (tested). Unavailable provider excluded, never converted to agent failure.

## 13. Certification integration/hardening

`Certify` now: loads stored requirements (legacy default without rows) → validates caller `environment_profile` as preference (unknown/inactive/unsatisfying → 400) → validates `runtime_contract_version` against the server allowlist (unknown → 400; mismatch with derived → 400) → scheduler selects → pins profile/provider/isolation/all contracts. `SetEnvironment` attaches the live service; nil-safe pure fallback keeps unit tests DB-free. Existing `*_test.go` expectations updated, not weakened.

## 14. Run lifecycle integration

Worker untouched (per spec): current Kubernetes behavior continues through the adapted provider. Execution entry points added without rewrites: `SelectForRequirements` (pure placement), `ProvisionForOwner` (records ownership-scoped instance), legacy versions resolve via the compatibility default claiming namespace only.

## 15. Legacy compatibility

No-requirements versions → `LegacyDefaultRequirements()` → `kubernetes.namespace:v1` on `kind-dev:v1`. Tested end-to-end through `SelectForRequirements` with empty input.

## 16. Reproducibility pins

New certifications pin `environment_profile`, `provider_id`, `provider_contract_version`, `isolation_tier`, `environment_profile_contract_version`, `environment_component_contract_version`, `runtime_contract_version`. Runs keep `environment_ref` plus instance linkage via `owner_id = run_id`.

## 17. API/RBAC

Authenticated reads (`/v1/environment-components|profiles|providers`, instance list/get, version environment); write roles for requirements PUT, resolve POST, transitions. Resolve accepts a ScenarioVersion reference only. Unknown JSON fields → 400. Workspace-scoped instance lookups (cross-tenant → 403/404).

## 18. Frontend environment UX

Task detail gains an Execution environment section (requirements, minimum isolation, eligible profile; planned tasks show required env + Not yet available). New `/environments` page lists profiles with provider/isolation/status and honest notes. `lib/api.ts` gains environment types; `CertificationRecord` extended with new pins.

## 19. Availability truthfulness

Only Kubernetes Namespace is shown available (development); virtual cluster/dedicated/GitOps are planned; AWS sandbox never presented as available. Kind labeled development-only in catalog, API, UI, and README.

## 20. Cleanup/idempotency

`destroyed → destroying/destroyed` valid (repeat destroy succeeds); destroyed → ready rejected twice (transition table + explicit guard). Adapter destroy delegates to idempotent `DeleteEnvironment`; owned deletes keep UID preconditions. Reset-failure and destroy-failure semantics documented in package README (failed instances route to destroy/sweeper, never reuse).

## 21. Platform-failure behavior

Scheduler refusal (`no_eligible_environment`) is a distinct platform signal, never an agent verdict (tested). Instance `failed` status is separate from run evaluation; reset failure means no reuse; destroy failure leaves a non-destroyed row for sweeper/recovery.

## 22. Focused backend tests

- `internal/environment`: 20 tests (catalogs, ordering, downgrade refusal, missing component, planned exclusion, unavailable provider, determinism, reason codes, kind-dev caps, lifecycle, legacy, explicit, allowlist, preference, credential surface, platform signal, no-cloud, owners) — pass.
- `internal/scenariocertification`: existing suite + 6 hardening tests (server selection, arbitrary profile/runtime rejection, pins, isolation/provider staleness) — pass.
- `internal/httpapi`: existing suite + 7 environment route tests — pass.
- `internal/coverage`, `internal/kubeenv`, `internal/executionworker` (GOMAXPROCS=2): pass, unchanged behavior.

## 23. TypeScript/build

`npx tsc --noEmit` clean; `npm run build` succeeds including `/environments` and all `/scenarios/[id]` paths.

## 24. Confirmation Kind is not represented as production

`kind-dev:v1`: `production=false`, `development_only=true` in catalog, capabilities, health text, API output, `/environments` page, task detail copy, and READMEs. No code path marks it production.

## 25. Confirmation caller cannot invent authoritative profile/runtime

Unknown profile → 400; planned profile → 400; unsatisfying profile → 400; unknown runtime → 400; runtime ≠ derived → 400; scheduler empty → 400 (no downgrade). `DisallowUnknownFields` still rejects `state/simulate/variant_configs/reference_remediation/gate_results/failure_codes/evaluator_version`.

## 26. Confirmation no AWS/GCP/Terraform implementation

Four future providers exist as planned catalog rows only; `DefaultAvailability` reports them unavailable; test asserts unavailability + planned status; no cloud SDKs, no provisioning code, no credentials columns.

## 27. Confirmation no raw kubeconfig/provider credential exposure

No credential columns in either new table; `ProviderRef` is an opaque namespace handle; `SafeView` returns no secrets; provider interface has no credential methods; HTTP tests confirm shape. Agents remain on mediated HTTP/MCP paths.

## 28. Confirmation no LLM grading

No LLM imports in `internal/environment` or certification changes; scheduler/matching/validation are pure deterministic functions; certification engine untouched.

## 29. Deferred work

Real shared/vcluster/dedicated/AWS providers; vCluster installation; Terraform/Crossplane; GitOps runtime + Argo CD installation; Qualification Suite Builder combining coverage+certification+environment; billing; pass@k; trajectory explorer; worker-side instance creation wiring (hooks provided); run-row environment pins beyond `environment_ref`/instance linkage.

## 30. Real-runtime limitations

Migration 18 and `InsertCertification`'s new columns are exercised only by `migrationtest` (needs `TEST_DATABASE_URL`; skipped here — no live Postgres in this environment). `KindDevProvider` provisioning paths need a real cluster (not exercised; scheduler/lifecycle covered with deterministic fakes). `migrationtest` version bumped 13 → 18 to match the new head.
