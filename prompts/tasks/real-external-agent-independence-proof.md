CODEX: GPT-5.6 SOL — MEDIUM

Repository:
~/projects/test-infra-agents

Branch:
infrastructure-agent-verification

TASK:
Build the next milestone identified by the Stage 1–5 reconciliation:

REAL EXTERNAL AGENT INTEGRATION + INDEPENDENCE PROOF

Do not generalize the verifier.
Do not start a new product architecture.
Do not touch already-proven Stage 1–5 semantics unless direct evidence requires it.

No local Kind.
Do not commit.
Do not push.

==================================================
CURRENT PROVEN STATE
==================================================

The latest reconciliation established:

Stage 1 — E2E-PROVEN
- explicit evidence applicability/completeness
- required evidence
- four verdict classes
- missing required evidence -> INCONCLUSIVE
- no fabricated values

Stage 2 — PROVEN
- no-op terminal behavior
- agent_step_limit_reached cannot become VERIFIED
- post-latest-mutation complete-lookback temporal semantics
- insufficient evidence -> INCONCLUSIVE

Stage 3 — PROVEN
- live Kubernetes action attribution
- desired-state Git evidence attribution
- ATTRIBUTED and NOT_ATTRIBUTED paths
- attribution failure blocks VERIFIED

Stage 4 — implemented for current reference proof
- contracts/provenance pinned
- run/result identity agreement
- implementation identity fields exist
- remaining gap:
  ProvenanceLevel is still fundamentally self-attested for an external agent;
  nothing independently binds an external agent's artifact_digest /
  model-provider identity to the code actually serving requests.

Stage 5 — E2E-PROVEN
- typed live vs desired-state action semantics
- live action goes only through live executor
- desired-state action goes through Git path
- verifier routes by action type
- attribution/action-execution provenance preserved
- no silent legacy switching

Current remaining work:

1. Positive-control hosted confirmation on current HEAD.
   This is confirmation only, not new engineering.

2. REAL EXTERNAL AGENT INTEGRATION + INDEPENDENCE PROOF.
   This is the milestone to implement now.

==================================================
PRIMARY PRODUCT QUESTION
==================================================

Prove:

Can Realverax independently verify a genuinely separate infrastructure agent
that Realverax does not control internally?

The external agent must be able to report success.

Realverax must independently determine whether the resulting infrastructure
state is acceptable.

The external agent must NOT be able to influence its own verifier verdict.

==================================================
1. START WITH SOURCE RECONCILIATION
==================================================

Before editing, inspect the existing implementation for:

- agent.Create
- AgentVersion
- agentConnection
- external/direct HTTP agent protocol
- action proposal contract
- action gateway
- execution policy
- verifier
- provenance structures
- action attribution
- qualification harness
- current positive/negative/warning/inconclusive controls

Do NOT rebuild machinery that already exists.

Identify the minimum existing extension points required to onboard one
external agent.

Report internally what you found, then implement.

==================================================
2. EXTERNAL AGENT MUST BE A REAL SEPARATE ACTOR
==================================================

Create/integrate ONE separate external infrastructure agent implementation.

It must run as a separate process/container/service from:

- Realverax API
- verifier
- worker
- action gateway
- qualification harness

It must communicate only through the existing external agent protocol.

Prefer the existing direct HTTP protocol for this milestone.

Do NOT introduce MCP as another variable unless the repository already
requires MCP for this exact path.

The external agent must not be compiled into the verifier process and must not
share verifier-internal function calls.

==================================================
3. CREDENTIAL ISOLATION IS NON-NEGOTIABLE
==================================================

The external agent must have NO:

- Kubernetes credentials
- kubeconfig
- Git write credentials
- Argo credentials
- PostgreSQL/database credentials
- cloud credentials
- direct access to verifier persistence
- direct filesystem access to Realverax state

The agent may only:

observe what the protocol intentionally exposes
        ↓
reason
        ↓
propose an action
        ↓
receive platform result

All actual infrastructure mutations must continue through the existing
Realverax action policy/execution boundary.

Add explicit hosted proof/assertions that the external agent runtime does not
receive prohibited credentials/secrets.

Do not merely document this assumption.

==================================================
4. ONBOARD THROUGH EXISTING PRODUCT CONTRACTS
==================================================

Use the real registration/onboarding path.

At minimum exercise:

agent.Create
→ AgentVersion
→ agentConnection
→ protocol interaction
→ proposed action
→ authorized execution
→ observation
→ evaluation
→ VerificationResult

Do not insert the agent directly into qualification internals.

The proof should resemble what a future customer agent would actually do.

==================================================
5. INDEPENDENT IMPLEMENTATION IDENTITY
==================================================

Close the remaining Stage 4 provenance gap for THIS external proof.

Current problem:

An agent can declare something like:

artifact_digest = X

but declaration alone does not prove that X identifies the code actually
serving the protocol.

For the hosted proof, establish a platform-observed implementation identity.

Prefer a strong, simple mechanism such as:

external-agent container image
        ↓
immutable image digest
        ↓
platform/qualification environment observes digest independently
        ↓
AgentVersion pins expected digest
        ↓
VerificationResult/provenance references the observed/pinned identity

The external agent must not be the sole authority for this value.

If the agent uses an LLM/provider, also preserve declared:

- provider
- model identifier
- relevant version/config identity where available

But do NOT pretend a provider/model string cryptographically identifies the
serving implementation.

Distinguish:

PLATFORM-OBSERVED artifact identity

from:

AGENT-DECLARED metadata

Persist/prove that distinction.

Do not build a full signing/attestation PKI unless actually required for the
milestone.

==================================================
6. BUILD THE EXTERNAL VERIFIED CONTROL
==================================================

Create one hosted external-agent qualification control that can legitimately
reach VERIFIED.

Use a deterministic infrastructure scenario where:

- baseline is known
- intent is explicit
- required evidence is available
- agent observes permitted information
- agent proposes a correct action
- Realverax authorizes/executes it
- resulting live state is correct
- desired state is correct where applicable
- attribution succeeds
- outcome passes
- invariants pass
- no blocking side effect exists
- stability passes
- no blocking residual risk exists
- evidence completeness passes
- implementation provenance passes

Expected result:

VERIFIED

But VERIFIED must emerge from the existing verifier derivation.

Do NOT hard-code:

external agent => VERIFIED

==================================================
7. BUILD A HOSTILE / ADVERSARIAL PROBE
==================================================

The same external-agent boundary must also prove that independence is real.

Create a narrow hostile-probe control.

The agent should attempt at least one action that existing policy forbids or
does not authorize.

Examples should be chosen from ACTUAL existing policy capabilities, not
invented APIs.

The proof must establish:

external agent requests forbidden action
        ↓
Realverax policy denies it
        ↓
mutation does not occur
        ↓
denial/safety evidence is persisted
        ↓
external agent cannot convert that denial into VERIFIED

IMPORTANT:

Do not invent a new verdict rule solely for this test.

Use existing derivation semantics.

The hard requirement is:

a denied/unsafe/incomplete external action must NEVER produce VERIFIED.

Assert the precise FAILED/INCONCLUSIVE result only if current semantics
deterministically require one.

==================================================
8. PROVE NO OUT-OF-BAND MUTATION
==================================================

This is one of the most important assertions.

For the external-agent proof, show that observed infrastructure mutation can
be linked to Realverax's execution boundary rather than direct agent access.

Use the attribution work already proven in Stage 3.

Proof should include relevant platform-observed facts such as:

- requested action
- authorized action
- executor action
- target
- requested value
- observed before/after identity where available
- resource UID/generation/resourceVersion if already captured
- action/execution IDs
- temporal relationship
- resulting state evidence

Do not create a fake causal claim unsupported by evidence.

The result must demonstrate that the external agent cannot simply mutate the
environment and then ask Realverax to bless the result.

==================================================
9. AGENT TRAFFIC MUST NOT LEAK SECRETS
==================================================

Add a bounded proof/assertion that agent-facing request/response traffic
contains no:

- kubeconfig
- Kubernetes bearer token
- DB URL/password
- Git credentials
- Argo credentials
- cloud credentials
- Realverax internal secrets

Do not log secrets merely to prove they are absent.

Inspect schemas/keys/allowed payloads safely.

==================================================
10. TERMINATION SEMANTICS MUST REMAIN PROVEN
==================================================

The external agent must obey existing Stage 2 limits.

Explicitly confirm:

- max agent steps remain bounded
- agent_step_limit_reached cannot become VERIFIED
- protocol timeout/error cannot become fabricated success
- missing required evidence remains INCONCLUSIVE where existing semantics say so

Do not special-case external agents around these protections.

==================================================
11. POSITIVE CONTROL HEAD CONFIRMATION
==================================================

Before claiming the milestone complete, ensure the current positive control is
still included in hosted qualification on HEAD.

This is not a redesign task.

Expected existing result:

healthy reference-agent path -> VERIFIED

This acts as a regression anchor alongside the external-agent proof.

==================================================
12. HOSTED QUALIFICATION TARGET
==================================================

The final hosted qualification milestone should prove at minimum:

REFERENCE CONTROLS

unsafe state
→ FAILED

healthy state
→ VERIFIED

non-blocking concern
→ WARNING

missing required evidence
→ INCONCLUSIVE


EXTERNAL AGENT CONTROLS

correct external remediation
→ VERIFIED

forbidden/hostile external request
→ never VERIFIED


INDEPENDENCE ASSERTIONS

external agent has no infra/database/Git/Argo credentials
platform executes mutations
action attribution succeeds where appropriate
external artifact identity is independently observed/pinned
agent cannot set its own outcome/invariants/stability/risk/verdict
termination semantics remain enforced

==================================================
13. TESTING LADDER
==================================================

Do not begin with giant hosted E2E debugging.

First:

LEVEL 1 — unit/source
- external registration
- protocol parsing
- implementation identity
- provenance
- hostile denial semantics
- credential isolation configuration

LEVEL 2 — component
- external agent container/service starts
- protocol handshake
- AgentVersion/connection works
- image/artifact digest observed correctly
- allowed action proposal works
- forbidden proposal is denied

LEVEL 3 — hosted E2E
- external VERIFIED control
- hostile probe
- reference positive control regression

LEVEL 4 — full regression
- existing four verdict controls remain green

==================================================
14. REUSE PREVIOUS E2E LESSONS
==================================================

Do not copy old controls blindly.

But reuse already-learned reliability patterns:

- bounded readiness
- no arbitrary sleeps
- explicit diagnostics
- asynchronous cleanup waits
- no hidden set -e exits
- fixture isolation
- per-run namespaces
- deterministic cleanup
- explicit migration/version diagnostics
- complete telemetry windows
- artifact upload on failure
- preserve failed-container logs where useful

Do not rediscover solved operational failures.

==================================================
15. DO NOT DO THESE THINGS
==================================================

Do not:

- generalize Realverax to healthcare/finance/etc
- extract a universal verifier kernel
- add robotics
- add MCP marketplace functionality
- redesign Stage 1–5
- run local Kind
- mutate remote EKS
- add customer multi-tenancy
- build billing
- build production SaaS deployment
- perform unrelated frontend redesign
- modify app/realverax branding files
- stage unrelated working-tree changes

==================================================
16. VALIDATION
==================================================

Run appropriate focused tests.

Also:

- gofmt on changed Go
- bash -n on changed scripts/workflow run blocks
- workflow YAML parse
- git diff --check
- focused Docker/container validation where relevant
- database migration validation only if schema changes are actually required

Do not claim E2E-PROVEN from local tests.

Hosted GitHub Actions remains runtime truth.

==================================================
17. REPORT BEFORE COMMIT
==================================================

Report:

1. external agent implementation chosen;
2. why it qualifies as a separate actor;
3. exact onboarding path;
4. credential isolation mechanism;
5. action path;
6. platform-observed implementation identity mechanism;
7. provenance changes;
8. external VERIFIED scenario;
9. hostile-probe scenario;
10. no-out-of-band-mutation proof;
11. termination behavior;
12. positive-control regression status;
13. tests run;
14. files changed;
15. remaining hosted-only assertions;
16. any blocker that prevents a defensible independence claim.

Do NOT commit.
Do NOT push.

Stop and report before commit/push.
