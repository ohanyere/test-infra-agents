EVALPLANE — BILLING, USAGE & ENTITLEMENTS

Read AGENTS.md first.

Inspect:

- current /pricing implementation and pricing data
- workspace / membership / RBAC
- Agent / AgentVersion
- EvaluationSuite / SuiteVersion / SuiteExecution
- run/trial lifecycle
- environment scheduler + profiles/providers/isolation
- qualification planner
- reliability valid/platform-invalid semantics
- trajectory platform-invalid semantics
- existing audit patterns
- migrations
- frontend settings/navigation
- backend configuration/secrets conventions

Do not broadly refactor the repository.

==================================================
PURPOSE
==================================================

Build Evalplane's server-side commercial-control layer.

The system must answer:

- What plan is this workspace on?
- What features is it entitled to use?
- What limits apply?
- How much billable evaluation usage has it consumed?
- Does it have enough entitlement/credits to start more work?
- Was usage reserved, committed, released or refunded?
- Why was an operation blocked?
- Can billing state be reconstructed and audited?

Do not rely on frontend hiding.

Do not let a payment provider become the product's internal authorization
source of truth.

==================================================
1. CONTRACTS
==================================================

Introduce versioned contracts, preferably:

billing-catalog-contract:v1
entitlement-contract:v1
usage-meter-contract:v1

Pin relevant versions on persisted records where necessary.

Historical usage must remain interpretable when pricing or entitlement rules
change.

==================================================
2. CURRENT PLAN CATALOG
==================================================

Preserve the existing customer-facing plan direction:

Developer
Starter
Pro
Organization
Enterprise

Current public working prices:

Developer      free
Starter        $49 / month
Pro            $299 / month
Organization   $999 / month
Enterprise     custom

Inspect the existing /pricing page before changing wording.

Do not invent annual discounts.

Do not invent a final overage price if one is not already specified.

Do not hardcode external-provider product IDs into domain logic.

==================================================
3. PLAN IDENTITY
==================================================

Use stable machine IDs, e.g.:

developer
starter
pro
organization
enterprise

Display names/prices are catalog metadata.

Plan identity must not depend on a mutable display string.

==================================================
4. BILLING ACCOUNT
==================================================

A workspace should have one billing account/domain record.

Track at minimum:

workspace
current plan
billing status
effective period
subscription/customer provider refs where applicable
catalog version
created/updated timestamps

Suggested billing statuses:

free
trialing
active
past_due
canceled
suspended

Use existing conventions where better.

Do not let clients submit arbitrary `active`/`paid` status.

==================================================
5. DEFAULT WORKSPACE PLAN
==================================================

New workspaces default to Developer unless repository behavior already defines
another safe default.

Existing workspaces must receive deterministic migration/backfill behavior.

Do not accidentally place historical workspaces on a paid plan.

==================================================
6. ENTITLEMENTS
==================================================

Separate:

subscription/billing status

from:

product entitlements.

Examples of entitlement dimensions:

can_run_evaluations
can_build_qualification_suites
can_use_api
can_use_mcp
can_use_incident_imports
can_use_advanced_reliability
can_use_gitops_qualification_when_runtime_exists
allowed_environment_isolation
max_parallel_trials
max_workspace_members
evidence_retention_days

Do NOT invent arbitrary product restrictions merely to differentiate plans.

Inspect current product/pricing copy and implement only defensible V1
entitlements.

Entitlement decisions must be server-owned.

==================================================
7. ENTITLEMENT SNAPSHOT
==================================================

An entitlement decision should be reproducible.

Return/pin approximately:

plan
billing status
entitlement contract
catalog contract
entitlement key
allowed
limit/value
reason

Machine-readable denial codes required.

==================================================
8. BILLING VS AUTHORIZATION
==================================================

RBAC and billing are independent.

Example:

workspace viewer:
billing plan may allow running trials
but RBAC still denies writes

workspace member:
RBAC may allow execution
but billing/entitlement may deny because quota/credits are exhausted.

Both gates must pass.

Do not replace existing auth/RBAC checks.

==================================================
9. USAGE MODEL
==================================================

Create an append-only usage ledger.

Do not maintain only one mutable counter.

Suggested event concepts:

grant
reservation
commit
release
refund
adjustment

Use existing naming conventions if appropriate.

Every monetary/credit-impacting change must be traceable.

==================================================
10. BILLABLE UNIT
==================================================

Do not equate API calls with billable usage.

Primary billing event is actual evaluation execution.

A useful V1 abstraction is:

evaluation execution usage units / credits

Usage entries should pin context such as:

workspace
SuiteExecution if applicable
run/trial
ScenarioVersion
environment profile
provider
isolation tier
meter contract
quantity
reason

Do not finalize different dollar multipliers for namespace/vcluster/dedicated
unless already product-approved.

The meter architecture must allow those differences later.

==================================================
11. RAW USAGE VS PRICE
==================================================

Keep raw resource/evaluation usage separate from commercial price conversion.

For example:

trial executed
environment isolation = namespace
execution duration = X
meter units = Y

Pricing policy may later convert units to included credits/overage.

Do not lose the raw usage facts when pricing changes.

==================================================
12. CREDIT / ALLOWANCE LEDGER
==================================================

If current product terminology uses credits, support an internal credit ledger.

Balance must be derivable from immutable entries.

Do not let clients set their own balance.

Avoid floating-point money.

For monetary amounts use integer minor units.

For credits/usage use integer or fixed deterministic units.

==================================================
13. RESERVATION BEFORE EXPENSIVE EXECUTION
==================================================

Prevent race-condition overspend.

Before scheduling billable work:

authorization/RBAC
→ entitlement check
→ quota/credit reservation
→ execution may start

The reservation must be idempotently linked to the expensive operation.

Two concurrent requests must not spend the same remaining allowance.

==================================================
14. SETTLEMENT
==================================================

When a trial reaches a billable valid terminal result:

reservation
→ commit

A valid PASS is billable.

A valid agent FAIL is also billable.

The customer consumed a valid evaluation in both cases.

Do not refund merely because the agent failed.

==================================================
15. PLATFORM-INVALID HANDLING
==================================================

Use existing Stage-33/34 validity semantics.

A platform-invalid evaluation must not become committed agent evaluation usage.

Reserved usage should be:

released

or, if commitment happened before validity was known:

refunded through an append-only correcting entry.

Do not delete historical usage rows.

Do not count platform failure as customer evaluation consumption.

==================================================
16. ABANDONED / CANCELED WORK
==================================================

Define deterministic settlement for:

user cancellation before execution
provisioning failure
worker abandonment
stale lease recovery
timeout
platform invalid
valid agent timeout

Reuse existing outcome/failure taxonomy.

Do not collapse all timeouts into free usage.

A valid agent timeout may be billable.

A platform timeout/failure is not.

==================================================
17. IDEMPOTENCY
==================================================

Usage reservations/commits/releases/refunds must be idempotent.

Use stable operation/idempotency keys tied to:

run
suite execution
provider event
manual adjustment

Repeated worker completion callbacks must not double-charge.

==================================================
18. CONCURRENCY
==================================================

Use database constraints/transactions to prevent:

double reservation
double commit
negative balance race
duplicate provider event application
two active subscription states for one workspace where forbidden

Test concurrent logical behavior where possible without a live DB.

==================================================
19. LIMITS
==================================================

Enforce applicable product limits at the server layer.

Examples if represented in the V1 entitlement catalog:

parallel trial limit
workspace member limit
environment fidelity/isolation access
usage allowance

Return structured errors such as:

entitlement_denied
plan_limit_reached
insufficient_credits
billing_past_due
environment_tier_not_entitled

Use repository conventions.

==================================================
20. EXECUTION INTEGRATION
==================================================

Do not rewrite the execution engine.

Integrate billing at the narrow lifecycle boundaries.

Suite planning remains free/read-only unless product rules explicitly say
otherwise.

Creating a QualificationPlan must not consume execution credits.

Creating a SuiteVersion must not consume execution credits.

Starting actual trials is where reservation occurs.

Terminal validity determines settlement.

==================================================
21. ENVIRONMENT ENTITLEMENT
==================================================

Entitlement must be able to restrict environment fidelity without lying to the
scheduler.

Example future rule:

plan allows namespace
but not dedicated_cluster

The scheduler should receive/consult the authorized maximum fidelity rather
than provision and fail afterward.

Do not downgrade a task that REQUIREs dedicated isolation to namespace.

Instead return an entitlement blocker.

==================================================
22. QUALIFICATION PLANNER RELATIONSHIP
==================================================

Do not make Stage-32 planner confuse:

technically executable

with:

commercially entitled.

If cleanly supported, qualification readiness may report a separate commercial
blocker.

Do not mutate historical planner semantics unnecessarily.

At minimum suite execution must enforce billing before money/resource spend.

==================================================
23. PROVIDER ABSTRACTION
==================================================

Define a narrow payment/subscription provider interface.

Evalplane domain models must not import provider SDK types.

Provider responsibilities may include:

checkout-session creation
customer portal creation
subscription reconciliation
signed webhook/event verification

Internal responsibilities remain:

plan mapping
billing state
entitlements
usage
credits
execution authorization

==================================================
24. EXTERNAL PAYMENT PROVIDER
==================================================

If a payment-provider adapter already exists, integrate it safely.

If none exists, implement the interface plus deterministic fake/test provider
first.

Do not block core entitlement/usage work on external credentials.

Do not claim live payment E2E unless it actually ran.

If implementing Stripe in this stage:

- official SDK only
- test mode
- no secret in repository
- signed webhooks
- idempotent event persistence
- provider price/product mapping in configuration
- stale/out-of-order events must not blindly overwrite newer subscription truth

Otherwise explicitly defer real Stripe E2E.

==================================================
25. PROVIDER EVENT INBOX
==================================================

If external provider events are accepted, persist an idempotent event inbox.

Track:

provider
provider_event_id
event type
received_at
processed_at
status
safe failure reason

Never persist full secret-bearing request headers.

Duplicate events must be no-ops after the first successful application.

==================================================
26. PLAN CHANGES
==================================================

Model upgrades/downgrades without allowing clients to directly set plan state.

For provider-backed subscriptions, provider reconciliation owns effective
subscription state.

Do not invent proration logic yourself.

Do not downgrade historical entitlement snapshots.

Current decisions use current effective entitlement state.

==================================================
27. PAST-DUE / CANCELED
==================================================

Define conservative behavior.

Read access to historical evaluations should generally remain available.

Expensive new execution may be blocked when commercial state requires it.

Do not delete evidence when a subscription ends.

==================================================
28. RETENTION
==================================================

If evidence retention becomes a plan entitlement:

do not physically delete anything in this stage unless the repository already
has a safe retention worker.

Expose the entitlement/retention policy only.

Actual deletion lifecycle may remain deferred to governance/production
hardening.

==================================================
29. API
==================================================

Add authenticated workspace-scoped APIs consistent with current routing.

Suggested read APIs:

GET /v1/workspaces/{workspaceID}/billing
GET /v1/workspaces/{workspaceID}/entitlements
GET /v1/workspaces/{workspaceID}/usage

Provider-backed write APIs only if supported:

POST /v1/workspaces/{workspaceID}/billing/checkout
POST /v1/workspaces/{workspaceID}/billing/portal

Webhook/provider callback route must use provider authentication/signature,
not workspace-cookie authorization.

Do not expose administrative balance mutation publicly.

==================================================
30. USAGE API
==================================================

Usage response should expose safe audit data:

current allowance/balance
reserved
committed
available
current billing period
usage ledger summary
recent safe usage entries

Do not expose internal provider secrets.

==================================================
31. FRONTEND — BILLING SETTINGS
==================================================

Add a restrained workspace Billing/Usage page.

Show:

current plan
billing state
included allowance/credits where defined
used
reserved
available
current period
entitlements
upgrade/manage billing action where provider-backed flow exists

No giant KPI dashboard.

No fake invoice history.

==================================================
32. FRONTEND — PRICING
==================================================

Keep /pricing consistent with the server plan catalog.

Do not silently change current public prices.

Do not display usage multipliers/overage numbers that are not finalized.

If checkout is unavailable in development:

use a truthful disabled/manage state rather than fake success.

==================================================
33. FRONTEND — ENTITLEMENT DENIALS
==================================================

When an execution is blocked, show the actual structured reason.

Examples:

Plan limit reached
Insufficient evaluation credits
Dedicated environments are not included in this plan
Billing action required

Do not replace backend enforcement with disabled buttons only.

==================================================
34. AUDITABILITY
==================================================

Billing-affecting actions should be traceable.

At minimum:

subscription-state change
plan change
usage reservation
usage commit
release/refund
manual internal adjustment if supported

Integrate with existing audit architecture where practical.

Do not expose sensitive payment data.

==================================================
35. SECURITY
==================================================

Never store:

full card details
CVC
raw provider secret key
raw webhook signing secret in DB
customer payment method secrets

Provider secrets come from environment/secret management.

Webhook signatures validated before state mutation.

Workspace isolation mandatory.

Users may not submit:

plan=pro
billing_status=active
credit_balance=999999
usage_committed=0

as authoritative values.

==================================================
36. DATABASE / MIGRATION
==================================================

Use the next migration number after the current repository maximum.

Design normalized tables for the minimum necessary concepts, likely:

billing accounts/subscriptions
usage ledger
usage reservations if separate
provider event inbox if provider integration exists

Prefer immutable append-only usage entries.

Use constraints and indexes deliberately.

Down migration required.

Do not create redundant mutable balance truth if it can be derived safely.

==================================================
37. TESTS
==================================================

Focused tests must cover at minimum:

contracts/version pins
workspace defaults to Developer
stable plan IDs
client cannot self-upgrade plan
RBAC independent of entitlement
entitlement allowed
entitlement denied
parallel limit enforcement if implemented
reservation before execution
reservation idempotency
double commit impossible
commit valid PASS
commit valid agent FAIL
platform-invalid releases/refunds
platform-invalid not included in committed evaluation consumption
provisioning/platform failure not charged
valid agent timeout handling
abandoned worker recovery does not double charge
balance/allowance cannot race negative logically
raw usage preserved separately from price
environment entitlement cannot downgrade required isolation
plan/build qualification does not consume credits
suite creation alone does not consume credits
actual execution performs reservation
provider event duplicate idempotency if provider implemented
out-of-order/stale provider update protected if provider implemented
past-due behavior
canceled-plan historical read behavior
workspace isolation
viewer cannot mutate billing
no raw payment/provider secrets
frontend cannot fabricate paid state
existing Stage-32 planner regression
Stage-33 reliability regression
Stage-34 trajectory regression
no LLM involvement

==================================================
38. VERIFICATION
==================================================

gofmt touched Go files.

Run focused packages:

billing/entitlement package
evaluationsuite
execution lifecycle package
environment
qualification planner
httpapi
audit if touched

plus Stage-33 reliability and Stage-34 trajectory regressions.

Do not run go test ./...

Do not start Docker/Kubernetes unless an explicitly bounded database
integration check is required and local runtime is available.

Frontend:

npx tsc --noEmit

one production build if practical.

==================================================
39. DO NOT IMPLEMENT
==================================================

No:

home-grown card processing
tax/VAT engine
invoice PDF generator
accounting system
complex proration
annual pricing unless already specified
unfinalized overage pricing
AWS/GCP cost pass-through
production EKS provisioning
new evaluation engine
LLM billing decisions
full governance redesign
CI release gates

==================================================
DEFINITION OF DONE
==================================================

Evalplane has a server-enforced commercial-control layer.

A workspace has a deterministic plan and entitlement state.

Expensive evaluation work reserves allowance before execution.

Valid evaluations settle billable usage.

Platform-invalid evaluations do not consume committed agent-evaluation usage.

Usage changes are immutable, idempotent and auditable.

RBAC and billing remain separate gates.

Environment fidelity can be commercially restricted without violating required
technical isolation.

Customers can inspect plan, entitlements and usage safely.

Payment-provider state, where implemented, feeds Evalplane's internal billing
domain rather than becoming the execution authorization model.

No customer can make themselves paid, grant themselves credits, or bypass
limits through API input.

==================================================
COMPLETION REPORT
==================================================

Report:

1. files changed
2. migration/schema
3. billing/catalog contract versions
4. plan catalog
5. workspace billing account model
6. billing states
7. entitlement model
8. plan entitlement matrix
9. usage/meter model
10. ledger model
11. credit/allowance model
12. reservation lifecycle
13. settlement lifecycle
14. platform-invalid handling
15. valid agent failure billing
16. abandoned/canceled handling
17. idempotency/concurrency
18. execution integration
19. environment entitlement integration
20. qualification planner relationship
21. provider abstraction
22. payment-provider implementation/status
23. provider event idempotency if applicable
24. API/RBAC
25. billing/usage frontend
26. pricing-page consistency
27. entitlement denial UX
28. audit integration
29. security/secrets
30. focused backend tests
31. Stage-32 regression
32. Stage-33 regression
33. Stage-34 regression
34. TypeScript/build
35. confirmation client cannot self-upgrade
36. confirmation platform-invalid is not charged
37. confirmation valid agent failure is billable
38. confirmation no double charge
39. confirmation no technical-isolation downgrade
40. live payment/runtime limitations
41. deferred work

Write to:

prompts/reports/billing-entitlements.md

DO NOT overwrite this task file.
