# CAPABILITY & CLAIM MODEL — COMPLETION REPORT

Source task: `prompts/tasks/capability-claim-model.md` (read completely; task file left untouched).
Model: current OpenCode/Muse session, AGENTS.md read first; existing agent/AgentVersion,
capability catalog, scenario, qualification, suite, MCP, and HTTP API inspected before editing.
No broad refactor.

## 1. Files changed

Backend:
- `backend/internal/runtimecontract/version.go` — added `InfrastructureCapabilityCatalogVersion = "infrastructure-capability-catalog:v1"`.
- `backend/internal/competency/catalog.go` (new) — server-owned semantic catalog, 24 entries, Domain→Family→Capability.
- `backend/internal/competency/claims.go` (new) — `ValidateClaimIDs`, `MissingRequirements`, `ValidateCompatibility`, `MaxClaims=32`.
- `backend/internal/agent/model.go` — `Version.{ClaimedCapabilities,CapabilityCatalogVersion,ClaimStatus}`, `VersionInput.ClaimedCapabilityIDs`, new `CapabilityClaim`.
- `backend/internal/agent/validation.go` — claims validated (exists/active/no-dups) + runtime-compatibility gate; missing permissions never auto-added.
- `backend/internal/agent/repository.go` — claims persisted on `CreateVersion`, batch-loaded on all reads, `ListVersionClaims` with workspace scoping.
- `backend/internal/agent/service.go` — `ListVersionClaims` (viewer-readable, same isolation as versions).
- `backend/internal/httpapi/capability_catalog.go` (new) — `GET /v1/capability-catalog`, `GET /v1/capability-catalog/{id}`, `GET /v1/.../versions/{versionID}/capability-claims`.
- `backend/internal/httpapi/agents.go` — `versionRequest.claimed_capability_ids`, extended `AgentService` interface.
- `backend/internal/httpapi/router.go` — protected (authenticated) catalog + claims routes.
- `backend/internal/httpapi/agents_test.go` — fake extended with `ListVersionClaims`.
- `backend/internal/agentregression/model.go` + `compare.go`, `backend/internal/qualification/evaluate.go` — reproducibility pins infrastructure catalog.
- `backend/README.md` — new endpoints documented.
- `backend/migrations/000015_agent_capability_claims.{up,down}.sql` (new).

Tests (new):
- `backend/internal/competency/catalog_test.go`, `claims_test.go`
- `backend/internal/agent/validation_claims_test.go`
- `backend/internal/httpapi/capability_catalog_test.go`
- `backend/internal/qualification/capability_claims_test.go`

Frontend:
- `lib/api.ts` — `CompetencyCapability`, `CapabilityClaim(s)Response`, extended `AgentVersion`.
- `app/agents/new/agent-form.tsx` — hierarchical `Claimed capabilities` + renamed `Connection & tool access`, sends `claimed_capability_ids`.
- `app/agents/[id]/agent-detail.tsx` — `Capability claims` section with CLAIMED + Tested — / Qualified — placeholders.

## 2. Migration/schema

`000015_agent_capability_claims` (repo was at `000014`, so numbering adapted per spec):
- `agent_version_capability_claims(agent_version_id UUID FK CASCADE, capability_id TEXT, catalog_version TEXT DEFAULT 'infrastructure-capability-catalog:v1' CHECK = contract, claimed_at TIMESTAMPTZ, PK(agent_version_id, capability_id))`
- Index `(capability_id, agent_version_id)`.
- Immutability trigger `agent_version_capability_claims_immutable` (`BEFORE UPDATE OR DELETE`, raises `23514` “register a new AgentVersion”).
- Down drops trigger/function/table. No catalog table — catalog is versioned code, claims store `catalog_version` text.

## 3. Catalog architecture/version

- Package `competency`, contract `infrastructure-capability-catalog:v1` aliased from `runtimecontract.InfrastructureCapabilityCatalogVersion` (single source).
- `Capability{id,domain,family,display_name,description,catalog_version,status,requires}`; server-owned, no customer prose, no arbitrary IDs.
- `List()` stable ID order, `Get()`, `IsActive()` (only `active` claimable), `Domains()` hierarchy helper.
- Statuses `active|experimental|planned` supported; V1 ships all `active`.

## 4. Initial capability hierarchy

24 entries (small, no AWS/Terraform/GCP):
- Kubernetes — Workload Operations: `workload.diagnosis/remediation`; Capacity: `capacity.diagnosis/remediation`; Deployment: `deployment.diagnosis/recovery`; Networking: `networking.diagnosis`, `dependency.diagnosis` (8).
- Observability — Incident Diagnosis: `metrics.diagnosis`, `telemetry.missing_data_reasoning`, `recovery.verification` (3).
- GitOps — Drift: `drift.detection/remediation`; Desired State: `desired_state.diagnosis/remediation`, `argocd.diagnosis/recovery` (6).
- Delivery — Progressive Delivery: `rollout.diagnosis/recovery`, `progressive_delivery.diagnosis/recovery` (4).
- Safety — Operational Boundaries: `scope.boundary_adherence`, `protected_resource_avoidance`, `stop_when_recovered` (3).

## 5. Competency vs tool-capability separation

- Competency (`competency`, dotted IDs) = claimed accomplishment class.
- Tool/runtime (`agentcapability`, e.g. `inspect`, `scale_workload`, `request_rate`) = compatibility/tool exposure; vocabulary reused, never duplicated.
- Qualification = later derived evidence; no `qualified=true` flag stored.
- Frontend headings: `Claimed capabilities` vs `Connection & tool access`; never one undifferentiated list.

## 6. AgentVersion claim persistence/immutability

- Claims written once in `CreateVersion` transaction; `ListVersionClaims` joins `agents` for workspace ownership.
- All version reads (`Get/List`, `LatestVersion`) attach sorted claims + `capability_catalog_version` + `claim_status="claimed"` (empty → `[]`, no status).
- DB trigger + no update/delete path enforce immutability; behavior change requires new `AgentVersion`.
- Historical claims keep stored `catalog_version`; v2 cannot rewrite them.

## 7. Low-level compatibility validation

- Each competency declares `Requires` in `agentcapability` vocabulary (e.g. `capacity.remediation → [inspect, kubernetes_workloads, request_rate, scale_workload]`).
- `ValidateCompatibility` fails registration with structured message naming capability, required set, and missing set (e.g. missing `[scale_workload]`); nothing auto-added.
- Unit-tested compatible/incompatible paths.

## 8. API/RBAC

- `GET /v1/capability-catalog` (authenticated list `{contract, items}`), `GET /v1/capability-catalog/{capabilityID}` (404 if unknown), `GET /v1/workspaces/{wid}/agents/{aid}/versions/{vid}/capability-claims` (workspace-scoped, `{contract, claim_status:"claimed", items[], tested:"—", qualified:"—", qualification_note}`).
- Creation accepts `claimed_capability_ids` on both agent and version endpoints. No `PATCH` claims endpoint.
- Catalog: authenticated read. Claims create: same as version registration (`requireWrite`: owner/admin/member; viewer 403). Viewer can read catalog/claims/versions. Workspace ownership enforced server-side in SQL + service. Origin/CORS unchanged.

## 9. Reproducibility behavior

- `ReproducibilityManifest.infrastructure_capability_catalog_version` added; set in `qualification/evaluate.go` and `agentregression/compare.go` alongside existing `capability_catalog_version` / `runtime_contract_version`.
- `AgentVersion.capability_catalog_version` records per-version contract (empty when zero claims).
- Stage 22 QualificationPolicy untouched (still qualifies AgentVersion vs SuiteExecution+PolicyVersion; coverage wiring deferred).

## 10. Old-AgentVersion compatibility

- Zero-claim versions validate, persist, and load unchanged; UI shows “No capability claims declared.”; claims API returns `[]`.
- No guessing from tool permissions; no retroactive SRE labels.

## 11. /agents/new changes

- `Connection & tool access` section (endpoint/auth/MCP/runtime checkboxes) with “not qualifications” explainer.
- `Claimed capabilities` section: “What can this agent operate?”, domain→family hierarchy (Kubernetes/Observability/GitOps/Delivery/Safety), disclaimer “Claims are not qualifications. Evalplane must test them before they become qualified.”, contract code shown.
- Sends both `capabilities` and `claimed_capability_ids`. No AWS/GCP/Terraform implied. No thousand-checkbox flat list.

## 12. Agent detail changes

- Per-version `Capability claims` section: hierarchy (`Display — Domain / Family`), `CLAIMED` badge, `capability_id` code, `Tested — · Qualified —`, catalog note.
- `Connection & tool access` row keeps runtime caps; `Tested —` / `Qualified —` restrained placeholders; no green Qualified, no percentages.
- Falls back to `version.claimed_capabilities` + catalog metadata if claims fetch fails.

## 13. Claimed/tested/qualified UX behavior

- `claim_status="claimed"` only; `tested`/`qualified` always `"—"` in V1; qualification note rendered in UI and API.
- Verified no `"qualified":true` string in claims responses, no `qualified` key on `Version`.

## 14. Backend tests

Focused only (per §19; no `go test ./...`, no Docker/K8s):
- `GOCACHE=/tmp/evalplane-go-cache go test ./internal/competency/... ./internal/agent/... ./internal/httpapi/... ./internal/qualification/... ./internal/agentregression/... ./internal/runtimecontract/...` — all `ok`.
- `go test ./internal/agentcapability/...` — `ok` (no regression).
- Covers: version constant, hierarchy, unique IDs, unknown/inactive/duplicate rejected, immutability migration SQL, distinct version claims, history pinned, incompatibility structured, no `qualified=true`, empty-claims load, catalog read + auth + viewer-403, reproducibility contract, no cloud, no LLM.
- `gofmt -w` applied to all touched Go files.

## 15. Frontend verification

- `npx tsc --noEmit` — clean (fixed Map-iteration for `target:es5`).
- One `npm run build` — success; routes `/agents/new` (4.34 kB) and `/agents/[id]` (3.94 kB) built; `/agents/new` + `/agents/[id]` manually checked in code (hierarchical claims, disclaimers, placeholders).

## 16. Confirmation claims never imply qualification

- API: `claim_status` always `"claimed"`; `qualified` is `"—"` string, never boolean; `Version` has no `qualified` field.
- UI: “Claims are not qualifications…” text in form + detail + API `qualification_note`; Tested/Qualified render `"—"`.
- Tests assert no `"qualified":true` and `ClaimStatusClaimed != "qualified"/"tested"`.

## 17. Confirmation no fake AWS/Terraform/GCP capabilities

- Catalog tests scan IDs/domains/descriptions for `aws|terraform|gcp|google cloud|amazon|azure` — none.
- Architecture allows later addition without migration redesign (code list + `catalog_version` on claims).

## 18. Confirmation no LLM grading

- No LLM imports in `competency`/`agent`/claims paths; catalog text scanned for `llm grade|llm grading|llm judge|model graded` — none.
- Incident `OPENAI` authoring untouched; no grading, suites, coverage, or reference solutions added.

## 19. Deferred work

- Scenario Families, coverage engine (`Claim → Scenario Families → Suite → Policy`), automatic suite construction, reference solutions, scenario certification.
- AWS/Terraform/GCP/Argo CD runtime, new K8s scenarios, PagerDuty/Datadog, billing/Stripe, LLM grading, frontend-wide redesign.
- Future `tested|qualified|not_qualified|inconclusive|untested` per-capability states from coverage engine only.
