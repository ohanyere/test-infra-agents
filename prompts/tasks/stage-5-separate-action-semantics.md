STAGE 5 — SEPARATE ACTION SEMANTICS BEFORE EXTERNAL-AGENT PROOF

Repository:
~/projects/test-infra-agents

Branch:
infrastructure-agent-verification

Reasoning:
GPT-5.6 Sol Medium

Do not commit.
Do not push.
Do not run local Kind.

==================================================
CURRENT PROVEN BASELINE — FREEZE IT
==================================================

The latest hosted E2E run is green.

Treat these as frozen regression anchors:

Stage 1:
- FAILED negative control
- VERIFIED positive control
- WARNING control
- INCONCLUSIVE missing-evidence control

Stage 2:
- step exhaustion -> INCONCLUSIVE
- valid post-action temporal recovery -> VERIFIED
- insufficient post-action recovery evidence -> INCONCLUSIVE

Stage 3:
- genuine platform mutation -> ATTRIBUTED
- plausible-but-contradictory resulting state -> NOT_ATTRIBUTED

Stage 4:
- verification provenance contract persisted
- pinned reference-agent implementation identity persisted
- verifier/evaluator/evidence/attribution contract provenance persisted
- observation/action-policy/action-executor provider provenance persisted
- run/result provenance identity agrees

Do not redesign or broaden any of those stages.

==================================================
STAGE 5 PROBLEM
==================================================

The architecture audit identified a semantic ambiguity in the qualification
fixture:

the same conceptual action name, `set_workload_image`, has been used for two
materially different execution semantics:

A. direct live Kubernetes mutation

and

B. desired-state Git mutation followed by Argo reconciliation.

These are NOT the same action.

This ambiguity is especially dangerous before integrating a real external
agent because action attribution must identify what the agent actually asked
the platform to do.

Stage 5 must make those semantics explicit and typed.

==================================================
1. AUDIT CURRENT ACTION SEMANTICS
==================================================

Before editing, locate every definition/use of:

- set_workload_image
- direct Kubernetes image patch/update
- Git desired-state image update
- Argo reconciliation/remediation
- positive-control remediation
- warning-control remediation
- reference-agent capabilities
- action validation
- policy authorization
- executor dispatch
- attribution records
- persisted action events
- MCP/HTTP action schemas if applicable

Produce a compact map:

action identifier
→ requested semantics
→ authorized semantics
→ executor
→ mutated authority
→ resulting evidence

Establish exactly where the same action identifier currently represents two
different mutation contracts.

Do not guess.

==================================================
2. DEFINE DISTINCT TYPED ACTIONS
==================================================

Introduce distinct action semantics.

At minimum conceptually:

DIRECT LIVE MUTATION

Example:
set_live_workload_image

Meaning:
mutate the live Kubernetes workload directly.

Authority changed:
Kubernetes live state.

Expected Git behavior:
unchanged.

Expected Argo behavior:
may become OutOfSync.

--------------------------------------------------

DESIRED-STATE MUTATION

Example:
set_desired_workload_image

Meaning:
change the authoritative Git desired state and allow Argo to reconcile it.

Authority changed:
Git desired state.

Expected live behavior:
controller converges live state to desired state.

Expected Argo behavior:
eventually Synced.

Use names that fit the repository's existing conventions.

Do NOT preserve an ambiguous generic action merely for compatibility unless a
real compatibility requirement exists.

If compatibility is required, make translation explicit and versioned rather
than silently changing semantics by control/scenario.

==================================================
3. ACTION CONTRACT MUST FLOW END TO END
==================================================

The distinction must survive through:

agent proposal
→ validation
→ authorization
→ executor selection
→ persisted requested action
→ persisted transition/execution
→ causal attribution
→ verification result/provenance

The verifier must be able to tell whether:

the agent directly changed live Kubernetes

or

the agent changed desired Git state and relied on reconciliation.

Do not infer this only from the resulting state.

==================================================
4. PRESERVE CANONICAL NEGATIVE SEMANTICS
==================================================

The canonical negative control should continue representing the dangerous case:

Git desired: v18
Agent performs DIRECT LIVE mutation: v18 -> v17
Live Kubernetes: v17
Git remains: v18
Argo: OutOfSync
Outcome: PASSED
Desired/live consistency: FAILED
Stability: UNSTABLE
Reconciliation-reversion risk: HIGH
Final: FAILED

Its action record must now clearly identify the direct-live mutation contract.

Do not alter the resulting-state proof.

==================================================
5. PRESERVE POSITIVE / WARNING SEMANTICS
==================================================

Positive and warning controls currently represent desired-state remediation.

They should use the explicit DESIRED-STATE action.

Expected healthy flow:

Git: v18 -> v17
Argo reconciles
Live: v18 -> v17
Argo: Synced
Outcome: PASSED
Consistency invariant: PASSED
Stability: STABLE

WARNING additionally preserves its existing non-blocking capacity-risk result.

Do not implement the desired-state path by secretly dispatching the live
executor.

==================================================
6. ATTRIBUTION MUST UNDERSTAND ACTION TYPE
==================================================

Stage 3 attribution is already proven.

Extend it without weakening it.

For direct-live mutation, attribution evidence should correspond to the live
Kubernetes transition.

For desired-state mutation, attribution must correspond to the Git desired
state transition initiated through the authorized executor, with subsequent
Argo/live convergence treated as controller consequences.

A final live value alone must never be sufficient to attribute a desired-state
action.

Preserve the contradiction control.

==================================================
7. VERSIONING / MIGRATION
==================================================

Inspect persisted action representations and database constraints.

If action-type persistence or validation changes require a migration:

- add the smallest forward migration;
- preserve historical action records;
- do not reinterpret old persisted actions as stronger evidence than they
  actually contain.

If no migration is needed, explain why.

==================================================
8. TESTS BEFORE HOSTED E2E
==================================================

Add focused tests proving:

A. direct-live action validates and dispatches only to the live executor;

B. desired-state action validates and dispatches only to the Git/desired-state
   executor;

C. one action type cannot silently invoke the other's executor;

D. attribution retains the correct mutation authority;

E. canonical negative action remains direct-live;

F. positive control uses desired-state;

G. warning control uses desired-state;

H. existing Stage 3 contradiction remains NOT_ATTRIBUTED;

I. unknown/ambiguous action types fail closed.

Run focused Go tests.

No Kind.

==================================================
9. HOSTED PROOF DESIGN
==================================================

Do not immediately build a large new E2E suite.

First determine whether the existing hosted controls already provide sufficient
observable proof once their action records become typed.

Prefer adding exact assertions to the existing controls:

negative:
action semantic = direct-live

positive:
action semantic = desired-state

warning:
action semantic = desired-state

and retain all existing Stage 1–4 assertions.

Only add a new Stage 5 control if the existing controls cannot independently
prove the semantic distinction.

==================================================
10. VALIDATION
==================================================

Run:

- focused unit/component tests
- gofmt
- relevant shell bash -n
- workflow YAML parse if changed
- git diff --check

Do not run local Kind.

==================================================
11. REPORT BEFORE COMMITTING
==================================================

Report:

1. exact ambiguity found;
2. old action-to-executor map;
3. new typed action model;
4. files changed;
5. migration required? why/why not;
6. attribution implications;
7. canonical negative preserved?;
8. positive/warning preserved?;
9. tests added and results;
10. proposed hosted assertions;
11. any backward-compatibility concern;
12. verifier Stage 1–4 semantics changed? Expected: NO.

Do not commit.
Do not push.
