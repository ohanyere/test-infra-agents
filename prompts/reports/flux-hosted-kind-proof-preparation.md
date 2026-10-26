# Flux hosted Kind proof preparation

Status: **prepared locally, not yet hosted-proven**. The Flux integration is
wired into `.github/workflows/verification-e2e.yml`. A meaningful hosted run
requires publishing these local changes to the selected ref. Local Docker
access is denied, so no local Kind claim is made.
The published `infrastructure-agent-verification` tip is `15acd35a692e39bc1062bd22542bd9e4f5fe1ded`;
its [existing E2E run](https://github.com/ohanyere/test-infra-agents/actions/runs/39984582185)
passed before the local Flux files were added. That run took about 41 minutes;
the job timeout is now 90 minutes to leave room for both new 12-minute-bounded
qualification pods, diagnostics, and artifact upload.

## One hosted run

After publishing the intended ref, dispatch **Infrastructure agent verification
E2E** on that exact ref with `scenario=external-agent`. This focused route runs
the existing external and hostile controls, followed by the Flux positive and
forbidden controls. The workflow uploads `verification-e2e-<run_id>` even on
failure. Record the run URL and commit SHA with the proof.

```sh
gh workflow run verification-e2e.yml --ref <published-ref> -f scenario=external-agent
gh run list --workflow verification-e2e.yml --limit 5
gh run watch <run-id> --exit-status
gh run download <run-id> --name verification-e2e-<run-id> --dir /tmp/flux-hosted-proof
```

The decisive artifact is `verification-proof-summary-flux.json`. It must show
`positive.verdict=VERIFIED`, `positive.attribution=ATTRIBUTED`,
`positive.desired_image=evalplane-reference:v17`,
`forbidden.verdict=FAILED`, `forbidden.denied_count=3`, and
`forbidden.desired_image=evalplane-reference:v18`. Inspect the paired
`verification-result-flux*.json` and `qualification-flux*.log` for the
persisted input, proposal, transition, evaluation, verification, and
platform-observed image identities. `flux-policy.yaml` records the upstream
ImagePolicy choice, while `flux-reflector-pod.json` and `flux-agent*-pod.json`
record the actual serving containers and their limited Kubernetes access.
The focused route reads the persisted verification result directly from the
qualification harness and saves it in the proof artifact. The existing
`scenario=full` option additionally retrieves `api-verification-result-flux*.json`
through the real API and compares IDs, verdicts, implementation identities,
and attribution statuses.

The Flux controller is pinned to upstream v1.2.5 and chooses v17 from a
disposable registry. A separate adapter reads that ImagePolicy and proposes the
desired-state change over the direct HTTP agent protocol. The verification
result is derived by the existing gateway, executor, attribution, and verifier.
The controller is a deterministic Kubernetes image-policy component, and the
adapter is a deterministic protocol bridge; neither is an autonomous AI agent.
The forbidden control sends a production-namespace mutation proposal through
the same boundary and asserts that no mutation executes or reaches VERIFIED.

The controller image is pinned by repository digest in the deployment; the
qualification harness separately records the Kubernetes-observed container
image ID. These digests can differ when a multi-architecture index resolves to
a platform image, so the proof checks both the pinned pod image and the
observed image ID rather than falsely claiming their hashes must match.

Upstream reference: [Flux image-reflector-controller v1.2.5 release](https://github.com/fluxcd/image-reflector-controller/releases/tag/v1.2.5).
The release API lists the CRD asset with SHA-256
`c28662b43ee30cfe17b1b177577564f3b52ee112ef345b52dcdda8b77049bde7`,
matching the runner pin.
Both pinned container references resolved as OCI indexes in a read-only
`docker manifest inspect` check; this does not substitute for the hosted pull
and Kind load steps.

The hosted run still depends on Docker Hub serving those two pinned manifests,
GitHub serving the checked CRD release asset, and the existing Kind, Argo CD,
PostgreSQL, k6, and Prometheus setup. The adapter Dockerfile uses the mutable
`gcr.io/distroless/static-debian12:nonroot` base tag; its final running image
digest is observed and persisted by the harness. No hosted pull or Kind load of
the new Flux images has been observed yet.

## Readiness checked locally

- Workflow YAML and all 26 embedded shell blocks parse; the Flux script passes
  `bash -n`, and all three Flux manifests parse as YAML.
- The Flux adapter and qualification binaries build and pass `go vet`; focused
  gateway, worker, evidence, verification, connection, and qualification tests
  pass. The adapter's Role grants only `get` on `checkout-stable`.
- The hosted script asserts the observed controller and adapter identities,
  scoped permissions, Secret isolation, policy choice, three structured
  `cross_namespace` denials, unchanged forbidden state, persisted result IDs,
  and the positive Git and Argo reconciliation outcome.
- `scenario=external-agent` reaches both Flux controls without running the
  full-only controls; authenticated API readback is available only through
  `scenario=full`. The workflow uploads artifacts even after a failed step and
  has a 90 minute job timeout. The baseline before publication was
  `15acd35a692e39bc1062bd22542bd9e4f5fe1ded`.
