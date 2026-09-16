# Incident connectors (PagerDuty + Datadog, read-only)

Stage 25 recovery. Workspace-owned read-only connectors browse a historical
provider incident and import it into the existing scenario-authoring flow:

provider → adapter → NormalizedIncident → `incident-redaction:v1` →
ScenarioDraftGenerator → compiler → human review → ScenarioVersion.

The imported incident is authoring input, never an evaluator.

## Credentials

PagerDuty: `api_token` (REST API v2 `Token token=` auth).
Datadog: `api_key` + `application_key` (`DD-API-KEY` / `DD-APPLICATION-KEY`)
plus server-owned site enum (uppercase stored):

`DATADOGHQ.COM`, `US3.DATADOGHQ.COM`, `US5.DATADOGHQ.COM`, `DATADOGHQ.EU`,
`AP1.DATADOGHQ.COM`, `AP2.DATADOGHQ.COM`.

Credentials reuse Stage 23 `agentconnection.Cipher` (AES-GCM envelope in
`incident_source_connections.credential_envelope`). Read APIs never return
credentials; logs/errors use stable safe codes only. Rotation updates the
envelope and resets status to `untested` without rewriting provenance.

## Read-only behavior

Adapters issue only HTTP GET against fixed bases
(`https://api.pagerduty.com`, `https://api.<site>` for Datadog). No customer
base URLs, no OAuth, no webhooks, no continuous sync, no acknowledge/resolve/
escalate or any provider mutation. Browse is bounded (`limit` 1–50 default 20,
`offset` ≥ 0); timeouts 10s with context cancellation.

## Import + redaction gate

Both providers converge into one `NormalizedIncident` (no manufactured facts).
Every import goes through `scenarioauthoring.NormalizeIncident /
incident-redaction:v1` before any LLM call, via the single
`ScenarioDraftGenerator` pipeline (no provider-specific generators). Raw
provider bodies are never persisted; provenance records only safe IDs:
`source_type=connector`, `provider`, `external_incident_id`,
`source_connection_id`, `connector_contract_version`
(`pagerduty-incidents:v1` / `datadog-incidents:v1`), `imported_at`,
`redaction_contract=incident-redaction:v1`.

## RBAC

Workspace isolation mandatory (connection resolved through workspace ID).
Viewers may read safe connection metadata; create/rotate/test/browse/fetch/
import require member/admin/owner.
