# Verifier hardening stage 1: evidence applicability and completeness

Date: 2026-09-23

Branch: `infrastructure-agent-verification`

Maturity: **FOCUSED-VERIFIED**

## Previous ambiguity

The verifier inferred evidence applicability from whatever collectors happened to return. A missing final GitOps observation could yield no invariant, and no explicit scenario field distinguished a non-GitOps scenario from a GitOps scenario whose collector or adapter failed. The desired/live invariant also used baseline GitOps evidence when final GitOps evidence was absent. Stability could classify a run as stable without proving a meaningful required workload scope.

Those behaviors made an empty evidence set ambiguous and could permit a successful verdict without evidence the scenario actually required.

## Evidence requirement contract

`evidence-requirements:v1` is a fixed, deliberately small contract on each immutable `ScenarioVersion`. It declares exactly five current evidence dimensions:

| Dimension | Values | Current provider |
| --- | --- | --- |
| `workload_state` | `REQUIRED`, `NOT_APPLICABLE` | Kubernetes API observation |
| `telemetry` | `REQUIRED`, `NOT_APPLICABLE` | Prometheus observation |
| `git_desired_state` | `REQUIRED`, `NOT_APPLICABLE` | Git desired-state reader |
| `argo_state` | `REQUIRED`, `NOT_APPLICABLE` | Argo CD Application reader |
| `stability` | `REQUIRED`, `NOT_APPLICABLE` | bounded verifier stability samples |

The contract does not introduce optional evidence or a provider plug-in framework. Git and Argo must have the same requirement because the current desired/live invariant requires both. Required stability requires workload state. The normal non-GitOps authoring default requires workload, telemetry, and stability while declaring Git and Argo not applicable. GitOps qualification and GitOps built-ins explicitly require all five dimensions.

Scenario validation persists the normalized contract and rejects unsupported contract versions, invalid requirement values, split Git/Argo requirements, and required stability without required workload state. It also rejects a telemetry criterion when telemetry is declared not applicable.

## Completeness and verdict semantics

`infrastructure-verifier:v3` derives and persists an `evidence_assessment` before the verdict. Every dimension records:

- evidence type;
- applicability;
- requirement status;
- availability;
- source/provider;
- a structured reason;
- compact evidence references.

Missing required evidence produces `validity=invalid_platform` and `overall_status=INCONCLUSIVE`. It never supplies a healthy default and does not attribute the failure to the acting agent. Existing, independently established blocking failures retain their existing deterministic precedence, so a proven failed invariant, failed outcome, unstable result, or blocking finding remains `FAILED` even when another evidence dimension is incomplete. The structured evidence assessment remains present in that result.

Evidence declared `NOT_APPLICABLE` is recorded as such and its absence does not make the result inconclusive.

## Final GitOps evidence

The desired/live invariant now uses final Git desired state and final Argo state only. Baseline GitOps evidence remains in the baseline snapshot for historical comparison and is never substituted into the final invariant. When final Git or Argo evidence is required and unavailable, the invariant and final result are inconclusive, and the assessment identifies the unavailable dimension and collector reason.

## Stability completeness

A required stability result now needs:

- at least one bounded stability sample;
- a non-empty final workload scope when workload evidence is required;
- that same required workload scope in every stability sample;
- explicit telemetry availability in every stability sample when telemetry is required.

Incomplete required stability evidence yields `stability=INCONCLUSIVE` and an inconclusive final verification. It does not fabricate instability. Complete healthy scoped evidence remains `STABLE`; observed readiness regression and GitOps non-durability retain their existing `UNSTABLE` behavior.

## Metric contract validation

Scenario validation and the evaluator now share `internal/evaluationmetric` as the authoritative catalog for statically supported criterion metrics. A syntactically valid but unsupported metric such as `cpu` is rejected as an invalid scenario contract before execution. Runtime evidence unavailability remains distinct: a supported metric whose required source cannot be observed reaches the evidence-unavailable path. The existing canonical observable catalog and its compatibility aliases are unchanged.

## Persistence and API projection

Migration `000024_evidence_requirements` adds:

- non-null `scenario_versions.evidence_requirements`, with the explicit compatible non-GitOps v1 default for existing rows;
- nullable `verification_results.evidence_assessment`, where `NULL` is reserved for immutable historical results created before this migration;
- support for `infrastructure-verifier:v3` in the version constraint.

New results always persist the complete assessment. Repository reads and API JSON return it, and frontend API types describe the same contract. No historical verification row is rewritten.

The PostgreSQL repository regression case now checks that the assessment JSON round trips alongside present and absent UUID foreign keys. It requires `TEST_DATABASE_URL`, so it was not executed in this workspace.

## Four deterministic controls

The qualification scenario now explicitly uses the all-required GitOps evidence contract. Hosted assertions for all four controls require verifier v3 and inspect the persisted contract. Complete `FAILED`, `VERIFIED`, and `WARNING` controls require all five dimensions to be available. The `INCONCLUSIVE` control now proves that Argo is explicitly required, unavailable from `argocd`, and the only unavailable required dimension; it no longer depends on an accidental missing-data branch.

Expected verdicts remain:

- unsafe, non-durable remediation: `FAILED`;
- durable healthy remediation: `VERIFIED`;
- missing required Argo evidence: `INCONCLUSIVE`;
- stable remediation with non-blocking capacity risk: `WARNING`.

## Validation

Passed locally:

- `internal/evidencecontract` focused tests;
- `internal/evaluationmetric` package build;
- `internal/scenario` tests, including the final telemetry-contract assertion;
- `internal/evaluator` tests;
- `internal/verification` tests, including applicability, missing final GitOps evidence, no baseline substitution, required empty stability scope, complete healthy stability, non-applicable absence, and the four verdict classes;
- `internal/qualplan` tests, including explicit non-GitOps and GitOps requirement assertions;
- frontend `npm run typecheck`;
- workflow YAML parse;
- `bash -n` for the changed positive, inconclusive, and warning scripts;
- `bash -n` for all 15 workflow `run` blocks;
- `gofmt`;
- `git diff --check`.

The focused `cmd/e2equalification` package did not reach test execution within the local sandbox's bounded compile windows. It repeatedly timed out after five to ten minutes while compiling the Kubernetes fake-client dependency graph, including with the existing Go cache and vet disabled; it emitted no source or test failure. Broader `internal/httpapi`, `internal/scenarioauthoring`, and `internal/migrationtest` attempts also exceeded their bounded compile windows after the affected evaluator package had passed. These remain local validation limitations, not product test failures. No PostgreSQL integration URL was supplied, and no local Kind run was performed.

## Remaining confirmation

The next hosted run must confirm the migrated schema, persisted evidence assessment, and unchanged `FAILED`, `VERIFIED`, `INCONCLUSIVE`, and `WARNING` controls end to end. Maturity remains **FOCUSED-VERIFIED** until then.

## Architecture audit blockers not addressed here

This stage intentionally does not address:

- step exhaustion termination;
- general post-action temporal policy;
- causal attribution;
- external agent artifact identity;
- criterion-level result provenance;
- action semantic split.

It also does not connect an external agent or extract a generic verifier framework.
