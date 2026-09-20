# Evalplane Go API

Product decisions in this repository must follow the canonical doctrine in
[`docs/PRODUCT_DOCTRINE.md`](../docs/PRODUCT_DOCTRINE.md).

Go API for Evalplane. It provides process health, PostgreSQL readiness, and the first identity/workspace domain endpoints.

The API uses Argon2id password credentials and opaque, revocable server-side sessions. Only a SHA-256 digest of each session token is persisted; browsers receive the raw token in an HttpOnly, SameSite=Lax cookie.

Production login rate limiting, email verification, password reset, MFA, and OAuth are not implemented yet and are required considerations before internet exposure.

## Prerequisites

- Go 1.25+
- PostgreSQL 16, either directly in WSL/Linux or through Docker with Compose

For the lightweight native WSL setup, see
[`../docs/local-wsl-development.md`](../docs/local-wsl-development.md).

## Configuration

Copy values from `.env.example` into your shell. `DATABASE_URL` is required. `PORT`, `ENVIRONMENT`, and `LOG_LEVEL` default to `8080`, `development`, and `info`.

## Run locally

From the repository root:

```sh
docker compose up -d
cd backend
export DATABASE_URL="postgres://postgres:postgres@localhost:5432/infra_agents?sslmode=disable"
go run ./cmd/migrate up
go run ./cmd/api
```

Check the API:

```sh
curl http://localhost:8080/healthz
curl http://localhost:8080/readyz
```

`/healthz` reports process liveness without checking PostgreSQL. `/readyz` returns `200` when PostgreSQL responds and `503` otherwise.

## Identity API

Business endpoints are versioned under `/v1`. Public authentication routes are:

- `POST /v1/auth/signup`
- `POST /v1/auth/login`

Authenticated routes include:

- `POST /v1/auth/logout`
- `GET /v1/auth/me`
- `GET /v1/organizations`
- `GET /v1/organizations/{organizationID}`
- `GET /v1/organizations/{organizationID}/workspaces`
- `GET /v1/workspaces/{workspaceID}`
- `PATCH /v1/workspaces/{workspaceID}` (owner/admin workspace-name update only)
- `POST|GET /v1/workspaces/{workspaceID}/members`
- `PATCH|DELETE /v1/workspaces/{workspaceID}/members/{userID}`
- `GET /v1/workspaces/{workspaceID}/billing` (safe account and server-owned plan catalog; no payment secrets)
- `GET /v1/workspaces/{workspaceID}/entitlements` (reproducible current entitlement decisions)
- `GET /v1/workspaces/{workspaceID}/usage` (derived allowance, reserved/committed/available usage, recent append-only entries)
- `POST /v1/workspaces/{workspaceID}/billing/checkout|portal` (owner/admin only; provider session creation, never direct plan mutation)
- `POST /v1/billing/webhooks/{provider}` (provider-signature authenticated; idempotent event inbox, no cookie authorization)
- `POST /v1/workspaces/{workspaceID}/scenario-drafts/describe`
- `POST /v1/workspaces/{workspaceID}/scenario-drafts/import-incident`
- `POST|GET /v1/workspaces/{workspaceID}/incident-sources` (owner/admin/member create; any member list; read-only PagerDuty/Datadog connectors, see `docs/incident-connectors.md`)
- `GET /v1/workspaces/{workspaceID}/incident-sources/{connectionID}` (safe metadata only, never credentials)
- `POST /v1/workspaces/{workspaceID}/incident-sources/{connectionID}/rotate` (credential rotation; preserves provenance)
- `POST /v1/workspaces/{workspaceID}/incident-sources/{connectionID}/test` (real read-only provider check)
- `GET /v1/workspaces/{workspaceID}/incident-sources/{connectionID}/incidents` (bounded browse, `query|limit|offset`)
- `GET /v1/workspaces/{workspaceID}/incident-sources/{connectionID}/incidents/{externalID}` (redacted preview)
- `POST /v1/workspaces/{workspaceID}/incident-sources/{connectionID}/incidents/{externalID}/import` (NormalizedIncident → `incident-redaction:v1` → ScenarioDraft)
- `GET /v1/capability-catalog` (authenticated; server-owned `infrastructure-capability-catalog:v1`)
- `GET /v1/capability-catalog/{capabilityID}` (authenticated)
- `GET /v1/workspaces/{workspaceID}/agents/{agentID}/versions/{versionID}/capability-claims` (authenticated; `claim_status=claimed`, machine-readable `test_status=untested|partial|tested`, `qualification_status=null`, never implies qualification)
- `GET /v1/task-family-catalog` (authenticated; server-owned `task-family-contract:v1`)
- `GET /v1/capability-coverage-requirements` (authenticated; server-owned `capability-coverage-contract:v1`)
- `GET /v1/workspaces/{workspaceID}/agents/{agentID}/versions/{versionID}/capability-coverage` (authenticated; latest persisted coverage per claim with exact evidence scope)
- `GET /v1/workspaces/{workspaceID}/suite-executions/{executionID}/agents/{agentVersionID}/capability-coverage` (authenticated; deterministic live assessment for one exact execution, never persists)
- `POST /v1/workspaces/{workspaceID}/suite-executions/{executionID}/agents/{agentVersionID}/capability-coverage/assessments` (owner/admin/member; idempotent persisted assessment for one exact execution)
- `GET /v1/workspaces/{workspaceID}/agents/{agentID}/versions/{versionID}/reliability` (authenticated; deterministic AgentVersion reliability: task, family, and capability readings with `pass@k`/`pass^k`, sample sufficiency, and platform-invalid counts under `reliability-metrics-contract:v1`; read-only, never qualified)
- `GET /v1/workspaces/{workspaceID}/suite-executions/{executionID}/reliability` (authenticated; SuiteExecution reliability with per-agent task/family/capability readings and task saturation; optional `baseline_agent_version_id` + `candidate_agent_version_id` pair adds a reliability-delta comparison with structured `not_comparable` reasons; read-only)
- `GET /v1/workspaces/{workspaceID}/scenarios/{scenarioID}/versions/{versionID}/reliability` (authenticated; ScenarioVersion task reliability with optional `agent_version_id` filter plus `fresh|developing|saturated` task saturation; read-only, no hidden variant values)
- `POST|GET /v1/workspaces/{workspaceID}/scenarios/{scenarioID}/versions/{versionID}/task-family` (explicit family + case-type assignment; immutable once evaluation evidence exists)
- `GET|POST /v1/workspaces/{workspaceID}/scenarios/{scenarioID}/versions/{versionID}/certification` (read: any member; trigger: owner/admin/member; minimal `idempotency_key`/`environment_profile`/`runtime_contract_version` only; server-derived `scenario-certification-contract:v1`, rejects `state`/`certified`/`simulate`/`variant_configs`/`reference_remediation`/`gate_results`/`failure_codes`/`evaluator_version`; reference loaded server-side from stored evaluator-only remediation; server-selected variants pinned)
- `PUT /v1/workspaces/{workspaceID}/scenarios/{scenarioID}/versions/{versionID}/reference-remediation` (owner/admin/member; evaluator-only, never exposed to agents)
- `GET /v1/workspaces/{workspaceID}/scenarios/{scenarioID}/versions/{versionID}/certification/eligibility` (any member; `eligible_authoritative_evidence` is true only for fresh `certified`)
- `GET /v1/environment-components` (authenticated; server-owned `environment-component-catalog:v1`)
- `GET /v1/environment-profiles` (authenticated; server-owned `environment-profile-catalog:v1`; only `kubernetes.namespace:v1` is active, the rest are planned/experimental)
- `GET /v1/environment-providers` (authenticated; `kind-dev:v1` active development-only, never production; future providers planned with no fake health)
- `GET /v1/workspaces/{workspaceID}/environments`, `GET /v1/workspaces/{workspaceID}/environments/{environmentID}`, and `GET /v1/workspaces/{workspaceID}/environments/{environmentID}/components` (workspace-scoped aggregate and child instances; opaque `provider_ref` only, never kubeconfigs/tokens/credentials)
- `POST /v1/workspaces/{workspaceID}/environment-selection/resolve` (diagnostic scheduler; accepts a ScenarioVersion reference, never arbitrary requirements)
- `GET|PUT /v1/workspaces/{workspaceID}/scenarios/{scenarioID}/versions/{versionID}/environment` (read: any member with eligible profile; write: owner/admin/member sets server-reviewed requirements)
- `GET /v1/workspaces/{workspaceID}/runs/{runID}/events` (any member; persisted run events, `event_type|limit|offset`)
- `GET /v1/workspaces/{workspaceID}/runs/{runID}/actions` (any member; action lifecycle, `status|limit|offset`)
- `GET /v1/workspaces/{workspaceID}/runs/{runID}/evaluation` (any member; deterministic evaluation with criteria + findings)
- `GET /v1/workspaces/{workspaceID}/runs/{runID}/verification` (any member; immutable `infrastructure-verifier:v1` result with baseline, intent, action inspection, actual state diff, outcome, side effects, stability, forward risk, validity, and exact pins)
- `GET /v1/workspaces/{workspaceID}/runs/{runID}/verification/actions|snapshots|forward-risks` (any member; focused safe projections from the same authoritative result; never credentials or Secret values)
- `POST /v1/workspaces/{workspaceID}/evaluation-batches` accepts optional typed `source_metadata` (`provider`, `repository`, `commit_sha`, `pull_request`, `workflow`) and a workspace-scoped `idempotency_key`; repeated CI requests with the same key resolve to the original exact-version batch instead of creating duplicate authoritative runs. Source metadata is context only, never verification evidence.
- `GET /v1/workspaces/{workspaceID}/runs/{runID}/trajectory` (any member; deterministic observable trajectory under `trajectory-evidence-contract:v1`: ordered events with provenance, outcome summary, durations, reproducibility pins; `phase|event_type|actor|status|limit|offset`; `?compare_with={runID}` adds a summary-level comparison of two trials; read-only)
- `GET /v1/workspaces/{workspaceID}/runs/{runID}/evidence/{evidenceID}` (any member; bounded safe evidence detail with preview, truncation flag, byte size, content hash, provenance)
- `GET /v1/workspaces/{workspaceID}/suite-executions/{executionID}/trajectory-summary` (any member; per-trial outcome/actions/safety/recovery rows linking to trial trajectories; read-only)
- `GET /v1/qualification-tasks` (public; built-in Kubernetes/SRE qualification catalog, agent-safe projection only — no ground truth, reference remediation, or hidden variants; availability derived per task from environment scheduler truth)
- `GET /v1/qualification-tasks/{taskID}` (public; one catalog task or planned concept; planned concepts report runnable=false and never resolve to the namespace profile)
- `POST /v1/workspaces/{workspaceID}/agents/{agentID}/versions/{versionID}/qualification-plans` (owner/admin/member; server-derived `qualification-suite-planner:v1` plan from the version's claimed capabilities under `capability-coverage-contract:v2`; accepts only optional `capability_ids` subset, rejects `state`/task IDs/`certified`/coverage or qualification results; states `ready|partial|blocked`, never qualified)
- `GET /v1/workspaces/{workspaceID}/qualification-plans/{planID}` (any member; persisted immutable plan with pins, selections, trial counts, and machine-readable blockers)
- `POST /v1/workspaces/{workspaceID}/qualification-plans/{planID}/suite` (owner/admin/member; READY plans only; creates an immutable `purpose=qualification` EvaluationSuite/SuiteVersion pinning exact ScenarioVersions, trial counts, and all contract versions; never executes, never qualifies)
- Certification triggers resolve placement server-side (`ScenarioVersion` requirements → scheduler → server-owned profile/provider); `environment_profile` is an optional preference that must be a known active satisfying profile, `runtime_contract_version` must be server-owned, and new attempts pin `provider_id`/`isolation_tier`/all five environment contracts (see `internal/environment/README.md`)
- Scenario versions expose `task_family_id`, `case_type`, `task_family_contract_version` (empty when unassigned; unassigned versions never contribute coverage)
- AgentVersion creation accepts `claimed_capability_ids` (immutable competency claims; incompatible runtime requirements fail validation)

`POST /v1/bootstrap` and the direct Stage 3 creation routes are no longer public. Signup atomically creates the organization, workspace, user, password credential, owner membership, and initial session.

Signup using a cookie jar:

```sh
curl -i -c cookies.txt -X POST http://localhost:8080/v1/auth/signup \
  -H 'Content-Type: application/json' \
  -d '{"full_name":"Francis","email":"francis@acme.com","password":"a-long-secure-password","organization":{"name":"Acme","slug":"acme"},"workspace":{"name":"Acme Infrastructure","slug":"infrastructure"}}'
```

Use the authenticated session and then log out:

```sh
curl -i -b cookies.txt http://localhost:8080/v1/auth/me
curl -i -b cookies.txt http://localhost:8080/v1/workspaces/WORKSPACE_ID
curl -i -b cookies.txt -c cookies.txt -X POST http://localhost:8080/v1/auth/logout \
  -H 'Content-Type: application/json'
```

Workspace reads and membership operations require membership. Owner/admin roles may manage members, with owner-only owner operations and transaction-locked final-owner protection.

## Scenario authoring

Manual scenario creation works without an AI provider. To enable draft generation from a description or imported incident evidence, set both `OPENAI_API_KEY` and `SCENARIO_LLM_MODEL` before starting the API. Generated drafts are always returned for human review and are not saved until the user submits the canonical scenario form.

Incident import accepts `multipart/form-data` with optional `description` and `timeline` fields plus up to 10 `files`. Supported files are YAML, JSON, text, logs, and Markdown. Raw uploads are sanitized in memory and are not persisted by the authoring service.

Browser requests from the exact configured `APP_ORIGIN` receive credentialed CORS headers. Preflight supports GET, POST, PATCH, DELETE, OPTIONS, Content-Type, and X-Request-ID. Other origins are rejected and wildcard origins are never used. Authenticated mutations retain exact Origin validation; origin-less local CLI requests remain supported.

## Full local browser flow

Start PostgreSQL, then run the backend with `DATABASE_URL`, `APP_ORIGIN=http://localhost:3000`, `ENVIRONMENT=development`, and `COOKIE_SECURE=false`. At the repository root, create `.env.local` containing `NEXT_PUBLIC_API_BASE_URL=http://localhost:8080`, then run `npm run dev`. Visit `http://localhost:3000/signup` to exercise signup → onboarding → workspace → logout → login → workspace.

## Database migrations

Versioned SQL migrations live in `migrations/`. The API does not apply them automatically; schema changes and API startup are separate operations.

From `backend/`, with `DATABASE_URL` exported:

```sh
go run ./cmd/migrate up
go run ./cmd/migrate version
go run ./cmd/migrate down
```

`down` rolls back one migration by default. An explicit positive count is also supported, for example `go run ./cmd/migrate down 2`. Apply `up` afterward to return the database to the latest version.

The optional migration integration test uses a dedicated PostgreSQL database and skips when `TEST_DATABASE_URL` is unset:

```sh
TEST_DATABASE_URL="postgres://postgres:postgres@localhost:5432/infra_agents_test?sslmode=disable" go test ./internal/migrationtest
```

Identity repository integration coverage also uses `TEST_DATABASE_URL`, applies migrations, cleans its own domain data, and skips when unset.

## Verify

```sh
mkdir -p /tmp/evalplane-go-cache
GOCACHE=/tmp/evalplane-go-cache go test ./...
GOCACHE=/tmp/evalplane-go-cache go vet ./...
```
