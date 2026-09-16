# Qualification worker process

Run `cmd/worker` inside Kubernetes with `DATABASE_URL` supplied through a Secret,
a service account authorized for the existing environment manager, and:

```
worker -reference-image evalplane-reference:e2e
```

Set `AGENT_CREDENTIAL_ENCRYPTION_KEY` to the same explicit 32-byte base64 key as
the API. The worker resolves the exact persisted AgentVersion connection and
decrypts its credential immediately before each outbound call. The optional
`-agent-connections` JSON file remains only for explicit local/Stage 17 fixtures;
keep it out of source control. The process never logs connection contents.
The explicitly supplied image must implement the instrumented reference app contract.
The process creates no seed data and delegates the lifecycle to executionworker.
SIGINT/SIGTERM cancels active work and allows bounded finalization and cleanup.

For MCP tool access, set `MCP_PUBLIC_ENDPOINT` to the worker's externally
reachable Streamable HTTP URL and optionally set `MCP_LISTEN_ADDRESS` (default
`:8082`). The worker stores only hashes of short-lived run grants and revokes
them before owned-environment cleanup.

`cmd/referenceagent` is a deterministic local E2E fixture speaking the real HTTP
gateway protocol on port 8081. It requests four replicas when allowed and observed
errors are positive; it inspects while telemetry is missing and proposes no_op
once all four replicas are available and observed errors are zero. It has no
Kubernetes credentials, evaluator access, or grading logic. This fixture proves
pipeline mechanics only, not arbitrary AI-agent competence.

Live Stage 17 qualification remains unverified until the dedicated cluster run
has produced persisted telemetry, controlled-action evidence, and evaluation.
