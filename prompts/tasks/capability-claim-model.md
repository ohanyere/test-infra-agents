EVALPLANE — CAPABILITY & CLAIM MODEL

MODEL:
Use the current OpenCode/Muse session.

Read AGENTS.md first.
Inspect the existing agent, AgentVersion, capability catalog, scenario,
qualification, suite, MCP, and HTTP API implementation before editing.

Do not broadly refactor the repository.

==================================================
WHY THIS STAGE EXISTS
==================================================

Evalplane is evolving into:

"an evaluation harness and qualification platform for AI agents that operate
infrastructure."

The existing capability system is mainly low-level/runtime-oriented:

inspect
scale_workload
restart_pod
telemetry
etc.

Those remain useful.

But Evalplane now also needs to model a different concept:

AGENT CAPABILITY CLAIMS

Example:

An AgentVersion may claim:

- Kubernetes workload diagnosis
- Kubernetes capacity remediation
- GitOps drift remediation
- progressive-delivery recovery
- observability incident diagnosis

A claim means:

"This exact AgentVersion says it can perform this class of infrastructure work."

It does NOT mean the capability is tested or qualified.

==================================================
PRODUCT TRUTH
==================================================

The product must be able to distinguish:

CLAIMED
The agent/vendor says the AgentVersion supports the competency.

TESTED
Evalplane has exercised sufficient relevant tasks.

QUALIFIED
The evidence satisfies the applicable QualificationPolicy.

This stage implements CLAIMED only, plus the semantic catalog needed for later
coverage.

Do NOT fake TESTED or QUALIFIED if no coverage evidence exists.

==================================================
1. THREE DISTINCT CONCEPTS
==================================================

Do not collapse these into one table/concept.

A. COMPETENCY CAPABILITY

Examples:

kubernetes.workload.diagnosis
kubernetes.capacity.remediation
gitops.drift.remediation
delivery.progressive_delivery.recovery

Describes what the agent claims it can accomplish.

B. TOOL / RUNTIME CAPABILITY

Existing lower-level capabilities such as:

observe workloads
observe pods
scale workload
restart pod
read telemetry

These control compatibility and tool exposure.

C. QUALIFICATION STATUS

Derived later from evidence/coverage/policy.

Do not store a customer-editable "qualified=true" flag.

==================================================
2. VERSIONED SERVER-OWNED CAPABILITY CATALOG
==================================================

Create a server-owned semantic capability catalog.

Do NOT allow arbitrary customer-created capability IDs in V1.

The catalog must be versioned.

Suggested catalog contract:

infrastructure-capability-catalog:v1

Model hierarchy:

Domain
  → Capability Family
      → Capability

Example:

Kubernetes
  Workload Operations
    kubernetes.workload.diagnosis
    kubernetes.workload.remediation

  Capacity
    kubernetes.capacity.diagnosis
    kubernetes.capacity.remediation

GitOps
  Drift
    gitops.drift.detect
    gitops.drift.remediate

  Desired State
    gitops.desired_state.diagnose
    gitops.desired_state.remediate

Delivery
  Progressive Delivery
    delivery.progressive_delivery.diagnose
    delivery.progressive_delivery.recover

Observability
  Incident Diagnosis
    observability.metrics.diagnose
    observability.recovery.verify

Safety
  Operational Boundaries
    safety.scope.respect
    safety.protected_resource.avoid

Keep the initial catalog SMALL.

Do not create hundreds of entries.

==================================================
3. INITIAL V1 CATALOG
==================================================

Create an initial catalog sufficient for our V1 direction.

KUBERNETES

- kubernetes.workload.diagnosis
- kubernetes.workload.remediation
- kubernetes.capacity.diagnosis
- kubernetes.capacity.remediation
- kubernetes.deployment.diagnosis
- kubernetes.deployment.recovery
- kubernetes.networking.diagnosis
- kubernetes.dependency.diagnosis

OBSERVABILITY

- observability.metrics.diagnosis
- observability.telemetry.missing_data_reasoning
- observability.recovery.verification

GITOPS

- gitops.drift.detection
- gitops.drift.remediation
- gitops.desired_state.diagnosis
- gitops.desired_state.remediation
- gitops.argocd.diagnosis
- gitops.argocd.recovery

DELIVERY

- delivery.rollout.diagnosis
- delivery.rollout.recovery
- delivery.progressive_delivery.diagnosis
- delivery.progressive_delivery.recovery

SAFETY

- safety.scope.boundary_adherence
- safety.protected_resource_avoidance
- safety.stop_when_recovered

Use naming conventions cleanly and consistently.

Do not add AWS/Terraform/GCP capabilities yet.

The architecture must allow them later without migration redesign.

==================================================
4. CAPABILITY METADATA
==================================================

Each capability should expose server-owned metadata approximately:

id
domain
family
display_name
description
catalog_version
status

Status may support:

active
experimental
planned

Only active capabilities should normally be claimable.

Do not store arbitrary marketing prose from customers inside the catalog.

==================================================
5. LOWER-LEVEL REQUIREMENTS
==================================================

A competency capability may reference required low-level runtime capabilities.

Example:

kubernetes.capacity.remediation

may require things conceptually like:

observe workload
read telemetry
scale_workload

This is NOT scenario coverage yet.

It is only the minimum tool/runtime capability compatibility requirement.

Reuse the existing Stage-23/24 agent capability catalog.

Do not duplicate the low-level capability vocabulary.

==================================================
6. AGENTVERSION CLAIMS
==================================================

Add immutable capability claims to exact AgentVersions.

Likely migration:

000015_agent_capability_claims

Adapt numbering if the repository has advanced.

Conceptually:

agent_version_capability_claims

- agent_version_id
- capability_id
- catalog_version
- claimed_at

Claims are part of AgentVersion identity.

Once an AgentVersion is registered, its claimed competencies do not mutate.

If the behavioral capabilities change:

register a new AgentVersion.

Do not let a customer silently add/remove claims from an already-tested
AgentVersion.

==================================================
7. VERSION CREATION
==================================================

When registering a new AgentVersion, allow the customer to select competency
claims from the active server catalog.

Validate:

- capability exists
- capability is active
- no duplicates
- workspace owns AgentVersion
- underlying low-level runtime capability requirements are compatible

If an AgentVersion claims:

kubernetes.capacity.remediation

but its registered tool/runtime capabilities cannot even observe workloads or
scale, registration should fail with a structured incompatibility result.

Do not automatically add missing tool permissions.

==================================================
8. CLAIM DOES NOT EQUAL QUALIFICATION
==================================================

This is mandatory.

API/UI must never return:

qualified=true

merely because a claim exists.

Expose status approximately:

claim_status = claimed

Future coverage/qualification may augment with:

tested
qualified
not_qualified
inconclusive
untested

But do not fabricate those states now.

If qualification data genuinely exists for a capability later, that comes from
the coverage engine.

==================================================
9. CURRENT QUALIFICATION SYSTEM
==================================================

Do not break Stage 22 QualificationPolicy.

That system qualifies:

AgentVersion against SuiteExecution + PolicyVersion.

This stage does NOT replace it.

The next coverage stage will connect:

Capability Claim
→ required Scenario Families
→ Qualification Suite
→ QualificationPolicy

==================================================
10. API
==================================================

Add authenticated read APIs consistent with existing conventions.

Approximately:

GET
/v1/capability-catalog

GET
/v1/capability-catalog/{capabilityID}

GET
/v1/workspaces/{workspaceID}/agents/{agentID}/versions/{versionID}/capability-claims

AgentVersion creation API should accept:

claimed_capability_ids

or equivalent.

Do not add a separate mutable endpoint such as:

PATCH /agent-version/claims

unless it is restricted to pre-registration draft state.

Exact persisted AgentVersion claims must remain immutable.

==================================================
11. RBAC / ISOLATION
==================================================

Catalog:
authenticated read.

AgentVersion claim creation:
same permissions as AgentVersion registration.

Viewer:
read only.

Owner/admin/member:
register versions and select claims.

Enforce workspace ownership server-side.

==================================================
12. REPRODUCIBILITY
==================================================

Add capability catalog contract identity to AgentVersion/reproducibility data
where appropriate.

Example:

infrastructure-capability-catalog:v1

A historical AgentVersion must continue to indicate exactly which catalog
contract its claims came from.

Future catalog v2 must not silently rewrite historical claims.

==================================================
13. FRONTEND — AGENT REGISTRATION
==================================================

Update /agents/new and new-version registration.

Do NOT create thousands of checkboxes.

Use hierarchy:

Kubernetes
  Workload Operations
  Capacity
  Deployment
  Networking

Observability
GitOps
Delivery
Safety

Customers select what the AgentVersion CLAIMS it can do.

Use concise language:

"What can this agent operate?"

or:

"Claimed capabilities"

Include clear text:

"Claims are not qualifications. Evalplane must test them before they become
qualified."

Do not imply AWS/GCP/Terraform support yet.

==================================================
14. AGENT DETAIL PAGE
==================================================

Add a real section:

Capability claims

Show hierarchy and claim state.

Example:

Kubernetes

Capacity remediation
CLAIMED

GitOps

Drift remediation
CLAIMED

Do not show green "Qualified" status.

If the backend does not yet have coverage data:

Tested     —
Qualified  —

or equivalent restrained placeholder.

Do not use fake percentages.

==================================================
15. PRODUCT COPY
==================================================

Align with the repositioned frontend.

Agents do not merely have "capabilities."

Distinguish:

Connection/runtime capabilities
and
Capability claims.

Avoid confusing users by rendering both in one undifferentiated list.

Suggested headings:

Claimed capabilities
Connection & tool access

==================================================
16. COMPATIBILITY
==================================================

Existing AgentVersions created before this migration must continue to work.

They may have:

zero competency claims

which means:

No capability claims declared.

Do not guess claims from low-level tool permissions.

Do not retroactively label old agents as Kubernetes SRE agents.

==================================================
17. TESTS
==================================================

Focused backend tests must cover at minimum:

1. catalog version constant
2. domain/family hierarchy
3. capability IDs unique
4. unknown claim rejected
5. planned/inactive claim rejected where appropriate
6. duplicate claim rejected
7. AgentVersion claim immutable
8. new AgentVersion can have different claims
9. historical AgentVersion claims unchanged
10. low-level requirement mismatch rejected
11. claim does not produce qualified=true
12. existing AgentVersion with no claims still loads
13. workspace isolation
14. viewer cannot create version/claims
15. catalog read works
16. reproducibility records catalog version
17. no AWS/Terraform/GCP fake capability support
18. no LLM grading

==================================================
18. FRONTEND TEST / VERIFICATION
==================================================

Run:

npx tsc --noEmit

Run one Next.js production build if practical.

Do not repeatedly retry pathological builds.

Check:

/agents/new
/agents/[id]

==================================================
19. BACKEND VERIFICATION
==================================================

gofmt.

Run focused packages only:

agent
agentcapability / new semantic catalog package
httpapi
reproducibility packages directly modified

Do not run:

go test ./...
go vet ./...
Docker
Kind
Kubernetes
PostgreSQL
Prometheus

==================================================
20. DO NOT IMPLEMENT
==================================================

No:

Scenario Families
Coverage engine
automatic suite construction
reference solutions
scenario certification
AWS
Terraform
GCP
Argo CD runtime
new Kubernetes scenarios
PagerDuty
Datadog
billing
payment backend
Stripe
LLM grading
frontend-wide redesign

==================================================
DEFINITION OF DONE
==================================================

This stage is done only when:

- Evalplane has a versioned infrastructure competency catalog
- exact AgentVersions can make immutable claims against it
- claims are distinct from tool permissions
- claims are distinct from qualification
- claims respect low-level compatibility
- old agents continue working
- customer UI can select and inspect claims
- no fake qualification state is shown
- focused tests pass

==================================================
COMPLETION REPORT
==================================================

Report:

1. files changed
2. migration/schema
3. catalog architecture/version
4. initial capability hierarchy
5. competency vs tool-capability separation
6. AgentVersion claim persistence/immutability
7. low-level compatibility validation
8. API/RBAC
9. reproducibility behavior
10. old-AgentVersion compatibility
11. /agents/new changes
12. Agent detail changes
13. claimed/tested/qualified UX behavior
14. backend tests
15. frontend verification
16. confirmation claims never imply qualification
17. confirmation no fake AWS/Terraform/GCP capabilities
18. confirmation no LLM grading
19. deferred work

After completing the implementation, write this report to:

prompts/reports/capability-claim-model.md

DO NOT overwrite this task file.
