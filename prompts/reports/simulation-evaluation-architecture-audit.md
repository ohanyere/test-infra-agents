# Simulation & Evaluation Architecture Audit

## 1. Executive conclusion

The current architecture is salvageable and should be extended, not rewritten. It already contains most of the product control plane needed for “simulation and evaluation infrastructure for agents that operate infrastructure”: immutable `AgentVersion`s and `ScenarioVersion`s, repeated trials and hidden variants, deterministic evidence evaluation, platform-invalid handling, controlled HTTP and MCP agent interfaces, scoped Kubernetes actions, evidence and trajectory projections, reliability and regression analysis, and qualification as a policy layer over evidence.

It is not yet a general simulation platform. The live data plane is one Kubernetes traffic-spike fixture composed in code: a run-owned namespace, reference workload, per-run Prometheus, load generation, Kubernetes observations, and two mutations. The environment package describes compositions but does not materialize a component graph. A run owns one monolithic `EnvironmentInstance`, a provider provisions one profile, and the worker/runtime contains Kubernetes-specific initialization, workload, injection, observation, and cleanup logic.

No wholesale rewrite is needed. The largest real gap is a first-class, versioned world plan and lifecycle that can materialize and verify multiple related components while retaining the existing run, evidence, evaluator, gateway, ownership, and policy machinery. Stage 39 should introduce that missing runtime seam. Stages 40 and 41 should strengthen trust boundaries and prove it in a real repeated execution; they should not replace the working domains.

Approximate proximity is uneven: the evaluation control plane is substantially implemented and focused-verified; the provider-agnostic composed-world data plane is designed only in outline. The honest current claim is “a deterministic, controlled evaluation system with a development Kubernetes simulation fixture,” not yet “a general simulation environment platform.”

## 2. Repository state

- Repository: `/home/ohanyere_francis/projects/test-infra-agents`
- Branch: `main`
- HEAD: `d4e61ea` (`fix(migrations): correct capability coverage evidence guard`)
- Worktree before this report: clean (`git status --short` produced no output).
- Remote: `origin` at `https://github.com/ohanyere/test-infra-agents.git` for fetch and push.
- Tracking: `main` is three commits ahead of `origin/main` and zero behind (`origin/main...HEAD` = `0 3`). The audit did not fetch, so “remote status” means status against the locally recorded remote-tracking ref.
- Recent relevant history: environment composition (`b2e0da5`), deterministic scenario certification (`e62b866`), HTTP/MCP runtime (`b2e9c07`), evaluation/evidence (`211d070`), reliability (`639f4fc`), trajectory (`2992c36`), qualification (`18d0dc8`), API wiring (`ea3c2fd`), and frontend journey (`be2329c`). HEAD includes migrations through `000020`, despite the supplied repository note saying `000014`.

## 3. Current architecture map

| Subsystem | Path / responsibility and authority | Important contracts, persistence, call graph | Maturity and limitations |
|---|---|---|---|
| Agent / AgentVersion | `internal/agent`; logical agent and immutable executable version | `agents`, `agent_versions`, `agent_version_capability_claims`; API and execution load exact versions | **IMPLEMENTED**. DB trigger pins identity, endpoint, tool mode, capabilities and metadata. Only HTTP invocation is executed; `interface_kind=mcp` is not an inbound-agent transport. |
| Connections / credentials | `internal/agentconnection`, `agentgateway`; platform-to-agent endpoint and auth | `agent_version_connections`; encrypted bearer envelope; connection policy; worker resolves by workspace and exact version | **LIVE** for HTTP. Credential is AgentVersion-scoped, long-lived until rotated, and distinct from MCP tool grants. |
| Capability claims | `agentcapability`, `competency`, `coverage` | immutable claims, server catalogs and coverage requirements/families | **IMPLEMENTED / deterministic**. Correctly separates claims, tested coverage, and qualification. |
| ScenarioVersion | `scenario`, `scenarioauthoring` | `scenario_versions`, `scenario_criteria`; agent view and evaluator view projections | **IMPLEMENTED**, but several contracts remain raw JSON and runtime only supports traffic spike. |
| Task families | `coverage` | family catalog plus explicit ScenarioVersion assignment in migration `000016` | **IMPLEMENTED / deterministic**. Coverage assignment is explicit, not inferred. |
| Scenario certification | `scenariocertification` | certifications, gates, evaluator-only reference remediation; called by API and qualification planning | **FAKE/DETERMINISTIC ONLY** for runtime claims. The engine tests synthetic fixtures, including reset/leakage gates; it does not execute a live world. |
| Environment requirements | `environment/requirements.go` | one row per ScenarioVersion in `scenario_version_environment_requirements` | **IMPLEMENTED model**. Flat component IDs, isolation, capabilities, runtime contract; no relationships, versions/config, lifecycle hooks, or minimum fidelity. |
| Components / profiles | `environment/catalog.go` | server-owned versioned catalogs | **PARTIALLY LIVE**. Kubernetes and Prometheus active; Git marked active but no executable profile/provider; Argo/Helm experimental/planned. Profiles are static bundles, not graphs. |
| Providers | `environment/provider.go`, `kinddev.go` | `Provider` (`Provision/Get/Reset/Destroy`, health/capabilities); Kind adapter | **PARTIALLY LIVE**. Only `kind-dev:v1` is active. AWS/EKS/vCluster are planned constants. Interface is provider-neutral at the top but assumes one profile→one opaque environment handle. |
| EnvironmentInstance / scheduler | `environment/repository.go`, `service.go`, `scheduler.go` | `environment_instances`; deterministic least-tier matching; run/certification ownership | **IMPLEMENTED model, PARTIALLY LIVE runtime**. One instance per owner in the worker path, one provider/profile, no child components or composition plan. |
| Execution / worker | `execution`, `executionworker`, `cmd/worker` | batches/runs/leases; claim, provision, prepare, inject, agent loop, observe, evaluate, destroy; billing settlement | **LIVE for the narrow Kubernetes fixture**. Kubernetes dependencies are composed directly; only HTTP agent versions and traffic-spike execution specs run. |
| HTTP Agent Gateway | `agentgateway` | bounded request/response protocol v1; platform invokes agent with platform-to-agent bearer | **LIVE**. Sends run/version/task, projected context, resources/actions, observations, and optional MCP access. Direct-mode agents return proposed actions; they receive no tool credential. |
| MCP | `mcpserver`, migration `000013` | stateless Streamable HTTP; hashed run/version/workspace grant, TTL, budget, revocation | **LIVE for worker-scoped Kubernetes tools**. Strong logical run binding; in-process scope cache and shared worker network/process remain limitations. |
| Tool executor | `actionexec` | authorize/recheck/execute inspect, no-op, scale, restart; labels and namespace ownership | **LIVE but Kubernetes-specific**. No adapter-neutral tool registry or component routing. |
| Telemetry / observation | `telemetry`, `observation` | per-run Prometheus plus Kubernetes object projection | **LIVE for fixture**. No general collector contract, snapshot identity, or telemetry isolation proof beyond run namespace/service. |
| Evaluator | `evaluator` | `deterministic-evaluator:v1`; consumes terminal run, events, actions, scenario criteria | **IMPLEMENTED and focused-verified**. Objective metric/state reductions and safety denials; limited metric vocabulary; no arbitrary invariant verifier or durable snapshot. |
| Evidence | `evidence` | `run_events`, `agent_actions`, `evaluations`, criterion results/findings | **IMPLEMENTED**. Requested/authorized/executed lifecycle is retained with bounded safe payloads. No cryptographic append integrity or external attestation. |
| Trajectory | `trajectory` | deterministic safe projection with contract and reproducibility pins | **IMPLEMENTED**. Projection, not the authoritative raw truth store; logically redacted. |
| Repeated trials / variants | `evaluationsuite`, migrations `000008`/`000009` | immutable suite versions, per-run variant ID/seed/config and ordinals | **IMPLEMENTED**. Hidden config excluded from agent serialization and aggregated results; runtime variant schema is traffic-spike-only. |
| Reliability | `reliability` | valid-only metrics, family/task/capability reports, comparability pins | **IMPLEMENTED / deterministic**. Properly excludes platform-invalid evidence; real-world repeatability remains unproven. |
| Regression | `agentregression` | paired trial and criterion comparisons across exact versions | **IMPLEMENTED / deterministic**. Depends on comparable suite evidence and current limited runtime. |
| Qualification policy | `qualification` | immutable policies/assessments over suite, reliability, safety, regression facts | **IMPLEMENTED / deterministic**. Correctly derived rather than synonymous with evaluation. |
| Qualification planner | `qualplan` | claims→coverage requirements→certified runnable tasks→immutable suite | **IMPLEMENTED / deterministic**. Candidate eligibility can overstate runtime proof because certification is fixture-based. |
| Billing gate | `billing`, worker/API composition | entitlement reserve before execution and settle from persisted run | **IMPLEMENTED**. Scheduler never downgrades environment to fit entitlement. Not a world-engine concern. |

Major persistence also includes scenario certification (`000017`), environment pins (`000018`), qualification plans (`000019`), and billing entitlements (`000020`). `cmd/referenceagent` and `cmd/referenceapp` are deterministic fixtures, not competence or fidelity proof. `backend/e2e` is cluster-oriented qualification scaffolding and was not run.

## 4. Current end-to-end execution path

An API caller creates an evaluation batch or suite execution pinned to an exact ScenarioVersion and AgentVersion. Starting execution passes billing/authorization gates and queues runs. A worker atomically claims a queued run into `provisioning` with a lease.

The worker then:

1. Reloads the exact versions and rejects identity mismatch, non-HTTP agent interface, unsupported execution spec, or unsupported hidden variant as platform/scenario runtime errors.
2. Loads the ScenarioVersion’s stored environment requirements, falling back to Kubernetes+Prometheus namespace requirements for legacy versions.
3. Scheduler-selects an active adequate profile/provider without isolation downgrade; persists one run-owned `EnvironmentInstance`; calls the Kind provider, which creates a labeled namespace; records the safe `k8s://` reference and marks ready.
4. `KubernetesRuntime.Prepare` deploys the reference workload, waits ready, creates per-run telemetry, and waits for the metrics deployment. This combines initialization and baseline readiness but does not persist or independently verify a baseline snapshot.
5. `Start` launches the traffic spike (with hidden variant parameters). There is no separate “verify injected scenario exists” phase.
6. Marks the run `running`, repeatedly collects projected observations, invokes the HTTP agent, mediates actions, records decisions/actions/observations, and stops on an empty/no-op response or the step bound.
7. `Finish` waits for traffic completion. Deferred finalization records a final observation when possible, terminalizes the run, evaluates persisted evidence, settles billing, revokes MCP, and destroys the namespace.

Errors are typed as agent, platform, or control in the worker. Provision/prepare/start/action-executor/observation/final-observation/cleanup failures are platform-class evidence. Agent timeout, invalid response, or duplicate MCP/direct action behavior are agent-class failures, although the terminal run is still `failed`. The deterministic evaluator makes platform-class evidence inconclusive. Suite aggregation then marks terminal platform-evidenced trials `invalid_platform` and removes the evaluation result from agent consistency counts.

Crash recovery acquires a stale lease, records platform interruption, cleans the owned environment, fails the run, evaluates, and settles; it never reinvokes the agent or replays actions.

## 5. Research-derived target architecture

The target should be expressed as an evolution of current contracts:

`ScenarioVersion` → immutable typed `WorldSpec` reference/version → deterministic `CompositionPlan` → one trial-owned World containing component instances and relationships → staged initialize/baseline/workload/inject/verify lifecycle → current controlled gateway/MCP surface bound to the run and world → current evidence pipeline plus immutable state snapshots → privileged verifier → current evaluator/result → current suite/reliability/regression → qualification policy when applicable.

Keep `runs` as Trial identity and `environment_instances` as the trial-owned world lifecycle record if its semantics are broadened carefully. Add child component-instance records rather than treating each component as an independent Trial environment. A `CompositionPlan` should contain pinned WorldSpec/contract identity; selected component definitions and versions; component→adapter/provider placements; dependency edges and endpoint bindings; isolation/network policy; secret references (never secret values); ordered lifecycle steps; readiness and baseline checks; workload and condition-injection bindings; observation/tool bindings; verifier bindings; reset/destroy strategy; minimum/selected fidelity; and stable plan hash. It must not contain evaluator secrets in an agent-visible projection.

## 6. Scenario model audit

| Desired semantic | Current representation | Finding |
|---|---|---|
| Task/objective | `agent_task`, description | Present; agent task is explicitly projected. |
| Systems/components | `scenario_versions.environment` JSON plus typed `Requirements.RequiredComponents` | Partial. Requirements are authoritative for scheduling but flat. |
| Topology/relationships | none | Missing. Profiles imply co-presence but no endpoints, dependency edges, or data/control relationships. |
| Known baseline / initial state | reference `preconditions`, certification `InitialBroken`, runtime hard-coded deployment readiness | Partial and fragmented; no runtime baseline contract/snapshot. |
| Workload/activity | `execution_spec` traffic profile and `scenariorunner` | Present only as raw JSON + one runtime type. |
| Fault/condition | `conditions`, traffic spike start, hidden runtime config | Partial; conditions are not typed or bound to an injector contract. |
| Observables | typed string list plus agent projection | Present, vocabulary-controlled at runtime but not component-addressed. |
| Actions/tools | `allowed_actions`, `allowed_resources`, AgentVersion capabilities | Present for Kubernetes vocabulary; component/tool schema absent. |
| Success invariants | metric criteria and reference expected state | Partial. Criteria are deterministic; expected state belongs to certification reference and is not the live grading contract. |
| Forbidden invariants | free-form forbidden behaviors plus four structured safety denial classes | Partial. Unsupported free-form entries force inconclusive. |
| Reference remediation | dedicated evaluator-only table/model | Present and correctly described as solvability proof, not required trajectory. |
| Hidden variants | immutable suite run assignment, ID/seed/config | Present. Traffic-spike schema only; configs are hidden from agent JSON. |
| Reset contract | reference cleanup text and synthetic certification reset gate | Described but not live-executable or verifiable. |
| Fidelity requirements | minimum isolation only | Missing as evidence fidelity. Isolation is related but not equivalent. |
| Task family / case type | explicit version assignment | Present and version-relevant. |
| Runtime/environment contract | runtime contract in environment requirements | Present but only one allowed contract. |

The smallest evolution is a typed, versioned `WorldSpec v1` stored alongside each new ScenarioVersion (or referenced immutably by it), compiled from the existing environment/conditions/execution/observables/actions/criteria fields. It should use typed component nodes, typed edges, typed initializer/workload/injector/verifier/reset references, and versioned parameter schemas. Existing immutable ScenarioVersions must retain their current fields and resolve through a deterministic `legacy-kubernetes-traffic-spike` compiler; no historical JSON should be rewritten. Strong contracts should be normalized into versioned rows or validated JSON documents with discriminated types, not one giant untyped JSON blob.

Direct answer: current Environment Requirements can evolve into the requirement/selection portion of WorldSpec without a rewrite, but they cannot alone become the full WorldSpec. Preserve them as compatibility inputs or a derived index while adding topology and lifecycle contracts.

## 7. Environment/world engine audit

Current composition is flat, not graph-based. A ScenarioVersion can require multiple component IDs (and GitOps requirements list five), but selection merely finds one static profile whose component string set is a superset. Only the Kubernetes namespace profile is executable. One provider call provisions one opaque handle. There is no `CompositionPlan`, dependency representation, endpoint wiring, component version/config, per-component health, or rollback plan.

One current `EnvironmentInstance` represents the complete selected profile/world from the core’s perspective, not one provider resource. The Kind provider’s actual resource is one namespace containing several Kubernetes resources. The worker store lookup is singular by `(workspace, run)`; one trial cannot own multiple EnvironmentInstance rows through the authoritative path. This is a useful aggregate boundary. Retain it as the world-level record and add `environment_component_instances` (or equivalent) beneath it; do not place a second competing `World` above it unless compatibility semantics make broadening `EnvironmentInstance` impossible.

Provisioning and initialization are partially separate: provider provisioning creates the namespace; `Runtime.Prepare` deploys workload and telemetry and waits ready. But baseline initialization, component readiness, baseline verification, and telemetry setup are fused in Kubernetes runtime code. Workload generation (`scenariorunner.StartTrafficSpike`) is separate as a package but invoked through KubernetesRuntime. Fault/condition injection is not a generic interface; the traffic spike is simultaneously workload and scenario condition. Baseline is neither persisted nor compared. “Scenario exists” is inferred from successful start, not verified.

Reset is not proof. `KindDevProvider.Reset` only checks that the namespace handle exists. Normal execution destroys rather than pools/reuses. `ResetForRun` exists but is not in the normal worker path, does not transition resetting→ready, and performs no equivalence checks. There is no contaminated/quarantined state, baseline digest, reset evidence, or reuse lease. A reset error marks failed, but a successful no-op reset says nothing about cleanliness. Current reset proof is **DESIGNED**, not FOCUSED-VERIFIED or LIVE-PROVEN: focused tests verify lifecycle/idempotent destroy semantics, while the certification “reset cleanliness” gate uses deterministic simulation fixtures rather than a live reset.

Provider refs are length-bounded safe opaque strings and are checked for safe schemes in managed worker code; kubeconfigs/cloud tokens are not stored in `environment_instances` or returned to agents. Agents interact through observations/actions. The worker itself holds a Kubernetes client with authority determined by deployment configuration; network and RBAC isolation strength is external to these contracts.

Platform failures generally remain separate from agent failures. They create platform-class events, cause evaluator inconclusive, and become suite `invalid_platform`. A cleanup failure occurs after terminalization/evaluation, but it is recorded as platform evidence and suite aggregation queries platform events, so it invalidates the trial for aggregate agent outcomes. This is good. The missing class is explicit live `invalid_task`: malformed/unsupported scenarios usually become platform/scenario runtime failures or are blocked by certification, rather than a distinct trial validity.

The existing `Provider` abstraction can host Git/Kubernetes/Argo/AWS adapters without core provider-specific branching only if evolved from “one profile, one handle” to component-aware planning/execution. Its current neutral request is a sound outer boundary, but a single provider per profile cannot express Git supplied locally, Argo/Kubernetes supplied by a cluster adapter, and telemetry supplied by another adapter. Provider selection belongs per planned component or cohesive provider bundle, coordinated by the composition engine.

Extension versus redesign:

- Extend `Requirements`, catalogs, scheduler decisions, `EnvironmentInstance`, provider registry, worker lifecycle, evidence pins, and APIs.
- Keep run ownership, workspace scoping, opaque refs, no-downgrade scheduling, state-transition discipline, leases, cleanup recovery, and billing gate.
- Add composition plan, child component instances, lifecycle interfaces, snapshots, reset verification, quarantine, and fidelity.
- Refactor KubernetesRuntime behind those interfaces incrementally; do not replace execution/evidence/evaluator domains.

Real Git+Argo+Kubernetes+AWS-like composition cannot run today. Git/Argo entries are catalog metadata only; AWS providers are planned. Kubernetes+Prometheus traffic spike is real code and can run against a supplied cluster; most control-path tests use fakes. GitOps and certification capabilities are structurally described but not implemented as live providers.

## 8. Agent interface audit

### HTTP-connected trial

Platform → agent authentication uses the AgentVersion connection’s optional bearer token, encrypted at rest by `agentconnection.Cipher`, decrypted only during resolution, sent in the HTTP Authorization header, and cleared from local values afterward. The request binds `run_id`, `agent_version_id`, step session, task, visible context, allowed resources/actions, and projected observation. The agent returns bounded proposals. The worker independently checks ScenarioVersion policy, run namespace, declared targets, live ownership labels, and parameters before mutation.

For direct mode, agent → platform tool authentication does not exist because the agent does not call tools: it proposes actions in its HTTP response and the platform executes them. Platform-to-agent credentials are AgentVersion-scoped and long-lived, not run-scoped.

### MCP-connected trial

“MCP” is a tool-access mode on an HTTP-connected agent, not an alternative platform→agent invocation protocol. Before the loop, the worker creates an opaque random bearer grant whose SHA-256 hash is stored. The DB row binds workspace, running run, exact AgentVersion, expiry, and call budget. The clear token is sent only in `tool_access` with the worker’s public MCP endpoint. Each stateless request resolves an active grant, consumes budget, and uses an in-memory scope binding to the exact run environment and safe AgentView. Terminal run triggers and cleanup revoke it.

The exposed tools are projected workload/pod/event/telemetry reads, scale, restart, and no-op. MCP policy intersects ScenarioVersion allowed actions with AgentVersion capabilities, enforces target namespace/resources, then rechecks live Kubernetes ownership in the executor. This prevents one valid grant from selecting another run’s namespace through normal tool inputs. Workspace/run/version DB joins protect grant resolution.

The two credentials are distinct: platform→agent connection bearer versus agent→platform MCP run grant. Only the former is reversibly persisted, encrypted and AgentVersion-scoped; MCP stores only a hash and is temporary/trial-scoped. HTTP and MCP agents receive enough context for the current single namespace world: run ID, task, allowed resources/actions, observations, and (for MCP) a scope already containing the environment. They do not receive a general component topology or stable component endpoints, so this is insufficient for a future composed world.

Bypass resistance is bounded, not absolute. An agent cannot obtain raw provider credentials through the protocol. However, the platform deliberately calls a customer endpoint, and repo code does not prove network egress restrictions for that agent. If the endpoint independently possesses cluster/cloud credentials, Evalplane cannot prevent out-of-band actions. The worker/MCP/verifier/action executor share process and Kubernetes client. Stronger per-trial network identities, audience-bound tokens, component-scoped permissions, and separated verifier credentials can be added without changing the agent protocol’s basic tool-access shape.

MCP does not project ground truth/reference/hidden config. Its scope contains `AgentView`, version and environment, not `EvaluatorSpec`. HiddenVariant has `json:"-"`. This is good logical projection. It is not process/network isolation.

## 9. Evaluation/verifier audit

The authoritative path is ScenarioVersion criteria + terminal run + time-windowed `run_events` and `agent_actions` → normalized metrics/safety → deterministic result persisted with `deterministic-evaluator:v1`. Metrics use explicit reducers: maxima for error/latency, minimum availability, latest infrastructure counts, and action count. Missing or invalid metrics are inconclusive. Structured denied operations produce safety failures. Free-form forbidden behaviors outside four recognized categories produce an inconclusive limitation.

This is substantially state/evidence-based and does not compare the agent trajectory to reference remediation. Reference actions are stored in a separate certification domain and explicitly described as solvability proof. Final infrastructure counts use the latest observation, while time-series recovery metrics cover the run window. Therefore final-state grading is independent from reference remediation, but not yet sufficient for arbitrary worlds: it lacks typed state invariants, a privileged final snapshot, scenario-existence proof, and explicit recovery verification requirements. “Latest observation” can be unavailable or precede cleanup/failure.

Requested versus executed action is modeled correctly through `agent_actions` states (`proposed`, `allowed`, `denied`, `executed`, `failed`) with authorization and safe request/result payloads. Recovery is inferred by trajectory logic and metric criteria; it is not a generic verifier assertion. Agent timeouts/invalid responses are agent-class worker failures but yield run failure/inconclusive evaluation, not an explicit agent fail result. Platform timeouts are platform-class. Telemetry unavailable yields inconclusive and, when represented as a finding/event, platform-invalid aggregation. An environment failure after apparent agent success—including final observation or cleanup—can attach platform evidence and invalidate aggregate agent outcomes.

Trust separation is primarily logical:

- Strong logical data projection: AgentView excludes `GroundTruth` and evaluator execution spec; hidden variant serialization is disabled; reference remediation has no agent read path; MCP projects only allowed observation channels.
- Partial storage separation: reference remediation is a dedicated table/service and MCP grants store hashes.
- Weak runtime isolation: worker, observer, action executor, MCP bridge, runtime, and evaluator wiring share the same binary/process and often the same Kubernetes client/database. No separate verifier service account, network plane, signed snapshot, or one-way evidence channel is proven.

Results would be untrustworthy if certification’s synthetic gates were presented as live solvability/reset proof; if opaque event JSON could be written by an untrusted path; if final observation is absent but a criterion does not require it; if provider/runtime credentials are overprivileged; or if telemetry from reused/shared resources is not trial-labeled. Current code avoids some of these through ownership and invalid-platform evidence, but not through a hardened verifier boundary.

## 10. Reset/repeatability audit

Normal Trial 1 → Trial 2 behavior is independent namespace creation followed by destroy, not pool/reset/reuse. Each run gets a distinct environment row and run-labeled namespace. Evidence rows are keyed by run and evaluator filters events/actions to the run time window, so stale database evidence should not contaminate another trial. Hidden variant assignment is immutable per run. Kubernetes resources carry run, batch, workspace, managed, and component labels; actions revalidate them.

What is not proven:

- Namespace deletion completion/equivalent clean state before later provisioning.
- Prometheus sample isolation beyond the per-run namespace/service lifecycle; no persisted telemetry source identity or empty-window check.
- cleanup of external/non-namespace components, because none exist yet.
- hidden variant cleanup in a reusable world.
- reset idempotence/equivalence; only destroy is intentionally idempotent.
- quarantine after cleanup/reset verification failure.
- sweeper cleanup for an untracked provider instance: `sweepUntracked` is currently a no-op.

Crashed workers are handled well at the run level: lease recovery never invokes the agent or replays actions, attempts owned cleanup, records platform events, terminalizes, evaluates, and settles. If the provider is unavailable, the instance remains non-destroyed for later handling, but there is no implemented provider sweeper/quarantine workflow in the inspected code.

Classification: **DESIGNED**. There are focused tests for ownership, lifecycle transitions, idempotent destroy, recovery, and worker cleanup. There is no live proof that a dirty world is restored to an equivalent baseline and safely reused.

## 11. Fidelity audit

The code currently pins profile, provider, isolation tier, runtime contract and catalog contract versions, and reliability comparability treats these semantic pins separately from opaque instance refs. This is a useful provenance base. It is not a fidelity model. `Production`/`DevelopmentOnly` flags and isolation tiers answer availability or isolation, not whether evidence is synthetic, emulated, replica, or real-provider.

Minimal model:

- `synthetic_fixture`: deterministic in-memory/fake certification or reference components.
- `local_emulated`: local Kind, local Git/CI emulator, LocalStack-like service.
- `isolated_replica`: product-real software stack in isolated infrastructure (for example Git service + Argo CD + Kind/Kubernetes + Prometheus) without the external managed provider.
- `isolated_real_provider`: dedicated/scoped real provider resources and APIs.

Store a selected fidelity record on the immutable CompositionPlan and copy it to EnvironmentInstance, run evidence/reproducibility pins, certification, and evaluation metadata. Store dimensions rather than only one ordinal where relevant: control-plane fidelity, API/provider fidelity, isolation, telemetry fidelity, and destructive-action allowance; keep v1 small with a tier plus provider-declared evidence/details. The provider/adapter must declare a maximum supported fidelity and the scheduler must never upgrade the claim beyond observed/provider truth.

A ScenarioVersion WorldSpec may require minimum fidelity. QualificationPolicy should be able to require minimum fidelity per capability/task family and exclude lower-fidelity trials. Exact fidelity contract version, selected tier/dimensions, provider adapter version, component artifact/image/repository revisions, WorldSpec hash, and plan hash must be pinned so historical results remain interpretable. Existing runs retain “unspecified/legacy” rather than being backfilled with guessed fidelity.

## 12. Reliability/regression/qualification audit

The implemented relationship is fundamentally correct:

`Trial evidence` → deterministic `Evaluation` → suite aggregation with validity → reliability/coverage → version regression → qualification policy assessment.

Repeated runs per agent (up to 20), immutable suite versions, hidden variants, per-scenario/per-family/capability reliability, comparability manifests, and paired version regression all exist. Platform-invalid trials are excluded from valid denominators and tracked separately. Qualification requires policy facts such as valid trial count, pass rates, safety, invalid-platform rate, and optional no-regression; it does not mutate the underlying evaluation.

Gaps are runtime breadth and proof, not missing aggregation concepts. Hidden variants only affect the traffic-spike fixture. Task breadth catalogs exceed executable environment breadth. Scenario certification can gate authoritative qualification evidence, but its current live claims are based on simulated fixtures, so it should be labeled focused validation until Stage 41. Add explicit `invalid_task` alongside `invalid_platform` before broad live worlds; do not force bad grader/oracle/reference definitions into platform invalid or agent fail.

Subsystems that should absolutely not be rewritten: Agent/AgentVersion immutability, ScenarioVersion history, run/batch/suite identity, hidden variant assignment, evidence/action lifecycle, deterministic evaluator service boundary, trajectory projection, reliability valid-only math, regression pairing, qualification policy engine, coverage/task-family contracts, workspace authorization, billing reservation/settlement, and MCP hashed grant/revocation foundations.

## 13. Frontend information architecture audit

| Current route/concept | What it communicates | Decision | Recommended product concept |
|---|---|---|---|
| `/` | “Infrastructure agent qualification”; qualification is category and primary verb | Change now | Lead with simulation and evaluation infrastructure; qualification becomes one outcome. Keep evidence/safety/version proof content. |
| `/signup`, `/login`, `/onboarding` | account/workspace setup and connect-first journey | Keep | Onboard to “connect an agent and run an evaluation”; qualification can follow. |
| `/workspace` | qualification evidence, suite count, recent trial batches | Change now | Evaluation workspace: Agents, Scenarios, Evaluations/Runs, Evidence, then Qualification status. |
| `/agents`, `/agents/[id]`, `/agents/new` | exact versions, connections, claims, readiness | Keep/extend | Agent and AgentVersion remain core. Make “Run evaluation” primary; qualification readiness secondary. |
| `/qualification` | main workflow entry; claims→plan→suite→evaluation | Keep but demote | Policy/qualification area derived from evaluation evidence. |
| `/qualification/suites/[id]` | immutable grouped tasks/trials | Rename later | “Evaluation Suites”; retain qualification-policy association as metadata. Backend already calls them evaluation suites. |
| `/suite-executions/[id]` | grouped execution/results | Keep/rename later | Evaluation execution with reliability/coverage/qualification panels. |
| `/scenarios`, `/scenarios/[id]`, `/workspace/tasks` | public tasks and workspace Evaluation Tasks | Keep, normalize | “Scenarios” primary; “Evaluation Task” explanatory alias. Avoid duplicate Tasks/Scenarios nav concepts. |
| `/runs`, `/runs/[id]` | Activity; qualification executions and Trials | Change now | Label navigation “Runs” or “Evaluations”; one Run/Trial detail remains accurate. |
| `/runs/[id]/trajectory` | observations/actions/outcome evidence | Keep | Evidence/trajectory is already aligned. Add verifier snapshots later. |
| `/evaluations/[id]` | evaluation result | Keep/strengthen | Make Evaluation a first-class navigable concept tied to one Trial. |
| `/comparisons/[id]` | AgentVersion regression | Keep | Version comparison/regression remains downstream evaluation science. |
| `/environments` | static public profile catalog | Extend later | Worlds/Environments with fidelity and actual instance/plan status once live APIs exist. Do not fake availability. |
| `/workspace/billing`, `/workspace/settings` | account concerns | Keep | No category change required. |

The product navigation currently foregrounds `Qualification`, `Tasks`, and `Activity`; it omits Evaluations, Scenarios, Evidence, and Environments. Marketing repeats “qualification” as the product category. Required near-term conceptual changes are copy/nav hierarchy and surfacing evaluations/runs/evidence. Environment-instance UI, topology visualization, fidelity filters, reset/quarantine views, and WorldSpec authoring should wait for backend contracts.

## 14. KEEP / EXTEND / RENAME / MISSING / REMOVE matrix

| Subsystem / target concept | Current implementation | Decision | Gap / recommendation | Stage |
|---|---|---|---|---|
| Agent / AgentVersion | immutable models and connections | KEEP | Preserve exact-version binding and trigger | existing |
| Capability Claims | catalogs/claims/coverage | KEEP | Keep separate from results | existing |
| Scenario Definition | ScenarioVersion + criteria/views | EXTEND | Attach versioned typed WorldSpec and invariants | 39 |
| WorldSpec | fragmented environment/conditions/execution fields | MISSING | Add typed v1 plus legacy compiler | 39 |
| Component Catalog | static IDs/status | EXTEND | Version/config schema, adapter capabilities | 39 |
| System Topology | none | MISSING | Typed nodes/edges/endpoints | 39 |
| Composition Planner | profile set matching | EXTEND | Compile requirements/topology to pinned placements/lifecycle | 39 |
| Provider/Adapter Layer | one Provider per profile | EXTEND | Component/bundle adapters, no core provider branches | 39 |
| World/Environment Instance | one owner-scoped aggregate | KEEP | Treat as world aggregate; add component children/plan hash | 39 |
| Initializer | KubernetesRuntime.Prepare | EXTEND | Separate adapter-neutral initializer | 39 |
| Baseline Verification | readiness loops/synthetic gate | MISSING | Privileged verifier contract + snapshot | 39–40 |
| Workload Generator | scenariorunner traffic spike | EXTEND | WorldSpec-bound interface; component-scoped | 39 |
| Fault/Condition Injector | traffic spike Start | MISSING | Separate typed condition injector; workload may be an input, not always fault | 39 |
| Scenario-State Verification | success of Start only | MISSING | Verify injected condition before agent invocation | 39 |
| Agent Gateway | HTTP protocol v1 | KEEP | Add safe topology/tool context version | 39 |
| MCP Tool Surface | run-scoped stateless tools | EXTEND | Component-addressed registry and stronger audience/world binding | 39–40 |
| Trial-Scoped Tool Session | MCP grant only | EXTEND | Generalize to direct/tool sessions; bind plan/world and component scopes | 39–40 |
| Action Executor | Kubernetes actions | EXTEND | Adapter registry; retain authorize→recheck→execute evidence | 39 |
| Telemetry Collector | Kubernetes/Prometheus observer | EXTEND | Collector interface, source pins, snapshot/window boundaries | 39–40 |
| Final-State Snapshot | final observation event | EXTEND | Immutable privileged snapshot separate from projected observation | 39–40 |
| Independent Verifier | evaluator in worker trust domain | MISSING | Separate credentials/process/network capability where practical | 40 |
| Evaluator | deterministic metrics/safety | KEEP | Add typed invariant inputs and task-invalid outcome | 40 |
| Evidence Store | events/actions/evaluations | KEEP | Integrity hashes/attestation and snapshot lineage | 40 |
| Trajectory Projection | deterministic safe view | KEEP | Add world lifecycle and snapshot refs | 40 |
| Cleanup | deferred destroy/recovery | KEEP | Add component rollback ledger and sweep | 39 |
| Reset | provider method/no-op Kind | EXTEND | Real reset strategy only where reuse is allowed | 39 |
| Reset Verification | synthetic certification gate | MISSING | Compare clean snapshot/digest; invalidate/quarantine on failure | 39–40 |
| Environment Quarantine | failed status only | MISSING | Explicit state/reason and scheduler exclusion | 39 |
| Fidelity Contract | isolation/provider pins | MISSING | Versioned evidence-fidelity tier/dimensions | 39 |
| Repeated Trials | suites/runs_per_agent | KEEP | Feed independently clean worlds | existing/41 |
| Hidden Variants | immutable IDs/seeds/config | KEEP | Extend typed schemas by scenario runtime | 39 |
| Reliability | valid-only aggregation | KEEP | Include fidelity/task-invalid comparability | 40 |
| Regression | paired AgentVersions | KEEP | Preserve exact pins | existing/41 |
| Coverage | families/case types | KEEP | Do not equate catalog breadth with live runtime breadth | existing |
| Scenario Certification | deterministic fixture gates | EXTEND | Add live certification mode; clearly label fixture evidence | 40–41 |
| Qualification | policy assessment | KEEP | Add minimum fidelity/authoritative verifier requirements | 40 |
| Qualification Planner | deterministic eligible task selection | KEEP | Require live-proof level appropriate to policy | 40 |
| Billing execution gate | reserve/settle | KEEP | Remain outside world semantics | existing |
| “Qualification” as overall product category | frontend copy/nav | RENAME | Simulation & evaluation; retain Qualification domain | product copy |
| Legacy direct runtime fallback | worker provisions without environment service when absent | REMOVE | Remove only after composed engine is mandatory and migration-safe | later, not Stage 39 initial |

`REMOVE` is intentionally rare. The fallback is the only removal candidate and should remain during transition.

## 15. Stage 39 proposal — Simulation Environment Platform

### Goal

Given an exact ScenarioVersion, compile and persist an immutable CompositionPlan, construct one trial-owned composed world, initialize and prove its baseline, start workload, inject and verify the scenario condition, expose only trial/world-scoped operations, capture final state, and destroy—or reset and prove clean—the world.

### Include

1. **WorldSpec v1**: typed component nodes, edges, artifact/version/config references, initialization, workload, condition, observations/tools, baseline/scenario/final/reset assertions, minimum fidelity, cleanup strategy, and contract versions. Reference remediation remains separate evaluator-only data.
2. **CompositionPlan v1**: immutable compiler output with component placements, dependency order, endpoints, safe agent projection, provider/adapters, isolation, lifecycle steps, fidelity, secret-reference IDs, plan hash, and rollback order.
3. **World ownership**: preserve `environment_instances` as the aggregate bound to workspace+run/certification; add child component instances and plan identity. Exactly one active world aggregate per Trial.
4. **Adapter evolution**: component or cohesive-bundle adapters with provision/get/destroy plus explicit initialize/readiness, workload, inject, observe, snapshot, and reset capabilities. Do not force every adapter to implement every capability.
5. **Lifecycle coordinator**: explicit stages and durable events: plan, provision, ready, initialize, baseline verify, workload start, inject, scenario verify, tool session, agent, quiesce, final snapshot, cleanup/reset verify.
6. **Baseline/scenario/final snapshots**: typed, immutable verifier inputs with safe agent projections separated.
7. **Reset verifier and quarantine**: only reusable profiles use reset; failure marks quarantined and scheduler-ineligible. Destroy remains default for the first world.
8. **Fidelity v1**: selected/required tier and pinned adapter/component provenance.
9. **Trial-scoped tool session**: extend the current MCP grant concepts to bind world/plan ID and explicit tool/component scopes. Direct HTTP proposal mode remains supported.

### Do not include yet

- Multiple cloud providers, Terraform, AWS/Azure/GCP accounts, production pooling, arbitrary shell execution, customer-authored adapter code, a generic workflow language, LLM grading, frontend topology design, or qualification-policy redesign.
- A physically separate verifier service belongs primarily to Stage 40; Stage 39 needs contracts and separate credentials/interfaces so it can be isolated later.

### Likely code and schema scope

- Extend `internal/scenario`, `environment`, `executionworker`, `runtimecontract`, `observation`, `telemetry`, `actionexec`, `mcpserver`, `evidence`, `trajectory`, and HTTP/API DTOs.
- Adapt `kubeenv` and `scenariorunner`; preserve them as Kubernetes implementations.
- Likely new packages: `worldspec` (schema/compiler), `composition` (plan/lifecycle), `worldverify` (assertion/snapshot contracts), and optionally `toolregistry` (component-scoped operations). Keep names domain-focused; do not rename existing packages merely for terminology.
- New migration(s): immutable WorldSpecs or ScenarioVersion binding; composition plans; environment component instances; lifecycle attempts/snapshots; fidelity pins; quarantine status/reason; tool grants bound to environment/plan. Add nullable foreign keys/default legacy semantics to historical rows.

### Compatibility contracts

- No mutation of historical ScenarioVersions, runs, evaluations, certifications, variants, or AgentVersions.
- A deterministic legacy compiler maps existing Kubernetes traffic-spike versions to WorldSpec v1.
- HTTP gateway protocol v1 and current MCP tool names remain accepted; add versioned optional context/tools.
- Existing evaluator, evidence, qualification, coverage, reliability, billing, auth, and run state contracts remain stable. Platform invalid must remain non-agent evidence.
- Provider refs remain opaque and secret-free; no fidelity downgrade.

### Focused tests

- WorldSpec strict validation/versioning and legacy compilation.
- Stable plan hash and deterministic topological ordering; cycles/missing capabilities rejected.
- Multi-component placement, endpoint binding, least-fidelity adequate selection, entitlement no-downgrade.
- Partial-provision rollback and idempotent destroy.
- Baseline and scenario verification block agent invocation on failure.
- Tool token cannot cross workspace/run/world/component or outlive run.
- Snapshot projection excludes hidden/reference/secret fields.
- Reset mismatch quarantines and creates platform-invalid evidence.
- Worker recovery resumes cleanup only and never reinvokes agent.

### Real-runtime proof and completion

Stage 39 completes only when the first recommended local composed world provisions through the plan, passes baseline and injected-state verification, permits controlled operations, captures final state, destroys cleanly, and produces pinned evidence in an actual cluster runtime. Unit/fake tests alone are insufficient. Reuse/reset can remain disabled initially if destroy-and-recreate is proven; do not claim reset reuse until verified.

## 16. Stage 40 proposal — Evaluation Integrity & Security

Stage 40 should harden the contracts introduced in Stage 39 and close actual trust gaps:

- Define explicit trial validity: `valid`, `invalid_platform`, `invalid_task`, `pending`; retain agent result separately from validity.
- Run privileged baseline/scenario/final/reset verifiers with credentials unavailable to the agent tool plane. Prefer separate Kubernetes service accounts and, where feasible, a separate process/network policy; document residual shared DB trust.
- Bind short-lived tool grants to workspace, run, AgentVersion, EnvironmentInstance, CompositionPlan hash, tool contract, component scopes, audience, expiry, and call budget. Continue storing only hashes.
- Establish evidence lineage: snapshot content hashes, ordered event-chain hashes or signed manifests, source/collector identity, contract versions, and append-only permissions. This is integrity metadata, not blockchain.
- Separate provider credentials per adapter/world, deliver via references, never persist in provider refs/evidence, and deny agent egress to provider control planes unless through tools.
- Treat tool/telemetry/event strings as adversarial data; maintain bounded structured projections and prevent instructions/secrets from crossing into agent-visible context.
- Add forbidden-operation/invariant verification independent of whether an action was denied at the gateway.
- Make final observation/snapshot requirements explicit; telemetry unavailability, verifier failure, cleanup failure, and reset mismatch invalidate platform evidence.
- Add quarantine/sweeper auditing and cross-workspace tests, including crash during every lifecycle phase.
- Add fidelity-aware qualification policy and comparability without retroactively assigning legacy fidelity.
- Upgrade scenario certification to distinguish synthetic contract checks from live runtime certification.

Do not replace the deterministic evaluator or add an LLM judge for objective infrastructure state. Do not expose reference remediation to the live verifier’s agent-facing process. Completion requires security tests plus at least one runtime demonstration of denied cross-run access, expired/revoked credentials, verifier-only truth, tamper detection, and contamination invalidation.

## 17. Stage 41 proposal — Full E2E Simulation & Evaluation Proof

Stage 41 is proof, not another abstraction stage. It should run two exact AgentVersions through an immutable suite against the first composed world:

1. Provision pinned components from WorldSpec/CompositionPlan.
2. Verify readiness and known baseline with privileged truth.
3. Start realistic workload and inject one operational failure.
4. Verify the failure exists before invoking the agent.
5. Exercise both direct HTTP proposals and MCP tool access (separate trials are acceptable) with short-lived run/world binding.
6. Record observations, requested/authorized/executed actions, final snapshot, deterministic invariants, safety, and validity.
7. Destroy and verify absence; optionally reset/reuse only if Stage 39 implemented proven reset.
8. Rerun with at least one hidden variant and independently clean starting world.
9. Produce reliability with platform-invalid denominators, compare the second AgentVersion, and derive a qualification assessment under an explicit fidelity policy.
10. Demonstrate one agent failure, one invalid-task fixture, and one platform failure without conflating them.

Focused verification means unit/integration tests with fakes, fake clients, or one package boundary. Actual E2E proof means real processes, database, cluster, composed components, network calls, telemetry, cleanup, and rerun observed by the harness. Stage 41 is complete only with captured command/runtime evidence, artifact/component pins, cleanup verification, and repeatable rerun. Passing `go test` or compiling the frontend is not E2E proof.

After Stages 39–41, the truthful claim would be: “Evalplane has an E2E-proven local simulation and deterministic evaluation platform for one composed infrastructure world, with trial-scoped controlled tools, independent verifier credentials, repeated hidden variants, reliability, regression, and policy-derived qualification.” It would not yet be product-ready across clouds or all cataloged task families.

## 18. Recommended first real composed world

| Option | Local cost / reuse | Composition value | Objective grading / reset | Risk |
|---|---|---|---|---|
| A. Kubernetes + Prometheus | Highest current reuse: already live code | Two systems, but telemetry is tightly embedded; modest topology proof | Strong metrics/state; namespace recreate is reproducible | Too close to current fixture to prove general composition planner |
| B. Git + Argo CD + Kubernetes + Prometheus | Can be entirely local with a local Git server/repository and Kind; reuses Kubernetes/Prometheus | Excellent multi-system desired-state→controller→runtime→telemetry chain | Git revision, Argo sync/health, K8s state and metrics are deterministic; namespace/repo recreation is feasible | More moving parts; Argo adapter/tooling is absent today |
| C. Git repository + CI | Lightweight and cheap | Good two-system workflow and artifact/log evidence | Objective commit/workflow checks; reset by disposable repo/runner | Least reuse of current runtime; CI sandbox/security and provider emulation become a new axis |
| D. Kubernetes + Prometheus + controllable dependency service | Local and high reuse | Adds a real service dependency while avoiding Git/Argo | Strong health/metrics/state; namespace destroy | Less representative of control-plane composition than GitOps |

Recommendation: **B, Git + Argo CD + Kubernetes + Prometheus, entirely local**, but scope the first scenario narrowly: a pinned local bare Git/Gitea-like repository contains desired Deployment state; Argo CD syncs to a run-owned namespace; Prometheus observes the workload; injection introduces live/desired-state drift or a bad desired revision; the agent can read bounded Git/Argo/Kubernetes/metrics state and make one controlled remediation; the verifier checks Git revision, Argo sync/health, Kubernetes rollout/availability, telemetry, and forbidden protected resources.

This is the smallest world that genuinely exercises topology, multiple control planes, eventual reconciliation, objective state grading, and current Kubernetes reuse. If Stage 39 schedule risk requires a stepping stone, A is appropriate for validating new lifecycle interfaces, but it should not be accepted as the sole Stage 41 composition proof.

## 19. External account/provider requirements

| Provider/world | Classification | Local proof and where real evidence becomes necessary |
|---|---|---|
| Kind/Kubernetes + Prometheus | LOCAL | Sufficient for local runtime contracts, namespace isolation, telemetry, action, cleanup mechanics; not managed-cloud or production isolation evidence. |
| Local Git + Argo CD + Kubernetes | LOCAL | Sufficient for first composed-world proof using disposable local repositories and controllers. |
| GitHub API/workflows/PRs | EMULATED initially; REAL PROVIDER REQUIRED for high fidelity | Local Git/CI can validate contracts; GitHub-specific permissions, rate limits, webhooks, Actions, branch protection and org isolation require a dedicated disposable org/repositories. |
| AWS resource operations | EMULATED initially; REAL PROVIDER REQUIRED for high fidelity | LocalStack/fakes can validate adapters and safety; IAM, CloudWatch, EC2/RDS semantics, quotas and eventual consistency require a dedicated evaluation account with SCP/budget/teardown controls. |
| Azure | EMULATED initially; REAL PROVIDER REQUIRED for high fidelity | Emulator/contracts first; managed identity, ARM semantics and service behavior require a dedicated subscription/tenant boundary. |
| GCP | EMULATED initially; REAL PROVIDER REQUIRED for high fidelity | Emulator/contracts first; IAM, Operations Suite and managed APIs require a dedicated project/org policy. |
| Managed EKS/GKE/AKS, vCluster | LOCAL/replica for software behavior; REAL PROVIDER REQUIRED for provider qualification | Kind proves workload path only. Managed control-plane/node/IAM/CNI/autoscaling claims require isolated real-provider environments. |
| Generic Kubernetes software stack | LOCAL or isolated replica | Real distributions in disposable clusters can reach replica fidelity without public cloud accounts. |

No account should be created merely to complete Stage 39. Qualification must state and enforce the fidelity at which evidence was produced.

## 20. Architecture invariants

1. AgentVersion and ScenarioVersion identities remain immutable; changed behavior/specification creates a new version.
2. A Trial binds exactly one AgentVersion, ScenarioVersion, hidden-variant assignment, CompositionPlan, world aggregate, evaluator contract, and fidelity record.
3. Evaluator truth, reference remediation, hidden values, grader secrets, and privileged snapshots never enter AgentView, gateway payloads, MCP outputs, trajectory safe metadata, or logs.
4. Workspace/run/world ownership is checked at storage, token resolution, observation, authorization, mutation, snapshot, cleanup, and read APIs.
5. Raw cloud/kube/provider credentials never appear in provider refs, API DTOs, evidence, events, logs, or AgentVersion metadata.
6. Required fidelity/isolation is never downgraded for availability, price, or entitlement.
7. Platform failure is never an agent FAIL; task/oracle invalidity is never an agent FAIL or silently platform failure.
8. Reference remediation proves solvability and never defines the only passing trajectory.
9. Deterministic state invariants supersede action-sequence matching where state can be verified.
10. Reset failure or unverified cleanup invalidates platform evidence and quarantines reusable infrastructure.
11. Destroyed or quarantined environments are never silently reused; repeated trials start from independently proven clean state.
12. Qualification derives only from authoritative, valid, appropriately faithful evaluation evidence; customers cannot self-mark certification or qualification.
13. Historical runs/results remain interpretable: WorldSpec, plan, provider/adapter, component artifact, tool, evaluator, trajectory, fidelity and policy contracts are pinned.
14. Hidden variants remain immutable and absent from agent projections while their non-secret identity/seed can support reproducibility controls.
15. Requested, authorized, executed, failed and denied action states remain distinct and append-evidenced.
16. Worker lease loss stops writes/actions; recovery never invokes an agent or replays mutations.
17. A provider opaque reference is never treated as fidelity identity or authorization.
18. Objective infrastructure grading remains deterministic; no LLM judge is authoritative where state verification is possible.
19. Catalog availability is truthful: planned/experimental entries are never scheduled or shown as live.
20. Cleanup failures occurring after agent completion still invalidate aggregate agent evidence.

## 21. Migration/backward-compatibility risks

- Historical ScenarioVersions contain overlapping raw `environment`, `conditions`, `execution_spec`, criteria and separate environment-requirement rows. Guessing a WorldSpec backfill would change meaning. Use a pinned legacy compiler and preserve original bytes.
- `environment_instances` currently means one provider/profile handle. Adding child components is safer than converting existing rows into per-component records. Legacy rows have no plan/fidelity and must remain valid as `legacy/unspecified`.
- Runs store one `environment_ref`; keep it as a safe world aggregate ref for compatibility and add plan/world IDs. Never put a list or credential JSON into it.
- Reliability comparability already uses profile/provider/isolation/runtime pins. New fidelity/plan pins should make legacy evidence non-comparable or explicitly limited, not silently equivalent.
- Certification rows currently pin environment contracts but certification execution is fixture-based. Adding `proof_mode`/fidelity must not relabel old rows as live proof; stale/legacy status may affect qualification eligibility.
- Qualification assessments and plans are immutable evidence. New policy rules apply to new versions/assessments; do not recalculate old verdicts in place.
- MCP protocol/tool contract additions must be additive/versioned; existing AgentVersions pin `tool_access_mode` and cannot be edited.
- New world lifecycle states must not invalidate existing DB checks without a safe state mapping. Quarantine should be explicit and scheduler-excluded.
- A component failure during partial provisioning needs durable rollback ownership before adapters can span external systems; otherwise orphan risk grows.
- The existing environment requirements row is workspace-owned. Public/built-in scenarios and future shared WorldSpecs need carefully scoped ownership/read semantics.

## 22. Product terminology changes

Change now in customer-facing hierarchy:

- Category: “Simulation and evaluation infrastructure for agents that operate infrastructure.”
- Use “Evaluation” and “Run/Trial” as primary workflow nouns; “Evidence” as the inspectable output.
- Keep “Scenario” as the canonical authored object and use “Evaluation Task” as explanatory copy, not a second competing model.
- Present Qualification as a policy conclusion derived from evaluation evidence.

Change later with backend support:

- “Environment” may gain “World” as explanatory language (“composed environment/world”), but do not rename packages/tables/routes solely for fashion.
- Rename customer-facing Qualification Suites to Evaluation Suites once navigation and API compatibility are coordinated.
- Add fidelity labels and composed topology only when backed by runtime pins.

Do not change:

- Agent, AgentVersion, ScenarioVersion, Trial/Run, Evidence, Evaluation, Reliability, Regression, Coverage, QualificationPolicy, Scenario Certification, or existing package names. Their distinctions are sound.

## 23. Critical blockers

1. No graph/topology or persisted CompositionPlan; one profile/provider/instance cannot materialize a heterogeneous world.
2. No generic initializer, workload, condition injector, scenario-existence verifier, final snapshot, or reset verifier contracts; KubernetesRuntime hard-codes the lifecycle.
3. No live proof behind scenario certification reset/solvability/leakage gates; they are deterministic fixtures.
4. Verifier truth is logically projected but not isolated by process/network/credential boundary.
5. Only one executable scenario runtime and Kubernetes-specific tool executor exist; catalog breadth is not runtime breadth.
6. No evidence fidelity contract or explicit live task-invalid validity class.
7. Cleanup/destroy lacks verified absence, quarantine, and a functional untracked-provider sweep.

These are blockers to a general trustworthy simulation platform, not reasons to rebuild the existing control plane.

## 24. Recommended implementation order

1. Define WorldSpec/CompositionPlan/fidelity and legacy compilation; persist plan/world/component ownership without changing existing evaluator or agent protocols.
2. Extract current Kubernetes+Prometheus lifecycle into initializer/workload/injector/observer/snapshot/reset-verifier interfaces and prove lifecycle ordering on the existing fixture.
3. Implement the local Git+Argo CD+Kubernetes+Prometheus adapters and component-scoped tool/session routing; default to destroy-and-recreate.
4. Add privileged verifier credentials, explicit task/platform validity, evidence lineage, quarantine, and fidelity-aware qualification.
5. Run and document Stage 41 E2E repeated/hidden-variant/regression proof; only then revise product maturity claims and expand providers.

## 25. Verification actually performed

Read-only inspection covered `backend/cmd`, all relevant `backend/internal` domains, migrations `000001`–`000020`, `backend/e2e`, scripts, existing reports, frontend routes, navigation and `lib/api.ts`. Particular traces covered worker orchestration/recovery, managed environment lifecycle, provider/scheduler/catalog, HTTP gateway, MCP grants/tools, Kubernetes action authorization, evaluator normalization/safety, evidence/trajectory, suite validity, reliability/regression, certification/reference boundaries, qualification/planning, HTTP routes, and frontend product hierarchy.

Commands:

```text
git status --short
git branch --show-current
git log --oneline -25
git remote -v
git branch -vv
git rev-list --left-right --count origin/main...HEAD
rg --files backend/cmd backend/internal backend/migrations backend/e2e scripts prompts/reports lib app | sort
rg and sed read-only searches/reads across the paths named above
GOMAXPROCS=2 GOCACHE=/tmp/evalplane-go-cache go test -p 1 ./internal/environment/... ./internal/executionworker/... ./internal/agentgateway/... ./internal/mcpserver/... ./internal/actionexec/... ./internal/evaluator/... ./internal/evaluationsuite/... ./internal/reliability/... ./internal/scenariocertification/... ./internal/trajectory/... ./internal/qualification/... ./internal/qualplan/...
```

Result: all 12 selected package groups passed (`ok`; cached results were accepted because source state was unchanged). No Docker, Kubernetes, database, migration, cloud, frontend build, push, or secret-bearing command was run. Existing cluster E2E was intentionally not run. A final `git status --short` was run after creating this report.

## 26. Final maturity assessment

| Area | Rating | Basis |
|---|---|---|
| Scenario model | IMPLEMENTED | Immutable versions, agent/evaluator views, criteria, task families and requirements exist; WorldSpec semantics are incomplete. |
| Environment engine | FOCUSED-VERIFIED | Scheduler/provider/ownership/lifecycle and Kind adapter have focused tests; only a narrow runtime is live and no composed graph exists. |
| Agent interface | FOCUSED-VERIFIED | HTTP and short-lived MCP tool access are implemented with ownership/authorization tests; strong network isolation is unproven. |
| Evaluator | FOCUSED-VERIFIED | Deterministic metric/safety evaluation and platform inconclusive behavior are tested; arbitrary state invariants and isolated verifier are missing. |
| Reset/reproducibility | DESIGNED | Destroy/recovery and repeat metadata exist, but clean-state equivalence/reset reuse is not live-proven. |
| Simulation platform | DESIGNED | The architecture has useful primitives and one Kubernetes fixture, but no general WorldSpec/composition engine. |
| Frontend customer flow | IMPLEMENTED | Connected journey/routes/API usage exist; information architecture over-emphasizes Qualification. |
| Total core product | IMPLEMENTED | Substantial control plane exists and is test-backed, but the new category’s defining composed-world runtime is not yet E2E-proven. |

No area is rated `PRODUCT-READY`. No runtime area is rated `E2E-PROVEN` because this audit did not run—and HEAD does not document sufficient proof of—the required real composed, repeated, cleanly reset/destroyed Stage 41 flow.
