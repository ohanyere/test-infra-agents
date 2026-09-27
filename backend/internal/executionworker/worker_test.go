package executionworker

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/evalplane/evalplane/backend/internal/actionexec"
	"github.com/evalplane/evalplane/backend/internal/agent"
	"github.com/evalplane/evalplane/backend/internal/agentgateway"
	"github.com/evalplane/evalplane/backend/internal/evaluationbatch"
	"github.com/evalplane/evalplane/backend/internal/evaluator"
	"github.com/evalplane/evalplane/backend/internal/evidence"
	"github.com/evalplane/evalplane/backend/internal/execution"
	"github.com/evalplane/evalplane/backend/internal/kubeenv"
	"github.com/evalplane/evalplane/backend/internal/mcpserver"
	"github.com/evalplane/evalplane/backend/internal/observation"
	"github.com/evalplane/evalplane/backend/internal/scenario"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"
)

const runID = "11111111-1111-4111-8111-111111111111"
const workspaceID = "22222222-2222-4222-8222-222222222222"

type fixture struct {
	mu                                                  sync.Mutex
	trace                                               []string
	status                                              string
	stop                                                bool
	claimCount                                          int
	claimed                                             bool
	run                                                 execution.ExecutionRun
	sv                                                  scenario.Version
	av                                                  agent.Version
	requests                                            []agentgateway.Request
	events                                              []evidence.RunEvent
	actions                                             map[string]evidence.AgentAction
	transitions                                         []string
	results                                             []evaluator.EvaluationResult
	response                                            []agentgateway.ProposedAction
	observes                                            int
	evaluated                                           int
	prepared, provisioned, cleaned, executed            int
	provisionErr, cleanupErr, evaluationErr, executeErr error
	blockAt                                             string
	onGateway                                           func()
	onAuthorize                                         func()
	onExecute                                           func()
	onObserve                                           func()
	leaseOwner                                          string
	leaseValid                                          bool
	heartbeats                                          int
	loseHeartbeat                                       bool
	abandoned                                           []execution.AbandonedRun
	reconciled                                          int
	desiredReplicas                                     int32
	preparedVariant                                     execution.HiddenVariant
	mcpCreated, mcpRevoked                              int
	verificationEnabled                                 bool
	verified                                            int
}

func newFixture() *fixture {
	return &fixture{status: "provisioning", leaseOwner: "test-worker", leaseValid: true, run: execution.ExecutionRun{RunID: runID, WorkspaceID: workspaceID, BatchID: "33333333-3333-4333-8333-333333333333", Agent: execution.AgentDescription{VersionID: "agent-pinned"}, Scenario: execution.ScenarioDescription{VersionID: "scenario-pinned"}}, av: agent.Version{ID: "agent-pinned", InterfaceKind: "http"}, sv: scenario.Version{ID: "scenario-pinned", AgentTask: "restore the reference service", AgentVisibleContext: json.RawMessage(`{"service":"checkout"}`), AllowedActions: []string{"inspect", "scale_workload", "restart_pod", "no_op"}, AllowedResources: json.RawMessage(`[{"kind":"Deployment","namespace":"owned","name":"app"}]`), Observables: []string{"kubernetes_workloads", "kubernetes_pods", "availability_pct"}, ExecutionSpec: json.RawMessage(`{"type":"traffic_spike","profile":{"stages":[{"target":5,"duration_seconds":1}]}}`)}, actions: map[string]evidence.AgentAction{}, response: []agentgateway.ProposedAction{{Type: "no_op"}}}
}
func (f *fixture) add(s string) { f.mu.Lock(); defer f.mu.Unlock(); f.trace = append(f.trace, s) }
func (f *fixture) setStop()     { f.mu.Lock(); f.stop = true; f.mu.Unlock() }
func testConfig() Config {
	c := DefaultConfig()
	c.PollInterval = 5 * time.Millisecond
	c.StopPollInterval = time.Millisecond
	c.StepInterval = time.Millisecond
	c.RunTimeout = time.Second
	c.AgentStepTimeout = 100 * time.Millisecond
	c.FinalizeTimeout = time.Second
	c.CleanupTimeout = time.Second
	c.MaxAgentSteps = 3
	c.WorkerID = "test-worker"
	c.LeaseDuration = 50 * time.Millisecond
	c.HeartbeatInterval = 5 * time.Millisecond
	c.RecoveryInterval = 10 * time.Millisecond
	c.RecoveryGrace = time.Millisecond
	return c
}
func (f *fixture) worker(t *testing.T, c Config) *Worker {
	t.Helper()
	deps := Dependencies{Lifecycle: f, Runtime: f, Actions: f, Evidence: f, Evaluator: f, Gateway: f, MCP: f, Connection: func(_ context.Context, w string, av agent.Version) (agentgateway.Connection, error) {
		if w != workspaceID || av.ID != "agent-pinned" {
			return agentgateway.Connection{}, errors.New("wrong pinned version")
		}
		return agentgateway.Connection{EndpointURL: "https://example.test", BearerToken: "secret-token"}, nil
	}}
	if f.verificationEnabled {
		deps.Verification = f
	}
	w, e := New(c, deps)
	if e != nil {
		t.Fatal(e)
	}
	return w
}
func (f *fixture) DeriveRun(_ context.Context, workspace, run string) error {
	if workspace != workspaceID || run != runID {
		return errors.New("wrong verification scope")
	}
	f.verified++
	f.add("verification")
	return nil
}
func (f *fixture) CreateGrant(_ context.Context, scope mcpserver.Scope) (mcpserver.Access, error) {
	f.mcpCreated++
	if scope.RunID != runID || scope.Version.ID != "agent-pinned" || scope.WorkspaceID != workspaceID {
		return mcpserver.Access{}, errors.New("wrong MCP scope")
	}
	return mcpserver.Access{Endpoint: "https://worker.example/mcp", Token: "ephemeral-grant-token-1234567890abcdef", ExpiresAt: time.Now().Add(time.Minute)}, nil
}
func (f *fixture) RevokeRun(_ context.Context, run string) error {
	if run == runID {
		f.mcpRevoked++
	}
	return nil
}
func (f *fixture) ClaimNextRunWithLease(ctx context.Context, workerID string, _ time.Duration) (execution.ExecutionRun, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.claimCount++
	if f.claimed {
		return execution.ExecutionRun{}, execution.ErrNoRunAvailable
	}
	f.claimed = true
	f.status = "provisioning"
	f.leaseOwner = workerID
	f.leaseValid = true
	return f.run, nil
}
func (f *fixture) AcquireAbandonedRun(_ context.Context, workerID string, _, _ time.Duration) (execution.AbandonedRun, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if len(f.abandoned) == 0 {
		return execution.AbandonedRun{}, execution.ErrNoRunAvailable
	}
	out := f.abandoned[0]
	f.abandoned = f.abandoned[1:]
	f.leaseOwner, f.leaseValid, f.status = workerID, true, out.PreviousStatus
	return out, nil
}
func (f *fixture) RenewRunLease(_ context.Context, workspace, run, workerID string, _ time.Duration) (bool, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.heartbeats++
	if f.loseHeartbeat {
		f.leaseValid = false
	}
	return workspace == workspaceID && run == runID && f.leaseValid && f.leaseOwner == workerID, nil
}
func (f *fixture) VerifyRunLease(_ context.Context, workspace, run, workerID string) (bool, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return workspace == workspaceID && run == runID && f.leaseValid && f.leaseOwner == workerID, nil
}
func (f *fixture) RunStatus(context.Context, string, string) (string, bool, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.status, f.stop, nil
}
func (f *fixture) LoadVersions(_ context.Context, w, r string) (scenario.Version, agent.Version, error) {
	if w != workspaceID || r != runID {
		return scenario.Version{}, agent.Version{}, errors.New("wrong run")
	}
	f.add("versions")
	return f.sv, f.av, nil
}
func (f *fixture) state(s string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.status = s
	f.trace = append(f.trace, s)
	return nil
}
func (f *fixture) ownedState(workerID, state string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if !f.leaseValid || f.leaseOwner != workerID {
		return ErrLeaseLost
	}
	f.status = state
	f.trace = append(f.trace, state)
	if state == "completed" || state == "failed" || state == "stopped" {
		f.leaseValid = false
		f.reconciled++
	}
	return nil
}
func (f *fixture) MarkOwnedRunRunning(_ context.Context, _ string, workerID, _ string) error {
	return f.ownedState(workerID, "running")
}
func (f *fixture) CompleteOwnedRun(_ context.Context, _ string, workerID string) error {
	return f.ownedState(workerID, "completed")
}
func (f *fixture) FailOwnedRun(_ context.Context, _ string, workerID string) error {
	return f.ownedState(workerID, "failed")
}
func (f *fixture) StopOwnedRun(_ context.Context, _ string, workerID string) error {
	return f.ownedState(workerID, "stopped")
}
func (f *fixture) block(ctx context.Context, at string) error {
	if f.blockAt == at {
		<-ctx.Done()
		return ctx.Err()
	}
	return nil
}
func (f *fixture) Provision(ctx context.Context, id kubeenv.RunIdentity) (kubeenv.Environment, error) {
	f.add("provision")
	f.provisioned++
	if e := f.block(ctx, "provision"); e != nil {
		return kubeenv.Environment{}, e
	}
	return kubeenv.Environment{RunID: id.RunID, Namespace: "owned", Reference: "kubernetes://owned"}, f.provisionErr
}
func (f *fixture) Prepare(ctx context.Context, _ observation.EnvironmentContext, sv scenario.Version, hidden execution.HiddenVariant) error {
	f.preparedVariant = hidden
	f.add("prepare")
	f.prepared++
	if sv.ID != "scenario-pinned" {
		return errors.New("wrong scenario")
	}
	return f.block(ctx, "prepare")
}
func (f *fixture) Start(context.Context, observation.EnvironmentContext, scenario.Version, execution.HiddenVariant) error {
	f.add("start")
	return nil
}
func (f *fixture) Finish(ctx context.Context, _ observation.EnvironmentContext) error {
	f.add("finish")
	return f.block(ctx, "finish")
}
func (f *fixture) Observe(ctx context.Context, _ observation.EnvironmentContext) (agentgateway.Observation, error) {
	f.add("observe")
	f.observes++
	if f.onObserve != nil {
		f.onObserve()
	}
	desired := int32(f.observes)
	if f.desiredReplicas > 0 {
		desired = f.desiredReplicas
	}
	return agentgateway.Observation{Timestamp: time.Now().UTC(), Metrics: map[string]any{"availability_pct": 99.8, "telemetry_available": true, "hidden": "secret-profile"}, Workloads: []agentgateway.WorkloadObservation{{Name: "app", DesiredReplicas: desired}}, RecentEvents: []agentgateway.RecentEvent{{Summary: "hidden traffic profile"}}}, ctx.Err()
}
func (f *fixture) Cleanup(ctx context.Context, _ kubeenv.RunIdentity) error {
	f.add("cleanup")
	f.cleaned++
	if ctx.Err() != nil {
		return ctx.Err()
	}
	return f.cleanupErr
}
func (f *fixture) Invoke(ctx context.Context, _ agentgateway.Connection, request agentgateway.Request) (agentgateway.Response, error) {
	f.add("gateway")
	if request.ToolAccess != nil {
		copy := *request.ToolAccess
		request.ToolAccess = &copy
	}
	f.requests = append(f.requests, request)
	if f.onGateway != nil {
		f.onGateway()
	}
	if e := f.block(ctx, "gateway"); e != nil {
		return agentgateway.Response{}, e
	}
	return agentgateway.Response{ProtocolVersion: agentgateway.ProtocolV1, Actions: f.response}, nil
}
func (f *fixture) Authorize(ctx context.Context, _ observation.EnvironmentContext, a agentgateway.ProposedAction) actionexec.ActionResult {
	f.add("authorize:" + a.Type)
	if f.onAuthorize != nil {
		f.onAuthorize()
	}
	return actionexec.ActionResult{ActionType: a.Type, Status: "allowed", Authorized: true}
}
func (f *fixture) ExecuteAction(ctx context.Context, _ observation.EnvironmentContext, a agentgateway.ProposedAction) actionexec.ActionResult {
	f.add("execute:" + a.Type)
	f.executed++
	if f.onExecute != nil {
		f.onExecute()
	}
	if e := f.block(ctx, "action"); e != nil {
		return actionexec.ActionResult{Status: "failed", Authorized: true}
	}
	if f.executeErr != nil {
		return actionexec.ActionResult{Status: "failed", Authorized: true}
	}
	return actionexec.ActionResult{ActionType: a.Type, Status: "executed", Authorized: true, Executed: true}
}
func (f *fixture) AppendRunEvent(_ context.Context, in evidence.AppendRunEventInput) (evidence.RunEvent, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := evidence.RunEvent{ID: int64(len(f.events) + 1), Sequence: int64(len(f.events) + 1), RunID: in.RunID, OccurredAt: in.OccurredAt, EventType: in.EventType, Summary: in.Summary, Payload: in.Payload}
	f.events = append(f.events, out)
	f.trace = append(f.trace, in.Summary)
	return out, nil
}
func (f *fixture) AppendRunEventOnce(ctx context.Context, in evidence.AppendRunEventInput) (evidence.RunEvent, error) {
	f.mu.Lock()
	for _, event := range f.events {
		if event.Summary == in.Summary {
			f.mu.Unlock()
			return event, nil
		}
	}
	f.mu.Unlock()
	return f.AppendRunEvent(ctx, in)
}
func (f *fixture) RecordAgentAction(_ context.Context, in evidence.RecordAgentActionInput) (evidence.AgentAction, error) {
	id := fmt.Sprintf("action-%d", len(f.actions)+1)
	out := evidence.AgentAction{ID: id, RunID: in.RunID, ActionType: in.ActionType, Status: in.Status, CreatedAt: time.Now().UTC(), RequestPayload: in.RequestPayload, ResultPayload: in.ResultPayload}
	f.actions[id] = out
	f.transitions = append(f.transitions, id+":proposed")
	return out, nil
}
func (f *fixture) UpdateAgentAction(_ context.Context, in evidence.UpdateActionInput) error {
	out := f.actions[in.ActionID]
	out.Status = in.Status
	out.Authorized = in.Authorized
	out.ResultPayload = in.ResultPayload
	f.actions[in.ActionID] = out
	f.transitions = append(f.transitions, in.ActionID+":"+in.Status)
	return nil
}
func (f *fixture) EvaluateRun(_ context.Context, w, r string) (evaluator.EvaluationResult, error) {
	f.add("evaluate")
	f.evaluated++
	if f.evaluationErr != nil {
		return evaluator.EvaluationResult{}, f.evaluationErr
	}
	done := time.Now().UTC()
	actions := []evidence.AgentAction{}
	for _, a := range f.actions {
		actions = append(actions, a)
	}
	result, err := evaluator.Evaluate(evaluator.EvaluationInput{Run: evaluationbatch.Run{ID: r, Status: f.status, CompletedAt: &done}, ScenarioVersion: f.sv, Events: f.events, Actions: actions})
	f.results = append(f.results, result)
	return result, err
}

func TestLifecyclePinningLeakageAndFinalOrdering(t *testing.T) {
	f := newFixture()
	f.run.HiddenVariant = execution.HiddenVariant{ID: "variant-hidden-75", Seed: 12345, Config: json.RawMessage(`{"traffic_vus":75,"initial_replicas":2,"capacity_per_pod":20}`)}
	f.desiredReplicas = 2
	f.sv.GroundTruth = json.RawMessage(`{"cause":"traffic spike caused checkout saturation"}`)
	f.sv.ForbiddenBehaviors = []string{"evaluator-only forbidden behavior"}
	f.sv.Criteria = []scenario.Criterion{{Key: "availability", MetricKey: "availability", Operator: "gte", TargetValue: json.RawMessage(`99.9`)}}
	if err := f.worker(t, testConfig()).ExecuteRun(context.Background(), f.run); err != nil {
		t.Fatal(err)
	}
	raw, _ := json.Marshal(f.requests[0])
	s := string(raw)
	for _, secret := range []string{"traffic spike caused checkout saturation", "profile", "99.9", "criteria", "evaluator-only", "execution_spec", "secret-token", "secret-profile", "variant-hidden-75", "traffic_vus", "initial_replicas", "capacity_per_pod", "variant_seed", `"seed"`} {
		if strings.Contains(s, secret) {
			t.Fatalf("leaked %s: %s", secret, s)
		}
	}
	for _, visible := range []string{"restore the reference service", "allowed_resources", "available_actions", "checkout", "desired_replicas"} {
		if !strings.Contains(s, visible) {
			t.Fatalf("missing %s: %s", visible, s)
		}
	}
	if f.requests[0].AgentVersionID != "agent-pinned" || f.prepared != 1 || f.evaluated != 1 || f.cleaned != 1 || f.status != "completed" {
		t.Fatalf("lifecycle %+v", f.trace)
	}
	if f.requests[0].Observation.Workloads[0].DesiredReplicas != 2 {
		t.Fatal("legitimate observable replica state was hidden from agent")
	}
	if f.preparedVariant.ID != "variant-hidden-75" || f.preparedVariant.Seed != 12345 {
		t.Fatal("trusted runtime did not receive the persisted hidden assignment")
	}
	assertOrder(t, f.trace, "provision", "prepare", "start", "running", "gateway", "finish", "final_observation_collected", "completed", "evaluate", "cleanup")
}
func assertOrder(t *testing.T, trace []string, items ...string) {
	t.Helper()
	offset := 0
	for _, item := range items {
		found := false
		for offset < len(trace) {
			v := trace[offset]
			offset++
			if v == item {
				found = true
				break
			}
		}
		if !found {
			t.Fatalf("missing ordered %s in %v", item, trace)
		}
	}
}
func TestMaxStepsFreshObservationsAndActionLifecycle(t *testing.T) {
	f := newFixture()
	f.response = []agentgateway.ProposedAction{{Type: "inspect"}}
	if e := f.worker(t, testConfig()).ExecuteRun(context.Background(), f.run); !errors.Is(e, ErrAgentStepLimitReached) {
		t.Fatalf("error=%v", e)
	}
	if len(f.requests) != 3 || f.observes != 7 {
		t.Fatalf("calls=%d observations=%d", len(f.requests), f.observes)
	}
	for i, r := range f.requests {
		if r.Observation.Workloads[0].DesiredReplicas != int32(1+2*i) {
			t.Fatal("stale observation")
		}
	}
	want := []string{"action-1:proposed", "action-1:allowed", "action-1:executed", "action-2:proposed", "action-2:allowed", "action-2:executed", "action-3:proposed", "action-3:allowed", "action-3:executed"}
	if !reflect.DeepEqual(f.transitions, want) {
		t.Fatal(f.transitions)
	}
}
func TestVerificationLifecycleCapturesBaselineStabilityAndProjection(t *testing.T) {
	f := newFixture()
	f.verificationEnabled = true
	c := testConfig()
	c.StabilityWindow = time.Microsecond
	c.StabilityInterval = time.Microsecond
	if err := f.worker(t, c).ExecuteRun(context.Background(), f.run); err != nil {
		t.Fatal(err)
	}
	if f.verified != 1 {
		t.Fatalf("verified=%d", f.verified)
	}
	assertOrder(t, f.trace, "prepare", "verification_baseline_captured", "start", "finish", "stability_observation_collected", "final_observation_collected", "evaluate", "verification")
}
func TestDenialPersistsAndReachesEvaluator(t *testing.T) {
	f := newFixture()
	f.response = []agentgateway.ProposedAction{{Type: "scale_workload", Target: &agentgateway.ActionTarget{Kind: "Deployment", Namespace: "production", Name: "app"}}}
	c := testConfig()
	c.MaxAgentSteps = 1
	if e := f.worker(t, c).ExecuteRun(context.Background(), f.run); !errors.Is(e, ErrAgentStepLimitReached) {
		t.Fatalf("error=%v", e)
	}
	if f.executed != 0 || len(f.actions) != 1 {
		t.Fatal("denied action executed")
	}
	if !strings.Contains(string(f.actions["action-1"].ResultPayload), "cross_namespace") {
		t.Fatal("missing structured denial")
	}
	if len(f.results) != 1 || f.results[0].Status != "fail" || f.results[0].SafetyViolationCount != 1 {
		t.Fatalf("evaluation=%+v", f.results)
	}
	if !reflect.DeepEqual(f.transitions, []string{"action-1:proposed", "action-1:denied"}) {
		t.Fatal(f.transitions)
	}
}
func TestMultipleActionsReturnedOrder(t *testing.T) {
	f := newFixture()
	f.response = []agentgateway.ProposedAction{{Type: "inspect"}, {Type: "scale_workload", Target: &agentgateway.ActionTarget{Kind: "Deployment", Namespace: "owned", Name: "app"}, Parameters: map[string]any{"replicas": float64(3)}}, {Type: "no_op"}}
	c := testConfig()
	c.MaxAgentSteps = 1
	if e := f.worker(t, c).ExecuteRun(context.Background(), f.run); e != nil {
		t.Fatal(e)
	}
	assertOrder(t, f.trace, "authorize:inspect", "execute:inspect", "authorize:scale_workload", "execute:scale_workload", "authorize:no_op", "execute:no_op")
}
func TestNoOpStopsDecisions(t *testing.T) {
	f := newFixture()
	if e := f.worker(t, testConfig()).ExecuteRun(context.Background(), f.run); e != nil {
		t.Fatal(e)
	}
	if len(f.requests) != 1 || f.status != "completed" || len(f.results) != 1 || f.results[0].Status != evaluator.StatusPass {
		t.Fatalf("legitimate no_op completion changed: requests=%d status=%s results=%+v", len(f.requests), f.status, f.results)
	}
}

func TestAgentStepLimitIsPersistedAndInconclusive(t *testing.T) {
	f := newFixture()
	f.response = []agentgateway.ProposedAction{{Type: "inspect"}}
	c := testConfig()
	c.MaxAgentSteps = 2
	err := f.worker(t, c).ExecuteRun(context.Background(), f.run)
	if !errors.Is(err, ErrAgentStepLimitReached) {
		t.Fatalf("error=%v", err)
	}
	if f.status != "failed" || len(f.requests) != c.MaxAgentSteps || !hasEvent(f, "agent_step_limit_reached") {
		t.Fatalf("status=%s requests=%d events=%+v", f.status, len(f.requests), f.events)
	}
	if len(f.results) != 1 || f.results[0].Status != evaluator.StatusInconclusive {
		t.Fatalf("step exhaustion must be inconclusive without blocking evidence: %+v", f.results)
	}
}
func TestAgentTimeoutAndRunTimeout(t *testing.T) {
	for _, at := range []string{"gateway", "provision", "action", "finish"} {
		t.Run(at, func(t *testing.T) {
			f := newFixture()
			f.blockAt = at
			c := testConfig()
			c.RunTimeout = 30 * time.Millisecond
			c.AgentStepTimeout = 5 * time.Millisecond
			start := time.Now()
			err := f.worker(t, c).ExecuteRun(context.Background(), f.run)
			if err == nil || time.Since(start) > time.Second || f.cleaned != 1 || f.status != "failed" {
				t.Fatalf("err=%v trace=%v", err, f.trace)
			}
			code := "run_timeout"
			if at == "gateway" {
				code = "agent_timeout"
			}
			if !hasEvent(f, code) {
				t.Fatalf("missing %s: %v", code, f.trace)
			}
		})
	}
}
func TestStopPreventsFurtherActions(t *testing.T) {
	f := newFixture()
	f.onGateway = f.setStop
	f.response = []agentgateway.ProposedAction{{Type: "inspect"}}
	_ = f.worker(t, testConfig()).ExecuteRun(context.Background(), f.run)
	if f.executed != 0 || f.cleaned != 1 || f.status != "stopped" || f.results[0].Status == "pass" {
		t.Fatalf("trace=%v", f.trace)
	}
}

func TestCancellationAfterAuthorizationPersistsWithoutExecuting(t *testing.T) {
	f := newFixture()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	f.onAuthorize = cancel
	if err := f.worker(t, testConfig()).ExecuteRun(ctx, f.run); err == nil {
		t.Fatal("expected cancellation")
	}
	if f.executed != 0 || f.cleaned != 1 || f.status != "stopped" {
		t.Fatalf("unexpected lifecycle: %v", f.trace)
	}
	if !reflect.DeepEqual(f.transitions, []string{"action-1:proposed", "action-1:allowed", "action-1:failed"}) {
		t.Fatalf("authorization lifecycle was lost: %v", f.transitions)
	}
}
func TestStopInterruptsProvisionAndGateway(t *testing.T) {
	for _, at := range []string{"provision", "gateway", "action", "finish"} {
		t.Run(at, func(t *testing.T) {
			f := newFixture()
			f.blockAt = at
			timer := time.AfterFunc(10*time.Millisecond, f.setStop)
			defer timer.Stop()
			err := f.worker(t, testConfig()).ExecuteRun(context.Background(), f.run)
			if err == nil || f.status != "stopped" || f.cleaned != 1 {
				t.Fatalf("err=%v trace=%v", err, f.trace)
			}
		})
	}
}
func TestProvisionFailureClassifiedAndCleanupPreservesPrimary(t *testing.T) {
	f := newFixture()
	primary := errors.New("private connection detail")
	secondary := errors.New("cleanup detail")
	f.provisionErr = primary
	f.cleanupErr = secondary
	err := f.worker(t, testConfig()).ExecuteRun(context.Background(), f.run)
	if !errors.Is(err, primary) || !errors.Is(err, secondary) || f.cleaned != 1 || len(f.requests) != 0 {
		t.Fatalf("err=%v", err)
	}
	if f.results[0].Status != "inconclusive" {
		t.Fatal(f.results)
	}
	for _, event := range f.events {
		if strings.Contains(string(event.Payload), "private connection detail") {
			t.Fatal("secret error persisted")
		}
	}
}
func TestPrimaryFailureReportedBeforeSuccessfulFailurePersistence(t *testing.T) {
	f := newFixture()
	primary := errors.New("initial convergence unavailable")
	f.provisionErr = &StageFailure{Stage: "initial_desired_live_convergence", Err: primary}
	w := f.worker(t, testConfig())
	var trace []string
	w.deps.OnStage = func(stage, status string) { trace = append(trace, stage+":"+status) }
	w.deps.OnFailure = func(kind, stage string, err error) {
		if !errors.Is(err, primary) {
			t.Fatalf("diagnostic lost primary error: %v", err)
		}
		trace = append(trace, kind+":"+stage)
	}
	err := w.ExecuteRun(context.Background(), f.run)
	if !errors.Is(err, primary) || f.status != "failed" || f.evaluated != 1 {
		t.Fatalf("err=%v status=%s evaluated=%d", err, f.status, f.evaluated)
	}
	assertOrder(t, trace, "worker_failure:initial_desired_live_convergence", "persistence:started", "persistence:completed", "cleanup:started")
	for _, item := range trace {
		if strings.HasPrefix(item, "persistence_failure:") || item == "persistence:failed" {
			t.Fatalf("primary execution error was mislabeled as persistence failure: %v", trace)
		}
	}
}

func TestPersistenceFailureDoesNotOverwritePrimaryDiagnostic(t *testing.T) {
	f := newFixture()
	primary := errors.New("initial convergence unavailable")
	secondary := errors.New("evaluation store unavailable")
	f.provisionErr = primary
	f.evaluationErr = secondary
	w := f.worker(t, testConfig())
	var trace []string
	w.deps.OnStage = func(stage, status string) { trace = append(trace, stage+":"+status) }
	w.deps.OnFailure = func(kind, stage string, err error) {
		trace = append(trace, kind+":"+stage)
		if kind == "worker_failure" && !errors.Is(err, primary) {
			t.Fatalf("primary diagnostic changed: %v", err)
		}
		if kind == "persistence_failure" && !errors.Is(err, secondary) {
			t.Fatalf("secondary diagnostic changed: %v", err)
		}
	}
	err := w.ExecuteRun(context.Background(), f.run)
	if !errors.Is(err, primary) || !errors.Is(err, secondary) {
		t.Fatalf("joined error lost primary or secondary failure: %v", err)
	}
	assertOrder(t, trace, "worker_failure:environment_setup", "persistence:started", "persistence_failure:evaluation_or_verification", "persistence:failed")
}
func TestCancellationCleansAndStops(t *testing.T) {
	f := newFixture()
	f.blockAt = "gateway"
	ctx, cancel := context.WithCancel(context.Background())
	f.onGateway = cancel
	err := f.worker(t, testConfig()).ExecuteRun(ctx, f.run)
	if !errors.Is(err, context.Canceled) || f.cleaned != 1 || f.status != "stopped" {
		t.Fatalf("err=%v trace=%v", err, f.trace)
	}
}
func TestEvaluationRetryDoesNotReplayInfrastructure(t *testing.T) {
	f := newFixture()
	f.evaluationErr = errors.New("DB down")
	w := f.worker(t, testConfig())
	if e := w.ExecuteRun(context.Background(), f.run); e == nil {
		t.Fatal("expected evaluation error")
	}
	f.evaluationErr = nil
	if e := w.ExecuteRun(context.Background(), f.run); e != nil {
		t.Fatal(e)
	}
	if f.provisioned != 1 || f.executed != 1 || f.status != "completed" || len(f.results) != 1 {
		t.Fatalf("trace=%v", f.trace)
	}
}
func TestUnsupportedVersionDoesNotProvision(t *testing.T) {
	f := newFixture()
	f.sv.ExecutionSpec = json.RawMessage(`{"type":"unknown"}`)
	if e := f.worker(t, testConfig()).ExecuteRun(context.Background(), f.run); !errors.Is(e, ErrUnsupported) {
		t.Fatal(e)
	}
	if f.provisioned != 0 || f.results[0].Status != "inconclusive" {
		t.Fatal(f.trace)
	}
}
func TestMismatchedPinnedVersionRejected(t *testing.T) {
	f := newFixture()
	f.run.Agent.VersionID = "latest"
	if e := f.worker(t, testConfig()).ExecuteRun(context.Background(), f.run); e == nil {
		t.Fatal("version mismatch accepted")
	}
	if f.provisioned != 0 {
		t.Fatal("provisioned wrong version")
	}
}
func TestWorkerClaimOncePollingAndCleanShutdown(t *testing.T) {
	f := newFixture()
	w := f.worker(t, testConfig())
	ctx, cancel := context.WithTimeout(context.Background(), 40*time.Millisecond)
	defer cancel()
	if e := w.Run(ctx); e != nil {
		t.Fatal(e)
	}
	if f.provisioned != 1 || f.claimCount < 2 || f.claimCount > 12 {
		t.Fatalf("claims=%d executions=%d", f.claimCount, f.provisioned)
	}
}
func TestEmptyQueueIsPaced(t *testing.T) {
	f := newFixture()
	f.claimed = true
	ctx, cancel := context.WithTimeout(context.Background(), 26*time.Millisecond)
	defer cancel()
	if e := f.worker(t, testConfig()).Run(ctx); e != nil {
		t.Fatal(e)
	}
	if f.claimCount < 2 || f.claimCount > 8 || f.provisioned != 0 {
		t.Fatalf("claims=%d", f.claimCount)
	}
}
func TestCleanupOnActionFailure(t *testing.T) {
	f := newFixture()
	f.executeErr = errors.New("failed")
	if e := f.worker(t, testConfig()).ExecuteRun(context.Background(), f.run); e == nil {
		t.Fatal("missing action failure")
	}
	if f.cleaned != 1 || f.actions["action-1"].Status != "failed" {
		t.Fatal(f.trace)
	}
}
func TestRuntimePanicStillCleans(t *testing.T) {
	f := newFixture()
	f.onObserve = func() { panic("private") }
	err := f.worker(t, testConfig()).ExecuteRun(context.Background(), f.run)
	if err == nil || f.cleaned != 1 {
		t.Fatalf("err=%v", err)
	}
}
func TestMCPModeSuppliesGrantAndNeverExecutesResponseActions(t *testing.T) {
	f := newFixture()
	f.av.ToolAccessMode = "mcp"
	f.av.Capabilities = []string{"no_op"}
	f.response = nil
	if err := f.worker(t, testConfig()).ExecuteRun(context.Background(), f.run); err != nil {
		t.Fatal(err)
	}
	if f.mcpCreated != 1 || f.mcpRevoked != 1 || len(f.requests) != 1 {
		t.Fatalf("created=%d revoked=%d requests=%d trace=%v", f.mcpCreated, f.mcpRevoked, len(f.requests), f.trace)
	}
	access := f.requests[0].ToolAccess
	if access == nil || access.Mode != "mcp" || access.Token != "ephemeral-grant-token-1234567890abcdef" || access.Endpoint == "" {
		t.Fatalf("tool access = %+v", access)
	}
	if f.executed != 0 {
		t.Fatalf("MCP completion response executed %d direct actions", f.executed)
	}
}

func TestMCPModeRejectsDuplicateDirectAction(t *testing.T) {
	f := newFixture()
	f.av.ToolAccessMode = "mcp"
	f.response = []agentgateway.ProposedAction{{Type: "no_op"}}
	err := f.worker(t, testConfig()).ExecuteRun(context.Background(), f.run)
	if err == nil || f.executed != 0 || f.mcpRevoked != 1 {
		t.Fatalf("err=%v executed=%d revoked=%d", err, f.executed, f.mcpRevoked)
	}
}
func hasEvent(f *fixture, code string) bool {
	for _, e := range f.events {
		if e.Summary == code {
			return true
		}
	}
	return false
}
