// Worker runs the bounded qualification lifecycle. Run in cluster so per-run
// Prometheus Service DNS is reachable. Persistent credentials are decrypted only
// by the worker; the optional mounted-file resolver remains a development fixture.
package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/evalplane/evalplane/backend/internal/agent"
	"github.com/evalplane/evalplane/backend/internal/agentconnection"
	"github.com/evalplane/evalplane/backend/internal/agentgateway"
	"github.com/evalplane/evalplane/backend/internal/auth"
	"github.com/evalplane/evalplane/backend/internal/database"
	"github.com/evalplane/evalplane/backend/internal/executionworker"
	"github.com/evalplane/evalplane/backend/internal/kubeenv"
	"github.com/evalplane/evalplane/backend/internal/scenariorunner"
)

type connection struct {
	WorkspaceID    string `json:"workspace_id"`
	AgentVersionID string `json:"agent_version_id"`
	Endpoint       string `json:"endpoint"`
	BearerToken    string `json:"bearer_token"`
}

func resolver(path string) (executionworker.ConnectionResolver, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, errors.New("cannot read agent connection file")
	}
	var entries []connection
	if json.Unmarshal(raw, &entries) != nil || len(entries) == 0 {
		return nil, errors.New("agent connection file must contain a nonempty JSON array")
	}
	connections := make(map[[2]string]agentgateway.Connection)
	for _, entry := range entries {
		key := [2]string{entry.WorkspaceID, entry.AgentVersionID}
		if key[0] == "" || key[1] == "" {
			return nil, errors.New("connections require workspace and exact agent version IDs")
		}
		if _, exists := connections[key]; exists {
			return nil, errors.New("duplicate pinned agent connection")
		}
		value := agentgateway.Connection{EndpointURL: entry.Endpoint, BearerToken: entry.BearerToken}
		if agentgateway.ValidateConnection(value) != nil {
			return nil, errors.New("invalid agent connection")
		}
		connections[key] = value
	}
	return func(_ context.Context, workspace string, version agent.Version) (agentgateway.Connection, error) {
		value, ok := connections[[2]string{workspace, version.ID}]
		if !ok {
			return agentgateway.Connection{}, errors.New("no connection for pinned agent version")
		}
		return value, nil
	}, nil
}

func run() error {
	cfg := executionworker.DefaultConfig()
	runtime := scenariorunner.DefaultConfig()
	var kubeconfig, connections string
	var mcpListen string
	flag.StringVar(&kubeconfig, "kubeconfig", "", "Explicit kubeconfig; otherwise use in-cluster configuration")
	flag.StringVar(&connections, "agent-connections", "", "Optional legacy development fixture with workspace/version-pinned HTTP connections")
	flag.StringVar(&runtime.ReferenceImage, "reference-image", "", "Required instrumented reference application image")
	flag.IntVar(&cfg.MaxConcurrentRuns, "concurrency", cfg.MaxConcurrentRuns, "Maximum concurrent runs")
	flag.IntVar(&cfg.MaxAgentSteps, "max-agent-steps", cfg.MaxAgentSteps, "Maximum decisions per run")
	flag.DurationVar(&cfg.RunTimeout, "run-timeout", cfg.RunTimeout, "Overall run deadline")
	flag.DurationVar(&cfg.AgentStepTimeout, "agent-timeout", cfg.AgentStepTimeout, "Gateway invocation deadline")
	flag.DurationVar(&cfg.StepInterval, "step-interval", cfg.StepInterval, "Delay between decisions")
	flag.StringVar(&cfg.WorkerID, "worker-id", cfg.WorkerID, "Optional stable worker identity for this process lifetime")
	flag.DurationVar(&cfg.LeaseDuration, "lease-duration", cfg.LeaseDuration, "Run ownership lease duration")
	flag.DurationVar(&cfg.HeartbeatInterval, "heartbeat-interval", cfg.HeartbeatInterval, "Active-run lease renewal interval")
	flag.DurationVar(&cfg.RecoveryInterval, "recovery-interval", cfg.RecoveryInterval, "Abandoned-run recovery sweep interval")
	flag.DurationVar(&cfg.RecoveryGrace, "recovery-grace", cfg.RecoveryGrace, "Additional grace after lease expiry")
	flag.StringVar(&cfg.MCPPublicEndpoint, "mcp-public-endpoint", os.Getenv("MCP_PUBLIC_ENDPOINT"), "Public Streamable HTTP MCP endpoint supplied to agents")
	flag.StringVar(&mcpListen, "mcp-listen", valueOrDefault("MCP_LISTEN_ADDRESS", ":8082"), "MCP HTTP listen address")
	flag.DurationVar(&cfg.MCPGrantTTL, "mcp-grant-ttl", cfg.MCPGrantTTL, "Maximum evaluation-scoped MCP grant lifetime")
	flag.IntVar(&cfg.MCPMaxToolCalls, "mcp-max-tool-calls", cfg.MCPMaxToolCalls, "Maximum tool calls per run grant")
	flag.Parse()
	if runtime.ReferenceImage == "" || os.Getenv("DATABASE_URL") == "" {
		return errors.New("reference-image and DATABASE_URL are required")
	}
	runtime.ReferenceInstrumented = true
	ctx, cancel := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer cancel()
	pool, err := database.NewPostgresPool(ctx, os.Getenv("DATABASE_URL"))
	if err != nil {
		return errors.New("worker database connection failed")
	}
	defer pool.Close()
	allowPrivate, err := strconv.ParseBool(valueOrDefault("ALLOW_PRIVATE_AGENT_ENDPOINTS", "false"))
	if err != nil {
		return errors.New("ALLOW_PRIVATE_AGENT_ENDPOINTS must be true or false")
	}
	if allowPrivate && valueOrDefault("ENVIRONMENT", "development") != "development" {
		return errors.New("private agent endpoints may be enabled only in development")
	}
	policy := agentgateway.EndpointPolicy{AllowPrivate: allowPrivate}
	credentialCipher, err := agentconnection.NewCipher(os.Getenv("AGENT_CREDENTIAL_ENCRYPTION_KEY"))
	if err != nil {
		return err
	}
	connectionService := agentconnection.NewService(pool, agentconnection.NewRepository(), auth.NewRepository(), credentialCipher, policy, cfg.AgentStepTimeout)
	var resolve executionworker.ConnectionResolver = connectionService.Resolve
	if connections != "" {
		resolve, err = resolver(connections)
		if err != nil {
			return err
		}
		slog.Warn("using legacy file-backed agent connections; development fixture only")
	}
	client, err := kubeenv.NewClientset(kubeconfig)
	if err != nil {
		return fmt.Errorf("Kubernetes configuration: %w", err)
	}
	httpClient := agentgateway.NewPolicyHTTPClient(policy, cfg.AgentStepTimeout)
	worker, err := executionworker.NewPostgresWorkerWithHTTPClient(pool, client, cfg, runtime, resolve, httpClient, func(run string, err error) { slog.Error("qualification execution error", "run_id", run, "error", err) })
	if err != nil {
		return err
	}
	slog.Info("qualification worker started", "concurrency", cfg.MaxConcurrentRuns, "worker_id", worker.ID())
	if worker.MCPHandler() == nil {
		return worker.Run(ctx)
	}
	server := &http.Server{Addr: mcpListen, Handler: worker.MCPHandler(), ReadHeaderTimeout: 5 * time.Second}
	workerErrors := make(chan error, 1)
	serverErrors := make(chan error, 1)
	go func() { workerErrors <- worker.Run(ctx) }()
	go func() { serverErrors <- server.ListenAndServe() }()
	var runErr error
	select {
	case runErr = <-workerErrors:
	case runErr = <-serverErrors:
		if errors.Is(runErr, http.ErrServerClosed) {
			runErr = nil
		}
	case <-ctx.Done():
	}
	cancel()
	shutdown, done := context.WithTimeout(context.Background(), 10*time.Second)
	defer done()
	shutdownErr := server.Shutdown(shutdown)
	return errors.Join(runErr, shutdownErr)
}

func valueOrDefault(name, fallback string) string {
	if value := strings.TrimSpace(os.Getenv(name)); value != "" {
		return value
	}
	return fallback
}

func main() {
	if err := run(); err != nil {
		slog.Error("worker stopped", "error", err)
		os.Exit(1)
	}
}
