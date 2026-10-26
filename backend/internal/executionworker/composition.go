package executionworker

import (
	"context"
	"errors"
	"github.com/evalplane/evalplane/backend/internal/actionexec"
	"github.com/evalplane/evalplane/backend/internal/agentgateway"
	"github.com/evalplane/evalplane/backend/internal/auth"
	"github.com/evalplane/evalplane/backend/internal/billing"
	"github.com/evalplane/evalplane/backend/internal/environment"
	"github.com/evalplane/evalplane/backend/internal/evaluator"
	"github.com/evalplane/evalplane/backend/internal/evidence"
	"github.com/evalplane/evalplane/backend/internal/execution"
	"github.com/evalplane/evalplane/backend/internal/kubeenv"
	"github.com/evalplane/evalplane/backend/internal/mcpserver"
	"github.com/evalplane/evalplane/backend/internal/scenariorunner"
	"github.com/evalplane/evalplane/backend/internal/verification"
	"github.com/jackc/pgx/v5/pgxpool"
	"k8s.io/client-go/kubernetes"
	"net/http"
)

// NewPostgresWorker composes the existing services. It creates no cluster resources
// and starts no goroutines until Run is called. The caller supplies cluster clients,
// a deliberately selected instrumented workload image, and trusted connections.
func NewPostgresWorker(pool *pgxpool.Pool, client kubernetes.Interface, config Config, runtimeConfig scenariorunner.Config, resolve ConnectionResolver, onError func(string, error)) (*Worker, error) {
	return NewPostgresWorkerWithHTTPClient(pool, client, config, runtimeConfig, resolve, nil, onError)
}

func NewPostgresWorkerWithHTTPClient(pool *pgxpool.Pool, client kubernetes.Interface, config Config, runtimeConfig scenariorunner.Config, resolve ConnectionResolver, httpClient *http.Client, onError func(string, error)) (*Worker, error) {
	if pool == nil {
		return nil, errors.New("database pool is required")
	}
	if err := config.validate(); err != nil {
		return nil, err
	}
	runtime, err := NewKubernetesRuntime(client, runtimeConfig)
	if err != nil {
		return nil, err
	}
	actions, err := actionexec.New(client, runtime.environments, actionexec.DefaultConfig(), nil)
	if err != nil {
		return nil, err
	}
	roles := auth.NewRepository()
	billingService := billing.NewService(pool, billing.NewRepository(), roles, billing.FakeProvider{}, billing.Config{Provider: "fake"})
	evidenceService := evidence.NewService(pool, evidence.NewRepository(), roles)
	// Production-shaped run environments: ScenarioVersion requirements →
	// scheduler → run-owned EnvironmentInstance → provider provision → ready.
	// The worker depends on the provider abstraction; Kind remains merely the
	// current development provider implementation.
	envRepo := environment.NewRepository()
	var runEnvs RunEnvironments
	if manager, managerErr := kubeenv.NewManager(client); managerErr == nil {
		if kindProvider, providerErr := environment.NewKindDevProvider(manager); providerErr == nil {
			runEnvs = NewPostgresRunEnvironments(pool, envRepo, environment.DefaultAvailability(), map[string]environment.Provider{
				environment.ProviderKindDev: kindProvider,
			})
		}
	}
	dependencies := Dependencies{
		Lifecycle: execution.NewService(pool, execution.NewRepository(), roles),
		Evidence:  evidenceService,
		Evaluator: evaluator.NewService(evaluator.NewRepository(pool)),
		Runtime:   runtime, Actions: actions, Gateway: agentgateway.NewHTTPGateway(httpClient, nil), Connection: resolve, OnError: onError,
		Environments: runEnvs,
		Billing:      billingService,
		Verification: verificationProjector{service: verification.NewService(pool, verification.NewRepository(), roles)},
	}
	worker, err := New(config, dependencies)
	if err != nil {
		return nil, err
	}
	if config.MCPPublicEndpoint != "" {
		bridge := mcpBridge{runtime: runtime, actions: actions}
		service, serviceErr := mcpserver.NewService(mcpserver.NewRepository(pool), bridge, bridge, evidenceService, mcpserver.Config{
			PublicEndpoint: config.MCPPublicEndpoint,
			GrantTTL:       config.MCPGrantTTL,
			MaxToolCalls:   config.MCPMaxToolCalls,
		})
		if serviceErr != nil {
			return nil, serviceErr
		}
		worker.deps.MCP = service
		worker.mcpHandler = service.Handler()
	}
	return worker, nil
}

type verificationProjector struct{ service *verification.Service }

func (v verificationProjector) DeriveRun(ctx context.Context, workspaceID, runID string) error {
	_, err := v.service.DeriveRun(ctx, workspaceID, runID)
	return err
}

type mcpBridge struct {
	runtime Runtime
	actions Actions
}

func (b mcpBridge) Observe(ctx context.Context, scope mcpserver.Scope) (agentgateway.Observation, error) {
	return b.runtime.Observe(ctx, scope.Environment)
}

func (b mcpBridge) Authorize(ctx context.Context, scope mcpserver.Scope, action agentgateway.ProposedAction) actionexec.ActionResult {
	return b.actions.Authorize(ctx, scope.Environment, action)
}

func (b mcpBridge) Execute(ctx context.Context, scope mcpserver.Scope, action agentgateway.ProposedAction) actionexec.ActionResult {
	return b.actions.ExecuteAction(ctx, scope.Environment, action)
}
