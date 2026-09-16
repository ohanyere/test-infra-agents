EVALPLANE — STAGE 36
COMPLETE CUSTOMER FRONTEND

Read AGENTS.md first.

Read the existing frontend and all backend APIs/contracts from Stages 23–35.

Do not redesign backend architecture unless a genuine frontend-blocking API gap is
proven.

==================================================
GOAL
==================================================

Turn the existing collection of Evalplane frontend pages into one coherent
customer product.

The product journey is:

workspace
→ connect agent
→ create/select AgentVersion
→ declare capability claims
→ inspect qualification readiness
→ build Qualification Suite
→ start evaluation
→ inspect trials
→ inspect trajectory/evidence
→ inspect reliability
→ inspect qualification status
→ compare AgentVersions/regressions
→ inspect billing/usage

The frontend must reflect real backend state.

No fake dashboards.
No fabricated activity.
No static qualification results.
No fake runs.
No fake billing state.

==================================================
1. PRODUCT LANGUAGE
==================================================

Use customer-facing terminology consistently:

Scenario                    → Evaluation Task
Scenario family             → Task Family
Run                         → Trial
Evaluation Suite            → Qualification Suite
events/actions              → Trajectory
final state                 → Outcome
capability competency       → Capability
claims                      → Capability Claims
policy result               → Qualification
version comparison          → Regression

Do not gratuitously rename backend types/routes if compatibility does not
require it.

UI language may differ from backend domain identifiers.

==================================================
2. INFORMATION ARCHITECTURE
==================================================

Audit the entire existing navigation.

Prefer a compact product navigation such as:

Overview
Agents
Qualification
Tasks
Activity
Billing

Settings where appropriate.

Do not preserve obsolete navigation merely because pages already exist.

Avoid an oversized sidebar.

==================================================
3. WORKSPACE OVERVIEW
==================================================

Build a truthful overview from existing APIs.

Show useful product state such as:

connected agents
AgentVersions
qualification status
recent Qualification Suites
recent Trials
current regressions/issues
usage state

Only show metrics the backend actually provides.

No invented trend charts.

No vanity KPI cards.

==================================================
4. AGENTS DIRECTORY
==================================================

Replace remaining mock/static agent data with backend data.

Show:

agent
current/default version where appropriate
connection state
claimed capability count
qualification state where derivable
last meaningful activity

Support search/filter only when backed by actual fields.

==================================================
5. AGENT DETAIL
==================================================

Make Agent detail the primary qualification workspace.

Sections should coherently expose:

Overview
Versions
Capability Claims
Qualification
Reliability
Regressions
Connection

Reuse existing APIs from Stages 23, 26, 32, 33 and later stages.

Do not duplicate backend-derived qualification logic in React.

==================================================
6. AGENT VERSION
==================================================

AgentVersion identity must be explicit throughout qualification.

Show:

version
protocol/runtime
connection information safe for display
claims
qualification coverage
reliability
regression comparisons

Do not silently aggregate multiple versions as if they were one agent.

==================================================
7. CAPABILITY CLAIMS
==================================================

Display:

CLAIMED
TESTED
QUALIFIED

with exact semantics.

CLAIMED:
AgentVersion declares the capability.

TESTED:
Required independent task coverage has been exercised.

QUALIFIED:
Qualification policy has passed.

Never imply CLAIMED means tested.

Never imply TESTED means qualified.

==================================================
8. QUALIFICATION READINESS
==================================================

Use Stage-32 planner APIs.

For each capability show:

required coverage
available certified tasks
technical blockers
environment blockers
commercial blockers if available
plan state:
ready
partial
blocked

Make blockers understandable without hiding their machine truth.

Do not fabricate readiness client-side.

==================================================
9. BUILD QUALIFICATION SUITE
==================================================

Provide a clean flow:

choose AgentVersion
→ inspect claims/readiness
→ generate qualification plan
→ inspect selected task coverage
→ build immutable Qualification Suite

Clearly separate:

plan
suite
execution

Building a suite does NOT mean it has run.

==================================================
10. QUALIFICATION SUITE DETAIL
==================================================

Show immutable suite/version information:

AgentVersion
capability scope
task families
tasks
certification state
environment requirements
trial requirements
created timestamp
contract/version information where useful

Primary CTA:

Start evaluation

only when technically and commercially allowed.

==================================================
11. EXECUTION START
==================================================

Starting execution must use real Stage-35 server enforcement.

Display structured errors:

insufficient_credits
plan_limit_reached
billing_past_due
environment_tier_not_entitled
entitlement_denied

Do not reproduce billing logic client-side.

==================================================
12. ACTIVITY / EXECUTIONS
==================================================

Create a coherent place for:

Qualification Suite executions
Trials

Avoid confusing SuiteExecution with individual Trial.

Show meaningful state:

queued
provisioning
running
evaluating
completed
failed
platform-invalid
canceled

Use actual backend vocabulary mapped carefully to customer labels.

==================================================
13. TRIAL DETAIL
==================================================

Unify Trial detail around:

task
AgentVersion
environment
status
Outcome
Trajectory
evaluation
validity
reliability context

Link to the Stage-34 trajectory/evidence explorer.

Do not duplicate the trajectory implementation.

==================================================
14. TRAJECTORY
==================================================

Polish existing Stage-34 trajectory UI.

Clearly distinguish:

agent-visible evidence
agent-requested action
authorized action
executed action
failed/denied action
evaluator/platform evidence
recovery
Outcome

Requested must never visually imply executed.

Internal evaluator evidence must never appear as evidence delivered to the
agent.

Preserve redaction.

==================================================
15. OUTCOME
==================================================

Outcome should explain objective result without using an LLM-generated
narrative.

Show deterministic findings:

passed
failed
inconclusive
platform-invalid

and relevant criterion results/evidence.

Do not invent reasoning.

==================================================
16. RELIABILITY
==================================================

Use Stage-33 APIs.

Show metrics with explanation:

valid attempts
successes
decisive pass rate where applicable
valid success rate
pass@k
pass^k
variant diversity
scenario/task-family breadth
saturation

Do not show unavailable k values as zero.

Clearly separate:

retries
variant diversity
independent task-family breadth

==================================================
17. QUALIFICATION
==================================================

Create a qualification view centered on capabilities.

For every claimed capability expose:

CLAIMED
TESTED
QUALIFIED

with evidence linking into:

tasks
trials
reliability
policy result

If qualification-policy integration is not yet implemented for a particular
surface, say so truthfully rather than synthesizing it.

==================================================
18. REGRESSION
==================================================

Expose AgentVersion comparison using existing Stage-21/22/33 data.

Compare:

baseline AgentVersion
candidate AgentVersion

Show factual deltas:

coverage
qualification state
reliability metrics
new failures
resolved failures
comparability blockers

Do not generate a subjective winner.

==================================================
19. TASK CATALOG
==================================================

Replace old/static scenario catalog behavior with backend qualification-task
data.

Show:

task name
task family
capabilities
case type
environment requirement
certification state
runnable/unavailable state

Clearly distinguish:

certified
uncertified/stale
runtime unavailable

Do not expose reference remediation or hidden variants.

==================================================
20. TASK DETAIL
==================================================

Expose safe customer-visible task definition:

problem
environment
observable conditions
success/failure criteria
allowed tools
forbidden behavior where customer-visible
certification/runnability

Never expose:

hidden variant configuration
reference remediation
evaluator-only ground truth
secret grading data

==================================================
21. EMPTY STATES
==================================================

Design real empty states.

Examples:

No agents:
Connect your first infrastructure agent.

No claims:
Declare what this AgentVersion is intended to operate.

No suite:
Build a Qualification Suite from its capability claims.

No execution:
Run the suite to collect qualification evidence.

No reliability yet:
Reliability requires valid repeated trials.

Avoid generic "No data" everywhere.

==================================================
22. ONBOARDING
==================================================

Create a lightweight first-run path:

Create/use workspace
→ Connect Agent
→ Test connection
→ create/version AgentVersion
→ Capability Claims
→ qualification readiness

Do not introduce a giant wizard framework.

Allow customers to leave and resume.

==================================================
23. CONNECTION UX
==================================================

Use Stage-23 connection APIs.

Support truthful protocol/config status.

Do not display encrypted credentials.

No secrets should reappear after submission.

Connection testing must clearly distinguish:

configuration saved

from:

connection successfully tested.

==================================================
24. MCP
==================================================

Expose MCP as an agent/tool connection capability where useful.

Do not imply live external MCP proof when Stage 24 only has focused
verification.

Represent actual connection/test state truthfully.

==================================================
25. INCIDENT INTEGRATIONS
==================================================

PagerDuty/Datadog integrations may appear in connection/integration surfaces.

Do not advertise them as live-connected unless credentials/test state prove
that.

No raw provider payloads.

==================================================
26. BILLING
==================================================

Integrate the Stage-35 billing page naturally.

Execution CTA should provide useful recovery paths for commercial denial.

Examples:

Insufficient credits → Billing/Usage
Environment tier unavailable → plan explanation
Past due → Manage billing when available

Do not fake checkout availability.

==================================================
27. PRICING
==================================================

Preserve current approved pricing/catalog.

Ensure terminology and allowances agree with Stage-35 server catalog.

Do not invent new economics.

==================================================
28. SETTINGS
==================================================

Keep settings focused.

Potential sections:

Workspace
Members/access
Agent integrations
Billing

Only expose settings with real backend functionality.

Do not build ornamental settings pages.

==================================================
29. ROUTING
==================================================

Audit every current route.

Categorize:

keep
rename UI label only
redirect
remove obsolete mock route
complete missing index route

Important:

Earlier `/runs` had no index page.

Provide a coherent Activity/Trials index rather than leaving dead navigation.

Preserve deep links where practical.

==================================================
30. API CLIENT
==================================================

Audit `lib/api.ts`.

Remove frontend reliance on mock/static qualification data.

Centralize typed API access.

Do not duplicate the same DTO under multiple names unnecessarily.

Preserve structured API errors for entitlement/auth/runtime failures.

==================================================
31. LOADING / ERROR STATES
==================================================

Every important backend-backed surface needs:

loading
empty
success
error

Do not convert network/backend failure into an empty state.

Show useful error messages without exposing internal stack traces.

==================================================
32. DESIGN
==================================================

Maintain the established Evalplane visual direction:

quiet
precise
infrastructure-native
neutral
Geist Sans/Mono

Avoid:

gradients
glass
orbs
bento-card marketing UI
fake terminals
oversized dashboard cards
radar charts
decorative charts
huge amounts of boxed content

Use:

typography
spacing
borders/separators
tables/lists
compact status indicators
dense-but-readable technical information

==================================================
33. ACCESSIBILITY
==================================================

Use semantic HTML.

Keyboard navigation.

Visible focus.

Proper labels.

ARIA only where needed.

Status must not rely solely on color.

==================================================
34. RESPONSIVENESS
==================================================

Desktop is primary for operational workflows, but pages must remain usable at
tablet/mobile widths.

Do not force complex tables beyond viewport without deliberate overflow.

==================================================
35. SECURITY
==================================================

Never render:

credential ciphertext
connection secrets
provider secrets
hidden variants
reference remediation
evaluator-only ground truth
raw payment secrets

Never trust client-derived:

qualification
billing status
entitlement
usage
trial outcome

==================================================
36. DELETE/REMOVE MOCK DATA
==================================================

Search the frontend for:

mock
fixture
hard-coded fake Agent
hard-coded fake run
hard-coded fake qualification
static scenario catalog

Remove or isolate development-only examples.

Production customer routes must be backend-driven.

Do not delete backend test fixtures.

==================================================
37. PRODUCT COPY
==================================================

Keep the core message:

Qualify infrastructure agents before they reach production.

Supporting idea:

Connect an infrastructure agent, test its claimed capabilities against realistic
infrastructure tasks, and get reproducible evidence of what it can actually be
trusted to operate.

Do not reposition Evalplane as:

generic AI eval platform
AI observability platform
AI SRE
agent marketplace
generic control plane

==================================================
38. VERIFICATION
==================================================

Run:

npx tsc --noEmit

npm run build

Run focused backend API tests only if frontend work requires backend changes.

Do not run go test ./... merely for frontend work.

Do not start Docker/Kubernetes.

==================================================
39. MANUAL ROUTE AUDIT
==================================================

At completion report every customer route and categorize:

working backend-backed
working static marketing
redirected
intentionally unavailable
removed obsolete/mock

Report broken/unimplemented routes honestly.

==================================================
40. DO NOT IMPLEMENT
==================================================

No:

new billing engine
new qualification engine
new reliability formulas
new trajectory semantics
new environment provider
Stripe
production Kubernetes
governance redesign
release CI gates
LLM-generated evaluation explanation

Those belong to existing/later stages.

==================================================
DEFINITION OF DONE
==================================================

A new customer can understand and navigate this lifecycle:

connect agent
→ declare capabilities
→ build qualification suite
→ start evaluation
→ inspect Trials
→ inspect trajectory/evidence
→ understand reliability
→ understand qualification
→ compare regressions
→ understand usage/billing

without relying on mock data or knowledge of Evalplane's internal backend
architecture.

All qualification, billing, reliability and outcome truth comes from server
contracts.

The product feels like one system rather than a collection of development-stage
screens.

==================================================
REPORT
==================================================

Write:

prompts/reports/complete-customer-frontend.md

Include:

1. frontend architecture
2. navigation
3. route inventory
4. terminology mapping
5. onboarding flow
6. agent directory/detail
7. AgentVersion UX
8. capability claims UX
9. qualification readiness
10. suite planning/build
11. suite detail
12. execution start
13. activity/executions
14. Trial detail
15. trajectory/evidence integration
16. Outcome
17. reliability
18. qualification
19. regression
20. task catalog/detail
21. empty states
22. connection/MCP/integrations
23. billing integration
24. settings
25. API client changes
26. mock/static removal
27. security/leakage review
28. accessibility
29. responsive behavior
30. TypeScript result
31. production build
32. backend tests if changed
33. all customer routes and status
34. known frontend gaps
35. screenshots/manual verification limitations
36. deferred work

Do not overwrite this task file.
