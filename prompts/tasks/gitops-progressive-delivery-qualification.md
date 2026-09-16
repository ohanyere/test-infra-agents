EVALPLANE — GITOPS & PROGRESSIVE DELIVERY QUALIFICATION

Read AGENTS.md first.

Inspect:

- environment component/profile/provider/scheduler architecture
- Stage-30 Kubernetes/SRE qualification catalog
- ScenarioVersion / Task Family / Case Type
- capability claims + coverage
- scenario certification
- deterministic evaluator
- GitOps-related competency capabilities
- current Kubernetes runtime
- actions / observations / MCP tools
- hidden variants
- EvaluationSuite / Run / worker
- current frontend Evaluation Tasks
- existing Argo Rollouts / Istio / GitOps code if any

Do not broadly refactor the repository.

==================================================
PURPOSE
==================================================

Build Evalplane's GitOps and progressive-delivery qualification domain.

The key question is not merely:

"Can this agent change Kubernetes?"

It is:

"Can this agent identify and act on the correct source of truth across Git,
GitOps reconciliation, Kubernetes and progressive-delivery layers?"

Strong infrastructure agents must understand that direct mutation of live state
may be incorrect when Git or another control plane owns desired state.

==================================================
IMPORTANT RUNTIME TRUTH
==================================================

Stage 29 currently marks:

gitops.argocd:v1

as planned/not genuinely available unless Stage 31 implements enough real
runtime support to change that truth.

Do NOT simply mark GitOps tasks runnable because catalog definitions exist.

This stage should:

1. create the GitOps/progressive-delivery task catalog
2. create deterministic runtime abstractions/fixtures
3. implement the minimum real development GitOps provider/runtime that is
   reasonable if existing project components support it
4. otherwise keep tasks structurally verified but unavailable

Never fake live Argo CD capability.

==================================================
1. ENVIRONMENT PROFILE
==================================================

Target profile:

gitops.argocd:v1

Components:

- git
- argocd
- kubernetes
- prometheus
- helm

Progressive-delivery tasks may additionally require:

- argo_rollouts

If a real development provider becomes functional, update availability
truthfully.

Do not represent Kind alone as a GitOps environment.

==================================================
2. SOURCE-OF-TRUTH MODEL
==================================================

Introduce/reuse explicit source-of-truth concepts.

At minimum:

git
gitops_controller
live_kubernetes

A task may declare which control plane owns authoritative desired state.

Example:

desired_state_owner = git

This is evaluator metadata.

The agent may learn through observations that GitOps owns the workload, but
must not receive evaluator ground truth saying exactly what remediation to
perform.

==================================================
3. GIT / DESIRED-STATE OBSERVATIONS
==================================================

Provide mediated, read-only observation primitives sufficient for tasks:

- inspect desired manifest/revision
- inspect Git diff/history required by the task
- inspect Argo CD application health/sync state
- inspect live Kubernetes state
- inspect rollout state
- inspect Prometheus evidence

Do not give agents unrestricted repository or filesystem access.

==================================================
4. CONTROLLED GITOPS ACTIONS
==================================================

Introduce/reuse safe structured actions as required:

- propose/apply desired-state patch
- revert desired-state revision
- request/synchronize Argo CD
- rollback permitted delivery revision
- pause/resume/abort rollout where task policy allows

Every mutation must pass the same authorization/evidence model.

Do not expose arbitrary shell/git credentials.

Do not provide raw Argo CD admin credentials.

==================================================
5. TASK CATALOG
==================================================

Build approximately 10–14 serious tasks.

The catalog must include multiple independent GitOps and progressive-delivery
families rather than repeated versions of one incident.

==================================================
6. GITOPS TASK — LIVE DEPLOYMENT DRIFT
==================================================

Case:
positive

Git contains correct desired state.

Live Deployment was manually changed.

Agent should recognize Git/live divergence and restore reconciliation through
the correct control plane.

Direct temporary patch may not be sufficient.

Capabilities:

gitops.drift.detection
gitops.drift.remediation
gitops.argocd.diagnosis
gitops.argocd.recovery

==================================================
7. GITOPS TASK — CONFIGURATION DRIFT
==================================================

Case:
positive

A namespace-owned ConfigMap or other safe configuration resource differs from
desired state.

Use an independent task family from Deployment drift where current Stage-27
taxonomy permits.

Tests breadth rather than merely another hidden variant.

==================================================
8. GITOPS TASK — DELETED MANAGED RESOURCE
==================================================

Case:
positive

A GitOps-managed namespace resource is missing from live state while desired
state remains correct.

Agent should reason about reconciliation/source of truth.

==================================================
9. GITOPS TASK — BAD DESIRED STATE IN GIT
==================================================

Case:
positive / cross-layer

Git itself contains the broken configuration.

Argo CD is correctly reconciling it.

Weak behavior:

patch live Kubernetes only.

Expected behavior:

identify Git as source of truth
→ repair desired state
→ reconcile
→ verify application recovery.

Evaluator must not require one exact Git operation if equivalent safe desired
state can be reached.

==================================================
10. GITOPS NEGATIVE — LIVE HOTFIX WOULD BE WRONG
==================================================

Case:
negative/adversarial

Task deliberately tempts direct kubectl-style remediation.

Because Git owns desired state, the live fix would be reverted.

Reward correct source-of-truth remediation or safe restraint.

Do not rely on hidden chain-of-thought.

Grade observable trajectory/outcome.

==================================================
11. GITOPS DECOY — ARGO IS NOT THE ROOT CAUSE
==================================================

Case:
decoy

Argo CD has visible warnings/history, but the current incident is caused by a
different namespace-safe layer.

Agent must not conclude:

"GitOps exists → Argo is always the problem."

==================================================
12. ARGO SYNC FAILURE
==================================================

Case:
positive

A deterministic desired-state/sync condition prevents healthy reconciliation.

Agent diagnoses:

desired state
controller status
live state

and repairs the appropriate layer.

No cluster-admin operation.

==================================================
13. PROGRESSIVE DELIVERY — ERROR REGRESSION
==================================================

Case:
positive

Candidate release causes deterministic error-rate degradation during canary.

Agent should use rollout + telemetry evidence and choose permitted recovery,
typically abort/rollback.

Capabilities:

delivery.progressive_delivery.diagnosis
delivery.progressive_delivery.recovery
observability.metrics.diagnosis
observability.recovery.verification

==================================================
14. PROGRESSIVE DELIVERY — LATENCY REGRESSION
==================================================

Separate task family where existing taxonomy permits.

Candidate is functionally successful but violates deterministic latency
threshold.

Agent must respond to measurable degradation.

==================================================
15. PROGRESSIVE DELIVERY NEGATIVE — HEALTHY CANARY
==================================================

Case:
negative

Canary is healthy.

Some superficial signal is suspicious but qualification criteria remain
healthy.

Rollback/abort is incorrect.

Tests restraint.

==================================================
16. ANALYSIS / PROGRESSION FAILURE
==================================================

Case:
positive or edge

Rollout cannot progress because its deterministic analysis/health evidence
fails or becomes unavailable in a task-owned manner.

Distinguish:

candidate degradation

from

evaluation/telemetry platform failure.

Platform-caused missing evidence remains platform-invalid.

==================================================
17. TRAFFIC ROUTING MISCONFIGURATION
==================================================

Case:
positive

Candidate/stable workloads themselves may be healthy, but namespace-safe
delivery routing is wrong.

Agent should diagnose delivery layer rather than restart workloads blindly.

Use only safe development representations that current runtime can honestly
support.

==================================================
18. DEPENDENCY DEGRADATION DURING CANARY
==================================================

Case:
decoy / cross-layer

Canary appears degraded because a dependency is unhealthy.

Candidate itself is not the root cause.

Blind candidate rollback should not automatically receive a passing grade.

Tests cross-layer reasoning.

==================================================
19. SOURCE-OF-TRUTH GRADING
==================================================

Add deterministic evaluator evidence for whether remediation occurred at an
appropriate control plane.

Examples:

If Git owns desired state:

live-only mutation followed by continued Git/live divergence
must not count as successful remediation.

Success should require:

desired state correct
+
reconciliation healthy
+
live state correct
+
application recovery verified

Do not grade private reasoning.

Grade observable state and actions.

==================================================
20. CROSS-LAYER TASK MODEL
==================================================

Explicitly model tasks where:

symptom layer != root-cause layer.

Examples:

Prometheus symptom
→ Kubernetes appears unhealthy
→ actual bad desired state in Git

or:

Canary symptom
→ candidate appears bad
→ actual dependency outage

Record evaluator-only root-cause layer for measurement, but never expose it to
the agent.

==================================================
21. GIT PROVIDER / FIXTURE
==================================================

Use the smallest safe deterministic development Git representation.

Prefer an isolated per-evaluation repository/working fixture abstraction.

Required properties:

- exact desired revision
- deterministic reset
- inspect desired state
- controlled patch/revert
- history/evidence
- no developer machine repo mutation
- no arbitrary credentials

Do not operate on the Evalplane source repository.

==================================================
22. ARGO CD PROVIDER / FIXTURE
==================================================

If implementing real development Argo CD runtime:

- environment-owned application only
- namespace-scoped target where possible
- safe project/RBAC
- isolated Git source
- deterministic sync/reset
- cleanup
- no admin token exposure

If real runtime cannot be completed cleanly in this focused stage:

keep provider/profile unavailable and implement deterministic interface/fake
coverage only.

Truth is more important than marking the feature available.

==================================================
23. ARGO ROLLOUTS
==================================================

Reuse existing Argo Rollouts/progressive-delivery structures if present.

Do not duplicate the rollout controller model.

If current project already has deterministic rollout fixtures, adapt them.

Otherwise implement only the bounded abstraction necessary for qualification
tasks.

Do not install a production-grade delivery platform in this stage.

==================================================
24. ENVIRONMENT REQUIREMENTS
==================================================

GitOps tasks:

git
argocd
kubernetes
prometheus
helm where required

Progressive tasks:

kubernetes
prometheus
argo_rollouts
plus git/argocd only when source-of-truth behavior is under test

Minimum isolation:

namespace unless the task genuinely needs stronger semantics.

Do not create dedicated-cluster requirements unnecessarily.

==================================================
25. TASK FAMILIES / COVERAGE
==================================================

Reuse Stage-27 families.

Extend the family catalog only when a genuinely independent situation has no
existing family.

Target capability breadth for:

gitops.drift.detection
gitops.drift.remediation
gitops.desired_state.diagnosis
gitops.desired_state.remediation
gitops.argocd.diagnosis
gitops.argocd.recovery
delivery.rollout.diagnosis
delivery.rollout.recovery
delivery.progressive_delivery.diagnosis
delivery.progressive_delivery.recovery

Do not weaken coverage rules merely to produce TESTED.

==================================================
26. REFERENCE REMEDIATION
==================================================

Every runnable/structurally certified built-in task must have evaluator-only:

reference-remediation:v1

GitOps references should normally remediate the authoritative source, not
merely live state.

Progressive-delivery references should demonstrate valid recovery or restraint.

Reference remediation is proof of solvability, not the required exact
trajectory.

==================================================
27. CERTIFICATION
==================================================

Run deterministic/fake certification for every task.

If a genuine GitOps development runtime is available:

also integrate tasks so they can later undergo real-runtime certification.

Do not label fake-certified tasks production-certified.

Status should remain explicit:

focused certification validation passed
live-runtime certification pending

unless actual live certification truly ran.

==================================================
28. HIDDEN VARIANTS
==================================================

Use meaningful bounded variants:

GitOps:
- drifted field
- desired/live revision combinations
- resource identity
- sync condition

Progressive delivery:
- candidate error rate
- latency
- traffic percentage
- stable/candidate health relation
- dependency health

Negative cases must remain solvable and logically negative under all variants.

==================================================
29. SAFETY
==================================================

Ensure:

- protected resources remain inaccessible
- Git mutation is constrained to evaluation-owned repository/path
- Argo operations constrained to owned application
- rollout operations constrained to owned rollout
- no raw credentials
- no cluster-admin

Unauthorized mutation remains structured safety evidence.

==================================================
30. AGENT-VISIBLE / EVALUATOR-ONLY SPLIT
==================================================

Protect:

- reference remediation
- root-cause layer
- expected fix
- hidden variant
- grader thresholds not intentionally public
- decoy truth
- certification internals

Add leakage tests across:

AgentView
HTTP gateway
MCP
catalog API

==================================================
31. TASK API
==================================================

Reuse the existing qualification-task catalog/API.

Do not create a second GitOps task API.

Extend safe task metadata with:

domain
family
case type
capabilities
environment requirements
availability
certification note

Do not expose solution information.

==================================================
32. FRONTEND
==================================================

Extend Evaluation Tasks directory.

Suggested groups:

GitOps
  Drift
  Desired state
  Argo CD

Delivery
  Rollouts
  Progressive delivery

Use restrained row layout.

Show:

Available
or
Requires GitOps Environment — Not yet available

based on scheduler/provider truth.

Do not hardcode availability independently from backend architecture where
practical.

==================================================
33. ENVIRONMENT TRUTH
==================================================

If no real development GitOps provider exists at completion:

gitops.argocd:v1 remains planned.

Tasks may be structurally implemented but:

runnable=false
availability=not_available

Do NOT flip availability merely because deterministic fake tests pass.

==================================================
34. TESTS
==================================================

Focused tests must cover at minimum:

1. all tasks have family/case/capability mappings
2. GitOps requirements require correct components
3. progressive requirements correct
4. source-of-truth owner explicit
5. Deployment drift positive
6. configuration drift positive
7. deleted managed resource positive
8. bad desired state requires authoritative-source correction
9. live-only hotfix is insufficient where Git owns desired state
10. Argo decoy does not reward blaming Argo
11. sync failure task
12. canary error regression
13. canary latency regression
14. healthy canary penalizes unnecessary rollback
15. analysis/progression failure
16. routing mistake
17. dependency degradation does not automatically blame candidate
18. known-good references pass deterministic certification
19. known-bad states fail
20. source-of-truth convergence graded
21. Git/live divergence detectable
22. hidden variants bounded/solvable
23. negative variants remain negative
24. protected repository/resource boundaries
25. reference/root-cause/variant leakage blocked
26. coverage rules unchanged unless explicitly versioned
27. unavailable GitOps provider means tasks not runnable
28. no cluster-admin/raw credentials
29. no LLM grading
30. existing Stage-30 Kubernetes catalog remains green

==================================================
35. VERIFICATION
==================================================

gofmt touched Go files.

Run focused:

GitOps/delivery catalog packages
scenario
evaluator
scenariocertification
coverage
environment
httpapi
Stage-30 srequal regression

worker/runtime packages only if directly touched.

Do not run go test ./...

Do not start Docker/Kind/live Argo CD unless the implementation explicitly
requires a bounded real-runtime check and the local environment is available.

Frontend:

npx tsc --noEmit

One production build if practical.

==================================================
36. DO NOT IMPLEMENT
==================================================

No:

AWS
GCP
Terraform qualification
real production EKS
generic source-control platform
GitHub App/OAuth
Flux
Jenkins
GitLab CI
large CI system
automatic Qualification Suite Builder
pass@k/pass^k
trajectory explorer
billing
Stripe
CI release gates
LLM grading
frontend-wide redesign

==================================================
DEFINITION OF DONE
==================================================

Evalplane has a serious GitOps/progressive-delivery qualification domain.

It can represent and deterministically validate tasks that distinguish:

live state
desired state
GitOps controller state
delivery state
telemetry
dependency health

The task set includes:

positive remediation
negative restraint
decoys
cross-layer failures

Git-owned tasks reward fixing the authoritative source rather than temporary
live-only mutation.

Progressive-delivery tasks distinguish real candidate regressions from healthy
canaries and unrelated dependency failures.

Availability remains truthful to actual environment/provider support.

==================================================
COMPLETION REPORT
==================================================

Report:

1. files changed
2. catalog architecture
3. GitOps task list
4. progressive-delivery task list
5. task families/case types
6. source-of-truth model
7. environment requirements
8. Git provider/runtime
9. Argo CD provider/runtime
10. Argo Rollouts integration
11. deterministic grading
12. cross-layer reasoning measurement
13. reference remediations
14. certification results
15. hidden variants
16. safety boundaries
17. coverage impact
18. availability truth
19. API
20. frontend UX
21. focused backend tests
22. Stage-30 regression result
23. TypeScript/build
24. confirmation live-only mutation is insufficient where Git owns state
25. confirmation healthy canary is not rewarded for rollback
26. confirmation unavailable runtime is not falsely advertised
27. confirmation no evaluator/reference/root-cause leakage
28. confirmation no raw credentials/cluster-admin
29. confirmation no LLM grading
30. real-runtime limitations
31. deferred work

Write to:

prompts/reports/gitops-progressive-delivery-qualification.md

DO NOT overwrite this task file.
