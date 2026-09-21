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
