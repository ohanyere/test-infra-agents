EVALPLANE — TRAJECTORY & EVIDENCE EXPLORER

Read AGENTS.md first.

Inspect the existing implementation for:

- Run / Trial execution lifecycle
- executionworker
- evidence repository/service
- observations
- action executor
- denied/unauthorized actions
- MCP tool calls
- HTTP agent gateway
- environment lifecycle evidence
- deterministic evaluator
- evaluation findings
- failure taxonomy
- SuiteExecution
- qualification reports
- Stage-33 reliability reports
- GitOps / progressive-delivery fixture vocabulary
- frontend run/trial/result pages
- existing audit/event structures

Do not broadly refactor the repository.

==================================================
PURPOSE
==================================================

Evalplane must make infrastructure-agent evaluations inspectable.

A customer should be able to answer:

- What did the agent observe?
- What actions did it attempt?
- Which actions actually executed?
- Which actions were denied?
- What changed in the environment?
- When did recovery occur?
- Did the agent keep mutating after recovery?
- What evidence caused the evaluator to pass or fail the trial?
- Was the failure caused by the agent or by the evaluation platform?

Build a deterministic Trajectory & Evidence Explorer over existing persisted
evidence.

Do NOT create a second execution engine.

Do NOT expose private chain-of-thought.

==================================================
1. TRAJECTORY CONTRACT
==================================================

Introduce a versioned contract:

trajectory-evidence-contract:v1

Every trajectory response must pin this contract.

Historical trajectories must remain interpretable if projection logic changes.

==================================================
2. OBSERVABLE TRAJECTORY ONLY
==================================================

Trajectory means observable operational behavior.

Allowed evidence includes:

- trial lifecycle
- environment lifecycle
- observations returned to the agent
- structured tool/action requests
- structured action results
- denied actions
- state changes
- GitOps / rollout operations
- telemetry snapshots or summaries
- recovery verification
- evaluator findings
- safety findings
- platform-invalid evidence
- final evaluation outcome

Do NOT store or expose:

- chain-of-thought
- hidden reasoning
- model scratchpads
- evaluator reference remediation
- hidden variant values
- evaluator-only root-cause metadata
- secrets

If an agent sends free-form explanation text as an ordinary protocol field,
treat it as user-visible agent output, not as privileged reasoning.

Do not infer hidden reasoning from it.

==================================================
3. NO SECOND SOURCE OF TRUTH
==================================================

Existing persisted evidence remains authoritative.

The explorer should be a read model / projection over:

run events
observations
actions
evidence
evaluation findings
environment events

Prefer deterministic reconstruction.

Only introduce new persistence if an existing source cannot provide stable,
reproducible ordering or necessary provenance.

Do not duplicate every event into another authoritative store.

==================================================
4. EVENT MODEL
==================================================

Create a canonical safe trajectory event projection.

Suggested fields:

event_id
sequence
occurred_at
event_type
phase
actor
summary
status
source_type
source_id
evidence_refs
safe_metadata

Possible actor vocabulary:

platform
agent
environment
tool
evaluator

Possible phases:

provisioning
setup
agent_execution
verification
evaluation
cleanup

Keep vocabularies closed/versioned where practical.

==================================================
5. EVENT TYPES
==================================================

Support at minimum:

trial_started
environment_provisioning_started
environment_ready
environment_setup
observation
agent_action_requested
agent_action_executed
agent_action_failed
agent_action_denied
tool_invocation
tool_result
telemetry_observation
state_change
recovery_detected
recovery_verified
evaluation_started
evaluation_finding
safety_finding
platform_invalid
evaluation_completed
cleanup_started
cleanup_completed
trial_completed

Reuse existing event names where appropriate rather than duplicating concepts.

==================================================
6. ORDERING
==================================================

Timeline ordering must be deterministic.

Prefer existing persisted sequence numbers where available.

If multiple evidence sources must be merged, define a stable ordering such as:

occurred_at
then source priority
then persisted sequence / stable ID

Document the rule.

Do not imply causal ordering merely because two timestamps are close.

Sequence should represent display order, not fabricated causality.

==================================================
7. EVIDENCE PROVENANCE
==================================================

Every meaningful trajectory event should preserve provenance.

Examples:

observation record ID
action record ID
run event ID
evaluation finding ID
telemetry evidence ID
environment instance ID

The UI should be able to show:

"Why is Evalplane saying this?"

without exposing evaluator-only secrets.

==================================================
8. ACTION LIFECYCLE
==================================================

Distinguish clearly:

requested
authorized
executed
failed
denied

Do not render:

"agent scaled deployment"

when the agent merely requested it and authorization denied it.

Example:

Agent requested:
scale workload checkout → 10

Result:
DENIED

Reason:
outside allowed resource scope

This distinction is mandatory.

==================================================
9. TOOL CALLS
==================================================

HTTP/MCP mediated operations should appear as safe structured tool events.

Show:

tool name
safe target
status
duration where available
safe arguments
safe result summary

Never expose:

authorization bearer tokens
API keys
MCP run grants
provider credentials
raw kubeconfigs
incident-source secrets

Apply existing redaction utilities where possible.

==================================================
10. OBSERVATIONS
==================================================

Show the evidence the agent was actually allowed to observe.

Examples:

deployment readiness
pod state
service availability
Prometheus summary
Argo CD sync state
rollout state
dependency health

Do not enrich historical observations with evaluator-only information the
agent did not receive.

The explorer may separately show evaluator evidence, clearly labeled as such.

==================================================
11. AGENT VIEW VS EVALUATOR VIEW
==================================================

Separate two concepts:

Agent-visible evidence
Evaluator evidence

Example:

AGENT-VISIBLE:
Error rate 12.4%
Deployment replicas 2/4 ready

EVALUATOR:
Recovery criterion error_rate_pct <= 1
Safety finding unauthorized_action_attempt

Do not mix these in a way that implies the agent knew hidden evaluator state.

==================================================
12. STATE CHANGE EVENTS
==================================================

Where evidence supports it, derive safe state transitions.

Examples:

replicas 2 → 4
rollout Progressing → Aborted
Argo CD OutOfSync → Synced
availability 72% → 100%

Derived events must link to the underlying evidence.

Do not derive speculative state changes.

==================================================
13. RECOVERY MOMENT
==================================================

Expose when deterministic recovery criteria first became satisfied.

Suggested:

recovery_detected_at

and, when explicit verification occurs:

recovery_verified_at

This enables analysis of:

time_to_recovery
actions_after_recovery

Do not invent recovery timestamps without evaluator evidence.

==================================================
14. ACTIONS AFTER RECOVERY
==================================================

Compute:

actions_before_recovery
actions_after_recovery

This is important for:

stop-when-recovered
restraint
unnecessary mutation

Do not automatically fail the trial here.

Expose the evidence to existing evaluator/qualification logic.

==================================================
15. SAFETY TRAJECTORY
==================================================

Safety events must be prominent and structured.

Examples:

unauthorized_action_attempt
cross_namespace_action_attempt
protected_resource_attempt
forbidden_behavior

Show:

requested action
safe target
denial
structured finding

Do not expose secret policy internals.

==================================================
16. FAILURE EXPLANATION
==================================================

Use existing deterministic evaluator findings and failure taxonomy.

A failed trial should show approximately:

Outcome: FAIL

Primary findings:
- Recovery criterion not satisfied
- Unauthorized action attempted

Failure classification:
agent_remediation_failure
safety_violation

Do not generate speculative natural-language blame with an LLM.

==================================================
17. PLATFORM-INVALID TRAJECTORY
==================================================

Platform-invalid trials need their own clear path.

Example:

Environment ready
Agent invoked correctly
Prometheus became unavailable
Evaluator lacked required telemetry
Trial marked INVALID_PLATFORM

The explorer must make it obvious that this was not an agent failure.

Reuse Stage-33 platform-invalid classification.

==================================================
18. OUTCOME SUMMARY
==================================================

Provide a deterministic summary object.

Suggested fields:

trial_status
evaluation_status
validity
pass_fail_outcome
failure_classes
finding_count
safety_violation_count
actions_requested
actions_executed
actions_denied
observations_count
time_to_recovery
recovery_verified
actions_after_recovery

Do not produce a new qualification verdict.

==================================================
19. EVIDENCE DETAIL
==================================================

Allow individual evidence items to be inspected safely.

For example:

GET trajectory
→ event references evidence item

GET evidence item
→ safe structured detail

Prefer existing evidence APIs if they already support this.

Do not add raw database/debug payload exposure.

==================================================
20. PAYLOAD SIZE
==================================================

Some observations/tool results may be large.

Implement bounded safe previews.

Suggested behavior:

- structured summary inline
- bounded text/json preview
- truncation flag
- original byte size if known
- content hash if useful
- evidence reference for authorized detail retrieval

Do not send megabytes of logs in the main trajectory response.

==================================================
21. REDACTION
==================================================

Centralize trajectory redaction.

Redact at minimum:

authorization headers
cookies
API keys
tokens
password fields
secret values
kubeconfigs
connection credentials
incident-source credentials

Reuse Stage-23/25 redaction and secret-handling patterns where practical.

Never rely only on frontend hiding.

==================================================
22. GITOPS TRAJECTORY
==================================================

Support Stage-31 vocabulary without falsely claiming live GitOps execution.

Future real trajectory should support:

inspect desired state
inspect Argo state
live-state observation
desired-state mutation
request Argo sync
reconciliation observed
rollout action
rollout state transition

Current unavailable GitOps tasks must not fabricate trajectories.

Fixtures may be unit-tested only.

==================================================
23. TRAJECTORY COMPARISON
==================================================

Allow deterministic comparison of two compatible trials where practical.

Do NOT build a full diff engine.

Useful summary comparison:

actions count
denied actions
time to recovery
recovery verified
findings
outcome
failure classes

Optional aligned event comparison only if straightforward.

Do not compare private reasoning.

==================================================
24. SUITE EXECUTION VIEW
==================================================

A SuiteExecution should expose its trials with concise evidence summaries.

Example:

Task                     Trial    Outcome   Actions   Safety   Recovery
Capacity saturation      1        PASS      3         0        21s
Capacity saturation      2        FAIL      7         1        —
Capacity decoy           1        PASS      1         0        n/a

Clicking a trial opens the detailed trajectory.

Do not turn this into a giant dashboard.

==================================================
25. AGENT VERSION VIEW
==================================================

From an AgentVersion, users should be able to navigate:

Qualification
→ SuiteExecution
→ Trial
→ Trajectory
→ Evidence

Stage-33 reliability can link into supporting trials.

Do not duplicate reliability calculations here.

==================================================
26. API
==================================================

Add authenticated workspace-scoped read APIs consistent with existing routes.

Potential shape:

GET
/v1/workspaces/{workspaceID}/runs/{runID}/trajectory

GET
/v1/workspaces/{workspaceID}/runs/{runID}/evidence/{evidenceID}

or equivalent existing Trial/Run route naming.

For SuiteExecution:

GET
/v1/workspaces/{workspaceID}/suite-executions/{executionID}/trajectory-summary

Use existing identifiers and conventions.

Read-only APIs are sufficient.

Viewer/member read access according to current workspace RBAC.

==================================================
27. PAGINATION
==================================================

Trajectory size may grow.

Support deterministic pagination/cursoring if existing event volume makes it
necessary.

Cursor must preserve stable ordering.

Do not silently return only the first N events without telling the client.

==================================================
28. FILTERING
==================================================

Allow safe server-side filters where useful:

phase
event_type
actor
status

Do not add arbitrary SQL/query-language filtering.

==================================================
29. FRONTEND — TRIAL TRAJECTORY
==================================================

Build a restrained evidence explorer.

No giant cards.
No fake terminal.
No neon timeline.
No bento grid.

Suggested layout:

Trial header
Outcome / validity / duration
small evidence summary

Timeline rows:

time
actor
event
status
expandable evidence

Example:

00:03  AGENT       Inspect Deployment checkout           success
00:07  TOOL        Query Prometheus                      success
00:11  AGENT       Scale checkout 2 → 4                  success
00:18  EVALUATOR   Recovery criteria satisfied           verified
00:23  AGENT       Restart dependency pod                success
00:26  EVALUATOR   Unnecessary mutation after recovery   finding

Use compact separators, typography and monospace identifiers.

==================================================
30. FRONTEND — EVIDENCE DRAWER/DETAIL
==================================================

Selecting a timeline item may show:

source
timestamp
safe structured metadata
input summary
output summary
linked findings
provenance IDs

Do not expose evaluator-only forbidden fields.

Use existing design language.

==================================================
31. FRONTEND — OUTCOME EXPLANATION
==================================================

Near the final outcome show deterministic findings.

Example:

FAIL

Recovery succeeded at 00:18.

The agent then performed two additional mutations.

One attempted action targeted a protected resource and was denied.

Do not generate this with an LLM.

Compose from structured findings/templates.

==================================================
32. FRONTEND — PLATFORM INVALID
==================================================

Platform-invalid should be visually distinct from FAIL.

Example:

INVALID — EVALUATION PLATFORM

Required Prometheus evidence became unavailable.

Agent reliability was not penalized.

Do not use red failure treatment that implies agent fault.

==================================================
33. TIME SEMANTICS
==================================================

Prefer relative trial time for trajectory readability:

+00:03
+00:07
+00:11

Retain absolute timestamp in evidence detail.

Do not assume workstation timezone.

==================================================
34. DURATION METRICS
==================================================

Where evidence supports them, compute:

trial duration
environment setup duration
agent active duration
time to first action
time to recovery
verification duration

Do not fabricate missing durations.

Represent unavailable values explicitly.

==================================================
35. REPRODUCIBILITY PINS
==================================================

Trajectory response should include safe reproducibility metadata:

AgentVersion
ScenarioVersion
SuiteVersion / execution where applicable
hidden variant identity only as opaque safe ID if already permitted
runtime contract
evaluator contract
environment profile/provider/isolation
trajectory contract

Never expose hidden variant contents.

==================================================
36. DATA INTEGRITY
==================================================

Trajectory projection must not silently accept impossible relationships.

Examples:

action belongs to another run
finding belongs to another workspace
evidence item belongs to another ScenarioVersion

Workspace/run ownership must be verified.

==================================================
37. TESTS
==================================================

Focused tests must cover at minimum:

trajectory contract pinned
deterministic ordering
stable tie-break ordering
trial ownership/workspace isolation
observation projection
requested vs executed action distinction
denied action projection
tool-call redaction
secret/header/token redaction
agent-visible vs evaluator evidence separation
state-change projection only when evidenced
recovery_detected
recovery_verified
actions_after_recovery
safety finding projection
platform-invalid projection
failure taxonomy reuse
final summary counts
time-to-recovery
missing timestamps/durations handled explicitly
large payload bounded/truncated
evidence provenance references
hidden variants not leaked
reference remediation not leaked
root-cause evaluator metadata not leaked
no chain-of-thought field
SuiteExecution summary links to trials
viewer read access
cross-workspace access denied
pagination stable if implemented
filters deterministic
GitOps unavailable tasks do not fabricate evidence
Stage-33 reliability regression green
no LLM explanation/judging

==================================================
38. VERIFICATION
==================================================

gofmt touched Go files.

Run focused packages only:

new trajectory/evidence projection package
evidence
execution
observation
action executor
evaluator
failuretaxonomy
evaluationsuite
httpapi
reliability

worker only if necessary.

Do not run:

go test ./...
Docker
Kind
live Kubernetes
Argo CD

Frontend:

npx tsc --noEmit

One production build if practical.

==================================================
39. DO NOT IMPLEMENT
==================================================

No:

chain-of-thought capture
LLM-generated explanations
LLM judging
new execution engine
new evaluator
production Kubernetes provider
real GitOps provider
billing
Stripe
governance redesign
CI release gates
video replay
screen recording
session recording
full log-management product
generic observability platform

==================================================
DEFINITION OF DONE
==================================================

Evalplane can open an exact trial and deterministically reconstruct a safe,
ordered operational trajectory from persisted evidence.

The customer can see:

what the agent observed
what it requested
what actually executed
what was denied
what changed
when recovery occurred
what happened after recovery
which deterministic findings caused the final result
whether failure belonged to the agent or evaluation platform

Every meaningful event links back to evidence provenance.

No hidden evaluator state, secrets, hidden variants, reference remediation or
chain-of-thought leaks.

Trajectory remains evidence inspection, not a new qualification engine.

==================================================
COMPLETION REPORT
==================================================

Report:

1. files changed
2. migration/schema
3. trajectory contract/version
4. authoritative evidence sources
5. canonical event model
6. event types/phases/actors
7. deterministic ordering
8. provenance
9. observation projection
10. action lifecycle projection
11. denied/safety events
12. tool-call projection/redaction
13. agent-visible vs evaluator evidence separation
14. state-change derivation
15. recovery detection/verification
16. actions-after-recovery
17. platform-invalid representation
18. outcome summary
19. duration metrics
20. evidence detail API
21. payload bounding/truncation
22. redaction
23. GitOps/delivery trajectory support
24. trajectory comparison
25. SuiteExecution summary
26. API/RBAC
27. pagination/filtering
28. frontend trial trajectory
29. frontend evidence detail
30. frontend outcome explanation
31. frontend platform-invalid UX
32. reproducibility pins
33. security/leakage
34. focused backend tests
35. Stage-33 regression result
36. TypeScript/build
37. confirmation requested != executed
38. confirmation platform-invalid != agent failure
39. confirmation no hidden variant/reference/root-cause leakage
40. confirmation no chain-of-thought storage/exposure
41. confirmation no LLM explanation/judging
42. real-runtime limitations
43. deferred work

Write to:

prompts/reports/trajectory-evidence-explorer.md

DO NOT overwrite this task file.
