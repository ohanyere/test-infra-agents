EVALPLANE — ENVIRONMENT PROFILES & COMPOSITION

Read AGENTS.md first.

Inspect the existing implementation for:

- Kubernetes environment manager
- run/execution lifecycle
- runtime contracts
- scenario execution requirements
- MCP/tool access
- Agent Gateway
- ScenarioVersion
- task families
- capability coverage
- scenario certification
- hidden variants
- cleanup/recovery
- qualification/reproducibility
- any existing environment/profile fields

Do not broadly refactor the repository.

==================================================
PURPOSE
==================================================

Evalplane now knows:

- what an agent claims
- what task families are required
- whether the claim has enough test coverage
- whether an Evaluation Task itself is certified

The next missing primitive is:

"What infrastructure does this task actually require, and what execution
environment should Evalplane provision for it?"

Do NOT build one giant environment containing everything.

Do NOT model every scenario as a bespoke Kubernetes setup.

Introduce:

Environment Components
        ↓
Environment Profiles
        ↓
Environment Providers
        ↓
Environment Scheduler
        ↓
Provisioned Environment

This stage is primarily the architecture/model/scheduling layer.

It does NOT implement AWS or a production EKS control plane.

==================================================
1. CORE TERMS
==================================================

Environment Component

A reusable infrastructure capability required by an evaluation environment.

Examples:

kubernetes
prometheus
git
argocd
helm
argo_rollouts

Later:

aws
terraform
gcp
datadog_runtime
cloudwatch

Environment Profile

A versioned server-owned composition of components plus an isolation/fidelity
tier.

Examples:

kubernetes.namespace:v1

kubernetes.virtual_cluster:v1

kubernetes.dedicated:v1

gitops.argocd:v1

Environment Provider

The implementation capable of provisioning a profile.

Examples conceptually:

kind-dev
kubernetes-shared
virtual-cluster
dedicated-eks

Future:

aws-sandbox
gcp-project

Environment Scheduler

Selects a provider/profile execution placement satisfying task requirements
while respecting isolation, capability, safety, and lifecycle constraints.

==================================================
2. CONTRACT VERSIONS
==================================================

Introduce explicit versioned contracts.

Suggested:

environment-component-catalog:v1
environment-profile-catalog:v1
environment-provider-contract:v1
environment-scheduler-contract:v1

Historical runs/certifications must pin the profile/provider/runtime identities
used.

Do not let future profile changes rewrite historical evidence.

==================================================
3. SERVER-OWNED COMPONENT CATALOG
==================================================

Create a small V1 server-owned component catalog.

At minimum:

kubernetes
prometheus
git
argocd
helm
argo_rollouts

Metadata approximately:

id
display_name
description
contract_version
status

Statuses may include:

active
experimental
planned

Do not create AWS/Terraform/GCP active components yet.

Architecture must permit them later.

==================================================
4. ENVIRONMENT PROFILE CATALOG
==================================================

Create a small server-owned V1 profile catalog.

At minimum:

A.
kubernetes.namespace:v1

Components:
- kubernetes
- prometheus

Isolation:
namespace

Purpose:
ordinary application/workload evaluations inside an existing/shared Kubernetes
cluster.

B.
kubernetes.virtual_cluster:v1

Components:
- kubernetes
- prometheus

Isolation:
virtual_cluster

Status may be planned/experimental if no provider exists yet.

Purpose:
tasks requiring stronger Kubernetes control-plane isolation without a whole
independent cloud cluster.

C.
kubernetes.dedicated:v1

Components:
- kubernetes
- prometheus

Isolation:
dedicated_cluster

Status may be planned if not implemented.

Purpose:
node/cluster/cloud-controller/CNI/Karpenter/CoreDNS-class scenarios.

D.
gitops.argocd:v1

Components:
- git
- argocd
- kubernetes
- prometheus
- helm

Optionally:
- argo_rollouts where supported

Isolation:
namespace or stronger according to implementation requirements.

If the actual runtime is not implemented yet:

mark the profile planned/experimental.

Do not claim it is available.

==================================================
5. ISOLATION TIERS
==================================================

Model explicit isolation/fidelity tiers.

At minimum:

namespace
virtual_cluster
dedicated_cluster

Do not infer isolation purely from profile name.

The scheduler must be able to reason over it.

Document semantics.

namespace:
shared control plane, workload isolation through namespace/RBAC/network/quota.

virtual_cluster:
isolated logical Kubernetes control plane on shared physical infrastructure.

dedicated_cluster:
independent Kubernetes cluster lifecycle.

Do not claim namespace isolation protects against cluster-scoped tasks.

==================================================
6. TASK ENVIRONMENT REQUIREMENTS
==================================================

Allow an exact ScenarioVersion to declare server-reviewed environment
requirements.

Requirements approximately:

required_components
minimum_isolation_tier
required_profile_capabilities
runtime_contract requirements where necessary

Examples:

traffic spike:
components = [kubernetes,prometheus]
minimum isolation = namespace

GitOps drift:
components = [git,argocd,kubernetes,prometheus]
minimum isolation = namespace

real node failure:
components = [kubernetes,prometheus]
minimum isolation = dedicated_cluster

Do not make unsupported scenarios runnable merely by assigning requirements.

==================================================
7. REQUIREMENTS ARE EVALUATOR-OWNED METADATA
==================================================

Environment requirements are part of task execution metadata.

They must not expose:

ground truth
reference remediation
hidden variant values
grader internals

Agent may know what infrastructure/tools are available when appropriate.

It must not receive evaluator-only reasoning about why a component is required.

==================================================
8. PROFILE MATCHING
==================================================

Implement deterministic matching:

Task Requirements
        ↓
eligible Environment Profiles

A profile is eligible only when:

- all required components are present
- profile isolation >= minimum isolation
- profile status permits execution
- provider exists and is healthy/available
- required runtime contract is supported

Do not silently select a profile missing requirements.

==================================================
9. ISOLATION ORDERING
==================================================

Define deterministic ordering:

namespace < virtual_cluster < dedicated_cluster

A stronger profile may satisfy a weaker minimum when otherwise compatible.

A weaker profile can never satisfy a stronger requirement.

Test this explicitly.

==================================================
10. PROVIDER INTERFACE
==================================================

Introduce or formalize a provider abstraction.

Conceptually:

ProvisionEnvironment(ctx, request)
GetEnvironment(ctx, environmentID)
ResetEnvironment(ctx, environmentID)
DestroyEnvironment(ctx, environmentID)
Capabilities()
Health()

Adapt names to existing architecture.

Do NOT force providers to expose cloud-specific implementation details to the
core execution engine.

Core should request:

profile
workspace
run/certification ownership
task requirements
lease/lifecycle metadata

not:

Terraform resource names
AWS account IDs
Kind implementation details

unless returned as safe opaque provider metadata.

==================================================
11. ADAPT CURRENT KUBERNETES ENVIRONMENT
==================================================

Wrap/adapt the existing current Kubernetes environment manager as the first
provider.

Current local/dev runtime:

Kind/local Kubernetes

must be treated as a DEVELOPMENT provider.

It is not proof of production isolation.

Suggested provider identity:

kind-dev:v1

or another naming consistent with existing runtime.

It may support:

kubernetes.namespace:v1

for development/focused testing.

Do not label Kind as production qualification infrastructure.

==================================================
12. FUTURE PRODUCTION PROVIDER MODEL
==================================================

Architecture must permit future providers:

shared-eks:v1
vcluster-eks:v1
dedicated-eks:v1
aws-sandbox:v1

Do NOT implement them now.

Provider registration/catalog may list them as planned only if useful.

Do not create fake health or availability.

==================================================
13. ENVIRONMENT INSTANCE
==================================================

Introduce a canonical provisioned EnvironmentInstance or equivalent.

Pin approximately:

id
workspace_id
owner_type
owner_id
profile_id
profile_contract_version
provider_id
provider_contract_version
isolation_tier
status
lease information
created_at
ready_at
destroyed_at
safe opaque provider reference

Possible owner types:

run
certification

Keep it extensible.

Do not expose sensitive provider credentials.

==================================================
14. ENVIRONMENT INSTANCE STATES
==================================================

Use machine-readable states.

Approximately:

provisioning
ready
resetting
destroying
destroyed
failed

Do not use display punctuation as state.

Transitions must be validated.

Do not allow destroyed environments to return to ready.

==================================================
15. LEASES / OWNERSHIP
==================================================

Reuse existing run/environment lease patterns where practical.

Every environment belongs to:

workspace
specific execution owner

No environment may be reused across unrelated tenants merely because its
profile matches.

Namespace shared-cluster execution may share the physical provider but not the
EnvironmentInstance ownership boundary.

==================================================
16. SCHEDULER
==================================================

Implement a deterministic scheduler.

Input:

workspace
ScenarioVersion environment requirements
purpose:
  trial | certification
optional policy constraints

Output:

selected profile
selected provider
reason codes

Scheduler priorities for V1:

1. satisfy requirements
2. satisfy minimum isolation
3. provider available
4. prefer least expensive/simplest adequate fidelity tier

Do not implement monetary billing/cost optimization yet.

"Cheapest" in V1 may be represented simply by profile priority/weight.

==================================================
17. SCHEDULER REASON CODES
==================================================

Return structured codes.

Examples:

profile_selected
missing_required_component
insufficient_isolation
profile_not_active
provider_unavailable
runtime_contract_unsupported
no_eligible_environment

Avoid only free-form scheduler errors.

==================================================
18. NO DOWNGRADE ON FAILURE
==================================================

Security requirement:

If a task requires dedicated_cluster and the dedicated provider is unavailable,
do NOT fall back to namespace.

Return:

no eligible environment

or structured provider unavailable.

Never trade required fidelity for availability.

==================================================
19. CERTIFICATION INTEGRATION
==================================================

Fix the temporary Stage 28 API/model issue.

Certification should no longer trust arbitrary environment profile/runtime
strings.

The server must resolve:

ScenarioVersion requirements
        ↓
Environment Scheduler
        ↓
server-owned Environment Profile
        ↓
provider/runtime contract

Public certification trigger should NOT be able to fabricate an arbitrary
profile or runtime contract.

If environment_profile remains accepted as an optional preference:

it must be a known server-owned active profile and must still satisfy task
requirements.

Prefer server selection.

runtime_contract_version should be derived from the selected provider/profile
where possible, not arbitrary caller text.

Update certification tests accordingly.

==================================================
20. RUN EXECUTION INTEGRATION
==================================================

Integrate without rewriting the worker.

Before environment provisioning:

ScenarioVersion requirements
→ scheduler
→ selected profile/provider
→ EnvironmentInstance
→ existing run lifecycle

Current Kubernetes run behavior should continue working through the adapted
development provider.

Do not break legacy runs that have no explicit environment requirements.

For legacy ScenarioVersions:

use a clearly defined compatibility/default profile for current supported
traffic-spike/Kubernetes behavior.

Do not silently claim stronger isolation.

==================================================
21. SCENARIO CERTIFICATION REQUIREMENTS
==================================================

Certification should record:

environment profile
provider
isolation tier
runtime contract

against which certification evidence was produced.

A certification against:

kind-dev + namespace

does NOT imply certification for:

dedicated EKS

unless explicitly re-certified under that context later.

Historical rows remain immutable.

==================================================
22. TASK CERTIFICATION STALENESS
==================================================

Update stale logic where necessary.

A certification becomes stale/ineligible for a requested authoritative context
when its pinned:

profile contract
provider/runtime contract
isolation/fidelity context

does not match the required measurement context.

Do not mutate old certification rows.

==================================================
23. COVERAGE / QUALIFICATION INTERACTION
==================================================

Do not merge environment scheduling with coverage.

Coverage still answers:

was the capability sufficiently exercised?

Certification still answers:

was the task trustworthy?

Environment scheduling answers:

where can this task validly execute?

Future Qualification Suite Builder will combine all three.

==================================================
24. API
==================================================

Add authenticated read APIs.

Approximately:

GET /v1/environment-components
GET /v1/environment-profiles
GET /v1/environment-providers

Workspace-scoped:

GET /v1/workspaces/{workspaceID}/environments
GET /v1/workspaces/{workspaceID}/environments/{environmentID}

Optional diagnostic/matching endpoint:

POST /v1/workspaces/{workspaceID}/environment-selection/resolve

for write-capable roles or authenticated workspace members depending existing
patterns.

It should accept a ScenarioVersion reference, not arbitrary fake requirements
if used for authoritative execution.

==================================================
25. FRONTEND
==================================================

Keep frontend focused.

Evaluation Task detail should show:

Environment requirements

Kubernetes
Prometheus

Minimum isolation:
Namespace

Eligible profile:
Kubernetes Namespace

For planned requirements:

Dedicated Kubernetes
Not yet available

Do not fake availability.

Add or update an Environments page only if an existing route already exists or
minimal implementation is straightforward.

Show:

profile
provider
isolation
status
owner
created

No giant metric cards.

==================================================
26. ENVIRONMENT AVAILABILITY TRUTH
==================================================

Truthful V1 status:

Kubernetes Namespace
Current development runtime available

Virtual Cluster
Planned / not available unless truly implemented

Dedicated Kubernetes
Planned

GitOps / Argo CD
Planned or experimental unless real provider exists

AWS Sandbox
Do not present as available.

Do not market Kind as production environment.

==================================================
27. REPRODUCIBILITY
==================================================

Pin environment information into run/certification reproducibility where
appropriate:

environment_component_contract_version
environment_profile_id
environment_profile_contract_version
provider_id
provider_contract_version
isolation_tier
runtime_contract_version

Historical results must remain interpretable even after catalogs evolve.

==================================================
28. SECURITY / TENANCY
==================================================

Workspace isolation mandatory.

EnvironmentInstance lookups scoped by workspace.

Provider-specific references must not allow cross-tenant access.

Do not expose:

kubeconfigs
service-account tokens
cloud credentials
provider secrets

through environment APIs.

Agent access remains through mediated HTTP/MCP/tool paths.

Do not give agents raw cluster-admin credentials.

==================================================
29. FAILURE BEHAVIOR
==================================================

Provision failure:

environment status failed
run/certification classified platform-invalid where applicable

Provider unavailable:

do not convert to agent failure

Reset failure:

do not reuse contaminated environment

Destroy failure:

record cleanup failure for later sweeper/recovery

Do not hide infrastructure failures inside generic agent evaluation failure.

==================================================
30. CLEANUP
==================================================

Preserve existing cleanup semantics.

Environment lifecycle must support:

provision
ready
use
reset/cleanup
destroy

or equivalent provider-specific safe lifecycle.

Repeated destroy must be idempotent.

Never accidentally destroy unrelated provider infrastructure.

==================================================
31. TESTS
==================================================

Focused tests must cover at minimum:

1. component catalog contract/version
2. profile catalog contract/version
3. provider contract/version
4. profile component composition
5. isolation ordering
6. namespace cannot satisfy dedicated requirement
7. dedicated may satisfy namespace minimum
8. missing component rejects profile
9. inactive/planned profile not selected for normal execution
10. unavailable provider excluded
11. no fidelity downgrade
12. deterministic scheduler
13. scheduler reason codes
14. current dev Kubernetes provider capabilities
15. Kind/dev not marked production
16. EnvironmentInstance workspace isolation
17. lifecycle state validation
18. destroyed cannot return to ready
19. repeated destroy idempotent
20. legacy ScenarioVersion compatibility
21. explicit requirements resolved
22. certification uses server-owned profile
23. arbitrary certification profile rejected
24. arbitrary runtime contract rejected
25. certification pins provider/profile/isolation
26. certification stale when required environment contract changes
27. provider credentials never returned
28. platform provisioning failure not agent failure
29. coverage unchanged
30. no AWS/GCP/Terraform implementation
31. no raw kubeconfig exposure
32. no LLM grading

==================================================
32. VERIFICATION
==================================================

gofmt touched Go files.

Run focused packages only:

environment/profile/provider/scheduler package(s)
existing Kubernetes environment package touched
scenario certification package
relevant httpapi package
worker/execution package only if directly changed

Do not run:

go test ./...
Docker
Kind
live Kubernetes
Prometheus

unless an existing focused test absolutely requires it.

Use deterministic fake providers for scheduler/lifecycle tests.

Frontend:

npx tsc --noEmit

One production build if practical.

==================================================
33. DO NOT IMPLEMENT
==================================================

Do NOT implement:

real EKS
vCluster installation
dedicated cluster creation
AWS sandbox
GCP
Terraform execution
Crossplane provisioning
new Kubernetes scenario corpus
GitOps runtime
Argo CD installation
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

Evalplane has a composable environment model.

An Evaluation Task declares infrastructure requirements.

The scheduler deterministically selects the least-fidelity adequate active
server-owned environment profile/provider.

No required isolation may be downgraded.

The current Kubernetes runtime is represented truthfully as a development
provider.

Certification and runs pin exact environment/profile/provider/runtime
contracts.

Arbitrary caller-provided environment strings can no longer determine
authoritative certification truth.

The architecture can later add:

shared EKS
virtual clusters
dedicated EKS
AWS sandboxes
Terraform/AWS composites

without rewriting the evaluation core.

==================================================
COMPLETION REPORT
==================================================

Report:

1. files changed
2. migrations/schema
3. component catalog/version
4. profile catalog/version
5. isolation model
6. task environment requirements
7. provider abstraction
8. current Kubernetes/dev provider adaptation
9. EnvironmentInstance model/lifecycle
10. scheduler algorithm
11. scheduler reason codes
12. no-downgrade behavior
13. certification integration/hardening
14. run lifecycle integration
15. legacy compatibility
16. reproducibility pins
17. API/RBAC
18. frontend environment UX
19. availability truthfulness
20. cleanup/idempotency
21. platform-failure behavior
22. focused backend tests
23. TypeScript/build
24. confirmation Kind is not represented as production
25. confirmation caller cannot invent authoritative profile/runtime
26. confirmation no AWS/GCP/Terraform implementation
27. confirmation no raw kubeconfig/provider credential exposure
28. confirmation no LLM grading
29. deferred work
30. real-runtime limitations

Write the completion report to:

prompts/reports/environment-profiles-composition.md

DO NOT overwrite this task file.
