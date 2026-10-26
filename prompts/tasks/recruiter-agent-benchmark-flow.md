CODEX: GPT-5.6 SOL MEDIUM

Repository:
~/projects/test-infra-agents

Branch:
infrastructure-agent-verification

CURRENT HOSTED FAILURE

The hosted qualification itself succeeded.

Passed assertions include:

- verification identity and contracts
- Stage 4 pinned agent implementation provenance
- Stage 4 contract provenance

The only failing assertion is:

PROOF ASSERTION FAILED: Stage 4 provider provenance

The canonical verifier result is otherwise correct:

overall_status = FAILED
outcome = PASSED
side_effects = NONE
Git desired = evalplane-reference:v18
live = evalplane-reference:v17
Argo = OutOfSync
DESIRED_LIVE_STATE_CONSISTENCY = FAILED
stability = UNSTABLE
gitops reconciliation risk = HIGH

Do NOT modify these semantics.

==================================================
1. FIND THE EXACT FAILING ASSERTION
==================================================

Locate the hosted workflow/script assertion named:

"Stage 4 provider provenance"

Show the exact expected condition / jq expression / shell test.

Determine exactly which provider provenance value is:

- missing,
- null,
- empty,
- different from expected,
- not persisted,
- not projected through API,
- or using a stale expected literal.

Do not edit anything before identifying the exact mismatch.

==================================================
2. TRACE PROVIDER PROVENANCE END TO END
==================================================

Trace provider provenance through:

collector/provider implementation
→ observation/evidence creation
→ evaluation
→ VerificationResult derivation
→ persistence
→ repository read
→ HTTP/API projection if applicable
→ hosted proof assertion

Identify the authoritative source for each provider identity.

At minimum inspect provenance for the providers used by this canonical run:

- Kubernetes/runtime observation provider
- telemetry/Prometheus provider
- Git desired-state provider
- Argo provider
- action/execution provider if represented
- stability provider if represented

Do not invent provenance values in the workflow.

==================================================
3. DISTINGUISH THE FAILURE TYPE
==================================================

Classify the defect as exactly one of:

A. provider provenance was never produced;
B. produced but not persisted;
C. persisted but not returned/projected;
D. projected but sanitized out of proof diagnostics;
E. assertion expects an incorrect/stale value;
F. version/provider identity is overwritten by another contract;
G. another concrete source-level cause.

Report evidence for the classification.

==================================================
4. FIX THE SOURCE OF TRUTH
==================================================

Make the smallest correct fix.

Important:

Do NOT weaken the Stage 4 provider-provenance assertion merely to make CI green.

If provider provenance is required by Stage 4, the persisted verification
record must actually contain trustworthy provider identity/version data.

If the assertion itself is stale and persisted provenance is already correct,
fix only the assertion.

Do NOT modify:

- verdict derivation
- FAILED/VERIFIED/WARNING/INCONCLUSIVE semantics
- GitOps invariant logic
- recovery telemetry semantics
- stability semantics
- causal attribution semantics
- canonical fixture behavior
- agent action sequence
- v18/v17 behavior

==================================================
5. IMPROVE DIAGNOSTICS IF NEEDED
==================================================

The current sanitized diagnostic does not make the failed provider provenance
obvious.

If useful, make the hosted failure diagnostic print a sanitized provider
provenance projection such as:

provider id
provider contract/version
evidence type
availability/applicability

Do not expose credentials, endpoints containing secrets, or tokens.

The next provider-provenance failure should tell us exactly what is missing or
mismatched.

==================================================
6. ADD FOCUSED TESTS
==================================================

Add or update focused tests proving:

- provider provenance is derived;
- provider provenance survives persistence roundtrip;
- historical/non-Stage-4 records remain compatible where required;
- API/read model preserves provider provenance if the hosted assertion reads
  through that path;
- Stage 4 result rejects/flags missing required provenance according to the
  current contract;
- distinct providers do not overwrite each other's identities.

Do not broaden this into another architecture refactor.

==================================================
7. VALIDATE CHEAPLY FIRST
==================================================

Run focused tests only.

Also run:

- gofmt on changed Go files
- relevant Go package tests
- workflow YAML parse if workflow changed
- bash -n for changed shell/workflow blocks
- git diff --check

No local Kind.

==================================================
8. REPORT BEFORE COMMITTING
==================================================

Report:

1. exact failing assertion;
2. expected provider provenance;
3. actual provider provenance;
4. exact root cause;
5. files changed;
6. focused tests run;
7. whether canonical verifier semantics changed
   - expected: NO;
8. whether hosted E2E should now proceed past Stage 4 provider provenance.

Do not commit.
Do not push.
