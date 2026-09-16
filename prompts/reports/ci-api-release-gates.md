# Stage 37 — CI/API Release Gates completion report

## 1. Existing automation audit

The repository had no GitHub Actions workflow, Make target, or release-gate
script. It had broad Go package tests, PostgreSQL integration tests gated by
`TEST_DATABASE_URL`, and `next build`; `next lint` was not a deterministic gate
because no ESLint configuration is committed. Stages 23–36 reports and the
current route/package/test layout were reviewed before defining the gates.

## 2. Workflows added/changed

Added `.github/workflows/release-gates.yml`, with focused jobs for repository
integrity, backend core, API/security, migrations, qualification,
reliability/evidence, billing, frontend, and one aggregate `release-gate`.

## 3. Release gate architecture

Each domain owns a separately named and diagnosable job. No job runs
`go test ./...`, and the aggregate does not rerun tests. All Go test jobs use
`GOMAXPROCS=2` and `go test -p 1`.

## 4. Repository/static gate

Added deterministic migration sequence/pair validation, repository-wide Go
format checking, and `go mod verify`.

## 5. Backend core gate

Focused coverage includes agents/connections, evaluation batches/suites,
execution, worker semantics, and environment integration.

## 6. Qualification gate

The gate directly runs agent capability, competency, Stage-27 coverage,
certification, SRE/GitOps qualification, qualification-plan, and qualification
packages. This preserves authoritative coverage-derived TESTED semantics and
the separation between claims, plans, and evidence.

## 7. Reliability/trajectory gate

Stage-33 reliability and Stage-34 trajectory packages run with evidence,
evaluator, failure taxonomy, action execution, and observation tests.

## 8. Billing gate

The Stage-35 billing package is isolated as its own gate. Its tests cover
catalog/entitlement decisions, reservation/commit/release/refund behavior,
idempotency, insufficient-credit and billing-state denials, fake-provider
signature/parsing, and stale-event rules. Stable frontend denial-code/status
mapping assertions were added.

## 9. API contract gate

The API job runs the existing HTTP API suite together with auth, identity,
agent gateway, MCP server, and incident-source boundary tests. This keeps
transport/security regressions distinct from domain failures.

## 10. Auth/workspace-isolation coverage

Existing HTTP, auth, and identity tests are now mandatory PR checks. They cover
session/auth behavior, authorization, cross-workspace rejection, and scoped
resource access without adding a second API implementation.

## 11. API error-code coverage

Existing HTTP error-contract tests remain in the API gate. Billing now also
pins `entitlement_denied` and `plan_limit_reached` to 403,
`insufficient_credits` and `billing_past_due` to 402, and
`environment_tier_not_entitled` to 403.

## 12. Migration validation

Added a static check requiring a contiguous `000001`–`000020` sequence and a
matching down migration for every up migration. Updated the live migration test
from version 18 to 20 and expanded schema assertions through capability
coverage, certification, environment, qualification-plan, and billing tables,
plus key idempotency/uniqueness and immutability objects.

## 13. Live PostgreSQL CI setup/status

The migration job starts an ephemeral PostgreSQL 16 service with a health
check and passes only its disposable database URL to the migration test. This
job was defined but was not run locally in this environment.

## 14. Migration round-trip

The CI database test applies all migrations, asserts version/schema, migrates
20 down to 19, reapplies 20, and asserts the final version. Static migration
validation passed locally; the live round-trip remains CI-only for this run.

## 15. Billing concurrency DB test/status

Existing billing tests exercise concurrency and idempotent ledger semantics at
the domain level. No new live PostgreSQL billing-concurrency test was added: the
current integration fixture does not safely provision the full billing foreign-key
graph independently. This remains a known DB-level race-coverage gap and is not
claimed as locally or CI verified.

## 16. Frontend gate

Added `typecheck`, `check:routes`, and `verify:frontend` npm scripts. CI uses
`npm ci`, then TypeScript checking, route/static integrity, and a production
Next.js build.

## 17. Route integrity

The static frontend check requires the customer lifecycle routes for workspace,
billing, tasks, agents, qualification, runs/trajectory, suite execution, and
comparisons. The production build also enumerates these App Router outputs.

## 18. Mock-data regression protection

The static check fails if any retired customer mock module is restored or if a
critical production route imports the retired mock-module names.

## 19. Secret/leakage protection

Existing API/MCP/gateway/incident-source tests protecting credentials, grants,
and safe errors are mandatory in the API gate. Billing fake-provider tests also
assert checkout surfaces do not contain payment-secret markers. No workflow
dumps environments or uploads secret-bearing artifacts.

## 20. Dependency/install reproducibility

Frontend CI uses the committed lockfile through `npm ci`; Go CI uses the module
files and a separate checksum-verification step. No lockfiles are rewritten.

## 21. Workflow permissions/security

Workflow permissions are explicitly read-only (`contents: read`). It uses no
pull-request secrets, privileged containers, mutable repository writes, or live
provider credentials.

## 22. CI triggers/concurrency/timeouts

Required validation runs on pull requests and pushes to `main`. Workflow-level
concurrency cancels superseded runs on the same ref. Focused jobs have 10–20
minute timeouts and the aggregate has a 5-minute timeout.

## 23. Caching

Supported setup-action caches are enabled for Go (keyed from `backend/go.sum`)
and npm (keyed from the lockfile). Cache misses do not affect correctness.

## 24. Final aggregate release-gate behavior

`release-gate` uses `if: always()` and explicitly requires every named dependency
result to equal `success`. A failure, cancellation, or unexpected skip therefore
cannot produce a green aggregate check. This is the stable branch-protection job.

## 25. Local verification workflow

Added `scripts/release-gate-local.sh`. It runs inexpensive static checks, module
verification, focused non-database Go package groups, frontend typecheck, route
integrity, and build. It intentionally excludes Docker, Kubernetes, and live DBs.

## 26. Documentation

Added `docs/release-gates.md` describing each gate, PostgreSQL scope, excluded
external systems, local invocation, aggregate semantics, and maturity language.

## 27. Tests run locally

The following completed successfully:

- `./scripts/check-migrations.sh`
- `./scripts/check-gofmt.sh`
- `node scripts/check-frontend-integrity.mjs`
- `go mod verify`
- focused Go groups in `scripts/release-gate-local.sh`, including billing and HTTP API
- `npm run typecheck`
- `npm run check:routes`
- `npm run build`

The first sandboxed Go run could not open a loopback listener used by an existing
`httptest` test. The same complete local script was rerun with loopback access
and passed. The Next.js build emitted existing Autoprefixer `end`/`flex-end`
compatibility warnings but completed successfully.

## 28. Checks only runnable in CI

The ephemeral PostgreSQL migration up/down/up test was not run locally and must
run in GitHub Actions. No claim is made that it passed before CI executes it.

## 29. Known gaps

There is no live PostgreSQL billing-concurrency race test yet, no browser E2E
runner, and no lint gate while `next lint` remains interactive/unconfigured.
Route integrity is deliberately a maintainable critical-route manifest rather
than a snapshot of every route.

## 30. No Kubernetes/live external-provider dependency

Normal PR validation starts no Kubernetes cluster and calls no live Stripe,
PagerDuty, Datadog, LLM, cloud, or other external provider. Deterministic local
fakes remain the provider boundary.

## 31. Maturity statement

Green CI means **FOCUSED-VERIFIED** automated release-critical contracts. It
does not mean **E2E-PROVEN**, **PRODUCT-READY**, or production-ready.
