// Package executionworker composes trusted run execution. Dependencies must honor
// context cancellation. Only this package's agent projection crosses the gateway.
package executionworker

import (
	"context"
	"errors"
	"github.com/evalplane/evalplane/backend/internal/actionexec"
	"github.com/evalplane/evalplane/backend/internal/agent"
	"github.com/evalplane/evalplane/backend/internal/agentgateway"
	"github.com/evalplane/evalplane/backend/internal/evaluator"
	"github.com/evalplane/evalplane/backend/internal/evidence"
	"github.com/evalplane/evalplane/backend/internal/execution"
	"github.com/evalplane/evalplane/backend/internal/gitops"
	"github.com/evalplane/evalplane/backend/internal/kubeenv"
	"github.com/evalplane/evalplane/backend/internal/mcpserver"
	"github.com/evalplane/evalplane/backend/internal/observation"
	"github.com/evalplane/evalplane/backend/internal/scenario"
	"github.com/google/uuid"
	"net/http"
	"strings"
	"time"
)

type Config struct {
	PollInterval      time.Duration
	MaxConcurrentRuns int
	RunTimeout        time.Duration
	AgentStepTimeout  time.Duration
	MaxAgentSteps     int
	StepInterval      time.Duration
	StopPollInterval  time.Duration
	FinalizeTimeout   time.Duration
	CleanupTimeout    time.Duration
	WorkerID          string
	LeaseDuration     time.Duration
	HeartbeatInterval time.Duration
	RecoveryInterval  time.Duration
	RecoveryGrace     time.Duration
	StabilityWindow   time.Duration
	StabilityInterval time.Duration
	MCPPublicEndpoint string
	MCPGrantTTL       time.Duration
	MCPMaxToolCalls   int
}

func DefaultConfig() Config {
	return Config{PollInterval: time.Second, MaxConcurrentRuns: 1, RunTimeout: 10 * time.Minute, AgentStepTimeout: 30 * time.Second, MaxAgentSteps: 8, StepInterval: 5 * time.Second, StopPollInterval: 250 * time.Millisecond, FinalizeTimeout: 30 * time.Second, CleanupTimeout: time.Minute, LeaseDuration: time.Minute, HeartbeatInterval: 15 * time.Second, RecoveryInterval: time.Minute, RecoveryGrace: 15 * time.Second, StabilityWindow: 15 * time.Second, StabilityInterval: 5 * time.Second, MCPGrantTTL: 10 * time.Minute, MCPMaxToolCalls: 64}
}
func (c Config) validate() error {
	if c.PollInterval <= 0 || c.MaxConcurrentRuns < 1 || c.MaxConcurrentRuns > 16 || c.RunTimeout <= 0 || c.AgentStepTimeout <= 0 || c.MaxAgentSteps < 1 || c.MaxAgentSteps > 100 || c.StepInterval <= 0 || c.StopPollInterval <= 0 || c.FinalizeTimeout <= 0 || c.CleanupTimeout <= 0 || c.LeaseDuration <= 0 || c.HeartbeatInterval <= 0 || c.HeartbeatInterval >= c.LeaseDuration || c.RecoveryInterval <= 0 || c.RecoveryGrace < 0 || c.StabilityWindow <= 0 || c.StabilityInterval <= 0 || c.StabilityInterval > c.StabilityWindow {
		return errors.New("worker limits must be positive; concurrency <=16 and steps <=100")
	}
	if c.MCPPublicEndpoint != "" && (c.MCPGrantTTL <= 0 || c.MCPMaxToolCalls < 1 || c.MCPMaxToolCalls > 1000) {
		return errors.New("MCP grant TTL and tool-call budget must be bounded")
	}
	if c.MCPPublicEndpoint != "" {
		if _, err := agentgateway.ValidateEndpoint(c.MCPPublicEndpoint); err != nil {
			return errors.New("MCP public endpoint must be an absolute HTTP(S) URL without credentials, query, or fragment")
		}
	}
	return nil
}

type Lifecycle interface {
	ClaimNextRunWithLease(context.Context, string, time.Duration) (execution.ExecutionRun, error)
	AcquireAbandonedRun(context.Context, string, time.Duration, time.Duration) (execution.AbandonedRun, error)
	RenewRunLease(context.Context, string, string, string, time.Duration) (bool, error)
	VerifyRunLease(context.Context, string, string, string) (bool, error)
	RunStatus(context.Context, string, string) (string, bool, error)
	LoadVersions(context.Context, string, string) (scenario.Version, agent.Version, error)
	MarkOwnedRunRunning(context.Context, string, string, string) error
	CompleteOwnedRun(context.Context, string, string) error
	FailOwnedRun(context.Context, string, string) error
	StopOwnedRun(context.Context, string, string) error
}
type Runtime interface {
	Provision(context.Context, kubeenv.RunIdentity) (kubeenv.Environment, error)
	Prepare(context.Context, observation.EnvironmentContext, scenario.Version, execution.HiddenVariant) error
	Start(context.Context, observation.EnvironmentContext, scenario.Version, execution.HiddenVariant) error
	Finish(context.Context, observation.EnvironmentContext) error
	Observe(context.Context, observation.EnvironmentContext) (agentgateway.Observation, error)
	Cleanup(context.Context, kubeenv.RunIdentity) error
}
type GitOpsObserver interface {
	ObserveGitOps(context.Context, observation.EnvironmentContext) (gitops.Evidence, error)
}
type Actions interface {
	Authorize(context.Context, observation.EnvironmentContext, agentgateway.ProposedAction) actionexec.ActionResult
	ExecuteAction(context.Context, observation.EnvironmentContext, agentgateway.ProposedAction) actionexec.ActionResult
}
type Evidence interface {
	AppendRunEvent(context.Context, evidence.AppendRunEventInput) (evidence.RunEvent, error)
	AppendRunEventOnce(context.Context, evidence.AppendRunEventInput) (evidence.RunEvent, error)
	RecordAgentAction(context.Context, evidence.RecordAgentActionInput) (evidence.AgentAction, error)
	UpdateAgentAction(context.Context, evidence.UpdateActionInput) error
}
type Evaluator interface {
	EvaluateRun(context.Context, string, string) (evaluator.EvaluationResult, error)
}
type MCPAccess interface {
	CreateGrant(context.Context, mcpserver.Scope) (mcpserver.Access, error)
	RevokeRun(context.Context, string) error
}
type BillingSettlement interface {
	SettlePersistedRun(context.Context, string, string) error
}
type VerificationProjector interface {
	DeriveRun(context.Context, string, string) error
}

// ConnectionResolver obtains secrets from trusted configuration, keyed to the
// pinned version. Its output is never included in evidence or agent request JSON.
type ConnectionResolver func(context.Context, string, agent.Version) (agentgateway.Connection, error)
type Dependencies struct {
	Lifecycle    Lifecycle
	Runtime      Runtime
	Actions      Actions
	Evidence     Evidence
	Evaluator    Evaluator
	Gateway      agentgateway.Gateway
	Connection   ConnectionResolver
	MCP          MCPAccess
	Billing      BillingSettlement
	Verification VerificationProjector
	Wait         func(context.Context, time.Duration) error
	Now          func() time.Time
	// Environments wires the Stage-29 run lifecycle (requirements →
	// scheduler → run-owned EnvironmentInstance → provider provision →
	// ready → cleanup). It depends on the environment service/provider
	// abstraction, never on Kind-specific orchestration. When nil the
	// worker falls back to direct runtime provisioning for backward
	// compatibility; production wiring must always set it.
	Environments RunEnvironments
	// OnError receives sanitized classification only, never arbitrary remote errors.
	OnError func(runID string, err error)
	// OnStage reports coarse execution boundaries without exposing run data.
	OnStage func(stage, status string)
	// OnFailure is for trusted diagnostics. Callers must redact error details before logging.
	OnFailure func(kind, stage string, err error)
}

// StageFailure identifies the operation that failed without changing its error.
type StageFailure struct {
	Stage string
	Err   error
}

func (e *StageFailure) Error() string { return e.Err.Error() }
func (e *StageFailure) Unwrap() error { return e.Err }

var ErrStopped = errors.New("run stopped")
var ErrLeaseLost = errors.New("worker lease lost")
var ErrUnsupported = errors.New("scenario unsupported by current runtime")

type Failure struct {
	Code, Class string
	Cause       error
}

func (e *Failure) Error() string { return e.Class + ": " + e.Code }
func (e *Failure) Unwrap() error { return e.Cause }
func platform(code string, err error) error {
	if err == nil {
		return nil
	}
	return &Failure{Code: code, Class: "platform", Cause: err}
}

type Worker struct {
	config     Config
	deps       Dependencies
	mcpHandler http.Handler
}

func (w *Worker) ID() string               { return w.config.WorkerID }
func (w *Worker) MCPHandler() http.Handler { return w.mcpHandler }

func New(config Config, deps Dependencies) (*Worker, error) {
	config.WorkerID = strings.TrimSpace(config.WorkerID)
	if config.WorkerID == "" {
		config.WorkerID = "worker-" + uuid.NewString()
	}
	if len(config.WorkerID) > 200 {
		return nil, errors.New("worker ID must be at most 200 characters")
	}
	if err := config.validate(); err != nil {
		return nil, err
	}
	if deps.Lifecycle == nil || deps.Runtime == nil || deps.Actions == nil || deps.Evidence == nil || deps.Evaluator == nil || deps.Gateway == nil || deps.Connection == nil {
		return nil, errors.New("all worker dependencies are required")
	}
	if g, ok := deps.Gateway.(*agentgateway.HTTPGateway); ok {
		deps.Gateway = g.WithoutEvidence()
	}
	return &Worker{config: config, deps: deps}, nil
}
