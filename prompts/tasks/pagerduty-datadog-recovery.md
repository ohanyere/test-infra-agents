EVALPLANE — STAGE 25 RECOVERY:
PAGERDUTY + DATADOG INCIDENT CONNECTORS

This is a RECOVERY AND COMPLETION task.

A previous coding session started this stage and was interrupted by a usage
limit. Do NOT restart the feature from scratch.

Read AGENTS.md first.

Then inspect the existing repository thoroughly enough to determine exactly
what Stage 25 already contains.

Known existing work includes:

- backend/migrations/000014_incident_sources.up.sql
- backend/migrations/000014_incident_sources.down.sql
- backend/internal/incidentsource/model.go
- backend/internal/incidentsource/repository.go
- backend/internal/incidentsource/service.go
- backend/internal/incidentsource/errors.go
- existing scenarioauthoring NormalizeIncident / redaction pipeline
- Stage 23 credential-encryption infrastructure

Preserve correct existing work.

Do NOT create a second incident-source migration.
Do NOT create a competing connector architecture.
Do NOT duplicate existing credential encryption.
Do NOT bypass scenarioauthoring normalization/redaction.

==================================================
PRODUCT GOAL
==================================================

Allow a workspace to connect PagerDuty or Datadog READ-ONLY, browse/select a
historical incident, and import it into the existing incident-authoring flow:

PagerDuty / Datadog
        ↓
provider adapter
        ↓
NormalizedIncident
        ↓
incident-redaction:v1
        ↓
ScenarioDraftGenerator
        ↓
deterministic compiler
        ↓
human review
        ↓
ScenarioVersion

The imported incident is authoring input.

It is NOT itself an evaluator.

==================================================
FIRST ACTION: RECOVERY INVENTORY
==================================================

Before modifying code:

1. Inspect migration 000014 completely.
2. Inspect the entire incidentsource package.
3. Search for:
   - PagerDuty clients/adapters
   - Datadog clients/adapters
   - HTTP routes
   - handlers
   - encryption use
   - incident import orchestration
   - scenarioauthoring integration
   - frontend integration
   - tests/fixtures
4. Determine what is complete, incomplete, inconsistent, or missing.
5. Continue the existing architecture unless it is objectively unsafe or
   incompatible with repository conventions.

Do not throw away working interrupted code simply because it is unfinished.

==================================================
1. PROVIDERS
==================================================

Support exactly:

pagerduty
datadog

No generic connector marketplace.

No additional providers.

==================================================
2. CONNECTION MODEL
==================================================

Reuse the existing 000014 schema if correct.

Incident-source connections must be workspace-owned.

Required concepts approximately:

id
workspace_id
provider
display_name
encrypted credentials / encrypted credential envelope
provider-specific configuration
connection status
last_tested_at
last_error_code
created_at
updated_at

Do not persist plaintext provider credentials.

If 000014 differs, adapt to its existing safe design rather than creating a
parallel schema.

==================================================
3. CREDENTIAL ENCRYPTION
==================================================

Reuse the existing Stage 23 encryption mechanism.

Do NOT implement another cryptosystem.

Credentials:

PagerDuty:
- api_token

Datadog:
- api_key
- application_key
- server-owned site enum

Credentials must never be returned by read APIs.

Credentials must never appear in logs, evidence, errors, or scenario drafts.

Credential rotation may update connection credentials without rewriting
historical imported incident provenance.

==================================================
4. PAGERDUTY ADAPTER
==================================================

Implement/finish a small explicit read-only PagerDuty adapter.

Use PagerDuty REST API v2 semantics.

Required operations:

- test connection
- list/browse incidents
- fetch one incident
- fetch incident log entries/timeline where available

Authentication:
Token-based authorization appropriate to PagerDuty API v2.

Requirements:

- bounded paging
- explicit timeouts
- context cancellation
- safe status-code mapping
- no arbitrary URL supplied by customer
- no write operations
- no acknowledgement
- no resolve
- no escalation
- no mutation of PagerDuty state

Map provider failures into existing structured incident-source error taxonomy.

==================================================
5. DATADOG ADAPTER
==================================================

Implement/finish a small explicit read-only Datadog incident adapter.

Use Datadog v2 incident APIs consistent with the repository's existing
provider contract.

Required operations:

- test connection
- list/browse incidents
- fetch one incident
- fetch enough incident/timeline/context data for authoring where supported

Authentication:

DD-API-KEY
DD-APPLICATION-KEY

Do not allow arbitrary base URLs.

Use a server-owned Datadog site enum / allowlist.

Examples may include supported Datadog sites, but use the current documented
set already represented by existing code if present.

Requirements:

- bounded paging
- explicit timeouts
- context cancellation
- safe error mapping
- read-only
- beta/unstable API details isolated inside the Datadog adapter

==================================================
6. NORMALIZED INCIDENT CONTRACT
==================================================

Both providers must converge into one provider-neutral NormalizedIncident.

Do not send provider-specific raw payloads directly into the LLM.

Normalized data may include approximately:

provider
external incident ID
title
summary/description
status
severity/priority if meaningful
started/resolved timestamps
timeline entries
service/resource references
safe tags/metadata
links if safe and required for provenance

Preserve source semantics.

Do not manufacture missing provider facts.

==================================================
7. REDACTION SECURITY GATE
==================================================

The existing incident redaction work is already a security boundary.

Every provider import must go through:

NormalizedIncident
    ↓
scenarioauthoring sanitizer / NormalizeIncident
    ↓
incident-redaction:v1
    ↓
only then any LLM call

No exception.

Do not build a connector-specific alternate sanitizer.

Ensure secrets can be scrubbed from:

- incident title/description
- timeline/log entries
- tags
- URLs
- structured metadata
- provider-returned text

Provider-returned credentials/secrets must never reach the LLM.

==================================================
8. RAW PROVIDER PAYLOADS
==================================================

Do NOT persist raw PagerDuty or Datadog payload bodies.

Persist only the normalized/sanitized information required by the existing
authoring pipeline and provenance model.

Do not add a generic "raw_json" column for convenience.

Tests should make this explicit.

==================================================
9. PROVENANCE
==================================================

Imported authoring data must retain safe provenance approximately:

source_type = pagerduty | datadog
source_connection_id
external_incident_id
provider_contract_version
imported_at
redaction_contract = incident-redaction:v1

If the existing ScenarioDraft/ScenarioVersion provenance model already has a
canonical shape, extend/reuse it rather than creating a parallel provenance
system.

Do not store credentials in provenance.

==================================================
10. AUTHORING PIPELINE INTEGRATION
==================================================

Provider import must feed the SAME authoring pipeline as manual/imported
incidents:

provider incident
→ NormalizedIncident
→ sanitizer/redaction
→ ScenarioDraftGenerator
→ compiler
→ review
→ ScenarioVersion

Do not create:

PagerDutyScenarioGenerator
DatadogScenarioGenerator

There must be one authoring pipeline.

LLM may help translate sanitized incident evidence into a draft.

LLM must not grade agents.

==================================================
11. CONNECTION TESTING
==================================================

Connection test must make a real read-only provider request.

Return safe structured states approximately:

untested
healthy
authentication_failed
permission_required
timeout
provider_unavailable
invalid_configuration

Reuse existing states/errors where already modeled.

Do not return provider response bodies to the client.

==================================================
12. API
==================================================

Inspect existing handlers/routes first.

Complete only missing endpoints.

Expected product operations approximately:

- create incident source connection
- list connections
- get safe connection details
- rotate/update credentials if current architecture allows
- test connection
- browse incidents
- fetch/select an incident
- import selected incident into ScenarioDraft authoring

Follow existing workspace/RBAC route conventions.

Do NOT expose raw credentials.

==================================================
13. RBAC / TENANCY
==================================================

Workspace isolation is mandatory.

Viewer:
- may read safe connection metadata if consistent with existing product RBAC
- may browse only if existing read policy permits it
- cannot create/update credentials

Member/admin/owner:
- follow existing write permissions

Every provider operation must resolve the connection through the current
workspace.

Never accept a connection ID without verifying workspace ownership.

==================================================
14. FRONTEND
==================================================

Keep frontend scope minimal.

Integrate into the existing scenario/incident-authoring flow.

User should be able to choose approximately:

Manual incident
Upload/import
PagerDuty
Datadog

For PagerDuty/Datadog:

- select or create connection
- test connection
- browse incidents
- choose incident
- import
- review generated draft before save

Do not redesign the whole frontend.

Do not claim continuous sync.

Do not claim webhooks.

Do not claim OAuth if only token/API-key auth exists.

Clearly label provider integrations truthfully.

==================================================
15. PAGINATION / LIMITS
==================================================

Provider browsing must be bounded.

No unbounded "fetch all incidents".

Use reasonable server-controlled limits.

Prevent user-controlled pathological provider requests.

Respect provider paging mechanisms.

==================================================
16. ERROR HYGIENE
==================================================

Never expose:

- provider response bodies containing sensitive information
- tokens
- API keys
- application keys
- authorization headers

Public/API errors should use stable safe codes.

Logs should contain only safe operational metadata.

==================================================
17. TEST FIXTURES
==================================================

Use deterministic provider fixtures/mock HTTP servers.

Do not require live PagerDuty or Datadog accounts for focused tests.

PagerDuty tests should cover at minimum:

- successful connection test
- auth failure
- permission failure
- timeout
- list incidents
- bounded pagination
- incident fetch
- log/timeline normalization
- no writes

Datadog tests should cover at minimum:

- successful connection test
- auth failure
- permission failure
- invalid site
- timeout
- list incidents
- bounded pagination
- incident fetch
- normalization
- no writes

==================================================
18. SECURITY SENTINEL TEST
==================================================

Add a provider-import sentinel test.

Construct a provider response containing representative secrets:

Bearer token
API key
password
GitHub token
AWS-looking credential
database URL password
private-key marker

Run it through:

provider adapter
→ NormalizedIncident
→ authoring/redaction boundary

Assert no sentinel secret reaches:

- ScenarioDraft
- LLM request payload
- persisted safe authoring representation
- API response
- logs/errors where testable

Reuse existing redaction tests/helpers where possible.

==================================================
19. NO RAW PAYLOAD PERSISTENCE TEST
==================================================

Assert provider raw response JSON/body is not stored.

Do not merely rely on code review.

==================================================
20. EXISTING INTERRUPTED CODE QUALITY
==================================================

The existing incidentsource package may contain files left unformatted because
the previous session stopped mid-edit.

Run gofmt on touched Stage-25 Go files.

Do not rewrite unrelated packages merely to make formatting globally clean.

Fix incomplete compile errors, dead paths, placeholder methods, or inconsistent
interfaces left by the interruption.

==================================================
21. DOCUMENTATION
==================================================

Document:

PagerDuty credentials required
Datadog credentials required
supported Datadog sites
read-only behavior
how incident import works
redaction/security boundary
no continuous synchronization
no provider write operations

Do not document unfinished features as available.

==================================================
22. VERIFICATION
==================================================

Run focused packages only.

Likely:

internal/incidentsource/...
internal/scenarioauthoring/...
relevant httpapi packages
relevant credential/encryption package if touched

Use gofmt.

Do not run:

go test ./...
Docker
Kind
Kubernetes
Prometheus

unless absolutely required by an existing focused test.

Frontend:

npx tsc --noEmit

One production build if practical.

Do not repeatedly retry pathological builds.

==================================================
23. DO NOT IMPLEMENT
==================================================

No:

OAuth
webhooks
continuous synchronization
generic connector framework
ServiceNow
Opsgenie
AWS
GCP
Terraform
automatic Qualification Suite construction
Scenario Certification
new evaluation scenarios
LLM grading
billing
Stripe
CI release gates
frontend-wide redesign

==================================================
DEFINITION OF DONE
==================================================

This recovery is complete only when:

- interrupted Stage-25 code has been inventoried
- correct existing work is preserved
- migration 000014 remains canonical
- PagerDuty is genuinely implemented read-only
- Datadog is genuinely implemented read-only
- credentials reuse existing encryption
- both providers normalize into one contract
- redaction occurs before every LLM boundary
- raw provider payloads are not persisted
- provenance is preserved safely
- browse/select/import works through existing authoring
- workspace isolation and RBAC hold
- provider/security fixtures pass
- focused backend tests pass
- frontend TypeScript passes
- no duplicate connector architecture exists

==================================================
COMPLETION REPORT
==================================================

Report:

1. recovery inventory — what already existed before this session
2. files preserved
3. files changed/added
4. migration 000014 status
5. connection model
6. credential encryption/reuse
7. PagerDuty adapter
8. Datadog adapter
9. paging/timeouts
10. NormalizedIncident mapping
11. redaction-before-LLM behavior
12. no-raw-payload persistence
13. provenance
14. authoring-pipeline integration
15. API/RBAC/workspace isolation
16. frontend import flow
17. safe error behavior
18. PagerDuty focused tests
19. Datadog focused tests
20. redaction sentinel test
21. no-raw-payload test
22. gofmt/focused Go tests
23. TypeScript/build result
24. confirmation no provider writes
25. confirmation no OAuth/webhooks/continuous sync
26. confirmation no LLM grading
27. remaining limitations

Write the report to:

prompts/reports/pagerduty-datadog-recovery.md

DO NOT overwrite this task file.
