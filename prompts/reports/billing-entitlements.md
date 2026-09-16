# Stage 35 completion report — Billing, usage, and entitlements

Completed 2026-09-16 from the existing Stage-35 repository state. Existing billing-domain work was preserved and completed at the execution, worker, environment, provider, HTTP, migration, and frontend boundaries.

## 1. Files changed

- Billing domain: `backend/internal/billing/{catalog,contracts,entitlement,errors,model,provider,repository,service,settlement}.go` and `billing_test.go` (existing work preserved; integration defects completed).
- Execution integration: `backend/internal/evaluationsuite/service.go`, `backend/internal/executionworker/{dependencies,orchestrator,recovery,composition}.go`.
- Environment integration: `backend/internal/environment/scheduler.go`.
- HTTP/application wiring: `backend/internal/httpapi/billing.go`, `backend/internal/httpapi/router.go`, `backend/cmd/api/main.go`.
- Schema: `backend/migrations/000020_billing_entitlements.{up,down}.sql`.
- Frontend: `app/workspace/billing/{page.tsx,billing.module.css}`, `app/workspace/layout.tsx`, `lib/api.ts`; existing `app/pricing/page.tsx` was reviewed and retained because its public prices/credits already match the catalog.
- Documentation/config: `backend/README.md`, `backend/.env.example`.
- This report: `prompts/reports/billing-entitlements.md`.

## 2. Migration/schema

Migration `000020` adds normalized `billing_accounts` (one per workspace), append-only `billing_usage_ledger`, and idempotent `billing_provider_events`. Existing workspaces are deterministically backfilled to Developer/free. Constraints cover stable plan/status/kind values, period ordering, positive non-adjustment quantities, workspace-scoped idempotency, provider event identity, and unique provider customer/subscription references. Supporting workspace-period, run, and inbox-status indexes are included. The down migration drops the three tables in dependency order.

## 3. Billing/catalog contract versions

- `billing-catalog-contract:v1`
- `entitlement-contract:v1`
- `usage-meter-contract:v1`

Accounts and ledger entries pin the relevant versions so historical usage remains interpretable.

## 4. Plan catalog

Stable IDs are `developer`, `starter`, `pro`, `organization`, and `enterprise`. Public prices remain $0, $49/month, $299/month, $999/month, and Custom. Published allowances remain 10, 100, 500, 2,000, and custom. No annual discount, dollar overage, or unreleased environment multiplier was invented.

## 5. Workspace billing account model

One account per workspace records current stable plan ID, billing status, effective period, catalog version, optional provider/customer/subscription references, and timestamps. Lazy creation and migration backfill both default safely to Developer/free.

## 6. Billing states

`free`, `trialing`, `active`, `past_due`, `canceled`, and `suspended`. Past-due/canceled/suspended states block new expensive evaluation work while historical reads remain available.

## 7. Entitlement model

Server-owned deterministic decisions pin plan, billing state, entitlement/catalog contracts, key, allowed state, limit/value, reason, and machine-readable denial code. RBAC remains an independent gate.

## 8. Plan entitlement matrix

The V1 catalog covers evaluation execution, qualification-suite authoring, API/MCP, incident import, advanced reliability, GitOps-when-runtime-exists, maximum isolation, parallel trials, members, and evidence-retention policy. Developer/Starter are intentionally conservative; Pro/Organization enable currently defensible advanced features; dedicated isolation remains Enterprise. Retention is policy-only—no deletion worker was added.

## 9. Usage/meter model

The billable unit is an actual evaluation trial, not an API call. V1 uses one deterministic meter unit per trial and separately pins raw run, batch, suite execution, scenario version, environment profile, provider, and isolation context where available. Isolation multipliers remain deferred.

## 10. Ledger model

Immutable kinds are `grant`, `reservation`, `commit`, `release`, `refund`, and signed internal `adjustment`. Repository code provides no ledger update/delete path. Balances are derived from entries.

## 11. Credit/allowance model

Monthly grants are idempotent by workspace/period. Reserved, committed, refunded, and available quantities are derived with integer arithmetic. Enterprise/custom is represented as unlimited/custom rather than fabricated monetary values.

## 12. Reservation lifecycle

Starting a SuiteExecution performs RBAC, billing-state/entitlement, and allowance preflight. After runs are generated, the billing account row is locked and one `reservation:<runID>` entry per run is inserted inside the same transaction that creates and queues the execution. Concurrent suite starts therefore cannot spend the same allowance. Suite/plan creation alone does not reserve credits.

## 13. Settlement lifecycle

After terminal persistence and deterministic evaluation reconciliation, the worker reads authoritative run/evaluation/platform evidence and invokes idempotent settlement. Valid terminal evaluations commit; pre-execution/control/platform failures release; a later platform-invalid determination refunds an existing commit or releases an open reservation.

## 14. Platform-invalid handling

Settlement uses the same platform-classified run-event semantics as suite validity aggregation. Platform-invalid evaluations never remain committed customer evaluation consumption. Corrections are append-only refunds; rows are never deleted.

## 15. Valid agent failure billing

Valid PASS, valid agent FAIL, and valid agent timeout consume a trial and commit once. Agent failure is not treated as a refund reason.

## 16. Abandoned/canceled handling

User/control stop, provisioning/start/load failures, stale lease/worker abandonment without valid evaluation, persistence failure, and platform timeout release without charge. Recovery settlement runs after recovery evaluation and is idempotent. A valid evaluation that survived a recovery signal still commits once.

## 17. Idempotency/concurrency

Workspace/idempotency uniqueness prevents duplicate grants/reservations/settlements. Account row locking serializes aggregate availability checks. Commit/release are mutually exclusive; refund requires a commit. Provider events are unique by provider/event ID. Stale provider timestamps do not overwrite newer account truth.

## 18. Execution integration

The existing execution engine was not rewritten. A narrow `ExecutionBillingGate` was added only to `StartSuiteExecution`; worker settlement is a narrow optional dependency. Legacy unit fixtures remain usable when the optional gate/settler is nil, while production API/worker composition wires billing.

## 19. Environment entitlement integration

The scheduler accepts an authorized maximum isolation ceiling. Candidates above it receive `environment_tier_not_entitled`; requirements are never weakened or downgraded to fit a plan. Reservation also enforces a pinned isolation tier when supplied.

## 20. Qualification planner relationship

Planning and building the immutable qualification suite remain free and retain Stage-32 technical-readiness semantics. Commercial enforcement occurs when the suite is actually started. No historical planner meaning was mutated.

## 21. Provider abstraction

`BillingProvider` narrowly defines checkout creation, portal creation, signed-event verification, and safe reconciliation parsing. Domain models import no provider SDK types; plan/status/entitlement/usage authority stays internal.

## 22. Payment-provider implementation/status

A deterministic fake/development provider is wired. No Stripe SDK or live payment E2E was added or claimed. Checkout/portal UI remains truthfully unavailable because the API reports no live provider-backed flow.

## 23. Provider event idempotency

The webhook route is outside cookie authorization and requires an HMAC signature before mutation. The inbox stores only a bounded safe reconciliation projection, never request headers or secrets. Applied duplicates are no-ops; stale events are marked stale; failures carry a safe reason.

## 24. API/RBAC

Added authenticated workspace-scoped GET APIs for billing, entitlements, and usage; owner/admin checkout and portal POST APIs; and provider-authenticated webhook ingestion. Any workspace member may read historical billing/usage. Viewers cannot mutate billing. No endpoint accepts authoritative plan, status, balance, or committed-usage fields.

## 25. Billing/usage frontend

`/workspace/billing` shows current plan/state/period, included/used/reserved/available credits, current entitlements with reasons, and recent safe ledger entries. It is intentionally restrained and contains no fake invoice history.

## 26. Pricing-page consistency

The existing page already matches server prices and allowances and truthfully states that billing automation is not enabled. No price, annual discount, or overage number changed.

## 27. Entitlement denial UX

The backend emits structured codes including `entitlement_denied`, `plan_limit_reached`, `insufficient_credits`, `billing_past_due`, and `environment_tier_not_entitled`. Existing API error propagation preserves these for execution UI surfaces; billing settings display server reasons rather than fabricating access.

## 28. Audit integration

There is no standalone audit package in this repository. Billing traceability is provided by immutable usage entries, account timestamps/provider references, and the provider-event inbox. Sensitive payment payloads are excluded.

## 29. Security/secrets

Webhook secrets are environment-only (`BILLING_WEBHOOK_SECRET`) and callbacks fail closed when absent/invalid. No card/CVC/provider secret is stored. Workspace membership is enforced on reads; write RBAC and billing are separate. Provider events cannot directly bypass catalog validation or stale-event protection.

## 30. Focused backend tests

Passed: billing, evaluationsuite, execution, executionworker, environment, qualplan, httpapi, config, migrationtest, API command, and worker command packages with `GOMAXPROCS=2 GOCACHE=/tmp/evalplane-go-cache`. Per instruction, `go test ./...` was not run. The migration integration package passed its non-DB path; no `TEST_DATABASE_URL` was supplied, so a live migration round-trip was not claimed.

## 31. Stage-32 regression

`go test ./internal/qualplan` passed.

## 32. Stage-33 regression

`go test ./internal/reliability` passed.

## 33. Stage-34 regression

`go test ./internal/trajectory` passed.

## 34. TypeScript/build

`npx tsc --noEmit` passed. `npm run build` completed successfully, including `/workspace/billing`.

## 35. Client self-upgrade confirmation

Confirmed: public clients cannot set plan/status/balance. Checkout only requests a known target plan and does not mutate internal subscription truth; signed provider reconciliation owns provider-backed state changes.

## 36. Platform-invalid charging confirmation

Confirmed: platform-invalid work releases an open reservation or appends a refund to an existing commit, and is excluded from net committed consumption.

## 37. Valid agent failure billing confirmation

Confirmed: valid agent FAIL and valid agent timeout commit billable evaluation usage.

## 38. No-double-charge confirmation

Confirmed by stable per-run keys, unique database constraints, mutually exclusive settlement checks, worker retry/recovery integration, and focused logical concurrency/idempotency tests.

## 39. No technical-isolation downgrade confirmation

Confirmed: commercial isolation filtering rejects over-tier candidates; it never substitutes a weaker profile for a task that requires stronger isolation.

## 40. Live payment/runtime limitations

No live Stripe/payment E2E was run. The fake provider is deterministic only. No Docker/Kubernetes was started. No live database migration was run because no scratch `TEST_DATABASE_URL` was configured. Only the namespace development runtime is presently executable; higher-fidelity profiles remain planned.

## 41. Deferred work

- Official Stripe adapter, configured product/price mapping, live checkout/portal UX, and signed Stripe webhook E2E.
- Product-approved non-flat isolation usage multipliers and overage pricing.
- A dedicated retention worker and broader governance/audit export.
- Live-DB concurrency/integration coverage in an explicitly provisioned scratch database.
