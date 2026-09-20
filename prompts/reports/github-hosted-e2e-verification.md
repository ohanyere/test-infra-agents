# GitHub-hosted Infrastructure Agent Verification E2E

## Status

This change defines the hosted proof but does not itself prove the runtime.
Maturity remains **FOCUSED-VERIFIED** until the workflow completes successfully.

## 1–3. Workflow, triggers, and runner

`.github/workflows/verification-e2e.yml` adds a separate workflow named
**Infrastructure agent verification E2E**. It is manual-only via
`workflow_dispatch`, runs on `ubuntu-latest`, and has a 40-minute job timeout.
The existing `release-gates.yml` was not changed.

## 4–6. Docker, PostgreSQL, and Kind

The job fails early unless Docker client/server inspection and `docker ps`
succeed. It uses a disposable PostgreSQL 16 service with CI-only credentials,
runs the real migration command, and asserts migration version `22` with
`dirty=false`.

Kind is pinned to `v0.33.0` through `helm/kind-action@v1.13.0`, with node image
`kindest/node:v1.35.0`. The only
cluster is `evalplane-e2e`, with a kubeconfig under the checked-out workspace.
The workflow asserts the exact context, node readiness, and readiness of the
API server, scheduler, controller-manager, etcd, and CoreDNS before testing.
It contains no AWS, EKS, remote-cluster, or customer credential integration.

## 7–8. Calibration and telemetry

The existing calibration harness must schedule, provision a run namespace,
execute a real scale action, observe telemetry, and clean up successfully. Any
failure is explicitly reported as an environment failure. The scenario's
existing lightweight per-run Prometheus fixture supplies telemetry; no external
monitoring stack is installed.

## 9–11. Production path, agent, and primary scenario

The harness retains its minimal wiring to `verification.NewService`, the same
production VerificationResult projector and analyzers. No CI-only evaluator or
verifier exists. The proof is labeled **REAL RUNTIME E2E WITH DETERMINISTIC
REFERENCE AGENT** and uses the existing checkout traffic/capacity scenario.

The deterministic assertion requires four available Deployment replicas. The
agent still chooses the remediation from observations; intent does not encode a
remediation sequence.

## 12. Negative scenario

Deferred. The current harness has no safe deterministic negative mode. Adding
one would be a material harness expansion, so the initial hosted proof does not
misrepresent a platform failure or intentionally damage the control plane.

## 13. Result assertions

The job requires a server-derived result with non-empty result, Run,
AgentVersion, and ScenarioVersion IDs; exact verifier/snapshot contracts;
`valid` validity; and `VERIFIED` overall status. It also asserts a persisted
requested/authorized/executed action, non-empty real infrastructure diff,
`PASSED` outcome and side effects, multiple `STABLE` samples, an executed
forward-risk array, gateway observations, and product-owned namespace cleanup.
The result is retrieved through the harness's authorized
`verification.Service.Get` path after persistence.

## 14–15. Diagnostics and artifacts

On failure, bounded diagnostics capture Docker state, nodes, pods, relevant pod
descriptions, bounded control-plane logs, and harness logs. Safe calibration,
qualification, migration, result, and summary files are uploaded for 14 days.
The kubeconfig, database Secret, tokens, `.env` files, and production
credentials are not uploaded.

## 16. Security

All credentials are disposable workflow-local values. The in-cluster database
URL is masked before Secret creation. The workflow grants only `contents: read`
and uses no cloud credentials or repository secrets.

## 17. Local validation

Local validation intentionally excludes another Kind run. These focused tests
passed:

```text
GOMAXPROCS=2 GOCACHE=/tmp/evalplane-go-cache go test -count=1 -p 1 \
  ./cmd/e2equalification ./internal/verification ./internal/executionworker
```

`gofmt` and `git diff --check` also passed. No YAML parser or `actionlint` is
installed locally, so workflow structure was reviewed manually; the Kind
action tag and `install_only`/`version` inputs were checked against its upstream
repository. GitHub has not yet parsed or executed the workflow.

## 18. Remaining assumptions

- GitHub's `ubuntu-latest` image supplies Docker, kubectl, PostgreSQL client,
  and jq, as documented for hosted runners.
- Pods in Kind can reach the PostgreSQL service's published host port through
  the Kind node's Docker default gateway.
- The pinned Kind/node-image combination remains downloadable.
- A negative verification remains future work after the primary hosted proof.

## 19. Push and run

After reviewing the diff:

```text
git add .github/workflows/verification-e2e.yml \
  backend/cmd/e2equalification/main.go \
  prompts/reports/live-e2e-verification-proof.md \
  prompts/reports/github-hosted-e2e-verification.md
git commit -m "ci: add hosted infrastructure verification e2e"
git push origin infrastructure-agent-verification
gh workflow run "Infrastructure agent verification E2E" \
  --ref infrastructure-agent-verification
gh run watch --exit-status
```

The workflow must complete successfully before changing maturity to
`E2E-PROVEN`.
