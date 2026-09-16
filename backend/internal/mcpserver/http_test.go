package mcpserver

import (
	"bytes"
	"context"
	"crypto/subtle"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/evalplane/evalplane/backend/internal/actionexec"
	"github.com/evalplane/evalplane/backend/internal/agent"
	"github.com/evalplane/evalplane/backend/internal/agentgateway"
	"github.com/evalplane/evalplane/backend/internal/evidence"
	"github.com/evalplane/evalplane/backend/internal/kubeenv"
	"github.com/evalplane/evalplane/backend/internal/observation"
	"github.com/evalplane/evalplane/backend/internal/scenario"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

type memoryStore struct {
	mu      sync.Mutex
	grant   Grant
	revoked bool
	active  bool
}

func (s *memoryStore) Create(_ context.Context, grant Grant) (Grant, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	grant.ID = "grant-1"
	s.grant, s.revoked, s.active = grant, false, true
	return grant, nil
}
func (s *memoryStore) Resolve(_ context.Context, hash []byte) (Grant, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.revoked || !s.active || time.Now().After(s.grant.ExpiresAt) || subtle.ConstantTimeCompare(hash, s.grant.TokenHash) != 1 {
		return Grant{}, ErrInvalidToken
	}
	return s.grant, nil
}
func (s *memoryStore) Consume(ctx context.Context, id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if ctx.Err() != nil {
		return ctx.Err()
	}
	if s.revoked || !s.active || id != s.grant.ID || time.Now().After(s.grant.ExpiresAt) {
		return ErrInvalidToken
	}
	if s.grant.ToolCalls >= s.grant.MaxToolCalls {
		return ErrCallBudget
	}
	s.grant.ToolCalls++
	return nil
}
func (s *memoryStore) RevokeRun(_ context.Context, runID string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.grant.RunID == runID {
		s.revoked = true
	}
	return nil
}

type fakeTools struct {
	observation    agentgateway.Observation
	executed       []agentgateway.ProposedAction
	observeStarted chan struct{}
	blockObserve   bool
}

func (f *fakeTools) Observe(ctx context.Context, _ Scope) (agentgateway.Observation, error) {
	if f.observeStarted != nil {
		close(f.observeStarted)
		f.observeStarted = nil
	}
	if f.blockObserve {
		<-ctx.Done()
		return agentgateway.Observation{}, ctx.Err()
	}
	return f.observation, nil
}
func (f *fakeTools) Authorize(_ context.Context, _ Scope, action agentgateway.ProposedAction) actionexec.ActionResult {
	return actionexec.ActionResult{ActionType: action.Type, Authorized: true, Status: "allowed"}
}
func (f *fakeTools) Execute(_ context.Context, _ Scope, action agentgateway.ProposedAction) actionexec.ActionResult {
	f.executed = append(f.executed, action)
	return actionexec.ActionResult{ActionType: action.Type, Authorized: true, Executed: true, Status: "executed"}
}

type memoryEvidence struct {
	events  []evidence.AppendRunEventInput
	actions []evidence.AgentAction
}

func (m *memoryEvidence) AppendRunEvent(_ context.Context, in evidence.AppendRunEventInput) (evidence.RunEvent, error) {
	m.events = append(m.events, in)
	return evidence.RunEvent{ID: int64(len(m.events))}, nil
}
func (m *memoryEvidence) RecordAgentAction(_ context.Context, in evidence.RecordAgentActionInput) (evidence.AgentAction, error) {
	row := evidence.AgentAction{ID: "action-1", RunID: in.RunID, ActionType: in.ActionType, Status: in.Status, RequestPayload: in.RequestPayload}
	m.actions = append(m.actions, row)
	return row, nil
}
func (m *memoryEvidence) UpdateAgentAction(_ context.Context, in evidence.UpdateActionInput) error {
	if len(m.actions) == 0 {
		return errors.New("missing action")
	}
	m.actions[len(m.actions)-1].Status = in.Status
	m.actions[len(m.actions)-1].Authorized = in.Authorized
	return nil
}

func testService(t *testing.T) (*Service, *memoryStore, *fakeTools, *memoryEvidence, Access) {
	t.Helper()
	store := &memoryStore{}
	tools := &fakeTools{observation: agentgateway.Observation{
		Workloads: []agentgateway.WorkloadObservation{{Kind: "Deployment", Name: "checkout", Namespace: "eval-run", DesiredReplicas: 2}},
		Pods:      []agentgateway.PodObservation{{Name: "checkout-1", Namespace: "eval-run", Ready: true}},
		Metrics:   map[string]any{"telemetry_available": false},
	}}
	events := &memoryEvidence{}
	service, err := NewService(store, tools, tools, events, Config{PublicEndpoint: "https://worker.example/mcp", GrantTTL: time.Minute, MaxToolCalls: 20})
	if err != nil {
		t.Fatal(err)
	}
	resources := json.RawMessage(`[{"kind":"Deployment","namespace":"$RUN_NAMESPACE","name":"checkout"}]`)
	access, err := service.CreateGrant(context.Background(), Scope{
		WorkspaceID: "workspace-1", RunID: "run-1",
		Version:     agent.Version{ID: "version-1", ToolAccessMode: "mcp", Capabilities: []string{"kubernetes_workloads", "request_rate", "scale_workload", "no_op"}},
		Environment: observation.EnvironmentContext{Identity: kubeenv.RunIdentity{WorkspaceID: "workspace-1", BatchID: "batch-1", RunID: "run-1"}, Environment: kubeenv.Environment{RunID: "run-1", Namespace: "eval-run"}},
		AgentView:   scenario.AgentView{Task: "restore health", AllowedObservations: []string{"kubernetes_workloads", "request_rate"}, AllowedActions: []string{"scale_workload", "no_op"}, AllowedResources: resources},
	})
	if err != nil {
		t.Fatal(err)
	}
	return service, store, tools, events, access
}

type bearerTransport struct {
	token string
	base  http.RoundTripper
}

func (t bearerTransport) RoundTrip(request *http.Request) (*http.Response, error) {
	clone := request.Clone(request.Context())
	clone.Header.Set("Authorization", "Bearer "+t.token)
	return t.base.RoundTrip(clone)
}

func TestOfficialClientDiscoveryToolsObservationAndAction(t *testing.T) {
	service, _, tools, events, access := testService(t)
	server := httptest.NewServer(service.Handler())
	defer server.Close()
	httpClient := &http.Client{Transport: bearerTransport{token: access.Token, base: http.DefaultTransport}}
	client := mcp.NewClient(&mcp.Implementation{Name: "stage24-test-client", Version: "v1"}, nil)
	session, err := client.Connect(context.Background(), &mcp.StreamableClientTransport{Endpoint: server.URL, HTTPClient: httpClient, DisableStandaloneSSE: true}, nil)
	if err != nil {
		t.Fatalf("official client connect/discover: %v", err)
	}
	defer session.Close()
	if got := session.InitializeResult().ProtocolVersion; got != ProtocolVersion {
		t.Fatalf("protocol version = %q, want %q", got, ProtocolVersion)
	}
	listed, err := session.ListTools(context.Background(), nil)
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	for _, tool := range listed.Tools {
		names = append(names, tool.Name)
	}
	for _, expected := range []string{ToolObserveWorkloads, ToolGetTelemetry, ToolScaleWorkload, ToolNoOp} {
		if !slices.Contains(names, expected) {
			t.Fatalf("tools/list = %v; missing %s", names, expected)
		}
	}
	if slices.Contains(names, ToolRestartPod) {
		t.Fatalf("disallowed restart tool was visible: %v", names)
	}
	result, err := session.CallTool(context.Background(), &mcp.CallToolParams{Name: ToolObserveWorkloads, Arguments: map[string]any{}})
	if err != nil || result.IsError {
		t.Fatalf("observation call: result=%+v err=%v", result, err)
	}
	telemetryResult, err := session.CallTool(context.Background(), &mcp.CallToolParams{Name: ToolGetTelemetry, Arguments: map[string]any{}})
	if err != nil || telemetryResult.IsError {
		t.Fatalf("telemetry call: result=%+v err=%v", telemetryResult, err)
	}
	telemetryJSON, _ := json.Marshal(telemetryResult.StructuredContent)
	if !bytes.Contains(telemetryJSON, []byte(`"available":false`)) || bytes.Contains(telemetryJSON, []byte(`"request_rate_rps":0`)) {
		t.Fatalf("unavailable telemetry was not explicit or fabricated zero: %s", telemetryJSON)
	}
	result, err = session.CallTool(context.Background(), &mcp.CallToolParams{Name: ToolScaleWorkload, Arguments: map[string]any{"kind": "Deployment", "namespace": "eval-run", "name": "checkout", "replicas": 3}})
	if err != nil || result.IsError {
		t.Fatalf("scale call: result=%+v err=%v", result, err)
	}
	if len(tools.executed) != 1 || tools.executed[0].Type != "scale_workload" {
		t.Fatalf("executed = %+v", tools.executed)
	}
	if len(events.actions) != 1 || events.actions[0].Status != "executed" {
		t.Fatalf("action lifecycle = %+v", events.actions)
	}
	denied, deniedErr := session.CallTool(context.Background(), &mcp.CallToolParams{Name: ToolScaleWorkload, Arguments: map[string]any{"kind": "Deployment", "namespace": "other", "name": "checkout", "replicas": 4}})
	if deniedErr == nil && (denied == nil || !denied.IsError) {
		t.Fatalf("cross-namespace action unexpectedly succeeded: result=%+v err=%v", denied, deniedErr)
	}
	if len(tools.executed) != 1 {
		t.Fatalf("denied tool mutated: %+v", tools.executed)
	}
	if events.actions[len(events.actions)-1].Status != "denied" {
		t.Fatalf("denial lifecycle = %+v", events.actions)
	}
	evidenceJSON, _ := json.Marshal(events)
	if strings.Contains(string(evidenceJSON), access.Token) {
		t.Fatal("raw MCP grant token entered evidence")
	}
}

func TestGrantTokensAreRandomAndOnlyHashesAreStored(t *testing.T) {
	service, store, _, _, first := testService(t)
	store.mu.Lock()
	firstHash := append([]byte(nil), store.grant.TokenHash...)
	store.mu.Unlock()
	resources := json.RawMessage(`[]`)
	second, err := service.CreateGrant(context.Background(), Scope{
		WorkspaceID: "workspace-1", RunID: "run-1",
		Version:     agent.Version{ID: "version-1", ToolAccessMode: "mcp", Capabilities: []string{"no_op"}},
		Environment: observation.EnvironmentContext{Identity: kubeenv.RunIdentity{WorkspaceID: "workspace-1", BatchID: "batch-1", RunID: "run-1"}, Environment: kubeenv.Environment{RunID: "run-1", Namespace: "eval-run"}},
		AgentView:   scenario.AgentView{AllowedActions: []string{"no_op"}, AllowedResources: resources},
	})
	if err != nil {
		t.Fatal(err)
	}
	store.mu.Lock()
	secondHash := append([]byte(nil), store.grant.TokenHash...)
	store.mu.Unlock()
	if first.Token == second.Token || bytes.Equal(firstHash, secondHash) || bytes.Contains(secondHash, []byte(first.Token)) || bytes.Contains(secondHash, []byte(second.Token)) {
		t.Fatal("grant tokens or stored hashes were not independently generated")
	}
}

func TestWireDiscoverAndGrantRevocation(t *testing.T) {
	service, _, _, _, access := testService(t)
	server := httptest.NewServer(service.Handler())
	defer server.Close()
	body := `{"jsonrpc":"2.0","id":1,"method":"server/discover","params":{"_meta":{"io.modelcontextprotocol/protocolVersion":"2026-07-28","io.modelcontextprotocol/clientInfo":{"name":"wire-test","version":"v1"},"io.modelcontextprotocol/clientCapabilities":{}}}}`
	request, _ := http.NewRequest(http.MethodPost, server.URL, strings.NewReader(body))
	request.Header.Set("Authorization", "Bearer "+access.Token)
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Accept", "application/json, text/event-stream")
	request.Header.Set("Mcp-Protocol-Version", ProtocolVersion)
	request.Header.Set("Mcp-Method", "server/discover")
	response, err := http.DefaultClient.Do(request)
	if err != nil {
		t.Fatal(err)
	}
	raw, _ := io.ReadAll(response.Body)
	response.Body.Close()
	if response.StatusCode != http.StatusOK || !bytes.Contains(raw, []byte(ProtocolVersion)) || !bytes.Contains(raw, []byte("supportedVersions")) {
		t.Fatalf("wire discover status=%d body=%s", response.StatusCode, raw)
	}
	if err = service.RevokeRun(context.Background(), "run-1"); err != nil {
		t.Fatal(err)
	}
	request, _ = http.NewRequest(http.MethodPost, server.URL, strings.NewReader(body))
	request.Header.Set("Authorization", "Bearer "+access.Token)
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Accept", "application/json, text/event-stream")
	response, err = http.DefaultClient.Do(request)
	if err != nil {
		t.Fatal(err)
	}
	response.Body.Close()
	if response.StatusCode != http.StatusUnauthorized || service.debugScopeCount() != 0 {
		t.Fatalf("revoked grant status=%d scopes=%d", response.StatusCode, service.debugScopeCount())
	}
}

func TestMissingMalformedExpiredAndMismatchedGrantsFailClosed(t *testing.T) {
	service, store, _, _, access := testService(t)
	server := httptest.NewServer(service.Handler())
	defer server.Close()
	discover := `{"jsonrpc":"2.0","id":1,"method":"server/discover","params":{"_meta":{"io.modelcontextprotocol/protocolVersion":"2026-07-28","io.modelcontextprotocol/clientInfo":{"name":"negative","version":"v1"},"io.modelcontextprotocol/clientCapabilities":{}}}}`
	status := func(token string) int {
		request, _ := http.NewRequest(http.MethodPost, server.URL, strings.NewReader(discover))
		request.Header.Set("Content-Type", "application/json")
		request.Header.Set("Accept", "application/json, text/event-stream")
		if token != "" {
			request.Header.Set("Authorization", "Bearer "+token)
		}
		response, err := http.DefaultClient.Do(request)
		if err != nil {
			t.Fatal(err)
		}
		response.Body.Close()
		return response.StatusCode
	}
	if status("") != http.StatusUnauthorized || status("malformed") != http.StatusUnauthorized {
		t.Fatal("missing or malformed token accepted")
	}
	store.mu.Lock()
	store.grant.ExpiresAt = time.Now().Add(-time.Second)
	store.mu.Unlock()
	if status(access.Token) != http.StatusUnauthorized {
		t.Fatal("expired token accepted")
	}
	store.mu.Lock()
	store.grant.ExpiresAt = time.Now().Add(time.Minute)
	store.grant.AgentVersionID = "wrong-version"
	store.mu.Unlock()
	if status(access.Token) != http.StatusUnauthorized {
		t.Fatal("token with mismatched AgentVersion scope accepted")
	}
	store.mu.Lock()
	store.grant.AgentVersionID = "version-1"
	store.grant.WorkspaceID = "wrong-workspace"
	store.mu.Unlock()
	if status(access.Token) != http.StatusUnauthorized {
		t.Fatal("token with mismatched workspace scope accepted")
	}
	store.mu.Lock()
	store.grant.WorkspaceID = "workspace-1"
	store.active = false
	store.mu.Unlock()
	if status(access.Token) != http.StatusUnauthorized {
		t.Fatal("completed or stopped run token accepted")
	}
}

func TestCurrentProtocolPropagatesToolCancellation(t *testing.T) {
	service, _, tools, _, access := testService(t)
	tools.observeStarted = make(chan struct{})
	tools.blockObserve = true
	server := httptest.NewServer(service.Handler())
	defer server.Close()
	httpClient := &http.Client{Transport: bearerTransport{token: access.Token, base: http.DefaultTransport}}
	client := mcp.NewClient(&mcp.Implementation{Name: "cancel-test", Version: "v1"}, nil)
	session, err := client.Connect(context.Background(), &mcp.StreamableClientTransport{Endpoint: server.URL, HTTPClient: httpClient, DisableStandaloneSSE: true}, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer session.Close()
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() {
		_, callErr := session.CallTool(ctx, &mcp.CallToolParams{Name: ToolObserveWorkloads, Arguments: map[string]any{}})
		done <- callErr
	}()
	select {
	case <-tools.observeStarted:
		cancel()
	case <-time.After(2 * time.Second):
		t.Fatal("tool handler did not start")
	}
	select {
	case callErr := <-done:
		if callErr == nil {
			t.Fatal("cancelled tool call returned success")
		}
	case <-time.After(2 * time.Second):
		t.Fatal("request cancellation did not reach tool call")
	}
}

func TestEvaluatorOnlyDataNeverAppearsInMCP(t *testing.T) {
	service, _, _, _, access := testService(t)
	server := httptest.NewServer(service.Handler())
	defer server.Close()
	client := &http.Client{Transport: bearerTransport{token: access.Token, base: http.DefaultTransport}}
	sdk := mcp.NewClient(&mcp.Implementation{Name: "leak-test", Version: "v1"}, nil)
	session, err := sdk.Connect(context.Background(), &mcp.StreamableClientTransport{Endpoint: server.URL, HTTPClient: client, DisableStandaloneSSE: true}, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer session.Close()
	listed, _ := session.ListTools(context.Background(), nil)
	observed, _ := session.CallTool(context.Background(), &mcp.CallToolParams{Name: ToolObserveWorkloads, Arguments: map[string]any{}})
	serialized, _ := json.Marshal([]any{listed, observed})
	for _, forbidden := range []string{"traffic spike caused checkout saturation", "99.9", "criterion", "12345", "traffic_vus", "execution_spec", "forbidden behavior", "PASS", "FAIL"} {
		if strings.Contains(string(serialized), forbidden) {
			t.Fatalf("MCP response leaked %q: %s", forbidden, serialized)
		}
	}
}
