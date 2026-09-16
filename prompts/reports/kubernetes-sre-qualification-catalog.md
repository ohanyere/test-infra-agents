# Completion report — Kubernetes/SRE Qualification Catalog

## 1. Files changed

New (backend):

- `backend/internal/srequal/catalog.go` — 12-task runnable V1 catalog + 4 planned cluster concepts, `Task` definition, `List/PlannedConcepts/Get`, `Requirements()`, `Validate()`, scheduler-derived `Availability()`, agent-safe `Safe()` projection.
- `backend/internal/srequal/faults.go` — reusable namespace-safe fault primitives (`CapacityFault`, `ReadinessFault`, `DependencyFault`, `ResourcePressureFault`, `HealthFault`, `TelemetryFault`, `ProtectedDecoyFault`; node/DNS markers are future-only and rejected for runnable tasks), `Injection` composition + `Validate()`, `ValidateVariantParams` bounds schema.
- `backend/internal/srequal/grading.go` — `scenario.Version` conversion, `AgentPayloads`/`Sentinels`, `GoodInput`/`BadInput`/`SafetyInput` via deterministic certification builders, `Fixture()`/`Certify()` fake certification, `platformInvalidInput`, telemetry-requirement derivation.
- `backend/internal/srequal/catalog_test.go` — 32 focused tests (no live Kubernetes, deterministic fakes).
- `backend/internal/httpapi/qualification_tasks.go` — public `GET /v1/qualification-tasks` + `GET /v1/qualification-tasks/{taskID}` serving the agent-safe projection with scheduler-derived availability.
- `backend/internal/httpapi/qualification_tasks_test.go` — 4 API tests (list/get/planned-404/leakage).

Reconstructed (pre-existing breakage):

- `backend/internal/httpapi/qualification.go` — the file was missing at HEAD (router referenced `QualificationService`, `qualificationHandlers` policy methods, `FailureTaxonomyService`, and `failureTaxonomyHandlers`, none of which existed, so `httpapi` did not compile). Reconstructed faithfully from router usage, `qualification_test.go` fakes, and service signatures. No behavior beyond HTTP translation was added.

Modified:

- `backend/internal/httpapi/router.go` — public qualification-task routes.
- `backend/README.md` — endpoint list kept in sync.
- `app/scenarios/scenarios-data.ts` — rewritten to the 12-task catalog (+ 2 planned placeholders), precise names, case types, truthful availability; removed fabricated evaluation counts.
- `app/scenarios/scenario-directory.tsx` — family filters + case-type display.
- `app/runs/[id]/run-data.ts`, `app/comparisons/[id]/comparison-data.ts`, `app/workspace/page.tsx` — demo references repointed to catalog IDs; workspace panel no longer shows fabricated counts.

## 2. Catalog architecture

New package `backend/internal/srequal` holds seed definitions over the existing scenario model only. No second runtime, grader, or catalog system: execution stays on scenariorunner/kubeenv via the environment scheduler, grading on `deterministic-evaluator:v1`, trust on `scenario-certification-contract:v1`, breadth on `capability-coverage-contract:v1`. The package declares WHAT the tasks are; existing machinery decides placement, grading, trust, and breadth.

## 3. Runnable task list

12 namespace-safe tasks: Traffic Capacity Saturation, Capacity Decoy / No Scale Needed, Deployment Readiness Failure, Healthy Rollout / Do Not Rollback, Dependency Outage, Dependency Decoy, Workload Resource Pressure, Restart Required, Restart Is Not the Fix, Missing Telemetry, Recovery Verification, Protected Resource Boundary.

## 4. Task-family mappings

Existing Stage-27 family IDs reused, no duplicates invented: `kubernetes.capacity.traffic_spike` (T1, T2), `kubernetes.capacity.resource_pressure` (T7), `kubernetes.workload.baseline` (T8, T9), `kubernetes.deployment.recovery` (T3), `kubernetes.networking.dependency` (T5, T6), `delivery.rollout.recovery` (T4), `observability.telemetry.missing_data` (T10), `observability.recovery.verification` (T11), `safety.protected_resource.avoidance` (T12).

## 5. Case-type distribution

Positive 6/12 (50%), negative 2 + decoy 2 (33%), edge 1 + adversarial 1 (remainder). Every decoy/negative carries restraint criteria (`agent_action_count` bounds) so "incident → always mutate" is never rewarded; spam-action inputs with healthy metrics deterministically fail.

## 6. Capability mappings

All IDs are active `infrastructure-capability-catalog:v1` entries: capacity diagnosis/remediation, workload diagnosis/remediation, deployment diagnosis/recovery, networking/dependency diagnosis, metrics diagnosis, missing-data reasoning, recovery verification, rollout diagnosis/recovery, scope adherence, protected-resource avoidance, stop-when-recovered. Each task lists the exact claims it exercises.

## 7. Environment requirements

All 12 runnable tasks: kubernetes + prometheus at namespace isolation, runtime `kubernetes-traffic-spike:v1` → `kubernetes.namespace:v1` on `kind-dev:v1`. No dedicated assignment anywhere runnable. The 4 planned concepts (node failure, cluster DNS, Karpenter, CNI) require `dedicated_cluster` and resolve to `no_eligible_environment`, never namespace fallback.

## 8. Reusable fault/injection primitives

`faults.go`: capacity, readiness, dependency, resource-pressure (workload scope by construction — percentages of owned limits, counts of owned pods), health, telemetry-gap (declared task-owned state), protected-decoy (never an actionable fixture). Bounded validation; at most two active primitives per task; no host/node/CNI/control-plane/cloud injection exists.

## 9. Deterministic grading

Criteria use only supported evaluator metrics with measurable operators (gt/gte/lt/lte/eq/neq — `required` rejected). Final-state and safety evidence graded, never exact action imitation: availability/error/latency recovery, replica/readiness counts, action-count restraint bounds, protected-resource untouched (via denied-action safety evidence), recovery verified. Reference remediation proves solvability; alternative passing trajectories are not excluded by the grader.

## 10. Reference remediation

Every runnable task defines evaluator-only `reference-remediation:v1` input (preconditions, ordered actions, expected state, verification + cleanup notes) valid under the allowed action model (`inspect/scale_workload/restart_pod/no_op` plus provider-neutral `verify_state` for T11). Restraint tasks reference `inspect` + `no_op`.

## 11. Certification results

All 12 tasks pass deterministic fake certification (`Certify` → `certified`): criteria gate, initial-state gate, reference-solvability + grader-positive via the real grader on known-good input, grader-negative + counterfactual on known-bad input, safety detection where forbidden behavior is declared, telemetry, 2 clean repetitions + cleanliness, 2 validated variants, leakage gate. Status is truthfully "focused certification validation passed; live-runtime certification pending" — never production-certified.

## 12. Hidden variants

Each task carries 2 bounded variants (capacity dimensions, dependency identity/mode, readiness bounds, pressure percentages, health counts, telemetry gap sets, decoy identities). `ValidateVariantParams` rejects unknown keys and out-of-range values; variant values never appear in agent payloads (tested per task).

## 13. Positive vs negative/decoy behavior

Decoy/negative tasks (T2, T4, T6, T9) grade restraint explicitly: `no_op`/inspect-only references, `agent_action_count` bounds, and tests proving 5–6 spam actions with healthy metrics fail. No hidden chain-of-thought required — observable actions/outcomes only.

## 14. Safety/adversarial behavior

T12 presents a tempting protected workload that is absent from allowed resources, so existing authorization denies it and the evaluator records `cross_namespace_action_attempt`/`unauthorized_action_attempt` (SafetyInput → violations > 0, status fail). No dangerous cluster fixture exists. Existing authorization was not weakened. Forbidden behaviors use only structured codes, so deterministic mapping never degrades to `forbidden_behavior_unsupported` inconclusive.

## 15. Coverage impact

Requirements untouched (all 8 still demand 2 distinct families × 2 valid trials). Catalog breadth honestly makes TESTED achievable for 4 capabilities (capacity diagnosis/remediation, workload diagnosis, deployment recovery — proven via `coverage.Assess` with valid trials). Capabilities lacking breadth (metrics diagnosis, recovery verification, scope adherence, stop-when-recovered) stay PARTIAL by rule, never weakened into TESTED.

## 16. Unsupported/future task handling

Node failure, cluster DNS, Karpenter, CNI exist only as planned concepts: dedicated requirements, `runnable=false`, "Requires Dedicated Kubernetes — Not yet available" in API and UI, never executed through the namespace profile (scheduler + API + catalog tests all assert this).

## 17. API

Public `GET /v1/qualification-tasks` (12 safe projections) and `GET /v1/qualification-tasks/{taskID}` (runnable or planned; unknown → 404). Responses contain name, domain/family, case type, capabilities, requirements, scheduler-derived availability, and honest certification notes — never ground truth, reference actions, hidden variants, or certification internals (asserted byte-wise in tests).

## 18. Frontend Evaluation Tasks UX

Directory rewritten around the catalog: grouped Kubernetes (Capacity, Workloads, Deployments, Dependencies) / Delivery (Rollouts) / Observability (Diagnosis, Recovery verification) / Safety (Boundaries / restraint), with case type shown per row and the same restrained row design (no marketplace cards). Detail pages carry per-task conditions, observables, expected/forbidden behavior, and criteria; planned tasks show the dedicated note with no trial action. Execution-environment section already renders scheduler truth.

## 19. Availability truth

Backend availability is computed per task from `environment.Select` (no separate flag); API test asserts view matches scheduler. Frontend static availability mirrors that truth: 12 namespace tasks available in development, 2 dedicated concepts planned. Kind remains labeled development-only.

## 20. Focused backend tests

`srequal` (32/32 spec items: families, case types, requirements, active-profile resolution, no-downgrade, criteria, references, evaluator-only references, fake-cert pass, bad-fails, balance, per-task behaviors 12–24, safety detection, variant solvability/leakage, coverage intactness, planned-not-runnable, scheduler-truth availability, no-LLM determinism) plus `httpapi` catalog tests. Full focused run green: `srequal, scenario, evaluator, scenariocertification, coverage, environment, competency, httpapi`.

## 21. TypeScript/build

`npx tsc --noEmit` clean; `npm run build` succeeds with all 14 `/scenarios/[id]` paths prerendered.

## 22. Confirmation no cluster-level task is falsely runnable

Workload pressure text contains no node claims and uses workload primitives; no runnable task carries node/cluster faults (validation rejects them); planned concepts never resolve; UI shows dedicated-only notes.

## 23. Confirmation no evaluator/reference leakage

Agent payloads (AgentView + gateway-shaped + MCP-shaped) contain no ground truth, reference JSON, decoy truths, variant values, sentinels, or evaluator-only keywords — asserted per task. API responses byte-scanned for leakage markers.

## 24. Confirmation no LLM grading

Scheduler, grader, and certification paths are pure deterministic functions; `Certify` is stable across runs; no LLM/assistant-model references in task data or the new package.

## 25. Real-runtime limitations

Fake certification proves structural certifiability only: reference solvability is established through synthetic grader fixtures, not live cluster execution. Live-runtime certification (provision → inject → remediate → evaluate → cleanup on kind-dev, then stronger providers) is still required before any task is production-certified. Telemetry-gap behavior under a real Prometheus outage still needs live proof that intentional absence stays valid while platform-caused absence stays `invalid_platform`.

## 26. Deferred work

Live-runtime certification runs; shared/vcluster/dedicated/AWS providers; GitOps/ArgoCD runtime (Stage 31); automatic Qualification Suite Builder; pass@k; trajectory explorer; billing; CI gates. None were started.
