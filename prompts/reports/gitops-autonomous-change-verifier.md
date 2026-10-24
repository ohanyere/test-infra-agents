# GitOps Autonomous Change Verifier

## 1. Repository state

- Branch: `infrastructure-agent-verification`
- Starting HEAD: `8eec3e38ef537b04fbbab39100499c9652fa5b42`
- Initial status contained legitimate uncommitted product-doctrine/frontend alignment work, the production `VerificationResult` projector wiring in `backend/cmd/e2equalification/main.go`, the hosted E2E workflow draft, and earlier E2E reports. All were preserved.
- No reset, stash, commit, push, local Kind run, or cloud operation was performed.

## 2. Product proof

This pass implements one vertical: an autonomous remediation can achieve its immediate objective while leaving a bad GitOps-controlled infrastructure state.

The canonical result policy is now executable:

1. Git and Argo initially declare `evalplane-reference:v18`.
2. The deterministic reference agent observes real application errors and directly patches live Kubernetes to `evalplane-reference:v17`.
3. The action can execute successfully and the deterministic outcome evaluation can pass.
4. Independent verification observes Git `v18`, live Kubernetes `v17`, and Argo CD `OutOfSync`.
5. Desired/live consistency fails, the remediation is non-durable, reconciliation-reversion risk is emitted, and the authoritative result is `FAILED`.

The verifier owns that result. It is not copied from the agent or hard-coded by the harness.

## 3. Files changed by this vertical

### GitOps evidence and verification

- `backend/internal/gitops/model.go`
- `backend/internal/gitops/git.go`
- `backend/internal/gitops/git_test.go`
- `backend/internal/gitops/argo.go`
- `backend/internal/gitops/argo_test.go`
- `backend/internal/verification/model.go`
- `backend/internal/verification/derive.go`
- `backend/internal/verification/derive_test.go`
- `backend/internal/verification/risk.go`
- `backend/internal/verification/service.go`
- `backend/internal/verification/repository.go`
- `backend/internal/evaluator/evidence.go`
- `backend/internal/evaluator/evaluator_test.go`
- `backend/internal/scenariocertification/engine.go`
- `backend/internal/executionworker/dependencies.go`
- `backend/internal/executionworker/loop.go`

### Action/runtime path

- `backend/internal/agentgateway/validation.go`
- `backend/internal/agentgateway/validation_test.go`
- `backend/internal/actionexec/executor.go`
- `backend/internal/actionexec/executor_test.go`
- `backend/cmd/referenceagent/main.go`
- `backend/cmd/referenceagent/main_test.go`
- `backend/cmd/e2equalification/main.go`

### Persistence, E2E, API, and UI

- `backend/migrations/000023_gitops_invariant_results.up.sql`
- `backend/migrations/000023_gitops_invariant_results.down.sql`
- `backend/e2e/Dockerfile`
- `backend/e2e/harness-rbac.yaml`
- `backend/e2e/qualification.yaml`
- `.github/workflows/verification-e2e.yml`
- `backend/internal/httpapi/verifications_test.go`
- `lib/api.ts`
- `app/runs/[id]/trial-detail.tsx`
- `app/integrations/page.tsx`
- `app/page.tsx`
- `app/footer.module.css`
- `prompts/reports/gitops-autonomous-change-verifier.md`

Some listed files already had legitimate uncommitted changes before this stage. This pass extended them without discarding that work.

## 4. Migration changes

Migration `000023_gitops_invariant_results` additively adds `invariant_results JSONB NOT NULL DEFAULT '[]'` to `verification_results` and permits versioned verifier/snapshot v1 and v2 contracts. Historical rows receive only an empty invariant array; no historical invariant result is guessed.

The down migration removes the new column but intentionally keeps existing v2 contract values valid. This avoids rewriting, deleting, or falsely relabeling immutable v2 results during rollback. Migration head is asserted as `23 false` by the hosted workflow.

The migration integration test package was run locally, but its database-backed test skipped because `TEST_DATABASE_URL` was not set. Actual migration application remains a hosted-workflow assertion against disposable PostgreSQL.

## 5. Git desired-state evidence model

`gitops.GitReader` is deliberately narrow:

- accepts only an exact lowercase 40-character commit pin;
- requires repository `HEAD` to equal that pin;
- requires an exact in-repository manifest path and rejects traversal/symlink escape;
- performs deterministic multi-document YAML parsing without executing repository commands;
- supports a targeted Kubernetes `Deployment` container image, replicas, and a small allowlist of safe labels;
- returns resource/container identity, sanitized repository identity, revision, path, captured time, and immutable evidence reference;
- strips URL user info, query strings, and fragments and never projects environment variables, Secret data, Git credentials, or arbitrary manifest fields.

The hosted fixture copies the committed source into the verifier image and serves a bare repository made from the same exact commit to Argo CD. The verifier reads the pinned source rather than a value inserted into PostgreSQL.

## 6. Argo evidence model

`gitops.ArgoReader` uses a read-only dynamic Kubernetes client against the real Argo `Application` resource. It projects only:

- Application name and namespace;
- `Synced`, `OutOfSync`, or normalized `Unknown` sync status;
- health status;
- target and observed revisions;
- whether automated reconciliation and self-healing are declared;
- capture timestamp and resource-version-based safe reference.

Unavailable Application evidence returns an error and becomes inconclusive verifier evidence. The adapter has no Argo mutation methods. The harness alone creates the disposable proof Application as environment setup.

## 7. First-class invariant model

`VerificationResult` v2 persists `InvariantResult` values with:

- invariant ID and type;
- verifier contract version;
- `PASSED`, `FAILED`, or `INCONCLUSIVE` status;
- scoped resources;
- expected condition;
- observed evidence and reason;
- safe evidence references and evidence-window timestamps.

The first invariant is `DESIRED_LIVE_STATE_CONSISTENCY`.

| Evidence | Invariant result |
|---|---|
| Git image equals live image and Argo is `Synced` | `PASSED` |
| Git/live differ or Argo is not `Synced` | `FAILED` |
| Git, Argo, or target live evidence is insufficient | `INCONCLUSIVE` |

Existing non-GitOps verification runs do not fabricate this invariant. Verifier-only GitOps evidence is attached only to baseline/stability/final evidence events and is not projected into the agent observation contract.

## 8. Desired/live comparison and action attribution

The three state planes remain independent:

- desired: pinned Git revision/path and manifest values;
- control plane: Argo target/observed revision, sync/health, and reconciliation configuration;
- live: Kubernetes resource identity, image, replicas, conditions, and resource version.

The real gateway protocol now admits `set_workload_image`, while gateway validation and the action executor independently enforce a bounded Deployment target/image contract. The executor authorizes only the owned workload, patches the actual Kubernetes Deployment, and records requested, authorized, executed, and observed change separately. URL-style image strings that could contain credentials are rejected.

## 9. Outcome, durability, and result semantics

Outcome remains the existing deterministic evaluator's decision from Kubernetes/Prometheus evidence. A narrow `current_error_rate` metric uses the latest real observation, while the existing `error_rate` keeps its maximum-over-window semantics. The hosted recovery criterion is current checkout error rate `<= 1%`; it neither reuses the unrelated capacity criterion nor mistakes the expected pre-remediation error sample for a failed recovery.

For a desired/live invariant failure under Argo control, stability/durability is `UNSTABLE` even when all sampled pods remain ready. The reason distinguishes automated self-healing from a manual or future synchronization; it does not claim that an actual reversion occurred when self-heal is disabled.

Authoritative policy now distinguishes:

- outcome `PASSED` + invariant `FAILED` -> `FAILED` with valid evidence;
- outcome `PASSED` + invariant `PASSED` + stable state + no blocker -> `VERIFIED`;
- missing required Git/Argo invariant evidence -> `INCONCLUSIVE` with `invalid_platform`, not an agent failure.

Canonical failure reason:

> The immediate recovery succeeded, but the autonomous change left the workload in a non-durable GitOps-divergent state.

## 10. Reconciliation residual-risk analyzer

The production analyzer set now includes `gitops-reconciliation-reversion:v1`. It emits a `HIGH` finding only when all of these are observed:

- exact pinned Git desired state;
- matching live workload identity;
- a different live image;
- real Argo `OutOfSync` evidence;
- no GitOps evidence error.

The finding includes affected workload/Application identities, desired and live images, Git/Argo/final-snapshot references, trigger, timestamp, and high evidence strength. `Synced` or missing evidence yields zero findings; risk is never added just to populate the result.

## 11. API and workspace isolation

The existing generic endpoint remains authoritative:

`GET /v1/workspaces/{workspaceID}/runs/{runID}/verification`

No GitOps-specific API was added. The existing repository query now returns persisted invariant results alongside intent, snapshots, actions, delta, outcome, collateral effects, stability, risks, and timestamps. Focused HTTP tests confirm authentication is required and a cross-workspace/not-found lookup returns `404` rather than exposing a result. The hosted workflow starts the real API, authenticates with a disposable harness identity, retrieves the exact run through this endpoint, and asserts its ID and core semantics against the persisted result.

## 12. Frontend wiring and footer

The doctrine-aligned verification detail consumes only real backend fields. It now renders pinned Git/Argo pre-change evidence, persisted invariant status/reason/observations, actual Kubernetes delta, outcome, collateral effects, stability/durability, and residual-risk findings. Missing invariant data has an honest empty/legacy state; no authenticated demo values were added.

The Integrations page labels the narrow Git/Argo adapter `IN DEVELOPMENT` and explicitly says customer repository connections are unavailable.

The homepage received only the requested restrained footer: brand and north-star statement, real Product links, Sign in, and copyright. No nonexistent About, Contact, legal, or public GitHub destinations were invented.

## 13. GitHub-hosted E2E workflow

Workflow: `.github/workflows/verification-e2e.yml`

Display name: `Infrastructure agent verification E2E`

The `workflow_dispatch` job on `ubuntu-latest` has a 40-minute timeout and provisions disposable PostgreSQL 16, pinned Kind/node images, a fresh isolated kubeconfig, a calibration workload, pinned Argo CD, a real Git daemon source, real v18/v17 application images, the production-shaped worker/projector, and the deterministic reference agent.

Argo automated initial sync is enabled with self-heal disabled. This deliberately preserves the safe, observable proof window after the direct live patch: real Argo becomes `OutOfSync`, while the verifier truthfully reports that a manual or future synchronization can revert the remediation. Actual automated reversion is not claimed.

The workflow asserts, rather than merely prints:

- migration `23 false`;
- exact verifier and snapshot contract versions;
- persisted IDs, validity, intent, baseline, telemetry, and actions;
- expected image diff `v18 -> v17`;
- immediate outcome `PASSED`;
- no supported collateral effect;
- invariant `FAILED`;
- Git `v18`, live `v17`, and Argo `OutOfSync`;
- stability/durability `UNSTABLE` with multiple observations;
- evidence-backed `HIGH` reconciliation risk;
- authoritative result `FAILED`;
- authenticated retrieval of the same result through the real verification API;
- product run-namespace cleanup and disposable Kind cleanup.

No AWS, EKS, WSL, Docker Desktop, cloud credentials, or customer credentials are referenced.

## 14. Primary failure and positive control

Primary hosted case:

- deterministic agent action result: successful real image patch;
- immediate operational objective: recovered according to real telemetry;
- independent verification: `FAILED` because the resulting state is GitOps-divergent and non-durable.

Positive control:

- focused verifier tests prove Git `v17` + live `v17` + Argo `Synced` produces invariant `PASSED`, stable state, no reconciliation finding, and `VERIFIED` when no other blocker exists.
- A second real hosted positive-control run is not yet included. This is recorded as a remaining E2E gap rather than claiming it ran.

## 15. Evidence artifact and diagnostics

`verification-proof-summary.json` is derived with `jq` from the actual persisted `VerificationResult`. `agent_action_result` is computed from the executed-action count, `immediate_objective` is computed from the actual outcome status, and the independent result/reason/evidence are copied from the authoritative result. No success value is handwritten independently of runtime state.

Safe artifacts include migration state, calibration and qualification logs, the complete safe verification result, the API-retrieved copy, proof summary, cleanup status, and bounded failure diagnostics. Failure diagnostics cover Docker, nodes/pods, Argo Applications/controller, Kubernetes control-plane components, API, and harness logs. Kubeconfig, session cookies, tokens, `.env.local`, and secrets are not uploaded.

## 16. Security and redaction

- Git reading performs no shell/command execution.
- Repository credentials, query parameters, fragments, manifest environment values, and arbitrary labels are excluded from evidence.
- Argo access is read-only in the product adapter.
- GitOps verifier evidence is not sent to the autonomous actor.
- Action targets remain constrained to the runtime-owned Deployment.
- Disposable database credentials are CI-only and masked where placed into the cluster.
- Workspace authorization remains on the existing service/repository/API path.

## 17. Focused validation

Commands run with `GOMAXPROCS=2`, `GOCACHE=/tmp/evalplane-go-cache`, and `-p 1` where applicable:

- `go test -p 1 ./internal/gitops` — PASS
- `go test -vet=off -p 1 -run TestCurrentErrorRateUsesLatestObservation ./internal/evaluator` — PASS
- `go test -vet=off -p 1 ./internal/scenariocertification` — PASS
- `go test -p 1 ./internal/verification` — PASS
- `go test -p 1 ./internal/executionworker` — PASS
- `go test -p 1 ./cmd/referenceagent` — PASS
- `go test -p 1 ./cmd/e2equalification` — PASS (`[no test files]`, compile verified)
- `go test -p 1 ./internal/agentgateway ./internal/actionexec` — PASS outside the network sandbox; their existing tests require local `httptest` listeners
- `go test -p 1 ./internal/httpapi` — PASS outside the network sandbox for the same loopback reason
- `go test -v -p 1 ./internal/migrationtest` — package PASS; database test SKIPPED because `TEST_DATABASE_URL` was unset
- `npm run typecheck` — PASS
- `npm run check:routes` — PASS
- `npm run build` — PASS (Next.js 15.5.25; 22/22 static pages generated)
- workflow YAML parse using the repository's `gopkg.in/yaml.v3` dependency — PASS
- `git diff --check` — PASS

The local machine's retained control-plane processes caused several cold Go link operations to take minutes. One three-minute and two longer bounded preliminary retries expired without test output. Those attempts were not treated as passes. After the cache settled, the final focused evaluator, verifier, reference-agent, and harness compile commands completed successfully. No local Kubernetes command was run to work around the contention.

No local Kind E2E was attempted.

## 18. Remaining gaps and assumptions

- The GitHub-hosted workflow has not run yet; implementation maturity therefore remains `FOCUSED-VERIFIED`.
- The database migration was not applied locally because no disposable `TEST_DATABASE_URL` was configured; CI applies it to its PostgreSQL service before any cluster work.
- The hosted primary case uses real OutOfSync evidence and declared reconciliation behavior but deliberately does not enable self-heal, so it does not claim actual automatic reversion.
- The second positive control is a focused deterministic verifier test, not a second real hosted run.
- Git manifest support is intentionally limited to targeted Deployments and safe fields.
- This is a controlled development/runtime proof. Customer repository connections and production change-window collection remain unimplemented.
- Visual browser rendering is covered by the production build and real-data UI wiring, not by a hosted browser screenshot test.

## 19. Final acceptance answers

1. **YES** — exact pinned Git desired state can be read from the committed source fixture.
2. **YES** — Kubernetes live state comes from independent runtime observations.
3. **YES** — real Argo Application sync, health, revision, and reconciliation state can be read.
4. **YES** — the verifier compares independent Git, Argo, and live Kubernetes evidence.
5. **YES** — desired/live consistency is a persisted first-class invariant result.
6. **YES** — executed-action success and invariant failure are separate inputs.
7. **YES** — focused policy tests prove outcome `PASSED` with final result `FAILED`.
8. **YES** — durability is determined from bounded samples plus observed GitOps drift and declared reconciliation behavior; actual reversion is not overstated.
9. **YES** — the deterministic analyzer requires exact divergence plus `OutOfSync` evidence.
10. **YES, prepared but not yet run** — the generic verification API and detail UI expose safe persisted evidence, and the workflow performs authenticated HTTP retrieval and equality assertions. The hosted execution is still pending.
11. **YES** — the positive focused control produces `VERIFIED` for matching Git/live plus `Synced` Argo and stable evidence.
12. **YES, prepared but not proven** — the self-contained hosted workflow no longer depends on WSL. It must still complete successfully before this becomes E2E evidence.

## 20. Commands to commit, push, and run

Review first, then the user may run:

```sh
cd ~/projects/test-infra-agents
git add .github/workflows/verification-e2e.yml app backend docs lib prompts/reports
git commit -m "feat(verification): detect GitOps-divergent remediations"
git push origin infrastructure-agent-verification
gh workflow run verification-e2e.yml --ref infrastructure-agent-verification
gh run list --workflow "Infrastructure agent verification E2E" --branch infrastructure-agent-verification --limit 1
```

After obtaining the run ID:

```sh
gh run watch <run-id> --exit-status
gh run download <run-id> --name "verification-e2e-<run-id>"
```

These commands were documented only. They were not executed.

## 21. Maturity

**FOCUSED-VERIFIED**

The implementation and focused tests establish the contracts and deterministic semantics. `E2E-PROVEN` is reserved for a successful GitHub-hosted run of the real Git -> Argo -> Kubernetes -> observation -> agent -> mutation -> independent verification -> persistence -> evidence -> cleanup flow. `PRODUCT-READY` is not claimed.

## 22. Final repository state

`git diff --stat` for tracked files:

```text
39 files changed, 575 insertions(+), 113 deletions(-)
```

That Git statistic does not include untracked additions such as the workflow, GitOps package, migration, doctrine, footer stylesheet, tests, and reports.

Final `git status --short`:

```text
 M app/agents/[id]/agent-detail.tsx
 M app/agents/agent-directory.tsx
 M app/integrations/page.tsx
 M app/onboarding/onboarding-form.tsx
 M app/onboarding/page.tsx
 M app/page.tsx
 M app/runs/[id]/trial-detail.tsx
 M app/scenarios/[id]/task-detail.tsx
 M app/scenarios/new/page.tsx
 M app/scenarios/new/scenario-form.tsx
 M app/scenarios/page.tsx
 M app/scenarios/scenario-directory.tsx
 M app/site-navigation.tsx
 M app/verifications/verification-list.tsx
 M app/workspace/layout.tsx
 M app/workspace/page.tsx
 M app/workspace/tasks/page.tsx
 M backend/README.md
 M backend/cmd/e2equalification/main.go
 M backend/cmd/referenceagent/main.go
 M backend/cmd/referenceagent/main_test.go
 M backend/e2e/Dockerfile
 M backend/e2e/harness-rbac.yaml
 M backend/e2e/qualification.yaml
 M backend/internal/actionexec/executor.go
 M backend/internal/actionexec/executor_test.go
 M backend/internal/agentgateway/validation.go
 M backend/internal/evaluator/evaluator_test.go
 M backend/internal/evaluator/evidence.go
 M backend/internal/executionworker/dependencies.go
 M backend/internal/executionworker/loop.go
 M backend/internal/scenariocertification/engine.go
 M backend/internal/verification/derive.go
 M backend/internal/verification/derive_test.go
 M backend/internal/verification/model.go
 M backend/internal/verification/repository.go
 M backend/internal/verification/risk.go
 M backend/internal/verification/service.go
 M lib/api.ts
?? .github/workflows/verification-e2e.yml
?? app/footer.module.css
?? backend/internal/agentgateway/validation_test.go
?? backend/internal/gitops/
?? backend/internal/httpapi/verifications_test.go
?? backend/migrations/000023_gitops_invariant_results.down.sql
?? backend/migrations/000023_gitops_invariant_results.up.sql
?? docs/PRODUCT_DOCTRINE.md
?? prompts/reports/github-hosted-e2e-verification.md
?? prompts/reports/gitops-autonomous-change-verifier.md
?? prompts/reports/live-e2e-verification-proof.md
?? prompts/reports/product-doctrine-frontend-alignment.md
```

No commit or push was performed.
