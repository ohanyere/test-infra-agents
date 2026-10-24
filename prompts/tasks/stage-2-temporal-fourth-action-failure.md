CODEX: SOL MEDIUM

Repository:
~/projects/test-infra-agents

Branch:
infrastructure-agent-verification

TASK:
Diagnose and fix ONLY the hosted Stage 2 temporal qualification failure.

Do not revisit Stage 1.
Do not change already-proven Stage 1 semantics.
Do not run local Kind.
Do not commit or push.

==================================================
CURRENT HOSTED RESULT
==================================================

The hosted E2E now proves:

Stage 1 negative       -> FAILED        PASS
Stage 1 positive       -> VERIFIED      PASS
Stage 1 inconclusive   -> INCONCLUSIVE  PASS
Stage 1 warning        -> WARNING       PASS

Stage 2 step-limit:
step exhaustion -> INCONCLUSIVE         PASS

The only failing control is:

Stage 2 temporal

Its qualification pod reaches:

- setup
- criteria
- database
- auth
- agent registration
- scenario
- batch
- run claim
- worker construction
- environment setup
- baseline capture
- telemetry start
- agent execution

Then this sequence occurs:

action_execution started
action_execution completed
immediate_outcome_and_post_action_snapshot completed

action_execution started
action_execution completed
immediate_outcome_and_post_action_snapshot completed

action_execution started
action_execution completed
immediate_outcome_and_post_action_snapshot completed

action_execution started
action_execution FAILED

worker_failure:
platform: action_execution_failed: controlled action failed

The worker then persists final state and cleans up correctly.

==================================================
1. DO NOT TOUCH PROVEN BEHAVIOR
==================================================

Freeze:

- all four Stage 1 controls
- evidence-requirements:v1
- infrastructure-verifier:v3
- Stage 1 verdict derivation
- Stage 1 GitOps evidence semantics
- Stage 1 action semantics
- Stage 2 step-limit behavior
- provenance assertions already passing
- cleanup behavior already passing

The current failure is specifically in the Stage 2 temporal control's
action execution sequence.

==================================================
2. IDENTIFY THE EXACT FOURTH ACTION
==================================================

Trace the Stage 2 temporal control from:

- workflow invocation
- run-stage2-control.sh
- temporal scenario/fixture
- reference agent behavior
- action proposal
- action validation
- action authorization
- action executor
- controlled action implementation

Determine exactly:

1. what actions 1, 2, and 3 were;
2. what action 4 was;
3. its exact payload;
4. why actions 1-3 succeed;
5. why action 4 fails.

Do not infer from the generic error alone.

Inspect persisted action/event data and qualification logs if available.

If the workflow artifact is available, inspect the relevant logs from:

verification-e2e-36996936135

The hosted run ID was:

36996936135

Artifact name:

verification-e2e-36996936135

Use existing GitHub authentication if available.
Do not ask for tokens.

==================================================
3. DETERMINE FAILURE CLASSIFICATION
==================================================

Classify the root cause as one of:

A. temporal fixture bug
B. reference-agent action sequence bug
C. action validator rejection
D. authorization/policy rejection
E. executor/runtime failure
F. Kubernetes resource/state mismatch
G. stale action assumptions after prior actions
H. intended temporal semantics incorrectly implemented

Provide source evidence for the classification.

==================================================
4. CHECK FOR SEQUENCE-DEPENDENT STATE
==================================================

This control performs multiple actions.

Inspect whether one of the earlier successful actions changes state that makes
the fourth action invalid.

Check specifically for:

- expected old image/version no longer matching;
- duplicate mutation;
- action precondition already satisfied;
- stale resourceVersion/generation;
- target resource no longer present;
- target image/state already changed;
- reference agent proposing an action that is invalid after prior actions;
- direct-live vs desired-state semantic mismatch;
- action capability/validator mismatch.

Do not "fix" this by simply ignoring the failed action.

==================================================
5. PRESERVE THE PURPOSE OF STAGE 2 TEMPORAL PROOF
==================================================

The Stage 2 temporal control is supposed to prove generalized post-action
temporal evidence semantics.

The intended proof is conceptually:

bad evidence before remediation
        ↓
mutating action at T0
        ↓
complete post-action observation window
        ↓
recovery metrics evaluated from eligible post-action evidence

and:

insufficient post-action evidence
        ↓
must not fabricate VERIFIED/FAILED from stale pre-action data

Do not weaken or bypass this proof merely to make the pod succeed.

The qualification should succeed because the intended action sequence is
valid and the temporal assertions are satisfied.

==================================================
6. APPLY THE SMALLEST FIX
==================================================

Once the exact failure is proven:

- change only the temporal fixture/agent/action/executor code directly responsible;
- do not refactor unrelated execution logic;
- do not alter Stage 1 controls;
- do not alter step-limit semantics;
- do not change verdict precedence unless direct evidence shows the temporal
  contract itself is wrong.

==================================================
7. ADD BETTER DIAGNOSTICS IF NEEDED
==================================================

The current hosted log only reports:

platform: action_execution_failed: controlled action failed

If the existing diagnostics lose the actual action identity or executor cause,
make the smallest improvement so a future failure records:

- action type
- target
- requested value
- action sequence/step number
- validation/authorization/execution stage
- underlying safe error reason

Do not expose secrets.

==================================================
8. VALIDATION
==================================================

Before another hosted push:

Run focused tests for:

- Stage 2 temporal behavior
- affected action executor/validator
- post-action evidence window semantics
- Stage 2 step-limit regression
- Stage 1 verdict regression if cheap

Also run:

- gofmt for changed Go files
- bash -n for changed shell
- workflow YAML parse if workflow changes
- git diff --check

No local Kind.

==================================================
9. REPORT BEFORE COMMIT
==================================================

Return:

1. exact fourth action;
2. exact root cause;
3. why first three actions succeeded;
4. why fourth failed;
5. files changed;
6. smallest fix;
7. temporal proof semantics preserved? YES/NO;
8. Stage 1 semantics changed? expected NO;
9. Stage 2 step-limit changed? expected NO;
10. focused test results.

Do not commit.
Do not push.
