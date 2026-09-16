EVALPLANE — SCENARIO CERTIFICATION & TASK QUALITY GATE

Read AGENTS.md first.

Inspect the existing implementation for:

- Scenario
- ScenarioVersion
- scenario authoring
- incident authoring
- deterministic evaluator
- criteria
- hidden variants
- task families
- case types
- capability coverage
- EvaluationSuite / SuiteVersion
- run execution
- environment/runtime contracts
- evidence
- qualification
- reproducibility contracts

Do not broadly refactor the repository.

==================================================
PURPOSE
==================================================

Evalplane now knows:

- what an AgentVersion CLAIMS
- which Task Families are needed to TEST those claims
- whether sufficient valid evidence exists

But Evalplane must not trust arbitrary Evaluation Tasks merely because they
compile.

A broken task can produce broken qualification evidence.

This stage creates a certification quality gate for Evaluation Tasks.

An Evaluation Task / ScenarioVersion should only be considered authoritative
qualification evidence when Evalplane has evidence that the task itself is:

- solvable
- reproducible
- correctly graded
- cleanly provisioned
- non-leaking
- safely resettable
- sufficiently specified

==================================================
CORE PRINCIPLE
==================================================

We evaluate agents.

Therefore we must also evaluate our evals.

Scenario certification is about the validity of the measurement instrument.

It is NOT agent qualification.

==================================================
1. CERTIFICATION CONTRACT
==================================================

Create an explicit versioned contract.

Suggested:

scenario-certification-contract:v1

Historical certifications must pin the exact certification contract.

Do not let future rule changes silently rewrite historical certification.

==================================================
2. CERTIFICATION STATES
==================================================

Use machine-readable states approximately:

uncertified
certifying
certified
failed
stale

Definitions:

uncertified:
No successful certification exists.

certifying:
Certification attempt is in progress.

certified:
All mandatory quality gates passed under the pinned certification contract.

failed:
One or more mandatory gates failed.

stale:
Previously certified, but something that affects measurement validity changed
and a new certification is required.

Do not use presentation characters such as em dashes as domain state.

==================================================
3. CERTIFICATION SCOPE
==================================================

Certification applies to an exact immutable ScenarioVersion.

Pin at minimum:

scenario_version_id
task_family_id
case_type
scenario certification contract
runtime/execution contract
environment profile/runtime identity where currently available
evaluator version
criteria definition/version
hidden variant contract/version
reference remediation contract/version

Do not certify a mutable scenario abstraction.

==================================================
4. REFERENCE REMEDIATION
==================================================

Add a reference remediation / reference solution contract.

Important:

The reference remediation proves that the task is solvable.

It is NOT the one required agent trajectory.

An agent may solve the task differently if the resulting infrastructure state
and safety constraints satisfy the deterministic grader.

Reference remediation should contain enough evaluator-only information to
execute or verify a known-good remediation.

Suggested contract:

reference-remediation:v1

Reference remediation is EVALUATOR-ONLY.

Never expose it through:

AgentView
HTTP Agent Gateway
MCP
task-visible metadata
coverage APIs
public scenario APIs
agent-facing errors

==================================================
5. REFERENCE REMEDIATION MODEL
==================================================

Model the smallest useful contract.

A reference remediation may include approximately:

- preconditions
- ordered reference actions or reference state transformation
- expected resulting state
- verification notes
- cleanup expectations

Do not force every future provider/environment into Kubernetes-specific action
types.

Reuse existing typed actions where practical.

Avoid arbitrary executable shell scripts as the default representation.

==================================================
6. SOLVABILITY GATE
==================================================

Certification must prove:

Starting from a clean provisioned task environment, the reference remediation
can bring the scenario to a state accepted by the deterministic evaluator.

Conceptually:

clean environment
→ inject task
→ verify initial failing condition
→ execute reference remediation
→ run evaluator
→ required criteria pass
→ cleanup/reset

If reference remediation cannot satisfy the evaluator:

certification fails.

Do not weaken evaluator thresholds automatically to make the reference pass.

==================================================
7. INITIAL-STATE VALIDATION
==================================================

Before remediation, certification must verify the task actually starts in the
intended failure/problem state.

Examples:

- the failure condition exists
- required resources exist
- required telemetry is available
- expected incident symptom is observable
- success criteria are not already satisfied

If the task is already "fixed" before the agent acts:

certification fails.

==================================================
8. CLEAN ENVIRONMENT VALIDATION
==================================================

Certification must distinguish task failure from dirty infrastructure.

Validate where supported:

- environment ownership belongs to certification run
- expected resources are present
- unexpected prior-run state is absent
- no stale actions/evidence contaminate the run
- required telemetry starts within bounded readiness period
- reset/provision creates equivalent starting state

Do not pretend this proves cloud-wide isolation.

Use only guarantees the current runtime can actually establish.

==================================================
9. RESET / REPEATABILITY GATE
==================================================

A task must be repeatable.

Certification should run enough repetitions to detect obvious state leakage.

For V1:

minimum 2 clean repetitions where practical.

Each repetition must:

provision/reset
inject
validate initial state
apply reference remediation
evaluate
cleanup

Both must produce equivalent certification outcome.

Do not introduce pass@k analytics here.

This is task repeatability, not agent reliability.

==================================================
10. DETERMINISTIC GRADER VALIDATION
==================================================

The grader must be tested against at least:

A. known-good reference outcome
Expected: PASS

B. known-bad/no-remediation outcome
Expected: FAIL

C. forbidden or unsafe outcome where applicable
Expected:
safety failure / applicable deterministic failure

A grader that passes both good and bad states is invalid.

A grader that fails the known-good reference is invalid.

==================================================
11. COUNTERFACTUAL / NEGATIVE GRADER CHECK
==================================================

Where practical, mutate one critical success condition after the reference
remediation.

Example:

Reference produces:

replicas = required target
error rate healthy

Counterfactual might restore:

replicas below requirement

or another domain-appropriate critical condition.

Evaluator must detect the broken state.

Do not implement arbitrary mutation frameworks.

Use scenario/evaluator-specific deterministic fixtures/helpers.

==================================================
12. CASE-TYPE VALIDATION
==================================================

Respect the existing:

positive
negative
decoy
adversarial
edge

classification.

Certification should validate case-type consistency where deterministic rules
exist.

Examples:

negative/decoy:
reference behavior may intentionally avoid a tempting remediation.

adversarial:
forbidden/unsafe action check must exist.

Do not use an LLM to infer or certify case type.

==================================================
13. LEAKAGE GATE
==================================================

This is mandatory.

Construct the final agent-visible task payload exactly through the existing
AgentView / HTTP / MCP boundaries.

Assert it does NOT contain evaluator-only information.

At minimum protect:

reference remediation
expected solution
ground truth
grader thresholds not intentionally agent-visible
hidden variant values
hidden evaluator metadata
qualification result
certification result
forbidden-action evaluator internals
private criteria metadata

Reuse existing MCP leakage protections where possible.

==================================================
14. LEAKAGE SENTINEL
==================================================

Add sentinel values to evaluator-only fields during certification tests.

Assert sentinel values never appear through:

- HTTP agent invocation payload
- MCP discovery
- MCP tools/list
- MCP tool responses
- agent-visible scenario/task APIs
- errors
- evidence intended for the agent

Do not merely search one JSON response.

Test the existing agent-facing surfaces relevant to current runtime.

==================================================
15. HIDDEN VARIANT VALIDATION
==================================================

Certification must understand hidden variants.

A ScenarioVersion with variants must verify:

- variants satisfy declared schema/bounds
- generated variants preserve task solvability
- hidden values remain hidden
- evaluator remains valid for representative variants

Do not certify every combinatorial variant.

For V1, deterministically choose a small representative set, including boundary
values where available.

Pin which certification variants were used.

==================================================
16. CRITERIA VALIDATION
==================================================

Reject certification when criteria are malformed or non-measurable.

Validate current deterministic criteria semantics:

- operator valid
- threshold/target valid
- unit consistent where required
- metric/resource reference resolvable where possible
- duplicate/conflicting criterion identity rejected
- mandatory criterion has observable data source

Do not invent thresholds.

Do not use LLM judgment.

==================================================
17. TELEMETRY / OBSERVABILITY VALIDATION
==================================================

For tasks requiring telemetry:

certification must prove required telemetry is actually observable before the
task is certified.

Examples:

required metric exists
query resolves
required label dimensions are present
telemetry source is available

Do not convert missing telemetry into an agent failure.

Missing certification telemetry is a platform/task-certification failure.

==================================================
18. SAFETY VALIDATION
==================================================

For scenarios with forbidden behavior or protected resources:

Certification should verify that the evaluator can detect at least one
representative forbidden action/outcome.

Do not execute destructive actions against unrelated/shared resources.

Use owned fixtures/current action executor safety boundaries.

==================================================
19. CERTIFICATION RECORD
==================================================

Persist certification attempts/results.

Use the next migration number.

A certification record should approximately include:

id
scenario_version_id
state
certification_contract_version
reference_remediation_contract_version
evaluator_version
runtime/environment contract identifiers
started_at
completed_at

gate_results structured JSON or normalized rows
reference_evaluation_id where appropriate
certification variant identifiers/seeds
failure codes

Pin exact evidence identifiers where practical.

Do not store secrets.

==================================================
20. GATE RESULTS
==================================================

Expose structured gate results.

Suggested gates:

initial_state
reference_solvability
grader_positive
grader_negative
safety_detection
telemetry
repeatability
reset_cleanliness
variant_validation
leakage
criteria_validation

Not every task requires every optional gate.

Each gate should return approximately:

required
status
code
safe detail

Statuses:

passed
failed
not_applicable

Avoid free-form-only certification logic.

==================================================
21. FAILURE CODES
==================================================

Use stable machine-readable failure codes.

Examples:

initial_state_invalid
reference_remediation_failed
reference_not_accepted_by_evaluator
grader_accepts_bad_state
grader_rejects_good_state
required_telemetry_unavailable
environment_not_clean
reset_not_reproducible
hidden_variant_leak
reference_solution_leak
criteria_invalid
unsafe_outcome_not_detected
variant_unsatisfiable

Choose names consistent with repository conventions.

==================================================
22. CERTIFIED TASK ELIGIBILITY
==================================================

Introduce an explicit helper/service decision:

Can this ScenarioVersion contribute authoritative qualification evidence?

For newly certification-aware paths:

certified = eligible

uncertified/failed/stale = not authoritative

IMPORTANT COMPATIBILITY RULE:

Do not unexpectedly break all existing Stage 1–27 tests/workflows by globally
rejecting every legacy ScenarioVersion.

Introduce compatibility carefully.

Existing historical scenarios may remain runnable.

But qualification-authoritative / certification-aware workflows must be able to
distinguish certified from uncertified.

Do not silently label legacy tasks certified.

==================================================
23. COVERAGE INTERACTION
==================================================

Do not rewrite Stage 27 coverage.

Coverage asks:

Was the capability validly exercised?

Certification asks:

Is this task a trustworthy measurement instrument?

Expose certification state alongside task-family evidence so future
qualification-suite construction can require:

certified task
+
valid trial
+
required family coverage

Do not automatically remove all legacy evidence from historical coverage.

Version the future eligibility behavior cleanly.

==================================================
24. API
==================================================

Add authenticated APIs consistent with current patterns.

Approximately:

GET
/v1/.../scenarios/{scenarioID}/versions/{versionID}/certification

POST
/v1/.../scenarios/{scenarioID}/versions/{versionID}/certification

or equivalent job/attempt resource if certification is asynchronous.

Read:
workspace members/viewers consistent with scenario visibility.

Trigger certification:
existing write-capable roles only.

Users cannot manually submit:

state=certified

Certification is server-derived.

==================================================
25. FRONTEND
==================================================

Keep frontend changes focused.

Evaluation Task detail should show:

Certification

CERTIFIED
or
UNCERTIFIED
or
FAILED
or
STALE

When available, show restrained gate details:

Reference solvability        Passed
Initial state                Passed
Grader positive check        Passed
Grader negative check        Passed
Leakage                      Passed
Repeatability                Passed

Do not create giant cards or scores.

Do not present uncertified tasks as production qualification evidence.

Do not fake certification for existing tasks.

==================================================
26. AUTHORING FLOW
==================================================

After a ScenarioVersion is created/reviewed, it may be:

Uncertified

Then certification is a separate explicit quality gate.

Do not have an LLM approve certification.

Incident-derived scenarios still require the same certification.

Customer-created tasks do not bypass certification.

==================================================
27. REFERENCE REMEDIATION AUTHORING
==================================================

Provide the minimum backend/manual mechanism necessary to associate evaluator-
only reference remediation with a ScenarioVersion.

Do not build a broad new UI builder unless required.

It is acceptable for V1 certification fixtures/built-in tasks to define
reference remediation through trusted structured backend data.

Never let ordinary agent-facing reads expose it.

==================================================
28. CERTIFICATION IDEMPOTENCY / CONCURRENCY
==================================================

Certification must be safe to retry.

Prevent two concurrent attempts from corrupting certification state.

Use existing job/lease patterns where appropriate without creating a broad new
orchestration framework.

A failed certification may be retried.

Historical attempts remain inspectable.

==================================================
29. STALE CERTIFICATION
==================================================

Define what makes certification stale.

At minimum, a certification must not be reused when its pinned:

certification contract
evaluator version
reference remediation version
runtime/environment contract

no longer matches the requested authoritative measurement context.

Do not mutate historical certified rows.

A new certification attempt creates new evidence.

==================================================
30. REPRODUCIBILITY
==================================================

Certification must record enough information to answer:

"Why did Evalplane trust this Evaluation Task?"

Pin:

ScenarioVersion
Task Family
Case Type
reference remediation contract
certification contract
evaluator version
runtime/environment contract
criteria definition
variant IDs/seeds used
gate results
evidence/evaluation identifiers

==================================================
31. TESTS
==================================================

Focused tests must cover at minimum:

1. certification contract version
2. reference remediation evaluator-only
3. initial broken state required
4. known-good reference passes evaluator
5. known-bad state fails evaluator
6. grader that accepts bad state causes certification failure
7. grader that rejects good state causes certification failure
8. missing required telemetry causes certification failure
9. reference remediation failure causes certification failure
10. clean/reset repeatability
11. second repetition contamination detected
12. hidden variant bounds validated
13. unsatisfiable variant rejected
14. hidden variant values do not leak
15. reference remediation does not leak
16. evaluator ground truth does not leak
17. unsafe representative outcome detectable when required
18. malformed criteria rejected
19. certification persisted with exact contracts
20. retry/idempotency
21. concurrent certification safe
22. failed certification does not become certified
23. legacy ScenarioVersion remains runnable but not silently certified
24. viewer cannot trigger certification
25. workspace isolation
26. no user API can directly set certified=true
27. coverage and certification remain separate concepts
28. no LLM grading

==================================================
32. VERIFICATION
==================================================

gofmt touched Go files.

Run focused backend packages only.

Likely:

internal/scenariocertification/...
internal/scenario/...
internal/evaluator/...
internal/scenarioauthoring/... if touched
internal/httpapi/... relevant tests
runtime/environment helpers directly touched

Do not run:

go test ./...
Docker
Kind
live Kubernetes
Prometheus

unless existing focused tests absolutely require them.

For live-environment-dependent certification, structure interfaces + deterministic
fakes in this stage.

Real runtime certification will be exercised later in the production/release
qualification stages.

Frontend:

npx tsc --noEmit

One production build if practical.

==================================================
33. DO NOT IMPLEMENT
==================================================

Do NOT implement:

large new scenario corpus
deep Kubernetes catalog
real Argo CD environment
environment profile scheduler
AWS
Terraform
GCP
automatic qualification-suite builder
pass@k / pass^k analytics
trajectory explorer
billing
Stripe
CI release gates
new connectors
LLM grading
frontend-wide redesign

==================================================
DEFINITION OF DONE
==================================================

ScenarioVersion certification exists as a reproducible server-derived quality
gate.

A task cannot become CERTIFIED merely because it compiles.

Certification proves, under the current deterministic/fakeable runtime
contracts:

- intended failure exists
- reference remediation solves it
- deterministic grader accepts known-good state
- deterministic grader rejects known-bad state
- required telemetry is measurable
- environment/reset is repeatable
- hidden evaluator information does not leak
- representative variants remain valid
- required safety detection works
- exact certification evidence/contracts are retained

Certification remains distinct from agent qualification.

==================================================
COMPLETION REPORT
==================================================

Report:

1. files changed
2. migration/schema
3. certification contract/version
4. certification states
5. reference remediation model
6. reference-remediation secrecy
7. initial-state gate
8. solvability gate
9. grader positive validation
10. grader negative/counterfactual validation
11. telemetry validation
12. environment cleanliness/reset validation
13. repeatability behavior
14. hidden-variant validation
15. leakage/sentinel validation
16. safety validation
17. certification persistence/reproducibility
18. stale behavior
19. authoritative-eligibility behavior
20. interaction with Stage 27 coverage
21. API/RBAC
22. frontend task-certification UX
23. backward compatibility
24. focused backend tests
25. frontend TypeScript/build
26. confirmation users cannot set certified manually
27. confirmation reference solution never reaches agent
28. confirmation no LLM grading
29. deferred work
30. real-runtime limitations

Write the completion report to:

prompts/reports/scenario-certification.md

DO NOT overwrite this task file.
