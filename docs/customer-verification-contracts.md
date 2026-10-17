# Customer verification contracts (current API)

This document describes the customer-facing contracts that are implemented
today. It does not promise production infrastructure support. Backend APIs are
versioned under `/v1`; workspace routes require an authenticated session and
workspace authorization. Verification results and evidence are persisted and
read-only after creation.

## Existing contract mapping

| Product concept | Existing contract / source of truth | Current boundary |
| --- | --- | --- |
| Organization and user | `organizations`, `users`, `workspaces`, memberships; `/v1/auth/*`, `/v1/organizations`, `/v1/workspaces/{id}/members` | Signup creates an initial organization/workspace. Session cookie auth; roles are owner/admin/member/viewer. |
| Agent and version | `Agent`, immutable `AgentVersion`, `agent-implementation-identity:v1`; `/v1/workspaces/{id}/agents` and version routes | HTTP Agent API and MCP tool access. Bearer credentials are encrypted. Implementation identity is declared, not attested. |
| Infrastructure environment | `EnvironmentProfile`, provider catalog, run-owned `EnvironmentInstance`; `/v1/environment-profiles`, `/v1/environment-providers`, workspace environment routes | Active Kind namespace provider is development-only. No customer cluster registration or production provider. |
| Verification configuration | `ScenarioVersion` + criteria/evidence requirements, environment requirements, immutable `QualificationSuiteVersion` | Plans and suites bind exact agent versions to server-supported cases. There is no general-purpose customer policy editor for arbitrary assertions. |
| Verification policy | `evidence-requirements:v1`, verifier/evaluator contracts, action authorization and fixed evidence rules | Mandatory evidence and safety rules are server-owned; missing required evidence cannot be configured away. |
| Verification run | `EvaluationBatch`, `Run`, `SuiteExecution`; evaluation-batch and suite-execution APIs | Controlled execution is implemented. A separate production observe-only run path is not available. |
| Action and policy decision | Persisted action proposals/transitions and `action-attribution:v1` | Requested, authorized, denied, executed, and failed states are distinct. The Kubernetes executor fails closed and no-op does not mutate. |
| Evidence and provenance | Immutable `VerificationResult`, snapshots, evidence assessment, run events, trajectory, provenance | Workspace-scoped verification, action, snapshot, forward-risk, event, and trajectory reads. Availability and source references are part of the result. |
| Verification conclusion | `VERIFIED`, `FAILED`, `WARNING`, `INCONCLUSIVE` in `VerificationResult` | Deterministic conclusion; process completion is not itself verification success. Invalid or insufficient evidence remains distinct. |
| Forward-risk finding | Analyzer-backed `ForwardRiskFinding` in persisted verification | Reports include risk type, severity, affected resources, supporting evidence, trigger, and evidence strength where available. |
| Reversal | No persisted reversal plan or customer-authorized reversal API | The report may guide operator review. realverax does not execute automatic rollback. |
| Integration and notification | HTTP Agent API, MCP; `/v1/billing/webhooks/{provider}` | Billing webhook is inbound only. No customer verification webhook, CI token, dedicated CI trigger, or shipped SDK. |

## Stable execution and result states

1. Register an agent identity, then an immutable version and scoped
   connection. A connection test checks reachability/protocol; it is not a
   qualification.
2. Build a qualification plan from declared capabilities and server-owned
   coverage/environment requirements. A built suite version is immutable.
3. Start a suite execution or evaluation batch. Runs progress through the
   execution worker's persisted lifecycle; terminal execution is followed by
   evaluation, verification, and cleanup.
4. Action transitions preserve proposal, authorization decision, and execution
   result separately. A denied action is not executed evidence.
5. Verification captures baseline/final state and required evidence. The
   persisted result is immutable and includes its contract versions and
   provenance. A missing required channel stays unavailable and can make the
   result `INCONCLUSIVE`.
6. Read `/v1/workspaces/{workspaceID}/runs/{runID}/verification` for the
   conclusion; `/verification/actions`, `/verification/snapshots`, and
   `/verification/forward-risks` for focused projections; `/trajectory` and
   `/events` for the ordered timeline. Access is scoped to workspace membership.

## API path for a controlled suite

- `GET /v1/workspaces/{workspaceID}/agents`
- `POST /v1/workspaces/{workspaceID}/agents`
- `POST /v1/workspaces/{workspaceID}/agents/{agentID}/versions`
- `POST /v1/workspaces/{workspaceID}/agents/{agentID}/versions/{versionID}/qualification-plans`
- `POST /v1/workspaces/{workspaceID}/qualification-plans/{planID}/suite`
- `POST /v1/workspaces/{workspaceID}/suite-executions`
- `GET /v1/workspaces/{workspaceID}/suite-executions/{executionID}` and `/results`
- Read the run verification and evidence endpoints above for each resulting run.

The browser currently uses the same session-authenticated APIs. CI systems can
use existing APIs only if they can safely establish an authenticated session;
there is no dedicated machine credential contract. Do not place session or
agent credentials in browser-visible configuration.

## Capability audit

| Capability | Status | Evidence in repository |
| --- | --- | --- |
| Auth, organizations, workspaces, tenant authorization | Implemented and tested | `internal/identity`, `internal/auth`, `internal/httpapi/*_test.go` |
| Agent/version registration, encrypted credentials, connection probes | Implemented and tested | `internal/agent`, `internal/agentconnection`, agent API and probe tests |
| Scenario authoring, criteria, suite planning/execution | Implemented; broad API tests exist | `internal/scenario*`, `internal/qualplan`, `internal/evaluationsuite`, qualification UI |
| Controlled Kubernetes execution, action authorization, telemetry | Implemented and exercised by qualification workflows | `internal/executionworker`, `internal/actionexec`, `backend/e2e` |
| Persisted verification, evidence, provenance, risk and trajectory | Implemented; API/unit coverage exists | `internal/verification`, `internal/evidence`, `internal/trajectory`, migrations 22–25 |
| Environment profile catalog and ephemeral development provider | Implemented and tested; development-only | `internal/environment`; no customer-managed provider |
| Customer-facing integration inventory | Partially implemented | Agent integration works; customer GitOps/cluster credential onboarding does not |
| Customer CI triggers, result webhooks, SDK | Missing | No CI auth/trigger contract, outgoing notification service, or shipped SDK |
| Reversal recommendation/planning/execution lifecycle | Missing as a product contract | No reversal-plan API or approval/execution/post-reversal verification workflow |
| Production observation and production readiness | Missing | No production environment provider or hosted customer qualification proof |

The hosted qualification workflow proves controlled test behavior only. It is
not evidence that customer production infrastructure integrations or production
isolation are ready.

# Independent customer action evidence (Stage 5A)

`customer-independent-action-evidence:v1` is an internal provider-neutral contract, not a customer-facing ingestion endpoint. The service has no independent audit/event provider yet; until a provider can authenticate and authorize its source, callers cannot submit evidence through this contract. Customer collector observations remain read-only state snapshots and agent action claims remain `UNVERIFIED_AGENT_CLAIM`.

Trust is derived by `internal/customeractionevidence` from server-side provenance assessment. `Assessment` is not request data: `Provider` has a package-private method, so only provider implementations in this package can produce evidence eligible for `Store` through `BuildFromProvider`. Such implementations are trusted server code and must verify source identity and environment authorization. Stage 5A contains no production provider or source authenticator; the provider seal is an internal code-boundary marker, not cryptographic proof or authenticated customer evidence. No HTTP endpoint exposes this path. `INDEPENDENTLY_OBSERVED_STATE` establishes only observed state, not actor or cause. `AUTHENTICATED_ACTION_RECORD` establishes an authenticated source record, not causation. `ATTRIBUTION_QUALIFIED` requires a verified actor and separately qualified causality. Missing/insufficient source identity is `INSUFFICIENT_PROVENANCE`; contradictions are `CONFLICTING_PROVENANCE`. Claims are always unverified. A SHA-256 digest detects payload changes and supports deterministic deduplication; it does not authenticate a source.

Evidence is scoped to the server-known workspace, environment, observation session, and run. PostgreSQL enforces the scope relationship and a unique source/event key; identical retries are idempotent and conflicting retries fail. Stored evidence is append-only. The persistence API requires a database transaction and validates active session state, collector revocation, and the baseline-to-now observation window. It is not wired to canonical verification or Stage 3 attribution. No classification in this contract alone changes a canonical verdict; platform action attribution continues to use the existing Stage 3 evidence contracts.

Future Stage 5B must provide a source-specific authenticated adapter (for example, Kubernetes audit events) and derive its authorization scope server-side. Kubernetes state diffs, Events, and controller reconciliations do not independently establish the actor or causal action. Stage 5C may adapt genuinely qualified evidence into the existing attribution evaluator; it must not create a parallel evaluator or treat agent claims as proof.
