EVALPLANE — SCENARIO CERTIFICATION API HARDENING

Read AGENTS.md first.

Read:
- prompts/tasks/scenario-certification.md
- prompts/reports/scenario-certification.md
- internal/scenariocertification
- scenario certification HTTP handlers/tests

This is a SMALL SECURITY / INTEGRITY PATCH.

Do not redesign certification.

==================================================
PROBLEM
==================================================

The current public certification trigger accepts test/internal controls such as:

simulate
variant_configs
reference_remediation

Authoritative certification must remain server-derived.

A workspace user may trigger certification.

They must not control the deterministic failure-injection model or choose the
authoritative hidden certification variants.

==================================================
1. REMOVE SIMULATION FROM PUBLIC HTTP API
==================================================

The certification POST endpoint must NOT accept:

simulate

Keep simulation/failure-injection structures available internally for focused
unit tests if useful.

They must not be part of the production HTTP request DTO.

Because DisallowUnknownFields is already used:

{"simulate": ...}

must return 400.

Add an explicit API test.

==================================================
2. SERVER-OWNED CERTIFICATION VARIANTS
==================================================

Remove caller-controlled:

variant_configs

from the authoritative certification HTTP request.

Certification must deterministically select its representative variants from
the server-owned ScenarioVersion / hidden-variant contract.

Reuse the existing deterministic variant-selection implementation where
possible.

Tests may inject variants through internal service/engine helpers.

Production HTTP callers may not choose the variant set that proves a task.

Add an HTTP test asserting variant_configs is rejected.

==================================================
3. STORED REFERENCE REMEDIATION
==================================================

The certification trigger should use the evaluator-only reference remediation
already associated with the exact ScenarioVersion.

Do not accept an inline reference_remediation through the certification POST.

Reference remediation should be created/updated through the existing dedicated:

PUT .../reference-remediation

endpoint.

The certification attempt then loads it server-side and pins:

reference remediation contract
content hash

Add an API test asserting inline reference_remediation is rejected.

Do not expose its contents in certification responses.

==================================================
4. PUBLIC TRIGGER SHAPE
==================================================

Keep the public certification trigger minimal.

Approximately:

idempotency_key
environment_profile

and only another field if genuinely required by existing architecture and
validated against a server-owned contract.

Do not allow callers to submit:

state
certified
simulate
variant_configs
reference_remediation
gate_results
failure_codes
evaluator_version

==================================================
5. INTERNAL TESTABILITY
==================================================

Do not delete useful deterministic simulation support.

Move/keep it behind internal Go APIs/tests.

Certification engine tests must retain the ability to prove:

good state
bad state
dirty environment
reference failure
leakage
variant unsatisfiable
grader failure

without exposing those controls through HTTP.

==================================================
6. TESTS
==================================================

Focused tests:

- simulate rejected by HTTP
- variant_configs rejected by HTTP
- inline reference_remediation rejected by certification trigger
- certified/state/gate_results still rejected
- stored reference loaded server-side
- reference hash pinned
- server-selected variants pinned
- existing certification engine tests still pass
- viewer cannot trigger
- workspace isolation unchanged
- reference remains evaluator-only
- no LLM grading

==================================================
7. VERIFICATION
==================================================

gofmt touched files.

Run focused:

internal/scenariocertification/...
internal/httpapi/... relevant certification tests

Do not run go test ./...
Do not start Docker/Kind/Kubernetes.

Frontend only if API types were affected:

npx tsc --noEmit

==================================================
DO NOT IMPLEMENT
==================================================

No new migration unless absolutely required.
No new certification architecture.
No environment profiles.
No Kubernetes runtime work.
No scenario corpus.
No billing.
No LLM grading.

==================================================
COMPLETION REPORT
==================================================

Report:

1. files changed
2. public certification request shape
3. simulation isolation
4. server-owned variant selection
5. stored reference-remediation behavior
6. rejection tests
7. engine regression tests
8. TypeScript result if affected
9. confirmation caller cannot influence authoritative certification gates

Write to:

prompts/reports/scenario-certification-hardening.md

Do not overwrite this task.
