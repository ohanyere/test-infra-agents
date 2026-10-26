CODEX: GPT-5.6 SOL — MEDIUM

Repository:
~/projects/test-infra-agents

Branch:
infrastructure-agent-verification

TASK:
Perform a narrow post-green-run roadmap reconciliation.

Do NOT implement new features yet.
Do NOT run Kind locally.
Do NOT commit or push.

We have a newly GREEN hosted verification E2E run.

Observed hosted proofs include:

Canonical negative:
- Stage 4 pinned agent implementation provenance
- Stage 4 contract provenance
- Stage 4 provider provenance
- Stage 4 run/result provenance pin agrees
- required evidence complete
- verification validity valid
- final FAILED
- direct-live image mutation executed
- action semantic = direct-live
- infrastructure diff v18 -> v17
- outcome PASSED
- desired/live invariant FAILED
- Git desired v18
- live v17
- Argo OutOfSync
- stability UNSTABLE
- reconciliation risk HIGH

INCONCLUSIVE:
- required Argo evidence genuinely unavailable
- no fabricated safe value
- final INCONCLUSIVE
- direct-live action semantic

WARNING:
- remediation authorized/executed
- action semantic = desired-state
- authoritative/live state v17
- invariants PASSED
- stability STABLE
- meaningful non-blocking headroom risk
- final WARNING

Stage 2:
- step exhaustion -> INCONCLUSIVE
- machine-readable step-limit reason
- temporal recovery -> VERIFIED
- insufficient post-action evidence -> INCONCLUSIVE

Stage 3:
- attributable platform mutation -> ATTRIBUTED
- resource identity and v18 -> v17 transition proven
- contradictory plausible final value -> NOT_ATTRIBUTED
- contradiction blocks VERIFIED

The whole hosted job completed and artifacts were uploaded.

==================================================
1. RECONCILE AGAINST OUR HARDENING ROADMAP
==================================================

Our previous roadmap was:

Stage 1:
explicit evidence applicability / completeness

Stage 2:
termination semantics + generalized temporal recovery evidence

Stage 3:
causal action attribution

Stage 4:
provenance + immutable external-agent implementation identity

Stage 5:
separate action semantics:
direct live mutation != desired-state/Git mutation

Then:
real external-agent integration and independence proof

Inspect CURRENT source and the hosted proof assertions.

For each stage classify:

A. IMPLEMENTED + HOSTED E2E-PROVEN
B. IMPLEMENTED BUT NOT YET HOSTED-PROVEN
C. PARTIALLY IMPLEMENTED
D. NOT IMPLEMENTED

Do not rely on stage names alone.
Judge the actual required properties.

==================================================
2. PARTICULARLY AUDIT STAGE 5
==================================================

Do not assume Stage 5 still needs implementation.

The hosted run already distinguishes:

- direct-live action semantic
- desired-state action semantic

Trace the action model, executor, persistence, verifier, and qualification paths.

Determine whether the original blocker:

"set_workload_image had different execution meanings between controls"

has actually been removed.

Prove from source whether:

- direct live Kubernetes image mutation has its own explicit semantic/type;
- desired-state Git mutation has its own explicit semantic/type;
- the same action identifier no longer ambiguously means both operations;
- persisted actions retain the semantic distinction;
- verifier/evidence logic can distinguish them;
- qualification controls assert the correct semantic.

If all of those are true and the hosted run exercises both semantics,
say clearly:

STAGE 5 IS ALREADY E2E-PROVEN

Do NOT create redundant Stage 5 work.

If anything is still missing, identify the smallest exact gap.

==================================================
3. AUDIT STAGE 4 COMPLETENESS
==================================================

The hosted negative path asserts Stage 4 provenance.

Determine exactly what is pinned:

- AgentVersion identity
- implementation/artifact identity
- model/provider identity if applicable
- protocol/contract version
- evaluator/verifier contract
- observation provider identities
- action policy/executor identity
- run/result provenance agreement

Compare that against the original architecture-audit blocker:

external AgentVersion must identify an immutable implementation,
not merely a mutable endpoint/database record.

State whether Stage 4 is genuinely complete for the current deterministic
reference-agent proof, and what remains specifically for a REAL EXTERNAL AGENT.

Do not confuse:
"reference fixture provenance is pinned"
with
"arbitrary external agent identity is independently proven."

==================================================
4. IDENTIFY THE NEXT REAL BLOCKER
==================================================

If Stages 1-5 are already sufficiently implemented/proven, the next milestone
should NOT be another internal stage.

It should be:

REAL EXTERNAL AGENT INTEGRATION
+
INDEPENDENCE PROOF

Determine the minimum work required to connect one real external infrastructure
agent through the existing direct HTTP protocol.

The external agent must:

- be genuinely separate from the deterministic reference agent;
- use the documented agent protocol;
- have a pinned AgentVersion/implementation identity;
- NOT receive Kubernetes credentials;
- NOT receive Git credentials;
- NOT receive Argo credentials;
- NOT receive database credentials;
- propose actions only through the Realverax-controlled boundary;
- be unable to mutate the environment out of band.

Realverax must independently:

- observe baseline;
- receive agent proposal;
- authorize/execute through its gateway;
- attribute the mutation;
- observe actual resulting state;
- evaluate outcome;
- evaluate invariants;
- evaluate side effects;
- evaluate stability;
- evaluate residual risk;
- produce VERIFIED/WARNING/FAILED/INCONCLUSIVE;
- persist sufficient provenance to reconstruct why.

==================================================
5. DO NOT GENERALIZE YET
==================================================

Do not extract a universal verifier kernel.
Do not add healthcare/finance/robotics abstractions.
Do not redesign the product.

Infrastructure remains the wedge.
==================================================
5.5 DISTINGUISH INTERNAL PROOF FROM EXTERNAL PROOF
==================================================

For every property classified as E2E-PROVEN, state whether it has been proven:

- with the deterministic reference agent only;
- with both reference and external agents;
- or independently of agent implementation.

Do not downgrade properties that are genuinely proven internally.

But do not use reference-agent proof to claim actor independence.

The purpose is to identify exactly which verifier properties are already
stable and which properties remain dependent on proving a real external actor.
==================================================
6. REPORT ONLY
==================================================

Return:

1. Stage 1 status + evidence
2. Stage 2 status + evidence
3. Stage 3 status + evidence
4. Stage 4 status + exact remaining gap, if any
5. Stage 5 status + whether semantic split is genuinely complete
6. Which original architecture-audit blockers remain unresolved
7. the single next engineering milestone
8. minimum implementation needed for it
9. what must be demonstrated in hosted E2E to call that milestone proven

No implementation.
No commit.
No push.
No Kind.
