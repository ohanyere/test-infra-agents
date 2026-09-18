# Stage 39A — World Spec & Composition Foundation

## Scope and repository state

- Branch: `stage39-simulation-environment-platform`
- HEAD before work: `cbc6e7a` (`docs(architecture): audit simulation evaluation platform`)
- Worktree before work: clean
- Architectural source: `prompts/reports/simulation-evaluation-architecture-audit.md`
- Migration: `000021_world_composition_foundation`

This stage adds control-plane contracts and persistence only. It does not route
the existing worker through composition planning and does not provision a live
multi-component world.

## Files changed

Created:

- `backend/internal/worldspec/worldspec.go`
- `backend/internal/worldspec/worldspec_test.go`
- `backend/internal/composition/plan.go`
- `backend/internal/composition/plan_test.go`
- `backend/internal/composition/repository.go`
- `backend/internal/environment/components.go`
- `backend/internal/environment/components_test.go`
- `backend/migrations/000021_world_composition_foundation.up.sql`
- `backend/migrations/000021_world_composition_foundation.down.sql`
- `prompts/reports/stage39a-world-composition-foundation.md`

Changed:

- `backend/README.md`
- `backend/internal/environment/README.md`
- `backend/internal/environment/provider.go`
- `backend/internal/environment/service.go`
- `backend/internal/httpapi/environments.go`
- `backend/internal/httpapi/environments_test.go`
- `backend/internal/httpapi/router.go`
- `backend/internal/migrationtest/migrations_test.go`

## Persistence decision and schema

WorldSpec is persisted independently with an immutable one-to-one binding to
the exact ScenarioVersion. This was chosen instead of adding fields to
`scenario_versions`: existing version bytes and immutable history remain
untouched, while a legacy spec can be materialized once and subsequently loaded
by stable identity/hash. Built-in ScenarioVersions may have a null workspace;
custom specs retain workspace ownership.

`world_specs` stores contract/compiler pins, the normalized typed document and
its SHA-256 identity. `composition_plans` stores the exact ScenarioVersion and
WorldSpec identities, normalized immutable compiler output, plan hash,
profile/isolation decision and minimum/selected fidelity. Update triggers make
both rows immutable.

`environment_instances` remains the aggregate trial/certification-owned world.
It gains nullable plan and fidelity pins. `environment_component_instances`
stores its planned/provisioned children with workspace ownership, component and
adapter/provider pins, stable dependency order, lifecycle timestamps, safe
opaque reference and reproducibility metadata. An old aggregate with no plan
and zero children remains valid.

Runs gain nullable CompositionPlan and EnvironmentInstance links. MCP grants
gain nullable environment/plan/hash, component-scope and tool-contract pins.
All additions are backward compatible; the current grant creation and worker
path remain unchanged. The environment binding trigger permits the initial null
to pinned transition but prevents replacing a pinned plan or fidelity.

## WorldSpec v1

`world-spec:v1` is a strongly typed structure containing:

- a pinned compiler ID;
- typed component nodes with IDs, types, versions/artifacts and required
  capabilities;
- directed typed relationship edges;
- typed initialization, workload, condition, observation and tool bindings;
- baseline, scenario, final and reset assertion references;
- minimum fidelity and minimum isolation as separate concepts; and
- a bounded cleanup strategy (`destroy` or `reset`).

It intentionally contains no reference remediation, ground truth, hidden
variant values, credential material, tokens, kubeconfigs or grader secrets.
Assertions are verifier contracts, not evaluator remediation content.

Validation rejects unknown contracts/fidelity/cleanup, blank or duplicate
nodes, missing edge endpoints, self/invalid edges, cycles, unknown lifecycle or
assertion component references and malformed artifacts. Topological sorting is
lexically stable when multiple nodes are ready.

## Fidelity v1

The ordered evidence-fidelity vocabulary is:

1. `legacy_unspecified`
2. `synthetic_fixture`
3. `local_emulated`
4. `isolated_replica`
5. `isolated_real_provider`

This is independent from namespace/virtual-cluster/dedicated-cluster isolation.
Adapters declare maximum truthful fidelity; the planner rejects a component if
no adapter meets its minimum. Selected plan fidelity is the weakest selected
adapter, so a bundle can never claim more than all of its components provide.
Historical results are not backfilled.

## Legacy compatibility compiler

`legacy-kubernetes-traffic-spike:v1` deterministically maps an existing
ScenarioVersion plus validated Environment Requirements into WorldSpec v1. It
accepts only the existing `traffic_spike` execution/runtime contract and fails
closed for unsupported runtimes. It creates legacy component pins, the
Kubernetes-to-Prometheus observation relationship where present, runtime
prepare/readiness, workload and condition references, plus sorted observation
and action bindings. It explicitly sets fidelity to `legacy_unspecified` and
cleanup to destroy. It never rewrites the ScenarioVersion or guesses historical
fidelity.

## CompositionPlan v1 and adapter model

`composition-plan:v1` pins the ScenarioVersion, WorldSpec hash, every component
version, component-to-adapter/provider placement, deterministic dependency
order, edges, profile/isolation, required capabilities, lifecycle ordering,
initialization/workload/condition/observation/tool/verifier bindings, cleanup
strategy and minimum/selected fidelity. Its agent projection contains only
safe placements/topology/observations/tools and excludes verifier assertions
and lifecycle internals.

Planning is contract/capability driven, not provider-name branching. Adapter
catalog entries declare supported component types, capabilities, maximum
fidelity, provider/profile and isolation truth. `LegacyAdapterCatalog` projects
the current Kind Kubernetes and Prometheus availability without making any new
provider live. `environment.ProviderCapabilities` now optionally exposes
component adapter capabilities; existing providers remain valid with none.

The planner rejects duplicate/unknown/cyclic graph data through WorldSpec
validation, rejects malformed adapter truth, missing component capabilities,
impossible fidelity and unsatisfied world capabilities. It never drops a
component. Heterogeneous placements are represented; no heterogeneous provider
is provisioned in this stage.

## Environment Requirements relationship

Environment Requirements are retained. For historical ScenarioVersions they
are the authoritative legacy compiler input and remain the current scheduler
index. Once a WorldSpec is persisted for a new ScenarioVersion, WorldSpec is
the richer topology/lifecycle authority; requirements are its compatibility and
selection projection, not an independent competing world definition. The
existing scheduler and runtime continue consuming requirements exactly as
before.

## Hashing and safe projections

WorldSpec normalization deep-copies input, sorts components, edges, capability
sets and binding/assertion collections, then hashes canonical Go JSON bytes with
SHA-256 (`sha256:<lowercase hex>`). Timestamps, persistence IDs and random values
are absent. CompositionPlan hash clears its own `plan_hash` field and hashes the
deterministically compiled normalized plan. ScenarioVersion ID is semantic plan
input; database plan ID and timestamps are not.

Tests cover stable semantic hashes and hash changes for component version and
tool contract changes. Agent projections exclude verifier-only assertions.
Safe-reference validation rejects URL userinfo and secret-like token, password,
authorization, kubeconfig and private-key material. Raw provider credentials
have no contract or schema field.

## API

Added:

- `GET /v1/workspaces/{workspaceID}/environments/{environmentID}/components`

The endpoint enforces existing workspace membership and aggregate ownership and
returns an empty list for valid legacy environments. No client-facing WorldSpec
or plan mutation endpoint was added: compilation/persistence is server-owned,
and Stage 39A does not grant clients authority to assert provider/fidelity
truth. Repository contracts support internal persist/load now; worker binding
is deferred.

## Verification performed

Passed:

```text
GOMAXPROCS=2 GOCACHE=/tmp/evalplane-go-cache TEST_DATABASE_URL= go test -p 1 \
  ./internal/worldspec ./internal/composition ./internal/environment \
  ./internal/httpapi ./internal/migrationtest ./internal/executionworker \
  ./internal/mcpserver ./internal/evaluator ./internal/qualplan

GOMAXPROCS=2 GOCACHE=/tmp/evalplane-go-cache go vet \
  ./internal/worldspec ./internal/composition ./internal/environment ./internal/httpapi
```

The migration test package passed its no-database path but skipped actual
up/down schema execution because `TEST_DATABASE_URL` was unset. No Docker,
cluster or cloud resource was started. Frontend types did not change, so
TypeScript validation was not applicable.

## Compatibility and limitations

- Existing Kubernetes traffic-spike execution is structurally unchanged.
- AgentVersion, ScenarioVersion, evidence, evaluator, reliability, regression,
  qualification and billing semantics are unchanged.
- Historical environment/run/grant rows remain valid with null pins and zero
  components.
- No historical fidelity or plans are fabricated.
- No public client can choose provider, adapter, fidelity or plan contents.
- Migration SQL still requires execution against a scratch PostgreSQL database
  before it can be called database-verified.
- Composition persistence and deterministic planning are implemented and
  focused-verified, but normal worker execution does not yet create/use these
  records.

## Deferred to Stage 39B

- bind normal run creation/claim to server-owned WorldSpec materialization and
  CompositionPlan compilation;
- transactionally bind Run → plan → aggregate environment → child rows;
- execute component/bundle provisioning and rollback in dependency order;
- implement readiness, baseline, condition, final and cleanup verification;
- extend tool sessions to enforce the newly persisted plan/component scopes;
- component lifecycle/recovery events and partial-provision rollback;
- live heterogeneous adapters/providers and a composed runtime demonstration;
- reset verification, quarantine and pooling/reuse.

## Maturity

- World contracts: **FOCUSED-VERIFIED**
- Composition planning: **FOCUSED-VERIFIED**
- Persistence schema: **IMPLEMENTED, database verification pending**
- General simulation runtime: **DESIGNED**
