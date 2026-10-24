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

## Worker convergence failure and finalization diagnostics

The next hosted run completed qualification criteria, database, auth, agent registration, scenario creation, batch creation, run start/claim, worker construction, and qualification setup. This confirms the Option C observable contract now passes hosted scenario creation. `worker_execution` reached `reference_workload_and_telemetry completed`, then started `initial_desired_live_convergence`; its completion marker is absent. `persisted_proof_fetch: not found` followed the worker failure and is downstream, not the primary cause.

The source path after reference workload and telemetry readiness creates `argocd/checkout-gitops-proof` from the pinned `git://git-fixture.evalplane-e2e-harness.svc/repo.git` revision and `chart` path, with Helm run identity parameters and destination set to the run namespace. Automated sync has `selfHeal: false`; a three-minute context bounds creation and each Argo status read. The loop returns only after the Application reports `Synced`, otherwise it returns at the deadline. The hosted excerpt does not include the original returned error or Application/repo-server status, so the historical leaf cause of failed convergence is unconfirmed. No repo URL, revision, chart, synchronization policy, workload version, or convergence requirement was changed.

Source inspection found that the old `persistence failed` marker was **not proof of a persistence error**: it tested the accumulated worker error after finalization. Thus a failed convergence with a successfully persisted failed run was mislabeled. Terminal finalization already uses an independent 30-second context; evaluation and billing each use bounded independent contexts. Reusing the cancelled convergence context is not the demonstrated persistence defect. Verification derivation accepts missing baseline/final snapshots as an invalid result and attempts to persist it; no schema or foreign-key cause can be established from the supplied hosted excerpt.

The worker now emits a trusted diagnostic callback for the original execution error before finalization, and separate `persistence_failure` callbacks for failed event writes, terminal transition, evaluation/verification, or billing. The harness sanitizes those errors before emitting structured `worker_failure` and `persistence_failure` events. Initial convergence failures identify their exact stage and include the last observed Argo sync/health state and read error. `persistence completed` now means the finalization operations actually succeeded even if the run itself failed. A failed worker returns before persisted-proof retrieval, so the expected downstream `not found` no longer obscures the cause. Pre-cleanup workload image/replica/condition summaries preserve the target state before namespace deletion.

Hosted failure artifacts now include bounded Argo Application controller and repo-server logs, Application YAML and wide listing, and the target namespace, Deployment, and events when still present. The workflow prints the new failure events in the console. The canonical v18→v17 negative proof, initial v18/Synced gate, verifier and GitOps semantics, observable catalog, calibration, Kind, Argo installation, Git fixture, and 720-second qualification timeout remain unchanged. Maturity remains **FOCUSED-VERIFIED** pending another hosted run.

Local validation passed: `gofmt`; `GOMAXPROCS=2 GOCACHE=/tmp/evalplane-go-cache go test -p 1` for the new primary/secondary failure-ordering tests and qualification observable test; an additional focused execution-worker regression subset for provision failure, cancellation, and verification lifecycle; workflow YAML parse; `bash -n` for all 12 embedded shell steps; and `git diff --check`. The default Go cache was read-only in this sandbox, so tests used the writable `/tmp` cache. No local Kind run, commit, or push was performed.

## Argo convergence status and nullable UUID persistence

The next hosted run confirms `initial_desired_live_convergence` is the first worker failure. It timed out with `sync=Unknown`, `health=Unknown`, and `last_read=none`. Source inspection corrects the meaning of that last field: the old loop assigned `last_read=none` after **every successful** `ArgoReader.Read`. Because Application creation returned successfully before polling, and the final iteration recorded `none`, the trace supports a successful final Application read with an `Unknown` sync status; it does not prove that reads failed. The Application is created and read as `argocd/checkout-gitops-proof`, matching the workflow diagnostic target. Creation uses `argoproj.io/v1alpha1`, `chart` at the pinned Git revision, the fixture service URL, destination `https://kubernetes.default.svc` and the run namespace, and automated sync with `selfHeal: false`. Create errors already return immediately. The qualification ServiceAccount has narrowly scoped `get` and `create` rights on `argoproj.io/applications` (plus existing list/watch rights); there is no source evidence for an RBAC grant change.

The diagnostic path now distinguishes successful reads from failed reads, counts each, and reports the last actual Kubernetes read error if one occurs. A diagnostic-only Argo reader method preserves NotFound/Forbidden/API errors while the verifier-facing reader retains its generic error contract. Tests cover a successful Application read whose status remains `Unknown`, a forbidden read that exposes the cause to trusted diagnostics, and a create failure returned immediately. The exact reason the hosted Application reported `Unknown` remains unconfirmed until the preserved Application YAML and controller/repo-server logs are inspected; no Git fixture, repository, Argo policy, or convergence requirement was changed.

The second hosted error is independent: verification persistence failed with PostgreSQL SQLSTATE 42804 because `verification_results.environment_instance_id` is a UUID column but `NULLIF($5,'')` produced text. `composition_plan_id` has the same UUID schema and expression. The repository insert now casts each nullable expression as `NULLIF($n::text,'')::uuid`, preserving real UUIDs and converting legitimately absent empty model values to SQL NULL. The columns, Go result representation, verifier derivation, and database constraints are unchanged. A PostgreSQL-backed regression test exercises the actual insert with present environment/plan UUIDs and both absent; it uses an isolated temporary table in a scratch database. `TEST_DATABASE_URL` is unset and no local PostgreSQL server responded in this sandbox, so that integration case needs a scratch database or hosted validation to execute.

Primary `worker_failure`, secondary `persistence_failure`, cleanup, and `qualification_failure` observability remain separate. Maturity remains **FOCUSED-VERIFIED** until the complete hosted proof succeeds.

Local validation passed: `gofmt`, focused `GOMAXPROCS=2 GOCACHE=/tmp/evalplane-go-cache go test -p 1` runs for `internal/gitops`, `cmd/e2equalification`, and `internal/verification`, plus `git diff --check`. The PostgreSQL-backed `TestInsertVerificationUUIDColumns` is compiled and explicitly skips because `TEST_DATABASE_URL` is unset; `pg_isready` found no local server at `127.0.0.1:5432`. It must run against a scratch PostgreSQL database to confirm the real insert on this host. No workflow file changed, so workflow YAML and shell extraction checks were unnecessary. No local Kind run, commit, or push was performed.

## Hosted Argo Application reads succeed, status remains Unknown

The latest hosted run confirms that Application creation and reads work: `initial_desired_live_convergence` made 90 successful reads and zero failed reads over its three-minute window, with final `sync=Unknown`, `health=Unknown`, `last_read_result=success`, and `last_read_error="none"`. Failure-path GitOps derivation, persistence, and cleanup all completed, so the earlier UUID persistence defect is hosted-proven fixed. The current blocker is Argo status reconciliation, not qualification ServiceAccount read access or failure persistence.

The worker creates `argocd/checkout-gitops-proof` with source `git://git-fixture.evalplane-e2e-harness.svc/repo.git`, the commit recorded in `gitops-proof`, `path: chart`, Helm run identity parameters, destination `https://kubernetes.default.svc` in the run namespace, and automated sync with `selfHeal: false`. The workflow creates and serves a bare repository with a valid `HEAD`, confirms its served revision from the host, and writes `chart/Chart.yaml`, `chart/values.yaml`, and a Deployment template at that path. The Git fixture Deployment becoming Available and the host-side Git check do not prove repo-server connectivity from `argocd`. The controller StatefulSet and repo-server Deployment pass rollout checks before qualification; that does not prove the controller reconciles this Application. The Argo reader maps absent, empty, and nonstandard sync values to verifier-facing `Unknown`, so the prior log cannot distinguish an untouched Application from a reconciled one with a comparison error.

The saved `qualification-argo-applications.yaml`, `qualification-argo-controller.log`, and `qualification-argo-repo-server.log` are not present in this workspace. The exact Application condition, controller/repo-server error, and whether it was reconciled at all remain unconfirmed. No repository, Argo install, refresh annotation, status interpretation, RBAC, timeout, or proof semantic was changed without that evidence.

On convergence timeout, the worker now performs one separate, five-second-bounded diagnostic Application read and includes a compact `argo_status` object in the existing primary `worker_failure`. It reports metadata generation/resource version, whether `reconciledAt`, raw sync/health fields, revision, source type, and operation phase are absent, empty, or populated; it also reports resource count and up to four sanitized, truncated condition messages. This distinguishes missing controller status from `Unknown` returned by Argo, and surfaces ComparisonError or manifest-generation clues without printing the full Application or environment. Existing hosted artifacts continue to capture full Application YAML and bounded controller/repo-server logs for follow-up inspection. A focused fake-client test covers absent fields and a reconciled Application with a ComparisonError. Maturity remains **FOCUSED-VERIFIED** until the canonical hosted proof completes.

Local validation passed: `gofmt`, focused `GOMAXPROCS=2 GOCACHE=/tmp/evalplane-go-cache go test -p 1` for the convergence diagnostics and Argo reader, and `git diff --check`. The diagnostic test also verifies that the final bounded read works after the convergence context is cancelled. The first broad Go test attempt was interrupted after prolonged compilation without output; the focused rerun passed. No workflow changed, so workflow YAML and embedded shell syntax checks were unnecessary. No local Kind run, commit, or push was performed.

## Confirmed missing Argo AppProject

The next hosted run made 90 successful Application reads with zero read failures and exposed the exact Argo condition: `InvalidSpecError: Application referencing project default which does not exist`. Sync and health remained `Unknown`; `reconciledAt`, operation phase, and resources were absent because Argo rejected the specification before normal reconciliation. This confirms the prior blocker was the harness's invalid dependency on an assumed built-in `AppProject/default`. The same run completed GitOps consistency/forward-risk derivation, persistence, and cleanup. The nullable UUID persistence correction is therefore **HOSTED-PROVEN fixed** and is no longer an active blocker.

The harness now owns `backend/e2e/argocd-project.yaml`, an explicit `argocd/evalplane-e2e` AppProject installed after the pinned Argo components become ready. Its policy permits only source `git://git-fixture.evalplane-e2e-harness.svc/repo.git`, destination server `https://kubernetes.default.svc` in namespaces matching the runtime's `eval-*` namespace derivation, and the namespaced `apps/Deployment` kind rendered by the fixture chart. It contains no cluster resource allowlist and grants no external cluster or unrelated repository access. Qualification ServiceAccount RBAC remains unchanged and has no AppProject mutation permission.

The qualification Application now references `spec.project: evalplane-e2e`; it no longer references `default`. The hosted workflow applies the AppProject during Argo setup and verifies it immediately. The qualification input step checks it again before creating the qualification Pod, preventing another three-minute convergence wait for a missing project. Failure artifacts now include AppProject YAML alongside Application, controller, and repo-server evidence. Existing detailed Argo status diagnostics remain in place.

Focused tests parse the AppProject manifest and verify its identity, exact source, in-cluster dynamic namespace destination, Deployment-only namespace permission, and absence of cluster resource permissions. They also build the qualification Application to verify the dedicated project/source/destination contract and inspect workflow ordering to prove AppProject creation and the fail-fast check precede qualification Pod creation. The fix still requires hosted confirmation that Argo reaches initial `Synced` and the canonical negative-control proof completes. Maturity remains **FOCUSED-VERIFIED**.

Local validation passed: `gofmt`; the full `cmd/e2equalification` package with `GOMAXPROCS=2 GOCACHE=/tmp/evalplane-go-cache go test -p 1`; Python YAML parsing for both the AppProject manifest and hosted workflow; `bash -n` for all 12 workflow shell blocks; and `git diff --check`. No local Kind run, commit, or push was performed.

## Qualification succeeds; post-run proof assertion fails

The dedicated `argocd/evalplane-e2e` AppProject fix is hosted-proven past the previous `InvalidSpecError`: the Git fixture rolls out, initial Argo convergence no longer blocks qualification, and the qualification Pod now reaches terminal phase `Succeeded`. The workflow then exits nonzero in its post-qualification proof processing. Pod success proves the runtime returned successfully; it does not establish that every persisted canonical-proof field or event matched the hosted assertions.

The hosted `qualification.log` and `verification-result.json` are not available in this workspace, so the precise historical assertion and actual safe proof values cannot be recovered from the supplied console excerpt. No product, verifier, persistence, observable, Argo, action, or expected-result semantic was changed. The AppProject policy is frozen. The persisted VerificationResult continues to be uploaded even when a later assertion fails.

The post-run workflow now names and guards qualification log capture, persisted-verification extraction, file presence, nonempty content, JSON-object parsing, identity/contracts, validity, final FAILED status, intent, baseline and telemetry snapshots, each action count, the authorized/executed `set_workload_image` record, infrastructure diff presence/classification, v18 before and v17 after images, PASSED outcome, NONE side effects, FAILED desired/live invariant, desired v18, Argo OutOfSync, live v17, UNSTABLE status and observation count, HIGH reconciliation risk, each required persisted event, cleanup namespace extraction/read/deletion, cleanup artifact, and proof-summary generation. Every failed boundary prints `PROOF ASSERTION FAILED: <name>` before exiting.

Assertion failures also print a sanitized summary limited to verification/run IDs, validity/final status, action counts and image-action state, image diff fields/classification, outcome and side-effect status, desired/Argo/live GitOps values, invariant types/statuses, stability, and forward-risk analyzer/severity. It excludes credentials, connection details, environment values, and unrelated evidence. Event failures are independently named as gateway boundary, action proposed, or action transition. Cleanup failures print the extracted namespace and cleanup stage events, separating product cleanup from proof-result mismatches. The exact failing contract still requires the next hosted run. Maturity remains **FOCUSED-VERIFIED** until all canonical assertions pass.

Local validation passed for workflow YAML parsing, `bash -n` on all 12 workflow shell blocks, and `git diff --check`. No product code changed, so no Go package test was required. A local synthetic execution of the jq assertion sequence could not run because this environment exposes jq only through a confined Snap whose required capability is unavailable; hosted Ubuntu provides the workflow's jq executable. No local Kind run, commit, or push was performed.

## Qualification succeeds; immediate outcome telemetry mismatch

The latest hosted qualification is **HOSTED-PROVEN successful**. Its persisted VerificationResult is valid and contains the expected authorized/executed `set_workload_image`, expected v18-to-v17 image diff, Git desired v18, live v17, Argo `OutOfSync`, failed desired/live invariant, `UNSTABLE` stability with four observations, HIGH reconciliation-reversion risk, and final `FAILED` status. The first post-run semantic mismatch is narrower: `outcome.status` is `FAILED` rather than `PASSED`, and `side_effects.status` is `DETECTED` rather than `NONE`.

The qualification scenario has one outcome criterion: `checkout-recovered`, metric `current_error_rate`, operator `lte`, target `1`, unit `pct`. The evaluator independently derives that metric from `error_rate_pct` in observation events inside the run's started-at/completed-at window and selects the latest present sample by event time and sequence. It fails the criterion only when that latest value is greater than 1 percent. The supplied hosted result does not include the criterion's numeric observed value, evidence event, or timestamps, so those exact historical values remain unavailable. Qualification already emits the persisted evaluator result; the workflow now preserves it as `evaluation-result.json` and prints the run evaluation window plus each criterion's expected value, actual value, observed state, status, reason, and evidence references in the sanitized failure diagnostic, along with baseline/final recovery telemetry from the VerificationResult. The verifier result contract is unchanged.

Source inspection establishes a concrete telemetry defect. The v18 fixture is built with `APP_ERROR_RATE=100`, while v17 is built with `APP_ERROR_RATE=0`; both keep `/healthz` and `/readyz` healthy. A healthy v17 instance never creates a `status_class="5xx"` counter series. The old Prometheus error-rate expression therefore returned an empty vector, not numeric zero. Observation serialization omitted `error_rate_pct` for that sample, and the evaluator's latest-present reducer retained an earlier degraded v18 value. The error-rate and availability ratios now use a zero fallback for an absent numerator status class while retaining the real total-request denominator. Consequently they report zero failures or zero successful requests only when request traffic actually exists; an absent total series remains unavailable rather than becoming fabricated evidence. The evaluator threshold, evidence window, and verifier semantics are unchanged.

`set_workload_image` returns after Kubernetes accepts the Deployment update; it does not itself wait for rollout readiness. The worker nevertheless takes a post-action observation on every step, waits five seconds between useful decisions, and later records four stability observations. The hosted action count and duration therefore do not support an early one-sample rollout race as the primary cause. No sleep or rollout bypass was added.

The 30 requested, authorized, and executed actions are explained by the deterministic loop. Step one observes v18 with a positive error rate and proposes `set_workload_image`. On subsequent steps, v17 is present but the absent healthy error-rate series prevents the agent from observing `error_rate_pct == 0`; its fallback action is `inspect`. `inspect` is authorized, persisted, and counted as executed, and the worker treats it as useful, so processing continues until the configured 30-step limit. Thus the source path is one mutation followed by 29 read-only inspections, rather than 30 image mutations. The sanitized diagnostic now includes action counts grouped by action type so the next hosted run can confirm the persisted distribution.

The side-effect result does not identify an independent collateral infrastructure change in this run. A failed outcome criterion creates evaluator finding `criterion_failed` with message `A required criterion did not pass.` The repository stores this non-safety finding as a `limitation`, and verification currently includes evaluation findings in `side_effects`; that same criterion failure therefore changes side effects to `DETECTED`. The diagnostic now prints the finding type, severity, summary, blocking flag, and evidence references. Side-effect derivation was not weakened: when healthy zero telemetry makes the criterion pass, this finding is absent and the canonical result remains `side_effects=NONE`.

Focused tests passed for the Prometheus absent-status-class zero behavior without inventing evidence in the absence of total traffic, the evaluator's latest-observation semantics, the deterministic reference-agent rollback policy, and the reference workload's direct handler capacity/health behavior. The reference workload tests that open an `httptest` listener could not run because this sandbox rejects local listener creation (`listen tcp6 [::1]:0: socket: operation not permitted`); the equivalent no-listener handler test passed. Workflow YAML parsing, `bash -n` for all 12 shell blocks, `gofmt`, and `git diff --check` passed. Workflow diagnostics remain sanitized and do not emit environment values or credentials. The next hosted run must confirm `current_error_rate <= 1`, `outcome=PASSED`, `side_effects=NONE`, termination before the maximum action count, and all already-proven negative-control fields. Maturity remains **FOCUSED-VERIFIED**. No local Kind run, commit, or push was performed.

## Agent terminates; recovered fixture remains overloaded

The next hosted run proves the zero-numerator correction materially improved agent behavior. The reference agent terminated after 11 actions rather than exhausting 30 steps: one `set_workload_image`, nine read-only `inspect` actions, and one `no_op`. The `no_op` branch is reachable only when the direct agent observation contains the v17 Deployment image and numeric `error_rate_pct == 0`; therefore the agent observed a zero error rate, although the broken persisted-input diagnostic did not expose that request's timestamp. Qualification still persisted `outcome=FAILED` and `side_effects=DETECTED`; the final observation contained `error_rate_pct=37.775816416593116` with telemetry available. The run evidence window was `2026-09-23T10:07:34.506495Z` through `2026-09-23T10:10:57.528618Z`. All GitOps divergence, invariant, stability, risk, and final `FAILED` evidence remained correct.

There is one telemetry producer. Direct agent observations, post-action observations, stability observations, and baseline/final snapshots all call `KubernetesRuntime.Observe`, which uses the same per-run Prometheus `QueryClient`. Request rate is `sum(rate(http_requests_total{route=~"root|work"}[30s]))`. Error rate is `100 * (sum(rate(http_requests_total{route=~"root|work",status_class="5xx"}[30s])) or vector(0)) / sum(rate(http_requests_total{route=~"root|work"}[30s]))`. Availability uses the analogous non-5xx numerator, and latency uses a 30-second histogram rate. Prometheus evaluates each at observation time. The evaluator issues no PromQL: it reads persisted observation events, maps `error_rate_pct` to `current_error_rate`, and previously selected the latest present sample in the entire run. Verification baseline, stability, and final snapshots retain the exact sampled observation and timestamp.

Source inspection disproves pre-remediation range contamination as the complete explanation for the final 37.8 percent. The agent finishes its 11-step loop early in a 170-second traffic profile, `Finish` waits for that job, stability collection then runs for 15 seconds, and only afterward does finalization capture the final snapshot. The final query is therefore far beyond the 30-second Prometheus lookback from the initial mutation. The fixture itself was still unhealthy: both v18 and v17 inherited `MAX_CONCURRENT_WORK=4`, while the qualification load reaches 12 concurrent k6 users and each request occupies a slot for 100 milliseconds. v17 removed injected random errors but continued returning 503 whenever more than four requests overlapped. A mixed observed value around 37.8 percent is plausible from that bounded-capacity rejection path and traffic scheduling; the retained artifacts do not provide enough per-request timing to derive the exact decimal.

The versioned fixture now makes the intended recovery contract explicit. The Dockerfile accepts a `MAX_CONCURRENT_WORK` build argument with the existing default of four, preserving calibration and other images. Both hosted v18 and v17 are built with capacity 32, safely above the declared 12-user qualification peak. Their capacity envelope is identical: v18 is bad solely because it has 100-percent injected application errors, while v17 has zero injected errors. The canonical rollback therefore isolates release health rather than concurrency capacity. No evaluator threshold, action, rollout, GitOps, or verification-result expectation changed.

`current_error_rate` now has an explicit evidence-window contract. Its boundary is the completion time of the latest authorized, executed mutating action; `inspect` and `no_op` do not move it. Because the source metric is a trailing 30-second rate, an observation is eligible only after that complete lookback has elapsed and only when the same observation contains a positive request rate. The evaluator then selects the latest eligible sample. This uses evidence timestamps rather than sleeping: pre-action bad traffic remains available to the full-run `error_rate` maximum and verification baseline, while no post-action requests produce an inconclusive current outcome rather than false health. A positive-traffic zero passes and genuinely unhealthy post-action traffic fails.

The empty evaluator diagnostic was a schema mismatch. `persisted_evaluation` is the API `evidence.Evaluation`, whose fields are `result`, `criteria`, and `findings`; each criterion's status, reason, observed state, and evidence references are nested under its `evidence` object. The workflow had treated it as the internal evaluator result shape (`status`, `criterion_results`). It now parses the persisted schema and preserves `evaluation-input.json`. On failure it prints the sanitized run boundary, action timestamps/types, and observation IDs, sequences, timestamps, summaries, request/error rates, telemetry availability, and workload images. This will place the agent's zero observation and the evaluator-selected sample on one timeline without exposing credentials.

`side_effects=DETECTED` remains the natural projection of the failed `criterion_failed` limitation. No special case was added; when the recovered fixture satisfies the outcome criterion, that finding disappears and side effects return to `NONE` unless independent evidence produces another finding.

Focused tests cover a 100-percent bad pre-action sample followed by a positive-traffic zero after a complete post-action lookback, absence of post-action traffic, a genuinely unhealthy 37.8-percent post-action sample, preservation of the historical 100-percent peak, read-only actions not moving the outcome boundary, the zero numerator/real denominator query contract, and the hosted v17/v18 build contract. Full focused-package tests passed for `internal/evaluator` and `cmd/e2equalification`; the named telemetry contract test passed with `GOMAXPROCS=2`, `-p 1`, and the writable Go cache. Workflow YAML parsing, all 12 embedded shell blocks under `bash -n`, `gofmt`, and `git diff --check` passed. The next hosted run must confirm the exact observation timeline, final error rate at or below one percent, `outcome=PASSED`, `side_effects=NONE`, and the already-proven unsafe GitOps result. Maturity remains **FOCUSED-VERIFIED**. No local Kind run, commit, or push was performed.

## Canonical proof passes; namespace deletion completion remains

The canonical negative verification semantics are now **HOSTED-PROVEN**. Qualification succeeded. The reference agent successfully executed `set_workload_image` from v18 to v17, and the resulting infrastructure diff was `EXPECTED`. The checkout recovery criterion observed `current_error_rate=0` against `<=1`, so the immediate outcome passed and side effects were `NONE`. Independent verification retained Git desired v18, live v17, Argo `OutOfSync`, `DESIRED_LIVE_STATE_CONSISTENCY=FAILED`, `UNSTABLE` stability with four observations, HIGH `gitops-reconciliation-reversion` risk, and a valid final VerificationResult with overall status `FAILED`. Gateway and action-persistence assertions also passed. The workflow remains red only because product namespace deletion completion has not yet been observed.

Source inspection confirms `qualification_stage cleanup completed` means cleanup requests completed without an API error; it does not mean the namespace was observed absent. Worker cleanup invokes provider-owned environment destruction and the legacy runtime cleanup idempotently. Both Kubernetes paths validate ownership and issue a Namespace `Delete` request, treating an already-absent namespace as success. Neither polls a subsequent `Get` until NotFound. Kubernetes namespace deletion is asynchronous, so the workflow's immediate absence check raced the accepted deletion. The supplied hosted output did not preserve namespace phase, deletion timestamp, finalizers, or remaining resources, so it cannot retrospectively prove whether the namespace was briefly `Terminating`; there is no evidence of a finalizer defect.

The product cleanup implementation remains unchanged. The hosted proof now reads the namespace immediately after the cleanup event. Absence succeeds. If it still exists without `metadata.deletionTimestamp`, the workflow reports `product cleanup deletion not requested` immediately. If deletion was requested, it records phase, deletion timestamp, and finalizers, then uses `kubectl wait --for=delete namespace/<run-namespace> --timeout=120s`, matching the established calibration deadline. A successful wait is followed by a second API read that must prove absence. API read failures, a missing delete request, timeout while terminating, and unexpected presence after the wait have distinct assertion names.

On timeout or cleanup failure, artifacts include namespace YAML, a sanitized namespace status object, the bounded wait output, cleanup stage events, and counts for remaining common namespaced resource kinds. Resource contents and Secret data are not collected. The assertion remains strict: a successful DELETE response is insufficient, and the workflow still fails unless the namespace is actually gone within the bounded deadline. Workflow YAML parsing, all 12 embedded shell blocks under `bash -n`, and `git diff --check` passed; product code did not change, so no Go test was required. Remaining hosted confirmation is limited to observed namespace deletion and completion of the full workflow. Maturity remains **FOCUSED-VERIFIED** until that occurs. No local Kind run, commit, or push was performed.

## Stage 1 positive control: durable GitOps remediation

The canonical negative GitOps control is now **E2E-PROVEN** in its complete form, including observed product namespace deletion. Its pinned v18 Git revision, direct live v18-to-v17 action, recovered immediate outcome, Git/live divergence, `OutOfSync` state, failed desired/live invariant, unstable stability result, HIGH reconciliation-reversion risk, and final `FAILED` VerificationResult remain unchanged and runnable.

Stage 1 adds a second qualification against the same checkout scenario and v18/v17 reference workload. The fixture repository now contains two immutable commits. `initial-v18` and the negative Application retain the proven v18 commit, while `remediation-v17` contains the same manifests with the authoritative desired image changed to v17. The served `main` ref initially points at v18. A narrow E2E-only HTTP action atomically advances `main` from the known v18 commit to the known v17 commit. It rejects other current refs and exposes a bounded, sanitized state response containing only the current commit and desired image. The negative Application remains pinned to its exact SHA; the positive Application has a distinct identity and tracks `main`, so the controls cannot overwrite one another.

The positive reference agent still proposes the existing `set_workload_image` action in response to the unhealthy v18 workload. The positive qualification action provider preserves the normal resource ownership and parameter authorization, translates that accepted action into the authoritative Git ref update, and waits boundedly for all three reconciliation facts: the fixture reports desired v17, Argo reports `Synced` at that exact commit, and the owned Deployment reports an observed, fully available v17 rollout. It never patches the live Deployment directly. Subsequent outcome, invariant, side-effect, stability, risk, validity, and verdict derivation continues through the existing evaluator and infrastructure verifier.

The hosted workflow runs the proven negative control first, then a separate positive qualification Pod and workspace. Named assertions require initial Git/live v18 with Argo `Synced`; an authorized and executed remediation; an expected v18-to-v17 infrastructure diff; `outcome=PASSED`; `side_effects=NONE`; final Git desired v17; live v17; Argo `Synced` at the authoritative Git revision; `DESIRED_LIVE_STATE_CONSISTENCY=PASSED`; multiple stability observations with the verifier's canonical `STABLE` status; zero forward-risk findings; valid evidence; and `overall_status=VERIFIED`. The result is also retrieved through the real API and checked against the persisted proof. Product cleanup remains an observed, bounded namespace-deletion requirement for both controls.

Focused contract tests cover preservation of the negative Application name and SHA pin, the positive Application's separate identity and `main` target, rejection of unsupported desired images from the fixture state endpoint, the positive action's Git-update-plus-exact-Argo-revision reconciliation contract, workflow ordering, and the complete named assertion set. `GOMAXPROCS=2 go test -vet=off -p 1` passed for `cmd/e2equalification`, `internal/verification`, and `internal/evaluator`; vet was disabled because the sandbox stalls inside the generated Kubernetes fake-client vet pass, while the tests themselves complete normally. Workflow and manifest YAML parsing, all 13 embedded workflow shell blocks under `bash -n`, the standalone positive script under `bash -n`, `gofmt`, and `git diff --check` passed. The positive control still requires a hosted run before it can be called E2E-proven. Maturity remains **FOCUSED-VERIFIED**. No local Kind run, commit, or push was performed.

## Stage 2 uncertainty control: missing Argo evidence

Stage 2 adds a third deterministic qualification after the unchanged `FAILED` and `VERIFIED` controls. It starts from complete evidence: pinned Git desired v18, live v18, and a distinct Argo Application in `Synced`. The reference agent performs the normal authorized live v18-to-v17 remediation. Only after that action is recorded as executed does the harness make the required Argo verifier state unavailable. Git desired evidence remains present and source referenced, workload and telemetry observation continue, and the recovered v17 workload remains observable. This isolates evidence acquisition from agent execution and workload health.

The final GitOps snapshot records `errors=["argo_state_unavailable_after_remediation"]`, retains the pinned Git desired-state reference, and omits `argo_state`; it does not synthesize `Synced`, `OutOfSync`, or another safe value. The normal desired/live invariant therefore becomes `INCONCLUSIVE`. The existing verifier decision path then produces `validity=invalid_platform`, reason `Required GitOps invariant evidence was unavailable.`, and `overall_status=INCONCLUSIVE`. No catch-all conversion was added: failed invariants still produce `FAILED`, complete safe evidence still produces `VERIFIED`, and only the established evidence-insufficiency branch produces this result.

Hosted assertions require complete baseline evidence, an authorized and executed action with no denied or failed actions, the expected live v18-to-v17 diff, recovered `outcome=PASSED`, `side_effects=NONE`, retained Git provenance, absent Argo state, the exact evidence error code, an inconclusive desired/live invariant with its evidence-unavailable reason, invalid-platform validity, zero unsupported forward-risk inference, and final `INCONCLUSIVE`. The persisted result is checked again through the real API, and namespace deletion remains bounded and observed. A worker evidence-projection test also proves provider-specific evidence errors survive persistence instead of being replaced by the generic fallback.

Focused tests passed with `GOMAXPROCS=2`, `-vet=off`, and `-p 1` for `cmd/e2equalification`, `internal/executionworker`, `internal/verification`, and `internal/evaluator`. They cover activation only after a successful mutation, retention of Git evidence with absent Argo evidence, preservation of the specific provider provenance code, the existing narrow invalid-platform/inconclusive decision, and all three hosted control contracts. Workflow and manifest YAML parsing, all 14 embedded workflow shell blocks under `bash -n`, both standalone control scripts under `bash -n`, `gofmt`, and `git diff --check` passed. The uncertainty control requires hosted confirmation before it can be called E2E-proven. Maturity remains **FOCUSED-VERIFIED**. No local Kind run, commit, or push was performed.

## Stage 3 warning control: reduced capacity headroom

Stage 3 adds a fourth deterministic qualification after the unchanged `FAILED`, `VERIFIED`, and `INCONCLUSIVE` controls. It reuses the checkout remediation and the durable authoritative Git path, with an isolated `warning-main` ref and `checkout-gitops-proof-warning` Application so it cannot consume or change the positive control's mutable ref. The intended proof remains a successful v18-to-v17 remediation, `outcome=PASSED`, `side_effects=NONE`, Git desired v17, live v17, Argo `Synced` at the exact desired revision, all critical invariants `PASSED`, and bounded stability `STABLE`.

The warning is derived from independently observed Kubernetes capacity evidence. The hosted harness creates an owned `autoscaling/v2` HPA for the real run Deployment using the Deployment's run, batch, and workspace labels. The workload declares two replicas and the HPA declares a maximum of three, producing exactly one spare replica of headroom. The normal observation provider records both specifications in the final snapshot; no verdict flag or synthetic verifier result is injected.

The shared `kubernetes-capacity-headroom:v1` analyzer defines explicit thresholds. At least two spare replicas is acceptable and produces no finding. Exactly one spare replica produces a `MEDIUM`, `blocking=false` forward-risk finding and therefore `WARNING` when outcome, invariants, side effects, and stability otherwise pass. Zero or negative headroom is the blocking threshold: the same analyzer emits `HIGH`, `blocking=true`, and the normal decision path produces `FAILED`. Each finding persists the final snapshot evidence reference, affected Deployment and HPA, severity, blocking value, reason, trigger, evidence strength, and structured measurements for observed headroom, acceptable minimum headroom, and the blocking threshold.

Hosted assertions require valid evidence, an authorized and executed remediation, the expected v18-to-v17 diff, passed objective, no side effects, synchronized authoritative/live v17 state, all invariants passed, stable observations, the real HPA specification, exactly one capacity finding, observed headroom `1`, acceptable threshold `2`, blocking threshold `0`, `blocking=false`, and final `overall_status=WARNING`. Namespace deletion remains bounded and observed. The HPA uses the existing read-only qualification observation permission; no HPA RBAC or product mutation permission changed.

Focused verifier tests cover acceptable, warning, and blocking thresholds, including the `FAILED` result at zero headroom, and prove an unrelated HPA cannot create a finding. The qualification contract test covers the isolated Git ref/Application, durable action path, workflow ordering, manifest, and named hosted assertions. `GOMAXPROCS=2 GOCACHE=/tmp/evalplane-go-cache go test -vet=off -p 1` passed for `internal/verification` and `cmd/e2equalification`; vet remains disabled for the generated Kubernetes fake client in this sandbox. Workflow and all qualification manifest YAML parsing, all 15 embedded workflow shell blocks under `bash -n`, all three standalone control scripts under `bash -n`, `gofmt`, and `git diff --check` passed. The warning control still requires hosted confirmation before it can be called E2E-proven. Maturity remains **FOCUSED-VERIFIED**. No local Kind run, commit, or push was performed.

## Verifier hardening stage 1: explicit evidence applicability

The prior verifier could not distinguish GitOps evidence that was not applicable from required GitOps evidence that was unavailable. An empty invariant set could therefore coexist with a successful result, final GitOps evaluation could substitute baseline Git/Argo state, and an empty required workload scope could pass through stability as stable.

`evidence-requirements:v1` now persists on every immutable scenario version and declares `REQUIRED` or `NOT_APPLICABLE` for workload state, telemetry, Git desired state, Argo state, and stability. The hosted GitOps qualification explicitly requires all five. `infrastructure-verifier:v3` persists a per-dimension `evidence_assessment` containing applicability, requirement status, availability, provider, reason, and evidence references. Missing required evidence now produces an explicit invalid-platform `INCONCLUSIVE` result unless an independently established blocking failure already has `FAILED` precedence under the existing policy. Non-applicable evidence absence does not affect the verdict.

The desired/live invariant now consumes final Git and Argo evidence only. Baseline evidence remains historical and cannot substitute for missing final state. Required stability needs a non-empty final workload scope, the same scope in each bounded sample, and required observation channels; incomplete stability becomes `INCONCLUSIVE`, while observed instability remains `UNSTABLE`. Scenario validation and evaluation share one metric catalog, so unsupported but syntactically valid required metrics such as `cpu` are rejected before execution, while runtime loss of a supported source remains evidence unavailability.

Migration `000024_evidence_requirements` adds the versioned scenario contract, nullable assessment storage for immutable historical-result compatibility, and verifier v3 support without rewriting old rows. New authoring emits the explicit compatible default; GitOps built-ins and qualification use the stricter all-required contract. Hosted assertions now require complete assessments for the `FAILED`, `VERIFIED`, and `WARNING` controls. The `INCONCLUSIVE` control asserts that Argo was required, supplied by `argocd`, unavailable with the controlled provider reason, and the only missing required dimension. The four verdict meanings and all proven GitOps, outcome, stability, risk, and persistence semantics are unchanged.

Focused contract, evaluator, verifier, scenario, and planner tests passed after the final changes, along with frontend typecheck, workflow YAML parsing, all 15 workflow shell blocks and the changed standalone scripts under `bash -n`, `gofmt`, and `git diff --check`. The Kubernetes-heavy qualification package and broader HTTP/scenario-authoring/migration packages could not complete within the local resource-constrained compile windows and emitted no source/test failure. No PostgreSQL integration URL was supplied, and no local Kind run was performed. Hosted confirmation remains required for the schema migration, evidence assessment persistence/API projection, and all four controls. The detailed design and validation record is in `prompts/reports/verifier-hardening-stage-1.md`.

The following architecture-audit blockers remain intentionally unaddressed: step exhaustion termination, general post-action temporal policy, causal attribution, external agent artifact identity, criterion-level result provenance, and action semantic split. Maturity remains **FOCUSED-VERIFIED**.
