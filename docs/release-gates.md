# Release gates

The `Release gates` workflow runs on pull requests and pushes to `main`. Branch
protection should require the single `release-gate` job. That aggregate job does
not rerun tests: it succeeds only when every mandatory focused job reports
`success`, including jobs that would otherwise be skipped after a dependency
failure.

The focused jobs protect these boundaries:

- repository integrity: migration pairing/order, Go formatting, and module sums;
- backend core: agent connections, evaluation/execution, worker, and environment contracts;
- API security: authentication, authorization, workspace isolation, safe errors,
  MCP grants, incident sources, and HTTP route contracts;
- PostgreSQL migrations: all migrations up, the latest migration down, and up again;
- qualification: Stage-27 authoritative capability coverage, certification, plan,
  SRE/GitOps, and qualification contracts;
- reliability/evidence: Stage-33 reliability and Stage-34 trajectory/evidence integrity;
- billing: Stage-35 entitlement, reservation, settlement, idempotency, and provider-event rules;
- frontend: locked install, TypeScript, critical customer-route/static integrity,
  mock-data regression protection, and a production Next.js build.

The migration job alone requires PostgreSQL. GitHub Actions starts an ephemeral
PostgreSQL 16 service and supplies `TEST_DATABASE_URL`; the database is discarded
with the job. Ordinary gates do not use Kubernetes, an LLM, Stripe, PagerDuty,
Datadog, or other live providers. Provider boundaries are exercised with local
deterministic fakes.

Run the inexpensive non-database set locally from the repository root:

```sh
./scripts/release-gate-local.sh
```

The script intentionally mirrors package groupings without becoming a single
`go test ./...` command. To run the database round-trip separately, provide a
scratch PostgreSQL database and run:

```sh
cd backend
TEST_DATABASE_URL='postgres://…' GOMAXPROCS=2 go test -p 1 ./internal/migrationtest
```

Green gates mean **FOCUSED-VERIFIED** automated coverage. They do not establish
cluster E2E proof, production readiness, or product readiness.
