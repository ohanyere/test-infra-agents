# Using Evalplane with an MCP-capable agent

Evalplane continues to invoke every automated agent through its registered HTTP
Agent API endpoint. MCP is the tool-access plane used during that invocation; it
does not launch the agent.

1. Register an exact AgentVersion with an HTTP invocation endpoint.
2. Select `mcp` as that version's immutable tool-access mode and declare its
   supported capabilities.
3. Start an evaluation normally. Evalplane invokes the pinned version.
4. The invocation includes an evaluation-scoped `tool_access` object containing
   a Streamable HTTP endpoint, bearer grant, and expiry.
5. Connect with an MCP `2026-07-28` client and discover the server.
6. Use `tools/list`; only the intersection of agent, scenario, runtime, and run
   permissions is returned.
7. Observe or act through those tools, then return an action-free completion
   response to the HTTP invocation.
8. Evalplane revokes the grant when the run completes, stops, fails, times out,
   is cancelled, or is recovered after worker abandonment.

The invocation fragment is shaped like this:

```json
{
  "tool_access": {
    "mode": "mcp",
    "endpoint": "https://worker.example.com/mcp",
    "token": "short-lived-secret",
    "expires_at": "2026-09-11T20:00:00Z"
  }
}
```

Use the official Go SDK's stateless Streamable HTTP client. Supply the bearer
grant on every request, allow the SDK to perform `server/discover`, then list and
call tools:

```go
client := mcp.NewClient(&mcp.Implementation{Name: "customer-agent", Version: "1.0.0"}, nil)
transport := &mcp.StreamableClientTransport{
    Endpoint: endpoint,
    HTTPClient: bearerHTTPClient(grant),
    DisableStandaloneSSE: true,
}
session, err := client.Connect(ctx, transport, nil)
if err != nil { return err }
defer session.Close()

tools, err := session.ListTools(ctx, nil)
result, err := session.CallTool(ctx, &mcp.CallToolParams{
    Name: "evalplane_observe_workloads",
    Arguments: map[string]any{},
})
```

The HTTP client helper must set `Authorization: Bearer <grant>` without logging
it. Do not persist the grant. A tool may disappear between listing and calling;
every call is independently authenticated and authorized.

The server implements only MCP Tools over stateless Streamable HTTP. Evalplane
does not newly use legacy HTTP+SSE, Roots, Sampling, Logging, Tasks, or MCP Apps.
Ground truth, criteria, hidden variants, qualification results, and other
evaluator-only data are not available through this interface.

## Invocation example

The HTTP Agent API request includes this field only for an MCP-enabled exact
AgentVersion:

```json
{
  "tool_access": {
    "mode": "mcp",
    "endpoint": "https://worker.evalplane.example/mcp",
    "token": "<short-lived bearer grant>",
    "expires_at": "2026-09-11T20:00:00Z"
  }
}
```

Use the official Go SDK's `mcp.StreamableClientTransport` against `endpoint`,
attach `Authorization: Bearer <token>` to every request, connect, then use
`ClientSession.ListTools` and `ClientSession.CallTool`. The SDK performs
`server/discover` and negotiates `2026-07-28`; do not send the removed legacy
initialize flow yourself. Treat the token as an in-memory evaluation credential:
never store or log it, and stop calling tools when the invocation ends.
