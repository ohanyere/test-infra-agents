CODEX: GPT-5.6 SOL — MEDIUM

Repository:
~/projects/test-infra-agents

Branch:
infrastructure-agent-verification

CURRENT HOSTED E2E FAILURE

The canonical negative qualification is now GREEN.

It successfully proved:

- infrastructure-verifier:v3
- evidence-requirements:v1
- Stage 4 agent implementation provenance
- Stage 4 contract/provider provenance
- required evidence complete
- direct-live action semantics
- outcome PASSED
- side effects NONE
- desired/live invariant FAILED
- Git desired v18
- Argo OutOfSync
- live v17
- stability UNSTABLE
- reconciliation risk HIGH
- final FAILED
- cleanup succeeded

DO NOT modify or weaken that path.

The next control, qualification-positive, fails.

Observed sequence:

- environment setup succeeds
- baseline capture succeeds
- telemetry starts
- agent execution starts
- action execution succeeds repeatedly
- immediate outcome/post-action snapshots repeat many times
- eventually:

worker_failure:
agent: agent_step_limit_reached: agent step limit reached

worker_execution status=failed

qualification fails.

IMPORTANT:

The new termination behavior is doing its job.

DO NOT fix this by:
- increasing the step limit;
- treating step exhaustion as success;
- allowing evaluation to continue to VERIFIED after exhaustion;
- weakening termination semantics.

We need to determine why the positive agent/control never reaches its intended
successful terminal state.

==================================================
1. TRACE THE POSITIVE CONTROL LOOP
==================================================

Inspect the positive-control path only.

Start from:

backend/e2e/run-positive-control.sh
backend/e2e/qualification-positive.yaml
backend/cmd/e2equalification/positive_control.go
backend/cmd/e2equalification/positive_control_test.go

Then trace into the shared worker/agent loop as needed.

Establish exactly:

1. what the positive reference agent observes on each step;
2. what action it proposes;
3. what action is executed;
4. how desired/live state changes after each action;
5. what condition should tell the agent that remediation is complete;
6. why that condition is never causing a terminal successful response.

Do not edit until the loop is understood.

==================================================
2. DETERMINE WHETHER THIS IS A FIXTURE BUG OR PRODUCT BUG
==================================================

Classify the root cause precisely as one of:

A. positive reference-agent state machine bug;
B. fixture HTTP state not advancing;
C. Git desired-state mutation not persisting;
D. Argo convergence not being observed correctly;
E. terminal-condition detection bug;
F. action semantic mismatch;
G. worker loop incorrectly asking for another step after completion;
H. another source-supported cause.

Do not assume which one it is.

==================================================
3. INSPECT THE ACTION HISTORY
==================================================

Add or use focused diagnostics/tests to determine the exact sequence of
positive-control actions.

We need to know whether the agent is repeatedly proposing:

- set_desired_workload_image;
- inspect;
- no_op;
- another action;

and what state it sees after each one.

The correct positive flow should eventually reach a state equivalent to:

Git desired            v17
Live workload          v17
Argo                    Synced
Outcome                 PASSED
Required evidence       AVAILABLE
Stability               STABLE
No blocking risk

At that point the reference agent must stop proposing remediation and terminate
successfully according to the protocol.

Do NOT hard-code VERIFIED into the agent.

VERIFIED must still be derived independently by the verifier.

==================================================
4. PRESERVE ACTION SEMANTICS
==================================================

The negative control now correctly proves:

set_live_workload_image
= direct mutation of live Kubernetes state

The positive control must use its intended desired-state/GitOps semantic.

Do not make:

set_live_workload_image
and
set_desired_workload_image

mean the same thing merely to make the test pass.

If the positive control is using set_desired_workload_image, verify that:

- Git desired state actually changes;
- Argo sees that desired state;
- reconciliation reaches live state;
- the agent can observe sufficient completion state;
- subsequent agent behavior terminates rather than reissuing the mutation.

==================================================
5. PRESERVE STEP-EXHAUSTION SAFETY
==================================================

Keep this invariant:

agent_step_limit_reached
→ non-successful worker termination

It must never silently proceed to VERIFIED.

Add/retain a focused test proving that invariant if one is not already present.

The positive control should pass by reaching a legitimate terminal condition
BEFORE the limit, not by modifying the limit behavior.

==================================================
6. USE THE HOSTED LOG AS A REGRESSION BOUNDARY
==================================================

Do not touch the already-green canonical negative behavior unless direct source
evidence proves shared code is defective.

The following are now proven in hosted CI and should remain frozen:

- cluster setup
- Argo setup
- calibration
- Git fixture
- database
- provenance
- evidence completeness
- negative direct-live action
- FAILED derivation
- cleanup

This task is specifically about the positive control's execution/termination
path.

==================================================
7. VALIDATE CHEAPLY BEFORE ANOTHER HOSTED RUN
==================================================

No local Kind.

First write/run focused tests that reproduce the positive agent sequence.

Prefer a deterministic test that feeds the reference agent successive states:

initial unhealthy state
→ desired-state remediation requested
→ Git desired changes
→ convergence state
→ healthy desired/live state

and proves:

- remediation action is proposed only when appropriate;
- once completion is observable, the agent returns terminal success;
- action count remains bounded;
- step limit is not reached.

Also run relevant existing tests for:

- positive control;
- worker termination;
- action semantics;
- GitOps state handling.

Then:

- gofmt changed Go files
- bash -n changed scripts if any
- workflow YAML parse if touched
- git diff --check

==================================================
8. REPORT BEFORE COMMITTING
==================================================

Report:

1. exact root cause;
2. exact repeated action sequence seen by the positive control;
3. why the agent failed to terminate;
4. whether the defect was in reference-agent fixture code or product worker code;
5. files changed;
6. focused test added/updated;
7. step-exhaustion semantics unchanged? expected YES;
8. negative-control semantics unchanged? expected YES;
9. validation results.

Do not commit.
Do not push.
Do not run Kind.
