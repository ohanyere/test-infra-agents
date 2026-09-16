# Stage 25 Recovery Report — PagerDuty + Datadog Incident Connectors

## 1. Recovery inventory — what already existed before this session
- `backend/migrations/000014_incident_sources.up.sql` / `.down.sql`: canonical
  `incident_source_connections` table (workspace-owned, provider check
  `pagerduty|datadog`, `credential_envelope BYTEA`, status enum, workspace index).
- `backend/internal/incidentsource/model.go`: providers, contracts
  (`pagerduty-incidents:v1`, `datadog-incidents:v1`), statuses, `Connection`,
  `NormalizedIncident`, `IncidentPage`, `Connector`/`Factory` interfaces.
- `repository.go`: CRUD + `SetCheck`, workspace-scoped, `contractFor`, no credential leaks.
- `errors.go`: structured `ProviderError` taxonomy (PD/DD auth/permission, invalid site,
  rate-limited, not-found, timeout, unreachable, malformed, generic).
- `service.go`: workspace RBAC, validation, encryption via Stage 23 cipher,
  bounded paging, `incidentInput` → authoring, `safePreview` — but **did not compile**:
  referenced undefined `scenarioauthoring.IncidentProvenance`.
- `scenarioauthoring` NormalizeIncident / `incident-redaction:v1` pipeline existed and clean.
- No adapters, no factory impl, no HTTP handlers/routes, no `main.go` wiring,
  frontend only a "Coming soon" placeholder.

## 2. Files preserved
- `backend/migrations/000014_incident_sources.{up,down}.sql` (untouched, canonical).
- `incidentsource/{model,repository,errors}.go` semantics (extended, not replaced).
- `scenarioauthoring/{incident,redaction}.go` sanitizer (reused as sole security boundary).
- Stage 23 `agentconnection.Cipher` (reused, no new cryptosystem).

## 3. Files changed/added
- Changed: `scenarioauthoring/model.go` (+`IncidentProvenance`, `IncidentInput.Provenance`),
  `scenarioauthoring/service.go` (safe provenance merge into `SourceMetadata`),
  `incidentsource/service.go` (`RedactionContract`, `safePreview` redacts services/teams/
  metadata/reference — sentinel-caught fix), `httpapi/router.go`, `cmd/api/main.go`,
  `app/scenarios/new/scenario-form.tsx`, `backend/README.md`.
- Added: `incidentsource/{http,pagerduty,datadog,factory}.go`,
  `incidentsource/{pagerduty,datadog,security}_test.go`,
  `httpapi/incident_sources.go`, `backend/docs/incident-connectors.md`.

## 4. Migration 000014 status
Canonical and unchanged. No second migration, no parallel schema, no `raw_json` column.

## 5. Connection model
Reused 000014: id, workspace_id, provider, display_name, provider_config (Datadog site),
`credential_envelope`, status, `last_diagnostic_code`/`last_checked_at`,
`credential_updated_at`, timestamps. Safe `Connection` JSON exposes `auth_configured`,
never envelope bytes.

## 6. Credential encryption/reuse
Stage 23 `agentconnection.Cipher` (AES-GCM) via `encryptCredentials`/decrypt in
`connector()`. Memory zeroing of raw JSON, credential structs cleared after use,
handlers clear request secrets. Rotation resets to `untested`, preserves provenance.

## 7. PagerDuty adapter
`pagerduty.go`: REST v2 (`Token token=`, `Accept: vnd.pagerduty+json;version=2`),
`TestConnection` (`GET /incidents?limit=1`), `ListIncidents` (bounded limit/offset,
in-memory query filter), `GetIncident` (`GET /incidents/{id}` + `GET .../log_entries`
timeline best-effort, ≤100 entries). GET-only, fixed base, 10s timeout + cancellation,
status mapping (401→auth, 403→permission, 429→rate-limited, timeouts, 5xx→provider_error).

## 8. Datadog adapter
`datadog.go`: v2 incidents (`DD-API-KEY`/`DD-APPLICATION-KEY`), server-owned site
allowlist (`DATADOGHQ.COM`, `US3...`, `US5...`, `DATADOGHQ.EU`, `AP1...`, `AP2...`),
`TestConnection`/`ListIncidents` (`page[size]`/`page[number]`, `sort=-created`),
`GetIncident` + `/updates` timeline best-effort (beta isolated in adapter). Same
timeouts/bounds/read-only guarantees as PagerDuty.

## 9. Paging/timeouts
Server-controlled: default 20, max 50, offset ≥ 0, handler rejects out-of-range;
`NextOffset` only when provider signals more. 10s provider timeout, HTTP client
12s cap, context cancellation propagated. No unbounded fetch-all.

## 10. NormalizedIncident mapping
Single contract for both providers: provider, external ID, title (≤500), summary
(≤8000), status, severity, started/resolved/updated, services, teams, timeline
(≤100/50, ≤2000 chars each), metadata (≤60), source reference. No manufactured facts;
absent provider fields stay empty.

## 11. Redaction-before-LLM behavior
`provider → NormalizedIncident → scenarioauthoring.NormalizeIncident →
incident-redaction:v1 → ScenarioDraftGenerator → SanitizeDraft → compiler`.
No connector-specific sanitizer; `RedactSecrets` reused for preview fields.
`ImportIncident` zeroes raw input after sanitize; generator receives only redacted form.

## 12. No-raw-payload persistence
No raw bodies stored; `NormalizedIncident` JSON has no `raw_*` keys (asserted).
`SourceMetadata` holds only safe provenance + file parse metadata. Raw uploads remain
in-memory only per `incident-security.md`.

## 13. Provenance
`IncidentProvenance{source_type=connector, provider, external_incident_id,
source_connection_id, source_incident_updated_at, imported_at,
connector_contract_version, redaction_contract=incident-redaction:v1}` merged into
draft `SourceMetadata.incident_provenance`. No credentials in provenance (asserted).

## 14. Authoring-pipeline integration
Single pipeline: connector `GetIncident` → `incidentInput` (facts sorted, timeline
joined, evidence summary) → `authoring.ImportIncident` → generator → compiler →
review → ScenarioVersion. No `PagerDutyScenarioGenerator`/`DatadogScenarioGenerator`.

## 15. API/RBAC/workspace isolation
- `POST|GET /workspaces/{id}/incident-sources`, `GET .../{connectionID}`,
  `POST .../rotate`, `POST .../test`, `GET .../incidents`,
  `GET .../incidents/{externalID}`, `POST .../incidents/{externalID}/import`.
- Every op resolves connection through workspace ID; viewer reads metadata only,
  all mutations + provider calls require member/admin/owner. Safe error codes, no
  bodies/tokens/headers leaked. Wired via `WithIncidentSourceService` in router +
  `main.go`.

## 16. Frontend import flow
`scenario-form.tsx` incident-import mode: provider select (PD/DD read-only),
connection list/create, test with status display, bounded browse (20), select,
import → review in existing `DraftEditor` before save. Truthful labels: token/API-key
only, no OAuth, no sync/webhooks, no provider writes, redaction notice.

## 17. Safe error behavior
Handler maps `ProviderError` → 401 authentication_failed, 403 permission_required,
400 invalid_configuration, 504 timeout, 404 not_found, 429 rate_limited, 502
provider_unavailable/error. Logs carry only safe metadata.

## 18. PagerDuty focused tests
`pagerduty_test.go`: success, auth/permission mapping, timeout via ctx cancel,
bounded pagination (500→50 + NextOffset), fetch + timeline, GET-only assertion. Pass.

## 19. Datadog focused tests
`datadog_test.go`: success, auth/permission + invalid-site mapping, timeout,
bounded pagination + NextOffset, fetch + updates normalization, GET-only. Pass.

## 20. Redaction sentinel test
`security_test.go::TestProviderImportSentinelRedaction`: provider text with
`Authorization: Bearer`, `api_key=`, `password=`, GitHub token, `AKIA…`,
DB-URL password, full PEM block → adapter → `NormalizeIncident` + `safePreview`.
Asserts none reach authoring payload, preview, or API response. Caught and fixed a
real `safePreview` leak (services/metadata unredacted). Pass.

## 21. No-raw-payload test
`TestNoRawPayloadPersistence`: normalized JSON has no raw keys/secrets;
provenance has IDs/contracts only, no key material. Pass.

## 22. gofmt/focused Go tests
`gofmt -w` on touched Stage-25 files. `go vet` clean on
`incidentsource/... scenarioauthoring/... httpapi/... cmd/api/...`.
`go test ./internal/incidentsource/` ok, `./internal/scenarioauthoring/` ok
(full `./...` / Docker / K8s not run per task §22).

## 23. TypeScript/build result
`npx tsc --noEmit` clean. One `npm run build` succeeded (static + `/scenarios/new`
6.96 kB). No repeated retries.

## 24. Confirmation no provider writes
Adapters contain only `http.MethodGet`; mock servers record `METHOD path` and tests
assert all calls are GET. No POST/PUT/PATCH/DELETE strings in adapter code; no
acknowledge/resolve/escalate paths.

## 25. Confirmation no OAuth/webhooks/continuous sync
Not implemented: no OAuth flows, no webhook handlers, no sync loops/cron, no generic
marketplace, no ServiceNow/Opsgenie/AWS/GCP/Terraform. Frontend + docs explicitly
state "no continuous sync, no webhooks, no OAuth".

## 26. Confirmation no LLM grading
LLM only via existing `ScenarioDraftGenerator.FromIncident` on sanitized input;
no grading, scoring, certification, or qualification-suite construction added.

## 27. Remaining limitations
- Browse/fetch/import require member+ (viewers read metadata only) — stricter than
  "viewer may browse", chosen to avoid incident-data leakage via provider calls.
- Datadog site enum fixed to 6 documented sites; new regions need code change.
- Provider query filter is substring post-filter, not native search.
- Timeline is best-effort (log/updates fetch failures degrade to incident without timeline
  except auth/permission/timeout/rate-limit which propagate).
- No live-account integration tests (mock servers only); DB-backed service import path
  covered via unit-level mapping, not end-to-end.
