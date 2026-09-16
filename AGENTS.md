# AGENTS.md — Evalplane (test-infra-agents)

Next.js 15 frontend at repo root + Go 1.25 API in `backend/` (module `github.com/evalplane/evalplane/backend`). No CI, no `opencode.json`; frontend lint is just `next lint`.

## Layout

- Frontend: `app/` routes (App Router), `lib/api.ts` (fetch wrapper), `lib/auth.tsx` (`AuthProvider`, mounted in `app/providers.tsx`).
- Backend: `backend/cmd/{api, migrate, worker, referenceagent, referenceapp, e2equalification}/`, domain services in `backend/internal/<domain>/`, HTTP wiring in `backend/internal/httpapi/` composed in `backend/cmd/api/main.go`. Migrations in `backend/migrations/` (`000001`–`000014`). K8s/cluster E2E in `backend/e2e/` (needs real cluster — do not run casually).
- Route truth is `backend/internal/httpapi/router.go`: `backend/README.md` lists only identity + scenario-draft endpoints and omits agents/scenarios/batches/suites/regressions/qualification/evidence/execution.
- Path alias: `@/*` → repo root (`tsconfig.json`).

## Run locally (order matters)

```sh
docker compose up -d            # postgres: infra_agents / postgres:postgres @ localhost:5432
cd backend
export DATABASE_URL="postgres://postgres:postgres@localhost:5432/infra_agents?sslmode=disable"
go run ./cmd/migrate up         # API never auto-applies migrations
go run ./cmd/api
```

Full browser flow: backend with `APP_ORIGIN=http://localhost:3000 ENVIRONMENT=development COOKIE_SECURE=false`, plus repo-root `.env.local` containing `NEXT_PUBLIC_API_BASE_URL=http://localhost:8080`, then `npm run dev` → `http://localhost:3000/signup`.

## Verify

```sh
# backend (from backend/; GOCACHE override per backend/README.md)
mkdir -p /tmp/evalplane-go-cache
GOCACHE=/tmp/evalplane-go-cache go test ./...
GOCACHE=/tmp/evalplane-go-cache go vet ./...
# integration tests silently SKIP unless set; point at a scratch DB, they migrate+clean themselves
TEST_DATABASE_URL="postgres://postgres:postgres@localhost:5432/infra_agents_test?sslmode=disable" go test ./internal/migrationtest ./internal/identity/...
# no DB/cluster needed:
GOMAXPROCS=2 go test ./internal/executionworker/...

# frontend (from root; no test runner, no eslint config committed — `next lint` may prompt for guided setup on first run)
npm run dev
npm run build
npm run lint
```

Migrations: `go run ./cmd/migrate up | version | down [N]` (`down` rolls back 1 by default).

## Config / auth gotchas

- Backend env (see `backend/.env.example`): `DATABASE_URL` required; `AGENT_CREDENTIAL_ENCRYPTION_KEY` (32-byte base64) required or `config.Load()` fails; `OPENAI_API_KEY` + `SCENARIO_LLM_MODEL` must both be set for AI draft generation, else manual authoring only. `APP_ORIGIN` defaults to `http://localhost:3000` in `development` only, required otherwise.
- `APP_ORIGIN` must exactly match the browser origin (credentialed CORS, no wildcards); authenticated mutations enforce exact Origin, but origin-less CLI curl still works. `COOKIE_SECURE=false` only allowed in `development`; `ALLOW_PRIVATE_AGENT_ENDPOINTS=true` only in `development` (fail-closed otherwise).
- Auth: opaque session token in HttpOnly `SameSite=Lax` cookie, only SHA-256 digest stored server-side. Test with cookie jar (`curl -c/-b cookies.txt`). Public: `POST /v1/auth/signup|login`, `POST /v1/bootstrap` is NOT public. Health: `/healthz` = liveness only, `/readyz` = Postgres check (200/503).
- Frontend: `NEXT_PUBLIC_API_BASE_URL` (see root `.env.example`); never put secrets in `NEXT_PUBLIC_*`. `lib/api.ts` sends `credentials: "include"`, throws `APIError(status, code, message)`, and falls back to `http://localhost:8080` in non-production when unset.
- Incident import (`scenario-drafts/import-incident`): multipart, ≤10 files, ≤5 MiB each / ≤20 MiB total; `.yaml/.yml/.json/.txt/.log/.md` only; raw uploads are sanitized in memory via `incident-redaction:v1` and never persisted (`backend/docs/incident-security.md`).

## Conventions

- Business routes versioned under `/v1`; keep `backend/README.md` endpoint list in sync when adding routes.
- Worker (`backend/cmd/worker/README.md`): run in K8s with `DATABASE_URL` Secret, `-reference-image`, same encryption key as API; never log connection contents; `-agent-connections` JSON is local-fixture only, never commit. `cmd/referenceagent` (port 8081) is a deterministic pipeline fixture, not a competence baseline.
- MCP: worker serves stateless Streamable HTTP only (no legacy SSE/Roots/Sampling); agent invocation embeds short-lived `tool_access {endpoint, token, expires_at}` — never persist/log grants (`backend/docs/mcp-agent-integration.md`).
- Execution semantics (`backend/internal/executionworker/README.md`): empty/`no_op`-only responses and step-bound stops are not success; terminal state → evaluation → cleanup; recovery never calls the agent or replays actions.
