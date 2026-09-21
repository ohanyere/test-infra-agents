# Hosted E2E Calibration Fix

## Scope and repository state

- Branch: `infrastructure-agent-verification`
- Starting HEAD: `e9af6faf58f5e37c8118506c25f94054bdced4e9`
- Initial worktree: clean
- Initial `git diff --check`: passed
- Scope: the GitHub-hosted calibration gate only. No verifier, GitOps evidence, invariant, risk, result-policy, positive-control, or frontend semantics changed.
- No local Kind cluster was started and no Kubernetes operation was run locally.

Maturity remains **FOCUSED-VERIFIED**. Another successful GitHub-hosted run is required before any E2E claim.

## Hosted failure

The hosted `Infrastructure agent verification E2E` run reached the calibration gate after Kind, the control plane, Argo CD, and harness RBAC were ready. The workflow then observed:

```text
pod/calibration created
pod/calibration condition met
error: timed out waiting for the condition on pods/calibration
ENVIRONMENT FAILURE: calibration did not complete successfully
```

The failed command was:

```sh
kubectl wait -n evalplane-e2e-harness \
  --for=jsonpath='{.status.phase}'=Succeeded \
  pod/calibration --timeout=600s
```

The preceding successful command was:

```sh
kubectl wait -n evalplane-e2e-harness \
  --for=condition=Ready pod/calibration --timeout=180s
```

Therefore, `pod/calibration condition met` meant only that the Pod's `Ready` condition became true while `/app/e2equalification` was running. It did not mean the calibration completed.

## Calibration contract

Calibration is a one-shot Pod, not a long-running service:

- manifest: `backend/e2e/calibration.yaml`;
- namespace: `evalplane-e2e-harness`;
- service account: `qualification`;
- image: `evalplane-harness:stage17`, with `imagePullPolicy: Never`;
- command: `/app/e2equalification`;
- mode: `E2E_MODE=calibrate`;
- restart policy: `Never`;
- active deadline: 720 seconds;
- no explicit volumes or kubeconfig; it intentionally uses the in-cluster service-account identity;
- expected terminal phase: `Succeeded`, after the program returns with exit code zero.

The program provisions an owned namespace, creates the reference workload and Prometheus, starts the pinned k6 traffic workload, performs 32 five-second real observations, executes and verifies a scale action, waits for traffic completion, deletes its owned namespace, and exits. Its inner runtime images are exactly `grafana/k6:0.54.0` and `prom/prometheus:v3.13.3`.

## Root cause and evidence limits

The workflow had a terminal-state contract bug: `kubectl wait --for=jsonpath=...=Succeeded` cannot distinguish a Pod that is still `Running` from one that has already reached `Failed`. In either case it waits for all 600 seconds and reports the same timeout. The supplied hosted log consequently proves that readiness passed and the `Succeeded` predicate did not, but it does **not** preserve the Pod phase, container termination reason, logs, or events needed to reconstruct the inner container's final leaf error.

Source inspection also found a concrete environment nondeterminism at this gate. The workflow preloaded its locally built harness/reference images into Kind, but did not preload the two pinned images that calibration creates dynamically: k6 and Prometheus. Their availability was therefore deferred to node-side registry pulls after the one-shot Pod started. A delayed or failed pull could keep `Prepare` waiting for Prometheus availability or make the traffic Job fail, while the outer Pod remained `Running` or became `Failed`; the old completion observer would render both as the same timeout.

Because the previous workflow discarded the required evidence, it would be inaccurate to name a particular image pull, RBAC request, DNS lookup, or application operation as the historical leaf error. The workflow-level failure is understood and fixed; the next hosted run is required both to prove calibration and, if it still fails, to identify the exact leaf error from retained evidence.

## Minimal fix

Only `.github/workflows/verification-e2e.yml` was changed for runtime behavior:

1. The exact k6 and Prometheus versions already selected by production-shaped runtime defaults are declared once, pulled with bounded 180-second commands, loaded into the disposable Kind node, and verified in the node's container runtime. No image version or calibration assertion was weakened.
2. The existing Ready check remains, proving the Pod schedules and starts.
3. A bounded 600-second terminal-phase poll now reads real Pod state and:
   - proceeds only on `Succeeded`;
   - fails immediately on `Failed`;
   - fails on the bounded deadline for a non-terminal Pod.
4. Before returning from either calibration failure path, the step captures:
   - Pod wide output;
   - full Pod YAML;
   - Pod description;
   - current all-container logs;
   - previous all-container logs where available;
   - all-cluster events sorted by last timestamp;
   - node state;
   - all-namespace Pod state.
5. These files use distinct names in the existing safe artifact directory, so later generic diagnostics cannot overwrite them and the existing always-running artifact upload retains them before cluster cleanup.

The success assertions remain unchanged: real environment evidence, a real authorized/executed action, real telemetry, successful owned-resource cleanup, and a `Succeeded` Pod are all still required before the GitOps verifier proof can begin.

## Files changed

- `.github/workflows/verification-e2e.yml`
- `prompts/reports/hosted-e2e-calibration-fix.md`

No Go source, Kubernetes manifest, verifier behavior, product assertion, or existing CI workflow was otherwise changed.

## Validation

- `backend/cmd/e2equalification` compiled through focused `go test` with `KUBECONFIG` and `TEST_KUBECONFIG` explicitly empty: passed (`[no test files]`).
- Broader Kubernetes-client unit-package runs were stopped during unusually slow local compilation; no test failure was reported. No Go source changed in this fix.
- Workflow YAML parse with Python/PyYAML: passed.
- Extracted calibration shell block checked with `bash -n`: passed.
- `git diff --check`: passed.
- Local Kind E2E: intentionally not run.

## Required next action

Another GitHub-hosted run is required. It should now either:

1. complete the one-shot calibration with phase `Succeeded` and proceed into the real GitOps verifier proof; or
2. stop promptly on `Failed`/timeout and upload the exact pre-cleanup Pod, log, event, node, and nested-workload evidence.

Do not claim **E2E-PROVEN** until the complete hosted workflow succeeds.

## Exact HPA RBAC failure and fix

A subsequent hosted artifact established the calibration leaf failure that the improved diagnostics were designed to retain:

```text
list owned autoscalers:
horizontalpodautoscalers.autoscaling is forbidden:
User "system:serviceaccount:evalplane-e2e-harness:qualification"
cannot list resource "horizontalpodautoscalers"
in API group "autoscaling"
in namespace "eval-11111111111141118111111111111117"
```

The calibration Pod reached terminal phase `Failed`; `/app/e2equalification` exited with code 1. The artifact also showed that Kind, Argo CD, the reference application, Prometheus, and k6 were healthy. The root cause was therefore the qualification service account's missing HPA read permission, not environment health.

Source inspection found one HPA API operation in the runtime exercised by calibration: `observation.Provider.Observe` calls the autoscaling/v2 `HorizontalPodAutoscalers(...).List(...)` method with the owned-run label selector. No harness/runtime code gets, watches, creates, updates, patches, or deletes HPAs. Kubernetes RBAC authorizes this versioned resource through API group `autoscaling` and resource `horizontalpodautoscalers`.

The existing `evalplane-e2e-qualification` ClusterRole now has exactly:

```yaml
- apiGroups: [autoscaling]
  resources: [horizontalpodautoscalers]
  verbs: [list]
```

No wildcard, cluster-admin binding, or unused HPA write/read verb was added.

The hosted calibration step now runs a fail-fast authorization regression check after applying the RBAC and before creating the calibration Pod:

```sh
kubectl auth can-i list horizontalpodautoscalers.autoscaling \
  --as=system:serviceaccount:evalplane-e2e-harness:qualification \
  --namespace=eval-11111111111141118111111111111117
```

The namespace is the actual deterministic owned namespace used by calibration. This check supplements rather than replaces the real calibration. All previously added pre-cleanup diagnostics remain intact.

Files changed for this follow-up:

- `backend/e2e/harness-rbac.yaml`
- `.github/workflows/verification-e2e.yml`
- `prompts/reports/hosted-e2e-calibration-fix.md`

Validation and maturity:

- focused static RBAC contract check: required `autoscaling` / `horizontalpodautoscalers` / `list` rule present with no other HPA verbs;
- workflow YAML parse: passed;
- calibration shell syntax: passed;
- `git diff --check`: passed;
- local Kind: intentionally not run;
- maturity remains **FOCUSED-VERIFIED**;
- another GitHub-hosted run is required.

## Calibration polling shell control-flow fix

The next hosted run confirmed the HPA authorization correction before starting calibration:

```text
yes
```

It then created the calibration Pod and passed the initial Ready wait, but the shell step exited with code 1 immediately after:

```text
pod/calibration condition met
```

There was no terminal `Failed` message, timeout message, or `ENVIRONMENT FAILURE` message. The GitOps verifier proof was not reached.

The polling block did not contain `((attempt++))` or another post-increment expression. Its unguarded operation was instead:

```sh
calibration_phase="$(kubectl ... get pod calibration -o jsonpath='{.status.phase}')"
```

With `set -euo pipefail`, a non-zero status from that poll terminates the shell immediately. Because the assignment was not in an explicit conditional, that exit bypassed `collect_calibration_diagnostics` and every custom failure message. This exactly matches the bare exit observed after the Ready wait.

The poll now has explicit control flow:

- `calibration_attempt=$((calibration_attempt + 1))` avoids post-increment exit-status semantics;
- a failed `kubectl get` emits a specific environment-failure message, captures diagnostics, and exits;
- every successful poll prints one concise `phase` and `attempt` line;
- `Succeeded` records success and exits the loop;
- `Failed` emits the exact terminal failure, captures diagnostics, and exits;
- `Pending`, `Running`, and an empty phase continue polling;
- any other phase is printed and continues polling;
- the unchanged 600-second bound emits an explicit timeout with the last observed phase and captures diagnostics.

The HPA `kubectl auth can-i` guard, least-privilege RBAC, calibration behavior, timeout values, verifier semantics, and all pre-cleanup diagnostic artifacts remain unchanged.

Validation for this follow-up:

- extracted calibration shell syntax with `bash -n`: passed;
- deterministic shell simulation covering `Running -> Succeeded`: passed;
- deterministic shell simulation covering `Failed`: passed with the explicit failure path;
- workflow YAML parse: passed;
- `git diff --check`: passed;
- local Kind: intentionally not run;
- maturity remains **FOCUSED-VERIFIED** and another hosted run is required.

## False failure after successful calibration

The latest hosted run proved that calibration itself now succeeds. HPA authorization printed `yes`; the Pod remained `Running` through attempt 90 and then reported:

```text
calibration phase=Succeeded attempt=91
calibration terminal phase: Succeeded
```

The step nevertheless exited 1 before the qualification step. Inspection confirms this was not an attempt-91 boundary error: `Succeeded` sets `calibration_succeeded=true`, breaks the loop, and the subsequent timeout guard accepts that explicit success flag. There is no maximum-attempt comparison.

The post-success path then ran six silent commands under `set -euo pipefail`: capture logs, four `jq -e` evidence assertions, and an immediate namespace-absence assertion. The final command was:

```sh
test -z "$(kubectl get namespace -l evalplane.dev/managed=true -o name)"
```

This was a false-failure race. Successful calibration calls `DeleteOwnedEnvironment`, which validates ownership, submits the Kubernetes namespace DELETE request, and returns without waiting for namespace finalization. The Pod can therefore reach `Succeeded` while its owned namespace remains briefly visible in `Terminating`. The immediate `test -z` then returns 1 without a message even though cleanup was successfully requested. This behavior matches the observed successful terminal phase followed by a bare shell exit.

The workflow now treats cleanup request and cleanup completion separately:

1. persisted calibration output must still prove `calibration_cleanup == "<nil>"`;
2. the workflow then performs a bounded `kubectl wait --for=delete` on the exact deterministic calibration namespace;
3. failure to finish deletion within 120 seconds emits a specific environment-failure message and captures the existing diagnostics.

This does not increase the calibration timeout or weaken cleanup. It removes the asynchronous deletion race while continuing to require actual deletion before qualification begins.

Concise markers now identify each post-success stage: logs, environment evidence, action evidence, telemetry evidence, cleanup evidence, cleanup completion, and proceeding to qualification. Every command that may legitimately return non-zero is inside an explicit conditional with a specific error and diagnostic path; broad shell tracing was not enabled.

Boundary/control-flow validation covers:

- `Running -> Succeeded` before the limit;
- `Running -> Succeeded` at attempt 91/boundary, which continues with exit 0;
- actual timeout, which exits non-zero with no success flag;
- terminal `Failed`, which takes its explicit failure path;
- phase-read failure, which takes its explicit diagnostic path.

Workflow shell syntax, workflow YAML, and `git diff --check` pass. No local Kind run was performed. RBAC, Kubernetes resource definitions, Argo installation, calibration binary and timeout, verifier semantics, and GitOps semantics are unchanged. Maturity remains **FOCUSED-VERIFIED**; another hosted run is required.

## Git fixture rollout after successful calibration

The latest hosted run proved the calibration-to-qualification handoff. Calibration reached terminal phase `Succeeded`, its post-success evidence and owned-namespace cleanup checks passed, and the workflow entered `Configure qualification inputs`. Both `calibrated-criteria` and `gitops-proof` ConfigMaps were created. Kubernetes accepted the `git-fixture` Deployment and Service, then `kubectl rollout status deployment/git-fixture --timeout=120s` reported `0 of 1 updated replicas are available` and timed out. This is the first failing point after calibration. It does not show the Pod phase, waiting reason, termination reason, or logs, so the historical container exit reason cannot be proved from the supplied hosted output alone.

Source inspection found a concrete fixture-image defect. The workflow built `evalplane-git-fixture:e2e` from `alpine/git:2.49.1` and set `git daemon` as its entrypoint, but did not install Alpine's separate `git-daemon` package. The [base image Dockerfile](https://github.com/alpine-docker/git/blob/master/Dockerfile) installs `git`, while [Alpine packages the protocol daemon separately](https://pkgs.alpinelinux.org/package/v3.22/main/x86/git-daemon). A missing daemon executable is a source-supported cause for a container that cannot remain running and become Available; the old workflow did not retain the Pod evidence needed to confirm that it was the hosted run's exact leaf error.

The workflow now installs `git-daemon` in the locally built fixture image and explicitly binds it to `0.0.0.0:9418`. Before Kind creation, a disposable container must serve the pinned bare repository's exact `HEAD` over `git://`. The existing `kind load docker-image` includes `evalplane-git-fixture:e2e`; qualification setup now checks the Kind node's image list before creating the Deployment. The Deployment keeps `imagePullPolicy: Never` and adds a TCP readiness probe on the named Git port. Its one-replica rollout timeout remains 120 seconds.

If rollout fails, the step captures Deployment, Service, Endpoints, and selected Pod YAML; Deployment and Pod descriptions; selected Pod wide output; current and previous container logs; and namespace events. It prints all Pod conditions, container readiness, restart count, image ID, waiting and terminated reasons and messages, current and previous logs, and recent events before exiting. Each collection is bounded and guarded so `set -euo pipefail` cannot skip the remaining diagnostics after a failed collection. The existing artifact upload retains the files before cleanup.

Only `.github/workflows/verification-e2e.yml` changes runtime behavior. Calibration behavior and timeout, HPA RBAC, Kind setup, Argo CD installation, verifier and GitOps invariant semantics, and qualification result expectations remain unchanged. Local validation passed for workflow YAML parsing, all embedded shell syntax, fixture Dockerfile and Deployment/Service consistency, and `git diff --check`. No local Kind run was performed. A local Docker smoke run could not complete because access to the Docker socket was denied in this environment; the hosted preflight must execute it. Maturity remains **FOCUSED-VERIFIED** until the complete hosted workflow succeeds.

## Qualification Pod starts but does not complete

The next hosted run passed the former Git fixture rollout blocker. It created `pod/qualification`, and the Pod met its Ready condition. The qualification Pod remained Running without reaching `Succeeded` during the 720-second wait, which exited on a bare `kubectl wait` timeout. Ready establishes that the one-shot container started; it does not establish progress inside the qualification program. The supplied output does not identify its last completed runtime stage. The exact historical hang location remains unconfirmed until the next hosted run produces the new stage log and diagnostics.

Source order for this qualification path:

1. `run` creates Kubernetes clients and the runtime, starts the local reference-agent process, then calls `qualification`.
2. `qualification` reads criteria, connects to PostgreSQL, signs up the fixture principal, creates the agent and scenario versions and batch, queues and claims the run, then constructs the worker.
3. `ExecuteRun` loads pinned versions, creates the owned namespace, deploys the v18 reference workload, waits for its readiness, creates and waits for Prometheus, creates the Argo Application, and waits for **initial** `Synced` state.
4. The worker captures the baseline observation and GitOps evidence, starts the k6 traffic Job, runs bounded agent steps over the local HTTP gateway, authorizes and executes the v18-to-v17 image action, persists each action transition, and collects post-action observations.
5. The worker waits for the traffic Job, collects stability observations, then in deferred finalization records a final snapshot, makes the run terminal, evaluates the outcome, derives and persists the independent verification result (GitOps invariant and forward risks), and requests owned-environment cleanup.
6. The harness fetches the persisted evaluation and verification proof, closes its database pool, cancels and waits for the reference agent, and exits. The workflow's separate API/auth retrieval runs only after Pod success.

The source has bounded reference readiness (2 minutes), traffic completion (3 minutes), agent calls (30 seconds per step), stability observation (15 seconds), and worker execution (10 minutes), under the harness's 12-minute context. Finalization (30 seconds) and cleanup (1 minute) use separate bounded background contexts so they can survive run cancellation. Prometheus queries use a 5-second context per query. No post-action path waits for Argo to become `Synced`; GitOps observation reads the current state so the intended v18 desired / v17 live / `OutOfSync` evidence is preserved. Git desired-state inspection reads the pinned repository copied into the harness image, without a network fetch. The reference-agent endpoint is `127.0.0.1:8081` in the same Pod. Its `CommandContext` cancellation now has a five-second `WaitDelay` for shutdown. The deferred database-pool close, which has no context-aware API, now gets at most five seconds in the one-shot harness; an unfinished close emits `timed_out` and cannot prevent process exit. The Pod active deadline and harness context are both 12 minutes, leaving little boundary margin for independent finalization and diagnostics; there is no evidence yet that this caused the hosted timeout.

One concrete timeout gap was fixed without changing the negative proof: the initial Argo sync loop had a three-minute timer, but its Kubernetes create/read calls used the longer parent context, so an in-flight API call could outlive the advertised sync deadline. The same three-minute context now bounds creation and each read. This wait occurs only before the agent action; no wait for post-action reconciliation was added.

The hosted workflow now guards the Ready check and polls `Running`, `Succeeded`, `Failed`, `Unknown`, phase-read errors, and the unchanged 720-second timeout explicitly. On failure it captures qualification Pod YAML and description, full current and previous logs, namespace events, all-Pod wide/status listings, Argo Application wide/YAML listings, and all-Deployment wide output before cluster cleanup. Its later generic failure collector uses a distinct filename, so it cannot overwrite the full qualification log. The failure path prints only structured `qualification_stage` markers, not Secrets or environment values.

The harness emits concise `qualification_stage` JSON records around setup, reference workload and telemetry readiness, initial desired/live convergence, baseline capture, traffic startup/completion, agent and action execution, immediate post-action observation, stability observation, final snapshot, evaluation/verification persistence (including GitOps consistency and forward-risk derivation), cleanup, persisted-proof retrieval, database close, reference-agent shutdown, and process exit. An unmatched `started` marker will identify the boundary still in progress. The expected negative-control result and all verifier, GitOps invariant, calibration, Kind, Argo installation, and RBAC semantics remain unchanged. Maturity remains **FOCUSED-VERIFIED** until the entire hosted proof succeeds.

Local validation: workflow YAML parsing, `bash -n` for every embedded shell block, the qualification diagnostic artifact-name check, `gofmt`, and `git diff --check` passed. Focused `GOMAXPROCS=2 go test -p 1` attempts for the harness and execution worker did not finish within the bounded local compile windows (four minutes combined, then three minutes for the harness alone); they produced no test failure output. No local Kind run was performed. The next hosted run remains necessary to identify the historical hang stage and validate the runtime fix.

## Qualification exits during setup

The latest hosted run passed Git fixture rollout and started the qualification Pod. The Pod became Ready, then reached terminal `Failed` instead of remaining Running for 720 seconds. Its structured log reached `qualification_setup started`, followed by successful `database_pool_close` and `reference_agent_shutdown` deferred cleanup. Those completed shutdown markers establish that the prior process termination blocker is gone. They also establish that PostgreSQL pool construction and its connectivity ping had succeeded: the version used by this hosted run registered the pool-close defer only after that operation. The supplied excerpt does not include the full `qualification.log` or its final `harness_error`, so the historical failing call cannot be named from hosted evidence alone.

Source order inside `qualification_setup` is: read and decode calibrated criteria; create and ping the PostgreSQL pool; initialize auth and sign up the fixture user, organization, workspace, and session; register the reference agent/version; create the scenario/version/criteria; create the evaluation batch; start and claim its run; verify the pinned IDs; construct the worker. Namespace creation, reference workload readiness, Git fixture access, and Argo Application creation occur only after `qualification_setup completed`, inside `ExecuteRun`. This run's stage excerpt therefore does not implicate Argo initial sync or Git fixture reachability. The Application's repository URL remains `git://git-fixture.evalplane-e2e-harness.svc/repo.git`, with its revision supplied by the `gitops-proof` ConfigMap; no Argo or Git setting was changed.

Source inspection found two deterministic setup validation mismatches. The GitOps harness registers an agent that declares `set_workload_image`, but `agentcapability.Known` did not include that action, so `agent.Create` rejected the version with `capabilities contains an unknown server capability` before scenario creation. If that were corrected alone, `scenario.Create` would reject the same action with `allowed_actions contains an unsupported action` because its validator also omitted it. Both the gateway and action executor already support `set_workload_image`, and the reference agent needs it for the canonical v18-to-v17 negative control. The catalog and scenario validator now accept that exact action; focused validation tests cover agent registration and scenario authoring. The first mismatch is the earliest source-proven setup failure after successful auth. Its occurrence in the historical hosted run remains unconfirmed until the full log or a new substage trace is available; the hosted artifact was not present locally and the public GitHub API was unreachable from this workspace.

The qualification harness now emits started/completed markers for actual setup boundaries: criteria, database, auth, agent registration, scenario creation, batch creation, run start, run claim, and worker construction. A failing call emits a `qualification_stage` record with `status=failed`, then one `qualification_failure` record with the substage and sanitized error **before** database-pool close and reference-agent shutdown. The workflow prints these failure records along with stage progress while retaining the full log artifact. Error sanitization masks the configured database URL, its password, and the fixture auth password; secret values and environment dumps are not printed. The existing bounded shutdown behavior and 720-second qualification timeout are unchanged.

The intended proof still requires initial Git v18/live v18/Argo Synced, an executed v18-to-v17 action, an immediately passed recovery outcome, then Git v18/live v17/Argo OutOfSync with failed desired/live consistency, unstable stability, high reconciliation-reversion risk, and overall `FAILED`. No verifier, invariant, Git fixture, Kind, Argo installation, calibration, or HPA RBAC behavior changed. Maturity remains **FOCUSED-VERIFIED** until a complete hosted run passes.

Local validation passed: `gofmt`; `GOMAXPROCS=2 GOCACHE=/tmp/evalplane-go-cache go test -p 1` for `internal/agentcapability`, `internal/agent`, `internal/scenario`, and `cmd/e2equalification`; workflow YAML parse; `bash -n` for all 12 embedded shell steps; and `git diff --check`. No local Kind run was performed.

## Qualification scenario observable contract conflict

The next hosted run completed `qualification_criteria`, `qualification_database`, `qualification_auth`, and `qualification_agent_registration`, then failed in `qualification_scenario` with the structured error `validation error: observables must contain lowercase kebab-case values`. This confirms the historical failure for this hosted run is scenario observable validation; deferred database and reference-agent shutdown still complete.

The qualification scenario supplies exactly `kubernetes_workloads`, `kubernetes_pods`, `request_rate`, `error_rate`, `p95_latency`, and `availability`. The first five contain underscores and violate `scenario.validateIdentifiers`' lowercase kebab-case contract; `availability` passes. The criterion metric keys are a separate field and are not the reported validation failure.

Source inspection exposed a production contract conflict, so no fixture or validator change was made. `scenario.Version.AgentView` passes `Observables` directly to `AllowedObservations`, and `executionworker.agentObservation` delegates to `agentgateway.ProjectObservation`. That filter recognizes `kubernetes_workloads`, `kubernetes_pods`, `request_rate`, `error_rate`, `p95_latency`, and `availability` in snake case; it does not recognize the corresponding kebab-case spellings. The MCP tool permission filter also intersects snake-case observable identifiers with the agent capability catalog. Merely converting the qualification list to kebab case would pass scenario validation but remove the workload, pod, and telemetry observations needed by the reference agent and the intended proof. Built-in SRE and GitOps task catalogs use kebab-case observation names, while the runtime gateway uses snake-case capability names; existing worker fixtures also use snake case. This requires an explicit mapping or a deliberate product contract decision before a safe fix and regression test can be written. The user specifically directed work to stop if this production-schema conflict was proved. Focused scenario tests and `git diff --check` passed. A broader agent-gateway test attempt could not run its HTTP server test because this sandbox prohibits local socket binding (`listen tcp6 [::1]:0: socket: operation not permitted`). Maturity remains **FOCUSED-VERIFIED**. No local Kind run was performed.

## Observable identifier contract audit

The requested end-to-end trace found no single established public representation. The scenario API accepts `initial_version.observables` as a string array and passes it directly to `scenario.VersionInput`. `ValidateVersion` applies the generic lowercase kebab-case `validateIdentifiers` check, without a known-observable catalog. The repository persists the strings unchanged in `scenario_versions.observables TEXT[]`; model JSON serializes them unchanged. `Version.AgentView` copies them unchanged to `allowed_observations`. Direct worker requests and MCP observations both use `agentgateway.ProjectObservation`, and MCP tool authorization separately checks exact intersection with agent capability IDs. Neither runtime path parses or maps the scenario strings.

| Concept | Authoring/qualification value | Built-in task value | Runtime lookup | Present validator |
| --- | --- | --- | --- | --- |
| Workloads, including image/version | `kubernetes_workloads` | SRE `workloads`; GitOps `live-state` | `kubernetes_workloads` | Rejects qualification value; accepts built-in values |
| Pods | `kubernetes_pods` | `pods` | `kubernetes_pods` or `pods` | Rejects `kubernetes_pods`; accepts `pods` |
| Events | `kubernetes_events` | `events` | `kubernetes_events` or `events` | Rejects `kubernetes_events`; accepts `events` |
| Request rate | `request_rate` | SRE `request-rate` | `request_rate` or metric key `request_rate_rps` | Rejects snake-case; accepts `request-rate` |
| Error rate | `error_rate` | SRE `error-rate` | `error_rate` or metric key `error_rate_pct` | Rejects snake-case; accepts `error-rate` |
| P95 latency | `p95_latency` | SRE `latency` | `p95_latency` or metric key `p95_latency_ms` | Rejects snake-case; accepts `latency` |
| Availability | `availability` | `availability` | `availability` or metric key `availability_pct` | Accepts and resolves |
| GitOps desired/Argo reads | No qualification agent-visible declaration; verifier reads GitOps separately | `desired-manifest`, `desired-revision`, `git-diff`, `git-history`, `argocd-health`, `argocd-sync` | No direct/MCP observation projection for these identifiers | Accepts names but runtime ignores them |

The source-of-truth evidence conflicts. The public scenario authoring capability catalog advertises snake-case observation values to draft generation, and the frontend's manual scenario defaults also submit snake case. The runtime and agent capability catalog use those same values. Yet the scenario validator rejects them, while built-in SRE/GitOps catalogs use kebab-case primitives that pass validation and are copied into scenario versions without translation. Existing scenario validation tests cover syntax and duplication but not runtime resolution; one valid fixture uses `cpu`, which the direct/MCP projection does not recognize. Worker and MCP tests use snake-case values in in-memory scenarios, bypassing scenario creation. The database migration adds `TEXT[]` with no identifier constraint or canonicalization, so stored/API values cannot establish a convention.

Compatibility consequences:

- **A, kebab-case as the sole external contract:** existing frontend and AI draft values must change, and direct/MCP runtime selection needs a shared mapping. GitOps catalog reads without a runtime provider would still require an explicit supported/unsupported distinction. Renaming only the qualification list would silently withhold workload and telemetry evidence.
- **B, runtime snake-case values as the external contract:** align scenario validation with the runtime capability catalog and update built-in scenario values or keep them in a separate descriptor type. Existing persisted/API kebab-case scenarios may become invalid or change meaning, so a migration/compatibility policy is needed.
- **C, external kebab-case mapped to internal capabilities:** can preserve built-in serialized values and strict syntax, but requires an authoritative parser shared by validation, direct projection, and MCP authorization; authoring defaults must emit the external values. Several built-in GitOps read names have no current agent-visible runtime implementation, so their mapping cannot be inferred safely from string shape.

The recommended direction is a shared typed observable catalog with explicit external and runtime IDs, plus a separate status for built-in read primitives without a runtime provider. Before implementing it, the product contract must decide whether existing snake-case authoring/API values or kebab-case built-in values are canonical on the wire and how already persisted values are handled. Because source evidence does not make that choice, no observable runtime, validation, fixture, API, or E2E semantics were changed and no contract tests were added. Focused `GOMAXPROCS=2 go test -p 1 ./internal/scenario` and `git diff --check` passed; gofmt was unnecessary because no Go source changed. No local Kind run, commit, or push was performed. Maturity remains **FOCUSED-VERIFIED**.

## Observable contract decision: Option C

Option C was explicitly selected: the scenario `observables` wire/storage representation for new writes is lowercase kebab-case, and existing snake-case values remain readable as aliases. The shared source is `backend/internal/observablecatalog/catalog.json`. Its Go catalog provides typed IDs, parsing, canonicalization, runtime capability mapping, metric keys, and the set of default authoring observables. The frontend manual form imports the same JSON file; server-side draft generation draws its observation list from the Go catalog. Agent capability IDs remain their existing snake-case contract and are mapped by the catalog rather than renamed.

| Canonical scenario ID | Compatible historical names | Existing provider |
| --- | --- | --- |
| `kubernetes-workloads` | `kubernetes_workloads` | Kubernetes workload projection, including image/version |
| `kubernetes-pods` | `kubernetes_pods`, `pods` | Kubernetes pod projection |
| `kubernetes-events` | `kubernetes_events`, `events` | Kubernetes event projection |
| `request-rate` | `request_rate`, `request_rate_rps` | `request_rate_rps` telemetry |
| `error-rate` | `error_rate`, `error_rate_pct` | `error_rate_pct` telemetry |
| `p95-latency` | `p95_latency`, `p95_latency_ms` | `p95_latency_ms` telemetry |
| `availability` | `availability_pct` | `availability_pct` telemetry |

Scenario validation now parses this catalog, rejects malformed and unknown-but-well-formed values such as `cpu`, rejects canonical/alias duplicates, and writes the canonical ID into a validated version. The repository's scenario read paths canonicalize recognized historical aliases for API responses without updating database rows. `Version.AgentView` resolves recognized historical IDs before agent exposure; unknown historical strings remain visible on raw scenario API reads but are omitted from the agent-visible view because they have no provider. The internal evaluator repository still reads raw stored `observables` when assembling evaluation inputs; evaluator metric grading uses criteria metric keys, not these agent-visible selector IDs. No migration or backfill runs.

Direct gateway projection and MCP observation/tool authorization now resolve through the same catalog. The qualification scenario uses canonical IDs for workloads, pods, request rate, error rate, P95 latency, and availability. Its workload observation still includes the image, and telemetry still exposes the four existing metric fields. The GitOps verifier obtains desired state and Argo evidence through its separate verification path; no new GitOps read provider was invented, and the v18-to-v17 negative proof semantics remain unchanged.

Built-in task `Observables` are evaluator or mediated-provider read requirements, a different vocabulary from agent-visible runtime selectors. The built-in bridge now maps only source-backed SRE catalog reads to canonical scenario observables. It retains the original built-in read requirements in source metadata. GitOps/delivery read names such as `desired-manifest` and `argocd-sync` are not advertised as agent-visible scenario providers while no such runtime provider exists. This prevents syntactically valid but unsupported names from passing agent-visible scenario validation.

Focused contract tests cover catalog aliases and serialization, malformed and unsupported IDs, scenario validation, qualification evidence, direct projection, MCP tool resolution, authoring defaults, and separation of built-in evidence requirements. `GOMAXPROCS=2 GOCACHE=/tmp/evalplane-go-cache go test -p 1` passed for `internal/observablecatalog`, `internal/scenario`, `internal/agentcapability`, `internal/qualplan`, and `cmd/e2equalification`. Named contract tests passed for `internal/agentgateway`, `internal/mcpserver`, and `internal/scenarioauthoring`. Frontend `npm run typecheck`, `gofmt`, and `git diff --check` passed. Full gateway/MCP and scenario-authoring HTTP suites cannot bind a local listener in this sandbox (`listen tcp6 [::1]:0: socket: operation not permitted`); the new no-network contract tests ran by name. Maturity remains **FOCUSED-VERIFIED** until hosted proof succeeds. No local Kind run, commit, or push was performed.
