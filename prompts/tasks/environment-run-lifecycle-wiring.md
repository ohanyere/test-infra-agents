EVALPLANE — ENVIRONMENT RUN-LIFECYCLE WIRING

Read AGENTS.md first.

Read:

- prompts/tasks/environment-profiles-composition.md
- prompts/reports/environment-profiles-composition.md
- backend/internal/environment
- backend/internal/executionworker
- backend/internal/execution
- backend/internal/kubeenv
- run/batch lifecycle code
- cleanup/recovery code

This is a NARROW COMPLETION PATCH for Stage 29.

Do not redesign the environment architecture.

==================================================
PROBLEM
==================================================

Stage 29 implemented:

ScenarioVersion environment requirements
→ environment scheduler
→ profile/provider selection
→ EnvironmentInstance model/provider APIs

But the normal worker execution path is not yet fully wired through this model.

The production-shaped run path must not bypass the environment scheduler/provider
architecture.

==================================================
1. RUN START PATH
==================================================

Before a runnable evaluation trial provisions its environment:

Run
→ exact ScenarioVersion
→ stored environment requirements
→ server-owned scheduler
→ profile/provider selection
→ EnvironmentInstance creation
→ provider provision
→ ready environment
→ existing execution

Do not let the worker directly provision Kubernetes while bypassing the environment
service.

Preserve the existing worker architecture as much as possible.

==================================================
2. LEGACY SCENARIOS
==================================================

Existing ScenarioVersions without explicit requirements must continue to work through:

LegacyDefaultRequirements()
→ kubernetes.namespace:v1
→ kind-dev:v1

Do not silently claim production isolation.

==================================================
3. ENVIRONMENT INSTANCE OWNERSHIP
==================================================

For each run environment:

owner_type = run
owner_id = exact run ID
workspace_id = run workspace

Persist the EnvironmentInstance before/around provisioning according to existing
repository conventions.

No cross-workspace environment reuse.

==================================================
4. PROVISIONING SUCCESS
==================================================

On successful provisioning:

EnvironmentInstance:
provisioning → ready

Run should retain/link the environment reference/instance identity using existing
fields where practical.

Do not expose kubeconfig or credentials.

==================================================
5. PROVISIONING FAILURE
==================================================

If scheduler resolution or provider provisioning fails:

- classify as platform/environment failure
- do not classify as agent failure
- do not invoke the agent
- do not produce misleading agent evaluation evidence

Use existing invalid_platform / platform failure conventions.

Persist enough safe evidence/status for diagnosis.

==================================================
6. PROVIDER UNAVAILABLE
==================================================

If required provider/profile is unavailable:

do NOT downgrade fidelity.

Example:

dedicated_cluster required
+
only namespace provider available
=
platform-invalid / no eligible environment

Never fall back to namespace.

==================================================
7. CLEANUP
==================================================

After terminal run completion:

- invoke provider cleanup/destroy according to current lifecycle
- update EnvironmentInstance state
- preserve idempotency
- repeated cleanup is safe

If cleanup fails:

- do not change an agent verdict
- record platform cleanup failure
- leave state suitable for sweeper/recovery

Do not destroy unrelated infrastructure.

==================================================
8. CANCEL / ABANDON / CRASH RECOVERY
==================================================

Integrate with existing worker lease/recovery semantics.

For:

cancelled run
abandoned run
worker crash
stale lease recovery

owned EnvironmentInstances must remain discoverable and cleanup-capable.

Do not introduce a second recovery framework.

Reuse Stage 18 worker recovery patterns where possible.

==================================================
9. RESET BEHAVIOR
==================================================

If a provider supports Reset and the run lifecycle currently requires reset/reuse:

- failed reset means environment must not be reused
- mark platform/environment failure appropriately

Do not add pooling/warm reuse if it does not already exist.

==================================================
10. RUN REPRODUCIBILITY
==================================================

Ensure the run/evidence path can identify:

environment_profile_id
environment_profile_contract_version
provider_id
provider_contract_version
isolation_tier
runtime_contract_version
EnvironmentInstance ID

Use existing run fields and EnvironmentInstance linkage where possible.

Do not add a migration unless genuinely necessary.

==================================================
11. WORKER ABSTRACTION
==================================================

The worker should depend on environment service/provider abstraction, not Kind-specific
implementation.

No direct new Kind-specific orchestration should be added to executionworker.

Kind remains merely the current development provider implementation.

==================================================
12. AGENT INVOCATION
==================================================

Agent invocation happens only after environment is ready.

Existing HTTP/MCP mediation remains unchanged.

Do not expose provider credentials, raw kubeconfig, or cluster-admin access.

==================================================
13. CERTIFICATION
==================================================

Do not redesign Stage 28 certification.

Keep its server-owned environment resolution behavior unchanged.

This patch is primarily normal run execution wiring.

==================================================
14. TESTS
==================================================

Use fake providers. Do not require live Kubernetes.

Focused tests must cover at minimum:

1. worker resolves ScenarioVersion requirements
2. legacy task uses legacy namespace default
3. scheduler-selected profile/provider creates EnvironmentInstance
4. environment ownership uses exact workspace/run
5. provider provision occurs before agent invocation
6. successful provision marks ready
7. scheduler failure prevents agent invocation
8. provision failure prevents agent invocation
9. provision failure becomes platform-invalid/platform failure
10. provider unavailable never becomes agent failure
11. dedicated requirement never downgrades to namespace
12. terminal run triggers cleanup
13. repeated cleanup idempotent
14. cleanup failure does not alter agent result
15. cancelled run cleanup
16. abandoned/stale run remains recoverable
17. workspace isolation
18. destroyed environment never reused
19. environment identity/pins remain available for reproducibility
20. worker has no Kind-specific dependency
21. no kubeconfig/provider secret exposure
22. coverage behavior unchanged
23. certification behavior unchanged
24. no LLM grading

==================================================
15. VERIFICATION
==================================================

gofmt touched Go files.

Run focused packages only:

internal/environment/...
internal/executionworker/...
internal/execution/... if touched
internal/kubeenv/... if adapter behavior touched
recovery/lease package(s) directly touched

Run relevant httpapi tests only if APIs changed.

Do not run:

go test ./...
Docker
Kind
live Kubernetes
Prometheus

Use deterministic fake providers.

No frontend work unless required.

==================================================
16. DO NOT IMPLEMENT
==================================================

No:

real EKS
vCluster
dedicated Kubernetes provider
AWS sandbox
Terraform
Crossplane
Argo CD runtime
new scenario corpus
environment pooling
warm clusters
billing
pass@k
trajectory explorer
LLM grading
frontend redesign

==================================================
DEFINITION OF DONE
==================================================

The normal evaluation Run lifecycle actually uses the Stage-29 environment model:

Scenario requirements
→ scheduler
→ server-owned profile/provider
→ EnvironmentInstance
→ provision
→ ready
→ agent execution
→ cleanup

Environment/platform failures are never mislabeled as agent failures.

Required isolation is never downgraded.

Legacy tasks continue through the truthful Kind development namespace profile.

==================================================
COMPLETION REPORT
==================================================

Report:

1. files changed
2. worker integration point
3. requirement resolution
4. scheduler usage
5. EnvironmentInstance creation/ownership
6. provisioning lifecycle
7. agent-invocation ordering
8. platform-failure behavior
9. no-downgrade behavior
10. cleanup behavior
11. cancellation/abandonment recovery
12. reproducibility linkage
13. legacy compatibility
14. worker/provider abstraction
15. focused tests
16. confirmation no Kind-specific worker dependency
17. confirmation no provider credential/kubeconfig exposure
18. confirmation environment failures never become agent failures
19. confirmation no live cloud/Kubernetes work
20. deferred real-runtime validation

Write to:

prompts/reports/environment-run-lifecycle-wiring.md

DO NOT overwrite this task file.
