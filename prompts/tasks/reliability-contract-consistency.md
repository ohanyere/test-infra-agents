EVALPLANE — STAGE 33 RELIABILITY CONTRACT CONSISTENCY PATCH

Read AGENTS.md first.

Read:

- prompts/reports/qualification-suite-builder.md
- prompts/reports/reliability-eval-science.md
- backend/internal/coverage
- backend/internal/reliability
- environment profile/provider contracts
- qualification planner coverage pins
- reliability comparison tests

This is a narrow Stage-33 correctness patch.

Do not redesign the reliability subsystem.

==================================================
PROBLEM 1 — COVERAGE CONTRACT VERSION
==================================================

Stage 32 preserved:

capability-coverage-contract:v1

and introduced:

capability-coverage-contract:v2

V2 contains additional GitOps/progressive-delivery coverage requirements.

The Stage-33 report says capability reliability currently uses:

coverage.GetRequirement

which is the historical V1 API.

Inspect actual code.

Reliability must explicitly select and pin the coverage contract used for
capability aggregation.

Do not silently use current/global coverage semantics.

Preferred behavior:

- reports pin coverage_contract_version
- existing V1 historical semantics remain reproducible
- current Stage-33 AgentVersion reliability uses the appropriate current
  qualification coverage contract, expected to be V2 where applicable
- GitOps/delivery capability reliability can use their V2 requirements
- capabilities unavailable in the selected contract return structured
  requirement-unavailable state rather than fabricated reliability

Add regression tests proving V1 remains unchanged.

==================================================
PROBLEM 2 — ENVIRONMENT COMPARABILITY
==================================================

Do not require two reliability samples to use the same physical
environment instance ID/ref merely to be comparable.

Compare semantic environment fidelity.

At minimum inspect/pin where available:

- environment profile contract/version
- profile ID
- provider contract/version
- provider ID/class
- isolation level
- runtime contract
- evaluator contract

Different environment instance refs may still be comparable when these
semantic execution properties match.

Keep exact environment_ref as provenance metadata.

Do not use it as the sole fidelity-comparison identity.

If the persisted evidence currently lacks sufficient semantic environment
metadata, return a structured comparability limitation rather than claiming
equal fidelity from opaque refs.

Do not weaken dedicated/virtual-cluster/namespace distinctions.

==================================================
PROBLEM 3 — PASS-RATE DENOMINATOR
==================================================

Make pass-rate semantics explicit and consistent with reliability-metrics-
contract:v1, or version the contract if changing existing semantics is
necessary.

Current report states:

pass_rate = passed / (passed + failed)

while valid inconclusive/stopped trials are omitted.

pass@k/pass^k use n = all valid trials.

This can make pass_rate and k-metrics appear contradictory.

Inspect actual outcome semantics.

Choose one precise design:

A. define primary reliability pass_rate over all valid attempts, where any
   valid non-pass is non-success,

or

B. retain decisive-only pass rate but rename/expose it explicitly as
   decisive_pass_rate and separately expose valid-attempt success rate.

Whichever design is chosen:

- expose valid_trials
- expose decisive_trials
- expose passed_trials
- expose failed_trials
- expose valid inconclusive/stopped counts where applicable
- document which denominator each metric uses
- never hide valid non-passing evidence
- keep platform-invalid outside all agent denominators

Do not silently change historical V1 semantics without a contract-version
decision.

==================================================
TESTS
==================================================

Add focused tests for:

1. reliability report pins coverage contract version
2. V1 coverage semantics remain unchanged
3. V2 GitOps/delivery capability requirements are visible to reliability
4. requirement unavailable remains structured
5. same profile/provider/isolation but different environment instance refs
   can be comparable
6. namespace vs dedicated remains not comparable
7. provider/profile contract mismatch remains not comparable
8. exact environment ref retained as provenance
9. pass-rate denominator explicitly tested
10. valid inconclusive trial behavior explicitly tested
11. platform-invalid remains excluded
12. pass@k/pass^k mathematical vectors unchanged
13. no historical contract mutation
14. no LLM logic introduced

Run focused:

internal/reliability/...
internal/coverage/...
internal/environment/...
internal/qualplan/...
internal/httpapi/... only relevant tests

Do not run go test ./...
Do not start Docker/Kubernetes/Argo CD.

Frontend changes only if API field naming changes.

Run:

npx tsc --noEmit

and one build if frontend types changed.

==================================================
COMPLETION REPORT
==================================================

Report:

1. root causes
2. coverage contract behavior
3. current coverage contract used by reliability
4. GitOps/delivery reliability behavior
5. environment semantic comparability
6. environment provenance behavior
7. pass-rate denominator decision
8. inconclusive/stopped behavior
9. contract-version decision
10. files changed
11. focused tests
12. TypeScript/build if applicable
13. confirmation pass@k/pass^k unchanged
14. confirmation platform-invalid remains excluded
15. confirmation no historical coverage/reliability contract silently mutated

Write to:

prompts/reports/reliability-contract-consistency.md

DO NOT overwrite this task file.
