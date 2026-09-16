EVALPLANE — STAGE 37
CI, API AUTOMATION & RELEASE GATES

Read AGENTS.md first.

Read the repository state and the completion reports for Stages 23–36 before
changing CI.

This stage creates deterministic automated release gates around the product
already implemented.

Do not redesign application architecture.

==================================================
GOAL
==================================================

A pull request or release candidate must automatically prove that the critical
Evalplane contracts still hold.

The automation must protect:

- Go backend compilation/tests
- API contracts
- migrations
- frontend type safety/build
- qualification semantics
- reliability semantics
- trajectory integrity
- billing integrity
- workspace isolation/auth boundaries
- secret/leakage boundaries
- route integrity

CI should produce actionable failures.

Do not create one opaque giant test job.

==================================================
1. AUDIT EXISTING AUTOMATION
==================================================

Inspect:

.github/
package.json
backend/go.mod
backend Makefile/scripts if present
migration tests
frontend scripts
existing CI workflows
existing validation scripts
AGENTS.md

Report what already exists before replacing anything.

Reuse working automation where practical.

Do not duplicate equivalent jobs.

==================================================
2. RELEASE-GATE MODEL
==================================================

Define explicit release gates.

Suggested logical gates:

1. Repository / static integrity
2. Backend core
3. API/security contracts
4. Database migrations
5. Qualification contracts
6. Reliability/evidence contracts
7. Billing contracts
8. Frontend
9. Final release gate

Names may differ if repository conventions are stronger.

The final gate should depend on all required gates rather than rerunning them.

==================================================
3. NO MONOLITHIC GO TEST GATE
==================================================

Do not make:

go test ./...

the only meaningful backend signal.

Use focused package groups so failures identify the affected contract.

A full Go suite may exist as an optional/additional job if affordable, but the
required release gates must remain understandable independently.

==================================================
4. GO RESOURCE CONTROL
==================================================

Keep Go CI reasonably resource-conscious.

Use caching.

Locally documented verification should remain compatible with:

GOMAXPROCS=2
go test -p 1

where appropriate.

Do not introduce needless parallel compilation pressure.

GitHub-hosted CI may use normal runner resources where safe.

==================================================
5. REPOSITORY INTEGRITY GATE
==================================================

Automate inexpensive checks such as:

- gofmt verification for Go files
- go vet on critical touched packages or appropriate backend scope
- migration numbering uniqueness/order
- task/report file integrity where sensible
- no accidentally committed generated secrets
- frontend typecheck

Do not invent brittle grep rules that constantly false-positive.

==================================================
6. BACKEND CORE GATE
==================================================

Run focused tests covering foundational packages required by the API/runtime.

Inspect actual package graph first.

Likely areas include:

auth
workspace/membership
agents/AgentVersion
execution
executionworker
environment
evaluationsuite

Use actual package names.

Do not refer to nonexistent packages.

==================================================
7. QUALIFICATION GATE
==================================================

Protect Stages 26–32.

At minimum cover the actual packages responsible for:

capability catalog/claims
task-family coverage
scenario certification
environment scheduling
Kubernetes/SRE catalog
GitOps qualification catalog
qualification planner

Critical invariants include:

CLAIMED != TESTED != QUALIFIED

platform-invalid trials do not create valid capability coverage

variant repetition is not independent task-family breadth

uncertified/unrunnable tasks do not become silently eligible

technical isolation requirements cannot be weakened

planner does not invent qualification

Use existing tests rather than recreating domain logic in scripts.

==================================================
8. RELIABILITY / EVIDENCE GATE
==================================================

Protect Stages 33–34.

Run the actual reliability and trajectory packages.

Ensure regressions catch:

pass@k behavior
pass^k behavior
valid vs decisive rates
platform-invalid exclusion
semantic environment comparability
worker recovery != service recovery
requested != executed
platform classification is persisted-evidence-driven
agent-visible != evaluator view
suite summary cannot silently omit Trials

Do not duplicate formulas in CI shell code.

==================================================
9. BILLING GATE
==================================================

Protect Stage 35.

Run billing plus the narrow execution/environment integration packages needed
to prove:

reservation before expensive execution
valid PASS commits
valid agent FAIL commits
valid agent timeout commits where existing semantics say so
platform-invalid releases/refunds
platform failures do not consume committed evaluation usage
idempotent settlement
no double charge on worker retry/recovery
commercial isolation cannot downgrade technical requirements
client cannot self-upgrade plan/status/balance

Use application tests, not shell reimplementations.

==================================================
10. API CONTRACT GATE
==================================================

Create/strengthen automated HTTP/API contract tests using the existing Go test
infrastructure.

Prefer httptest/in-process APIs where suitable.

Cover important boundaries including:

authentication required where expected
workspace isolation
viewer/member/admin/owner write boundaries
AgentVersion immutability
connection secret non-return
hidden/evaluator data non-return
qualification plan API
suite execution start
structured billing denial codes
trajectory/evidence safe projection
billing APIs
webhook authentication boundary

Do not require Kubernetes for ordinary API tests.

==================================================
11. API ERROR CONTRACTS
==================================================

Protect machine-readable error codes used by the Stage-36 frontend.

At minimum ensure stable behavior for applicable codes such as:

entitlement_denied
plan_limit_reached
insufficient_credits
billing_past_due
environment_tier_not_entitled

Also preserve existing auth/conflict/not-found semantics.

Do not snapshot entire error strings if codes are the stable contract.

==================================================
12. MIGRATION GATE
==================================================

Protect migration ordering and reversibility.

Current latest migration is 000020 unless repository inspection proves
otherwise.

Add automated validation for:

unique migration numbers
matching up/down files
ordered application
fresh-database apply

If practical in GitHub Actions, use a temporary PostgreSQL service to perform
a real migration round-trip.

This is a good place to improve the Stage-35 live-DB migration gap.

The migration job must operate only on an ephemeral CI database.

Never point tests at developer or production databases.

==================================================
13. MIGRATION ROUND-TRIP
==================================================

If existing migration tooling supports it safely:

fresh database
→ apply all migrations
→ run migration integration assertions
→ down/up boundary test where supported

Do not invent unsafe destructive migration tooling.

If full down-all is not supported by repository conventions, test the strongest
safe existing round-trip and document the limitation.

==================================================
14. DATABASE INTEGRATION
==================================================

Use a CI PostgreSQL service only for tests that genuinely require SQL
constraints/transactions.

Prioritize proving:

workspace isolation
billing idempotency constraints
billing reservation/settlement concurrency assumptions
provider-event uniqueness
critical migration constraints

Do not convert the entire test suite into DB integration tests.

==================================================
15. BILLING CONCURRENCY CHECK
==================================================

If the repository already has appropriate repository-level integration hooks,
add a bounded live-Postgres test proving concurrent reservation cannot consume
the same final allowance twice.

Also prove duplicate settlement cannot create duplicate committed usage.

Do not implement a new billing subsystem to obtain this test.

If the current architecture cannot test this without broad redesign, document
the gap instead of faking it.

==================================================
16. FRONTEND GATE
==================================================

Required:

npx tsc --noEmit
npm run build

Use dependency caching.

Do not treat a successful development server startup as a release gate.

Do not start a browser merely for compile verification.

==================================================
17. FRONTEND ROUTE INTEGRITY
==================================================

Protect the important Stage-36 route tree.

Do not hardcode a fragile exact total unless the repository deliberately
exposes a route manifest.

Verify critical routes/build outputs such as:

workspace
workspace/settings
workspace/billing
workspace/tasks
workspace/tasks/[id]
agents
agents/new
agents/[id]
qualification
qualification/suites/[id]
runs
runs/[id]
runs/[id]/trajectory
suite-executions/[id]
comparisons/[id]

Public routes may also be checked where useful.

The purpose is detecting accidental route disappearance, not pinning Next.js
internal output formatting.

==================================================
18. MOCK-DATA REGRESSION
==================================================

Protect Stage-36 removal of production customer mocks.

Implement a maintainable check or test ensuring production customer routes do
not re-import deleted mock modules/fixtures.

Do NOT ban the word "mock" repository-wide.

Backend tests and development fixtures are legitimate.

Scope the check narrowly to customer production routes/imports.

==================================================
19. SECRET / LEAKAGE AUTOMATION
==================================================

Use existing tests where possible to ensure APIs do not expose:

credential ciphertext
stored tokens
provider secrets
hidden variant config
reference remediation
evaluator-only ground truth
payment secrets

Do not add a home-grown high-noise secret scanner if the repository already has
a suitable tool/workflow.

If adding a standard secret scanner is lightweight and appropriate, pin its
version.

Never print CI secrets.

==================================================
20. DEPENDENCY / SUPPLY-CHAIN CHECKS
==================================================

Inspect existing dependencies and workflow policy.

Add only lightweight, reproducible checks appropriate for this repository.

Possible checks:

go mod verify
npm lockfile-respecting install
npm audit only if project policy already treats it as actionable

Do not make noisy ecosystem advisory output an unconditional release blocker
without a clear severity policy.

Do not auto-update dependencies in this stage.

==================================================
21. INSTALL REPRODUCIBILITY
==================================================

Frontend CI should use the lockfile-native deterministic installation command
appropriate to the repo, normally:

npm ci

if package-lock.json exists and is authoritative.

Do not use `npm install` in release CI where deterministic lockfile install is
available.

==================================================
22. WORKFLOW SECURITY
==================================================

GitHub Actions permissions should use least privilege.

Default to read-only repository contents unless a job genuinely needs more.

Do not expose secrets to pull requests from untrusted forks.

Do not use `pull_request_target` for running untrusted PR code with secrets.

Pin third-party actions to an appropriate stable version or immutable commit
according to repository policy.

==================================================
23. WEBHOOK TESTING
==================================================

Provider webhook tests must use deterministic test secrets.

Prove:

invalid/missing signature rejected
valid signed event accepted
duplicate provider event idempotent
stale event does not overwrite newer billing truth

No real payment provider required.

==================================================
24. EXTERNAL INTEGRATIONS
==================================================

Do not require live:

Stripe
PagerDuty
Datadog
MCP server
AWS
EKS
Kubernetes
ArgoCD

for normal PR gates.

These require credentials/runtime and belong in explicitly separate integration
or release environments later.

==================================================
25. KUBERNETES TESTS
==================================================

Do not make Kind/Kubernetes a normal Stage-37 PR requirement.

Stage 17 already demonstrated why local/runtime infrastructure can be expensive
and environment-dependent.

If an existing Kubernetes workflow exists, preserve it as optional/manual unless
there is a strong existing reason otherwise.

Production runtime qualification remains later-stage work.

==================================================
26. CI TRIGGERS
==================================================

Use sensible triggers.

Required validation should run for pull requests and relevant pushes.

Use path filtering only if it cannot accidentally skip a required cross-layer
gate.

Avoid elaborate trigger logic that makes CI correctness difficult to reason
about.

==================================================
27. CONCURRENCY / CANCELLATION
==================================================

Use GitHub Actions concurrency so superseded runs on the same branch/PR may be
canceled where safe.

Do not cancel an already-running release deployment because of unrelated CI
unless explicitly designed.

This stage is validation, not production deployment.

==================================================
28. TIMEOUTS
==================================================

Set reasonable job timeouts.

A hung test must not consume CI indefinitely.

Do not use extremely aggressive timeouts that make legitimate builds flaky.

==================================================
29. CACHING
==================================================

Use supported caching for:

Go modules/build cache
npm

Cache keys must include relevant lock/module files.

Caches are optimization only.

A cache miss must not break CI.

==================================================
30. ARTIFACTS
==================================================

Do not upload huge build directories unnecessarily.

On failure, useful small diagnostics may be preserved if appropriate.

Do not upload secret-containing environment dumps.

==================================================
31. FINAL RELEASE GATE
==================================================

Create one clearly named aggregate required job, for example:

release-gate

It must depend on all required validation jobs.

It should fail when any mandatory contract gate fails.

It should not rerun the whole suite.

This gives repository branch protection one stable required check name.

==================================================
32. RELEASE GATE OUTPUT
==================================================

The aggregate gate should make it clear which dependency failed through normal
GitHub Actions dependency status.

Do not fabricate a passing final gate when a required dependency was skipped
unexpectedly.

Be careful with `if: always()` semantics.

A required skipped/failed dependency must not turn green accidentally.

==================================================
33. LOCAL VERIFICATION SCRIPT
==================================================

If appropriate, create a small documented local script/Make target that runs the
same inexpensive release-critical checks developers can reasonably execute.

Do not require Docker/Kubernetes locally.

Keep heavy DB integration CI-specific if necessary.

Avoid duplicating workflow logic in a giant shell script.

==================================================
34. DOCUMENTATION
==================================================

Document:

required CI gates
what each protects
which tests require PostgreSQL
which integrations are intentionally not tested live
how to run the focused checks locally
what the final release-gate means

Do not claim production readiness merely because CI is green.

==================================================
35. STATUS TERMINOLOGY
==================================================

CI passing means focused automated verification.

It does NOT automatically mean:

E2E-PROVEN
PRODUCT-READY

Preserve the project maturity vocabulary.

==================================================
36. DO NOT IMPLEMENT
==================================================

No:

production deployment workflow
Terraform apply
EKS creation
live Stripe
live PagerDuty/Datadog
new qualification engine
new billing engine
new frontend redesign
new governance system
LLM-based test judging
automatic production release

==================================================
37. VERIFICATION
==================================================

After implementation, run the relevant new checks locally where possible.

At minimum:

frontend typecheck
frontend build
new CI/static validation scripts
focused Go packages touched by Stage 37

Do not run Docker/Kubernetes.

A live PostgreSQL migration/concurrency test may be left for GitHub Actions if
local scratch Postgres is unavailable.

Do not claim it passed locally when it did not run.

==================================================
DEFINITION OF DONE
==================================================

A pull request has deterministic automated gates for the critical Evalplane
contracts.

Failures identify the domain that regressed.

API security and workspace boundaries have automated coverage.

Qualification, reliability, trajectory and billing semantics are protected.

Frontend typecheck/build and critical route integrity are protected.

Migrations receive automated validation and, where safely implemented, a real
ephemeral PostgreSQL round-trip in CI.

External vendors and Kubernetes are not required for ordinary PR validation.

One stable aggregate release-gate can be used by branch protection.

Green CI means FOCUSED-VERIFIED automation, not Product Ready.

==================================================
COMPLETION REPORT
==================================================

Write:

prompts/reports/ci-api-release-gates.md

Report:

1. existing automation audit
2. workflows added/changed
3. release gate architecture
4. repository/static gate
5. backend core gate
6. qualification gate
7. reliability/trajectory gate
8. billing gate
9. API contract gate
10. auth/workspace-isolation coverage
11. API error-code coverage
12. migration validation
13. live PostgreSQL CI setup/status
14. migration round-trip
15. billing concurrency DB test/status
16. frontend gate
17. route integrity
18. mock-data regression protection
19. secret/leakage protection
20. dependency/install reproducibility
21. workflow permissions/security
22. CI triggers/concurrency/timeouts
23. caching
24. final aggregate release-gate behavior
25. local verification workflow
26. documentation
27. tests run locally
28. checks only runnable in CI
29. known gaps
30. confirmation no Kubernetes/live external provider dependency
31. confirmation green CI != E2E/Product Ready

Do not overwrite this task file.
