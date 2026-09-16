EVALPLANE — SCENARIO FAMILIES & CAPABILITY COVERAGE

Read AGENTS.md first.

Read the existing implementation for:

- competency capability catalog
- AgentVersion capability claims
- Scenario / ScenarioVersion
- EvaluationSuite / SuiteVersion / SuiteExecution
- Batch / Run
- deterministic evaluations
- hidden variants
- invalid_platform handling
- qualification
- reproducibility contracts

Do not broadly refactor the repository.

==================================================
PURPOSE
==================================================

Evalplane can now represent:

"What does this exact AgentVersion claim it can do?"

The product must now determine:

"Has Evalplane actually exercised enough independent infrastructure situations
to call that capability TESTED?"

This stage introduces:

1. Scenario / Task Families
2. Case-type classification
3. Capability coverage requirements
4. Deterministic coverage assessment
5. UNTESTED / PARTIAL / TESTED capability states

This stage does NOT determine per-capability QUALIFIED / NOT QUALIFIED.

TESTED means sufficient required evidence was exercised.

It does NOT mean the agent succeeded.

==================================================
CORE MODEL
==================================================

Capability Claim
    ↓
Coverage Requirement
    ↓
Multiple Task Families
    ↓
ScenarioVersions
    ↓
Repeated valid Trials
    ↓
Capability Coverage Assessment
    ↓
UNTESTED | PARTIAL | TESTED

Keep this separate from:

Capability Claim
    ≠ Tool Permission
    ≠ Test Result
    ≠ Qualification

==================================================
1. TASK FAMILY CONCEPT
==================================================

Introduce a stable Scenario/Task Family concept.

Customer-facing terminology:

Evaluation Task
Task Family

Backend names may continue using Scenario where appropriate.

A Task Family represents an independent class of infrastructure situation.

Examples:

kubernetes.capacity.traffic_spike
kubernetes.capacity.resource_pressure
kubernetes.capacity.misleading_symptom

gitops.drift.deployment
gitops.drift.config
gitops.drift.deleted_managed_resource

delivery.progressive.error_regression
delivery.progressive.latency_regression
delivery.progressive.false_positive

Do not implement all of these scenarios now.

The model must support them.

==================================================
2. CASE TYPES
==================================================

Each coverage-relevant ScenarioVersion must have an explicit case type.

Supported V1 values:

positive
negative
decoy
adversarial
edge

Definitions:

positive:
The expected capability really is needed.

negative:
The apparent problem exists, but the claimed remediation should not be used.

decoy:
Symptoms tempt an unrelated/common incorrect diagnosis.

adversarial:
The environment intentionally tempts unsafe or forbidden action.

edge:
Boundary behavior or unusual but valid conditions.

Do not infer case type from scenario name.

Do not use an LLM to classify it at runtime.

==================================================
3. VERSIONING
==================================================

Introduce explicit contract versions.

Suggested:

task-family-contract:v1

capability-coverage-contract:v1

Keep the existing:

infrastructure-capability-catalog:v1

Historical assessments must retain the exact contract versions used.

==================================================
4. PERSISTENCE
==================================================

Use the next valid migration number.

Introduce the smallest schema necessary to represent:

Task Families

ScenarioVersion → Task Family assignment

ScenarioVersion case type

Capability → required Task Families / coverage requirements

Coverage assessments

Prefer stable IDs.

Do not rewrite historical ScenarioVersions.

If a ScenarioVersion is already immutable, preserve that property.

Do not make family assignment silently mutable after evaluation evidence exists.

==================================================
5. BUILT-IN TASK FAMILIES
==================================================

Create a SMALL V1 task-family catalog aligned to capabilities that are actually
credible for the current product.

Do NOT pretend all families have runnable scenarios today.

At minimum structure families for:

Kubernetes:
- workload
- capacity
- deployment
- networking/dependency

Observability:
- metrics diagnosis
- missing telemetry reasoning
- recovery verification

GitOps:
- drift
- desired state
- Argo CD

Delivery:
- rollout
- progressive delivery

Safety:
- boundary adherence
- protected-resource avoidance
- stop-after-recovery

Use stable dotted family IDs.

==================================================
6. EXISTING SCENARIOS
==================================================

Inspect the current built-in scenarios.

Map ONLY scenarios that genuinely belong to a task family.

Do not invent coverage merely to make the dashboard look complete.

Example:

existing traffic_spike may legitimately map to a Kubernetes capacity family.

A future node-failure task must NOT be counted if the current environment
cannot actually run it.

Unsupported/future scenarios must not contribute coverage.

==================================================
7. CAPABILITY COVERAGE REQUIREMENTS
==================================================

Create a server-owned, versioned coverage requirement catalog.

Each competency capability may define:

- required Task Families
- minimum distinct families
- minimum valid trials per required family
- required case types where appropriate

Example conceptually:

kubernetes.capacity.remediation

requires:

family A: capacity traffic/saturation
family B: another independent capacity/remediation situation

plus at least:
- one positive case
- one negative/decoy/adversarial case

Do not require every capability to have the exact same rule.

Keep V1 requirements conservative.

If we do not yet have enough runnable task families for a capability:

coverage must remain PARTIAL or UNTESTED.

Do not weaken requirements merely so current agents become TESTED.

==================================================
8. INDEPENDENT EVIDENCE
==================================================

Coverage must reward independent situations, not repetition of the same task.

Ten trials of one Task Family do NOT equal ten independent families.

Distinguish:

family breadth

from

trial repetition.

Repeated trials establish reliability.

Task Families establish breadth.

==================================================
9. VALID TRIALS
==================================================

Only valid evaluable trials count toward TESTED coverage.

Do NOT count:

invalid_platform
environment provisioning failure
missing required telemetry caused by Evalplane
abandoned platform execution
other platform-invalid outcomes

Agent failures ARE still valid trials if the platform produced a valid
evaluation.

Important:

Coverage asks:

"Did we validly test the capability?"

It does NOT ask:

"Did the agent pass?"

Therefore:

valid agent failure contributes coverage.

platform-invalid execution does not.

==================================================
10. HIDDEN VARIANTS
==================================================

Reuse the existing hidden-variant system.

Multiple hidden variants inside one Task Family improve evidence depth but do
not become separate Task Families.

Coverage output should be able to expose:

valid trial count
distinct variant count

without leaking hidden evaluator-only variant values to the agent.

==================================================
11. COVERAGE ASSESSMENT SCOPE
==================================================

Coverage must be reproducible.

Do not derive an important TESTED state from an unversioned scan of arbitrary
historical runs.

Create deterministic coverage assessments scoped to:

exact AgentVersion
exact evidence source / SuiteExecution
exact capability claim
exact capability catalog version
exact task-family contract version
exact coverage contract version

Persist the assessment or otherwise make historical reproduction exact.

Prefer persisted assessment records.

==================================================
12. COVERAGE STATES
==================================================

Machine-readable states:

untested
partial
tested

Definitions:

UNTESTED:
No valid qualifying evidence for the capability.

PARTIAL:
Some required coverage exists, but one or more family/case/trial requirements
are missing.

TESTED:
All coverage requirements for that capability were satisfied by valid
evaluation evidence.

Do NOT create:

qualified
not_qualified

in this coverage engine.

Those belong to qualification policy/evidence outcome.

==================================================
13. COVERAGE OUTPUT
==================================================

For each claimed capability return structured information approximately:

capability_id
claim_status
test_status

coverage_contract_version
task_family_contract_version
capability_catalog_version

required_family_count
covered_family_count
valid_trial_count
distinct_variant_count

families:
  family_id
  required
  valid_trials
  case_types_exercised
  covered

missing_requirements:
  structured machine-readable codes

Avoid relying only on prose.

==================================================
14. MISSING REQUIREMENT CODES
==================================================

Use stable structured codes approximately:

missing_required_family
insufficient_valid_trials
missing_required_case_type
no_valid_evidence
platform_invalid_only

Do not encode coverage logic only in UI strings.

==================================================
15. EXISTING CLAIMS API CLEANUP
==================================================

The current capability-claims API exposes presentation placeholders like:

tested: "—"
qualified: "—"

Correct this design.

Backend APIs must use machine-readable values.

For claims without a coverage assessment:

test_status = "untested"

For per-capability qualification not yet implemented:

qualification_status = null

or omit it according to existing API conventions.

The frontend may display:

Tested —
Qualified —

or more useful status labels.

Do not return an em dash as domain state.

==================================================
16. API
==================================================

Add authenticated endpoints consistent with existing routing.

Approximately:

GET
/v1/workspaces/{workspaceID}/agents/{agentID}/versions/{versionID}/capability-coverage

and/or suite-execution scoped coverage:

GET
/v1/workspaces/{workspaceID}/suite-executions/{executionID}/agents/{agentVersionID}/capability-coverage

Use whichever structure best matches the existing repository.

Coverage must identify the exact evidence scope.

Do not silently aggregate unrelated runs.

==================================================
17. ASSESSMENT CREATION
==================================================

Integrate coverage assessment into an appropriate deterministic lifecycle.

It must not depend on an LLM.

Assessment must be safe to retry/idempotent.

If required evaluation evidence is not complete:

return/not persist premature TESTED state.

Race-safe behavior is required.

==================================================
18. CAPABILITY WITH NO REQUIREMENTS
==================================================

A claimed capability with no published V1 coverage requirement must NOT become
TESTED automatically.

Return a structured state such as:

partial

with reason:

coverage_requirement_unavailable

or equivalent.

Do not treat absence of requirements as success.

==================================================
19. FRONTEND — AGENT DETAIL
==================================================

Update Capability claims.

Replace fake/static Tested placeholders with real coverage data when available.

Example:

Kubernetes

Capacity remediation
CLAIMED
TESTED

Deployment recovery
CLAIMED
PARTIAL

GitOps drift remediation
CLAIMED
UNTESTED

Qualified should remain:

—

unless genuine per-capability qualification exists.

Do not derive Qualified from TESTED.

==================================================
20. FRONTEND — COVERAGE DETAIL
==================================================

Allow the user to understand WHY a capability is partial.

Keep it restrained.

Example:

Capacity remediation

2 / 3 task families exercised
7 valid trials
Positive case ✓
Negative/decoy case —
Missing: kubernetes.capacity.resource_pressure

Do not create giant metric cards, radar charts, stars, or fake scores.

No percentages unless mathematically necessary.

==================================================
21. CLAIMED / TESTED / QUALIFIED SEMANTICS
==================================================

The product truth must now be:

CLAIMED
AgentVersion declared the capability.

TESTED
Coverage contract has been satisfied by valid evidence.

QUALIFIED
NOT IMPLEMENTED PER CAPABILITY IN THIS STAGE.

Do not collapse Tested and Qualified.

==================================================
22. REPRODUCIBILITY
==================================================

Coverage assessments must pin:

AgentVersion
Capability ID
Capability catalog version
Task-family contract version
Coverage contract version
SuiteExecution / evidence scope
ScenarioVersion IDs
relevant evaluation IDs
valid trial identifiers

Do not rely on mutable current catalog state for historical assessment.

==================================================
23. SECURITY / TENANCY
==================================================

Enforce workspace isolation.

Viewer:
read only.

Members with appropriate existing permissions:
may trigger/run evaluation workflows as currently allowed.

Coverage itself is deterministic server-derived state.

Users cannot directly set:

tested=true

Do not expose hidden variant values or evaluator-only scenario fields through
coverage APIs.

==================================================
24. TESTS
==================================================

Focused tests must cover at minimum:

catalog/family IDs unique

case-type validation

ScenarioVersion family assignment

historical assignment immutability

coverage contract version

claim with no evidence → UNTESTED

some evidence → PARTIAL

all requirements met → TESTED

agent failure still counts as valid coverage

invalid_platform does NOT count

platform-invalid-only evidence does not become TESTED

same family repeated many times does not satisfy missing family breadth

hidden variants count depth but not family breadth

required negative/decoy/adversarial case enforced where configured

unknown Task Family rejected

workspace isolation

viewer read behavior

no user endpoint to set TESTED manually

historical assessment retains contract versions

claims API no longer returns em-dash domain states

no qualification inferred from TESTED

no LLM grading

==================================================
25. VERIFICATION
==================================================

gofmt touched Go files.

Run focused Go packages only.

Do not run go test ./...

Do not start Docker, Kind, Kubernetes, Prometheus, or PostgreSQL unless an
existing focused test absolutely requires it.

Frontend:

npx tsc --noEmit

One production build if practical.

Do not repeatedly retry pathological builds.

==================================================
26. DO NOT IMPLEMENT
==================================================

Do NOT implement:

automatic Qualification Suite construction
reference remediation / reference solution
Scenario certification
new large scenario corpus
new Kubernetes runtime scenarios
real Argo CD environment
AWS
Terraform
GCP
PagerDuty
Datadog
pass@k / pass^k analytics
trajectory explorer
billing
Stripe
CI release gates
LLM grading
frontend-wide redesign

Those belong to later stages.

==================================================
DEFINITION OF DONE
==================================================

Done means:

A capability claim can be deterministically evaluated for test COVERAGE.

The system knows which independent Task Families are required.

It distinguishes family breadth from repeated trials.

It distinguishes agent failure from platform-invalid evidence.

It produces UNTESTED / PARTIAL / TESTED.

TESTED does not mean QUALIFIED.

Historical coverage is reproducible.

The API uses machine-readable states.

The frontend can truthfully show why a claim is or is not tested.

==================================================
COMPLETION REPORT
==================================================

Report:

1. files changed
2. migration/schema
3. Task Family model
4. Task Family contract/version
5. case-type model
6. capability coverage contract
7. initial capability→family requirements
8. existing ScenarioVersion mappings
9. valid-trial rules
10. family breadth vs trial depth
11. hidden-variant treatment
12. persisted/reproducible coverage assessment
13. UNTESTED/PARTIAL/TESTED semantics
14. claims API cleanup
15. API/RBAC
16. Agent detail coverage UX
17. missing-requirement UX
18. backend focused tests
19. frontend TypeScript/build
20. confirmation TESTED never implies QUALIFIED
21. confirmation platform-invalid evidence never satisfies coverage
22. confirmation no LLM grading
23. deferred work

Write the completion report to:

prompts/reports/scenario-families-coverage.md

DO NOT overwrite this task file.
