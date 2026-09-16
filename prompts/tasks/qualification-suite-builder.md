EVALPLANE — AUTOMATIC QUALIFICATION SUITE BUILDER

Read AGENTS.md first.

Inspect the existing implementation for:

- Agent / AgentVersion
- competency capability claims
- runtime/tool capabilities
- Stage-27 capability coverage requirements
- Task Families / Case Types
- Stage-30 srequal catalog
- Stage-31 gitopsqual catalog
- Scenario / ScenarioVersion persistence
- Scenario certification + authoritative eligibility
- environment requirements/profiles/providers/scheduler
- EvaluationSuite / SuiteVersion / SuiteExecution
- hidden variants
- qualification policies
- AgentVersion regression
- reproducibility manifests
- task/catalog APIs

Do not broadly refactor the repository.

==================================================
PURPOSE
==================================================

A customer should not manually determine which Evaluation Tasks are required
to qualify an infrastructure agent.

Given an exact AgentVersion and its claimed capabilities, Evalplane should
deterministically answer:

"What must be tested to determine whether these claims can be qualified?"

Then, when sufficient authoritative tasks and environments exist, Evalplane
should create an immutable EvaluationSuite/SuiteVersion containing the exact
required task versions and trial counts.

Core chain:

AgentVersion
→ claimed capabilities
→ coverage contract
→ required Task Families/case types
→ eligible Evaluation Tasks
→ certification eligibility
→ environment executability
→ deterministic task selection
→ immutable Qualification Suite
→ existing SuiteExecution
→ existing QualificationPolicy

Do NOT create a parallel evaluation engine.

==================================================
1. QUALIFICATION PLAN FIRST
==================================================

Introduce a QualificationPlan or equivalent server-derived planning object.

A plan must be generated before suite creation.

Suggested machine states:

ready
partial
blocked

READY:
All requested capability claims have sufficient authoritative task coverage
available to construct an executable qualification suite.

PARTIAL:
Some capability coverage can be constructed, but one or more requirements
cannot currently be satisfied.

BLOCKED:
No authoritative executable qualification suite can currently be created.

Do not call PARTIAL or BLOCKED plans qualified.

==================================================
2. INPUT SCOPE
==================================================

Primary V1 input:

exact AgentVersion

Optional:

subset of capability IDs that are actually claimed by that AgentVersion.

Do not allow qualification of arbitrary capabilities the AgentVersion did not
claim.

If no subset is supplied:

use all declared competency capability claims.

An AgentVersion with zero claims should return a structured no-claims result,
not fabricate capabilities from tool permissions.

==================================================
3. CLAIMS ARE THE SOURCE
==================================================

Use Stage-26 competency claims.

Do not infer claims from:

tool permissions
MCP tools
agent name
marketing metadata
scenario compatibility

A tool capability remains distinct from a competency claim.

==================================================
4. COVERAGE CONTRACT VERSIONING
==================================================

Inspect Stage-27:

capability-coverage-contract:v1

Stage 31 intentionally added GitOps/delivery task families without publishing
new coverage requirements.

Do NOT silently mutate v1 semantics.

Introduce a new versioned coverage contract if required.

Preferred direction:

capability-coverage-contract:v2

v2 may add requirements for newly supported GitOps and delivery competencies
while preserving the meaning of all existing v1 requirements.

Historical v1 coverage assessments must remain reproducible.

Do not rewrite existing v1 assessment rows.

==================================================
5. V2 GITOPS / DELIVERY COVERAGE REQUIREMENTS
==================================================

Publish conservative requirements only where the current task corpus genuinely
has independent families/cases.

Candidates include:

gitops.drift.detection
gitops.drift.remediation
gitops.desired_state.diagnosis
gitops.desired_state.remediation
gitops.argocd.diagnosis
gitops.argocd.recovery
delivery.rollout.diagnosis
delivery.rollout.recovery
delivery.progressive_delivery.diagnosis
delivery.progressive_delivery.recovery

Do NOT make every capability TESTED merely because Stage 31 has tasks.

Requirements should still demand:

independent Task Family breadth
minimum valid trials
contrast/negative/decoy cases where appropriate

If the corpus is insufficient:

leave the capability requirement unavailable or unsatisfied.

Do not weaken requirements for convenience.

==================================================
6. TASK ELIGIBILITY
==================================================

An Evaluation Task may satisfy an authoritative QualificationPlan only when:

- it belongs to the required Task Family
- its case type satisfies the coverage requirement
- the exact ScenarioVersion is fresh-certified for authoritative evidence
- its environment requirements resolve to an available server-owned profile/provider
- its runtime contract is compatible
- it is not stale/failed/uncertified
- it is runnable in the requested qualification context

Fake certification from srequal/gitopsqual unit fixtures does NOT count as
authoritative Stage-28 certification.

A structurally validated catalog task that lacks real certification must
produce a plan blocker.

==================================================
7. AUTHORITATIVE VS STRUCTURAL TRUTH
==================================================

This distinction is mandatory.

Catalog metadata may say:

focused certification validation passed

That does NOT mean:

authoritative certification eligible.

Use the actual Stage-28 certification eligibility service/records.

Do not trust a catalog boolean that bypasses certification persistence.

==================================================
8. ENVIRONMENT ELIGIBILITY
==================================================

Use Stage-29 scheduler truth.

A task requiring:

gitops.argocd:v1

must remain unavailable while that profile/provider is planned.

A task requiring dedicated Kubernetes must remain unavailable while no
dedicated provider exists.

Do not downgrade fidelity.

Do not choose Kind merely because it is the only active provider.

==================================================
9. BLOCKER CODES
==================================================

Return structured machine-readable blockers.

Suggested codes:

no_capability_claims
coverage_requirement_unavailable
required_task_family_unavailable
required_case_type_unavailable
task_not_certified
task_certification_stale
environment_unavailable
runtime_contract_unsupported
agent_runtime_capability_incompatible
insufficient_independent_families
insufficient_valid_task_candidates
qualification_policy_unavailable

Use repository naming conventions.

Avoid prose-only planner failures.

==================================================
10. TASK SELECTION
==================================================

Selection must be deterministic.

For each capability:

coverage requirement
→ required families/case groups
→ eligible exact ScenarioVersions
→ deterministic minimal sufficient set

Stable tie-breaking should use explicit priority then stable IDs/content hashes.

Do not use an LLM to choose tasks.

Same:

AgentVersion
coverage contract
task catalog versions
certification state
environment availability

must produce the same plan.

==================================================
11. MINIMAL BUT SUFFICIENT
==================================================

Do not include every task in the catalog.

Select the smallest deterministic task set that satisfies all requested
capability coverage requirements while preserving:

family breadth
contrast requirements
case-type requirements
minimum trial counts

A single task may contribute evidence to more than one capability when its
metadata genuinely says so.

But one Task Family cannot masquerade as multiple independent families.

==================================================
12. TRIAL COUNT DERIVATION
==================================================

Use coverage requirements to derive minimum trial counts.

Do not invent arbitrary trial numbers in the frontend.

Example:

family requires 2 valid trials

SuiteVersion entry should request at least 2 trials for an appropriate exact
ScenarioVersion/task set.

Account for multiple tasks in one family deterministically.

Do not implement pass@k/pass^k here.

That belongs to Stage 33.

==================================================
13. NEGATIVE / DECOY / ADVERSARIAL COVERAGE
==================================================

If a capability coverage rule requires contrast evidence:

the plan must include suitable negative/decoy/adversarial/edge tasks.

Do not build suites consisting only of positive remediation cases.

Example:

capacity remediation qualification should include:

capacity-positive
+
a restraint/decoy case

when required by the coverage contract.

==================================================
14. BUILT-IN CATALOG → SCENARIOVERSION BRIDGE
==================================================

Inspect whether Stage-30/31 catalog tasks are currently only server-owned code
definitions or are already represented as immutable persisted ScenarioVersions.

Qualification SuiteVersion must ultimately reference exact ScenarioVersions.

Do NOT create a second suite item type for catalog tasks.

If a catalog task lacks a persisted ScenarioVersion:

implement the smallest deterministic materialization bridge.

Requirements:

- server-owned built-in task ID/version
- deterministic content hash
- idempotent materialization
- exact workspace ownership according to existing schema
- immutable ScenarioVersion
- task family/case/environment metadata preserved
- evaluator-only fields remain evaluator-only
- no duplicate ScenarioVersions on repeated plan/build
- reproducibility can identify originating built-in task contract/version

Do not silently modify an existing materialized version when the catalog
definition changes.

A changed definition must result in a new immutable version/contract identity.

==================================================
15. CERTIFICATION AFTER MATERIALIZATION
==================================================

Do NOT automatically label a newly materialized ScenarioVersion certified.

It must use the normal Stage-28 certification path.

Therefore planning may legitimately return:

task_not_certified

until authoritative certification exists.

This is correct behavior.

==================================================
16. QUALIFICATION SUITE
==================================================

When plan state is READY, allow creation of an immutable Qualification Suite
using the existing:

EvaluationSuite
SuiteVersion

model.

Do not create a second execution engine.

The created SuiteVersion must pin:

exact ScenarioVersion IDs
trial counts
stable ordering
planner contract/version
coverage contract/version
capability catalog version
task-family contract version
environment-profile contract version
selected capability IDs

Use existing metadata/reproducibility extension points where practical.

==================================================
17. QUALIFICATION SUITE IDENTITY
==================================================

A qualification-generated suite should be distinguishable from a manually
authored EvaluationSuite.

Suggested metadata:

suite purpose = qualification
planner contract = qualification-suite-planner:v1

Do not rename the existing EvaluationSuite database model unnecessarily.

Customer-facing wording may say:

Qualification Suite

while backend preserves existing architecture.

==================================================
18. PLANNER CONTRACT
==================================================

Introduce:

qualification-suite-planner:v1

Every generated plan/suite must pin it.

Historical suites must remain interpretable if planning logic changes later.

==================================================
19. PLAN REPRODUCIBILITY
==================================================

A persisted plan or build manifest should identify at minimum:

AgentVersion
selected capability claims
capability catalog version
coverage contract version
task-family contract version
planner contract version
candidate/selected ScenarioVersion IDs
certification eligibility identifiers
environment profile/provider requirements
trial counts
blocker codes
generated SuiteVersion ID when created

Do not rely on mutable current catalog state to explain an old plan.

==================================================
20. PLAN PERSISTENCE
==================================================

Prefer persisted QualificationPlan records.

Use the next migration number if persistence is required.

Plans should be inspectable later:

Why could/couldn't this agent be qualified?

Do not mutate historical completed plans.

If environment/certification availability changes:

generate a new plan.

==================================================
21. READY DOES NOT MEAN QUALIFIED
==================================================

READY means:

Evalplane can construct an authoritative qualification suite.

It does NOT mean:

the agent passed it.

Flow:

READY PLAN
→ Qualification Suite
→ SuiteExecution
→ valid trials/evidence
→ Stage-27 coverage assessment
→ Stage-22 QualificationPolicy
→ QUALIFIED / NOT_QUALIFIED / INCONCLUSIVE

Keep these states separate.

==================================================
22. EXISTING QUALIFICATION POLICY
==================================================

Do not replace Stage-22 QualificationPolicy.

The builder decides WHAT must be tested.

QualificationPolicy decides whether resulting evidence is sufficient to call
the AgentVersion qualified.

Do not encode pass/fail thresholds inside the suite planner unless they are
coverage-selection rules.

==================================================
23. EXECUTION
==================================================

Suite creation must NOT automatically execute unless the user explicitly asks
for execution through an existing workflow.

Keep:

plan
build suite
execute suite

as separate operations.

This improves auditability and prevents expensive accidental runs.

==================================================
24. COMPARISON / REGRESSION COMPATIBILITY
==================================================

Generated SuiteVersion must remain reusable by existing:

SuiteExecution
comparative report
AgentVersion regression

systems.

A comparison can later execute the same exact Qualification Suite against
multiple AgentVersions.

Do not implement a new comparison engine.

==================================================
25. MULTI-AGENT FAIRNESS
==================================================

V1 builder may build from one AgentVersion's claims.

Do not attempt complex automatic union/intersection planning across many agents
unless the existing architecture makes it trivial.

Existing comparison should still be able to execute an already-generated exact
suite against multiple compatible agents.

==================================================
26. TOOL/RUNTIME COMPATIBILITY
==================================================

Before marking a plan READY:

verify the AgentVersion's low-level runtime capabilities can satisfy the
selected tasks.

Reuse existing compatibility logic.

Do not automatically grant missing MCP/tool permissions.

Return structured blocker if incompatible.

==================================================
27. API
==================================================

Add authenticated workspace APIs consistent with existing routing.

Approximately:

POST
/v1/workspaces/{workspaceID}/agents/{agentID}/versions/{versionID}/qualification-plans

GET
/v1/workspaces/{workspaceID}/qualification-plans/{planID}

POST
/v1/workspaces/{workspaceID}/qualification-plans/{planID}/suite

Optional preview endpoint is acceptable if consistent with architecture.

Trigger/build:
write-capable roles.

Read:
workspace members/viewers according to existing conventions.

Users cannot submit:

state=ready
selected task IDs
certified=true
coverage result
qualification result

Those are server-derived.

==================================================
28. FRONTEND — AGENT DETAIL
==================================================

Add a focused:

Qualification

section.

Show approximately:

Claimed capabilities
Qualification readiness
Required task coverage
Blocking reasons
Qualification Suite when generated

Example:

Kubernetes capacity remediation
CLAIMED
Coverage requirement available
2 task families required
Environment available
Authoritative task certification missing
→ BLOCKED

Do not fake QUALIFIED.

==================================================
29. FRONTEND — BUILD FLOW
==================================================

Provide a restrained action:

Build qualification plan

Then show:

READY / PARTIAL / BLOCKED

If READY:

Create Qualification Suite

Do not automatically launch trials.

If blocked, explain exactly why.

Avoid giant cards/gauges/scores.

==================================================
30. GITOPS / DELIVERY CURRENT TRUTH
==================================================

At current repository state:

Stage-31 GitOps/delivery tasks are structurally validated but:

gitops.argocd:v1 remains planned
argo_rollouts provider/profile support is unavailable
no live authoritative certification exists

Therefore qualification plans containing those capabilities should normally be
PARTIAL/BLOCKED with truthful environment/certification blockers.

Do NOT manufacture READY.

This behavior is an acceptance requirement.

==================================================
31. KUBERNETES CURRENT TRUTH
==================================================

Stage-30 Kubernetes namespace tasks have an active development environment.

But fake structural certification is still not equivalent to authoritative
live-runtime certification.

If actual Stage-28 persisted authoritative certification does not exist:

planner must report task_not_certified rather than pretending READY.

Do not bypass this to demonstrate the feature.

==================================================
32. COVERAGE OUTPUT INTEGRATION
==================================================

Do not compute TESTED merely because the suite was generated.

Stage-27 coverage only changes after valid trial evidence exists.

Before execution:

test_status remains whatever existing evidence supports.

==================================================
33. SECURITY
==================================================

Planner responses may expose safe reasons and task metadata.

Never expose:

reference remediation
ground truth
root-cause evaluator metadata
hidden variant values
provider credentials
raw kubeconfig
certification sentinel values

Workspace isolation mandatory.

==================================================
34. TESTS
==================================================

Focused tests must cover at minimum:

planner contract/version
zero claims → structured blocked/no-claims
subset must belong to AgentVersion claims
unknown claim rejected
coverage v1 remains reproducible
coverage v2 version introduced if requirements expanded
GitOps/delivery requirements do not mutate historical v1 semantics
deterministic planning
same inputs → same selected tasks/order/trial counts
minimum sufficient task selection
family breadth preserved
negative/contrast requirement preserved
unavailable coverage requirement blocks
uncertified task blocks authoritative plan
stale certification blocks
unavailable environment blocks
GitOps claims currently not READY
dedicated-only task cannot downgrade
runtime/tool incompatibility blocks
no automatic permission grants
catalog→ScenarioVersion materialization idempotent if implemented
catalog definition change creates new immutable version
materialized task not auto-certified
READY plan can create existing SuiteVersion
PARTIAL/BLOCKED cannot create authoritative suite
suite pins exact ScenarioVersions/trial counts
suite purpose/planner version pinned
suite creation does not execute
existing SuiteExecution can consume generated suite
regression/comparison compatibility unchanged
workspace isolation
viewer cannot build
user cannot manually choose authoritative task IDs
user cannot set READY
planner never sets QUALIFIED
no evaluator-only leakage
no LLM selection/grading

==================================================
35. VERIFICATION
==================================================

gofmt touched Go files.

Run focused packages only:

qualification planner package
coverage
competency
scenario/task catalog bridge
scenario certification
environment
evaluation suite
qualification
httpapi
srequal
gitopsqual

executionworker only if necessary.

Do not run:

go test ./...
Docker
Kind
live Kubernetes
Argo CD
Prometheus

Frontend:

npx tsc --noEmit

One production build if practical.

==================================================
36. DO NOT IMPLEMENT
==================================================

No:

automatic execution after suite creation
pass@k/pass^k analytics
trajectory explorer
billing
Stripe
CI release gates
real EKS
real Argo CD provider
AWS/GCP/Terraform qualification
governance redesign
LLM task selection
LLM grading
frontend-wide redesign

==================================================
DEFINITION OF DONE
==================================================

Evalplane can deterministically turn an exact AgentVersion's claimed
capabilities into a reproducible QualificationPlan.

The plan tells the customer:

what must be tested
which exact tasks could satisfy it
how many trials are required
which requirements are currently blocked
why they are blocked

Only fresh-certified, environment-executable, compatible tasks can make an
authoritative plan READY.

A READY plan can produce an immutable existing SuiteVersion.

Creating that suite does not run it and does not qualify the agent.

No unavailable GitOps environment or fake certification is treated as real
qualification readiness.

==================================================
COMPLETION REPORT
==================================================

Report:

1. files changed
2. migration/schema
3. planner contract/version
4. QualificationPlan model/states
5. coverage contract v1/v2 behavior
6. GitOps/delivery coverage requirements
7. claim selection
8. task eligibility
9. authoritative certification enforcement
10. environment eligibility
11. runtime/tool compatibility
12. blocker taxonomy
13. deterministic selection algorithm
14. family breadth/contrast handling
15. trial-count derivation
16. built-in task → ScenarioVersion bridge
17. materialization immutability/idempotency
18. suite creation
19. SuiteVersion pins/metadata
20. separation of plan/build/execute
21. comparison/regression compatibility
22. current Kubernetes readiness truth
23. current GitOps/delivery readiness truth
24. API/RBAC
25. Agent-detail qualification UX
26. frontend plan/build flow
27. security/leakage
28. focused backend tests
29. TypeScript/build
30. confirmation planner never means QUALIFIED
31. confirmation unavailable GitOps cannot produce READY
32. confirmation fake certification does not count authoritative
33. confirmation no LLM task selection/grading
34. deferred work
35. real-runtime limitations

Write to:

prompts/reports/qualification-suite-builder.md

DO NOT overwrite this task file.
