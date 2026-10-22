package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/evalplane/evalplane/backend/internal/agent"
	"github.com/evalplane/evalplane/backend/internal/agentconnection"
	"github.com/evalplane/evalplane/backend/internal/agentgateway"
	"github.com/evalplane/evalplane/backend/internal/agentregression"
	"github.com/evalplane/evalplane/backend/internal/auth"
	"github.com/evalplane/evalplane/backend/internal/billing"
	"github.com/evalplane/evalplane/backend/internal/config"
	"github.com/evalplane/evalplane/backend/internal/coverage"
	"github.com/evalplane/evalplane/backend/internal/customerattribution"
	"github.com/evalplane/evalplane/backend/internal/customercollector"
	"github.com/evalplane/evalplane/backend/internal/database"
	"github.com/evalplane/evalplane/backend/internal/environment"
	"github.com/evalplane/evalplane/backend/internal/evaluationbatch"
	"github.com/evalplane/evalplane/backend/internal/evaluationsuite"
	"github.com/evalplane/evalplane/backend/internal/evaluator"
	"github.com/evalplane/evalplane/backend/internal/evidence"
	"github.com/evalplane/evalplane/backend/internal/execution"
	"github.com/evalplane/evalplane/backend/internal/failuretaxonomy"
	"github.com/evalplane/evalplane/backend/internal/httpapi"
	"github.com/evalplane/evalplane/backend/internal/identity"
	"github.com/evalplane/evalplane/backend/internal/incidentsource"
	"github.com/evalplane/evalplane/backend/internal/operational"
	"github.com/evalplane/evalplane/backend/internal/qualification"
	"github.com/evalplane/evalplane/backend/internal/qualplan"
	"github.com/evalplane/evalplane/backend/internal/reliability"
	"github.com/evalplane/evalplane/backend/internal/scenario"
	"github.com/evalplane/evalplane/backend/internal/scenarioauthoring"
	"github.com/evalplane/evalplane/backend/internal/scenariocertification"
	"github.com/evalplane/evalplane/backend/internal/trajectory"
	"github.com/evalplane/evalplane/backend/internal/verification"
	"github.com/jackc/pgx/v5/pgxpool"
)

const (
	readHeaderTimeout = 5 * time.Second
	readTimeout       = 15 * time.Second
	writeTimeout      = 15 * time.Second
	idleTimeout       = 60 * time.Second
	shutdownTimeout   = 10 * time.Second
)

func main() {
	if err := run(); err != nil {
		slog.Error("API stopped with an error", "error", err)
		os.Exit(1)
	}
}

func run() error {
	cfg, err := config.Load()
	if err != nil {
		return fmt.Errorf("load configuration: %w", err)
	}

	logger := newLogger(cfg.LogLevel)
	slog.SetDefault(logger)

	rootCtx, stopSignals := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stopSignals()

	pool, err := database.NewPostgresPool(rootCtx, cfg.DatabaseURL)
	if err != nil {
		return fmt.Errorf("connect to PostgreSQL: %w", err)
	}
	defer pool.Close()

	identityService := identity.NewService(pool, identity.NewRepository())
	identityRepository := identity.NewRepository()
	authService, err := auth.NewService(pool, auth.NewRepository(), identityRepository, cfg.SessionTTL, auth.DefaultPasswordParams)
	if err != nil {
		return fmt.Errorf("initialize authentication: %w", err)
	}
	agentService := agent.NewService(pool, agent.NewRepository(), auth.NewRepository())
	credentialCipher, err := agentconnection.NewCipher(cfg.AgentCredentialEncryptionKey)
	if err != nil {
		return fmt.Errorf("initialize agent credentials: %w", err)
	}
	agentConnectionService := agentconnection.NewService(pool, agentconnection.NewRepository(), auth.NewRepository(), credentialCipher, agentgateway.EndpointPolicy{AllowPrivate: cfg.AllowPrivateAgentEndpoints}, 10*time.Second)
	scenarioService := scenario.NewService(pool, scenario.NewRepository(), auth.NewRepository())
	var draftGenerator scenarioauthoring.ScenarioDraftGenerator
	openAIKey := strings.TrimSpace(os.Getenv("OPENAI_API_KEY"))
	openAIModel := strings.TrimSpace(os.Getenv("SCENARIO_LLM_MODEL"))
	if openAIKey != "" || openAIModel != "" {
		generator, generatorErr := scenarioauthoring.NewOpenAIGenerator(scenarioauthoring.OpenAIConfig{APIKey: openAIKey, Model: openAIModel})
		if generatorErr != nil {
			return fmt.Errorf("initialize scenario authoring: %w", generatorErr)
		}
		draftGenerator = generator
	}
	scenarioAuthoringService := scenarioauthoring.NewService(draftGenerator, pool, auth.NewRepository())
	incidentSourceService := incidentsource.NewService(pool, incidentsource.NewRepository(pool), auth.NewRepository(), credentialCipher, incidentsource.NewFactory(), scenarioAuthoringService)
	evaluationBatchService := evaluationbatch.NewService(pool, evaluationbatch.NewRepository(), auth.NewRepository())
	evaluationSuiteService := evaluationsuite.NewService(pool, evaluationsuite.NewRepository(), auth.NewRepository(), evaluationbatch.NewRepository())
	billingService := billing.NewService(pool, billing.NewRepository(), auth.NewRepository(), billing.FakeProvider{}, billing.Config{Provider: "fake", WebhookSecret: strings.TrimSpace(os.Getenv("BILLING_WEBHOOK_SECRET"))})
	evaluationSuiteService.SetExecutionBillingGate(billingService)
	agentRegressionService := agentregression.NewService(pool, agentregression.NewRepository(), auth.NewRepository(), evaluationSuiteService)
	failureTaxonomyService := failuretaxonomy.NewService(pool, failuretaxonomy.NewRepository(), auth.NewRepository())
	qualificationService := qualification.NewService(pool, qualification.NewRepository(), auth.NewRepository(), evaluationSuiteService, agentRegressionService, failureTaxonomyService)
	evidenceService := evidence.NewService(pool, evidence.NewRepository(), auth.NewRepository())
	verificationService := verification.NewService(pool, verification.NewRepository(), auth.NewRepository())
	operationalService := operational.NewService(pool, auth.NewRepository(), verificationService)
	canonicalEvaluator := evaluator.NewService(evaluator.NewRepository(pool))
	collectorFinalizer := customerCollectorFinalizer{evaluator: canonicalEvaluator, verifier: verificationService}
	customerCollectorService, err := customercollector.NewService(pool, auth.NewRepository(), collectorFinalizer)
	if err != nil {
		return fmt.Errorf("initialize customer Kubernetes collector: %w", err)
	}
	customerAttributionService := customerattribution.NewService(pool, auth.NewRepository())
	trajectoryService := trajectory.NewService(pool, trajectory.NewRepository(), auth.NewRepository())
	executionService := execution.NewService(pool, execution.NewRepository(), auth.NewRepository())
	coverageService := coverage.NewService(pool, coverage.NewRepository(), auth.NewRepository(), &agentClaimAdapter{service: agentService}, &executionMembership{pool: pool})
	reliabilityService := reliability.NewService(pool, reliability.NewRepository(), auth.NewRepository(), &reliabilityClaimAdapter{service: agentService})
	environmentService := environment.NewService(pool, environment.NewRepository(), auth.NewRepository(), environment.DefaultAvailability())
	certificationService := scenariocertification.NewService(pool, scenariocertification.NewRepository(), auth.NewRepository(), scenario.NewRepository())
	certificationService.SetEnvironment(environmentService)
	qualificationPlanService := qualplan.NewService(pool, qualplan.NewRepository(), auth.NewRepository(), agentService, scenarioService, scenario.NewRepository(), coverage.NewRepository(), environment.NewRepository(), environmentService, certificationService, evaluationSuiteService)
	server := &http.Server{
		Addr:              fmt.Sprintf(":%d", cfg.Port),
		Handler:           httpapi.NewRouter(pool, logger, httpapi.WithIdentityService(identityService), httpapi.WithAuthentication(authService, authService, cfg.CookieSecure, cfg.SessionTTL, cfg.AppOrigin), httpapi.WithAgentService(agentService), httpapi.WithAgentConnectionService(agentConnectionService), httpapi.WithScenarioService(scenarioService), httpapi.WithScenarioAuthoringService(scenarioAuthoringService), httpapi.WithIncidentSourceService(incidentSourceService), httpapi.WithEvaluationBatchService(evaluationBatchService), httpapi.WithEvaluationSuiteService(evaluationSuiteService), httpapi.WithAgentRegressionService(agentRegressionService), httpapi.WithQualificationService(qualificationService), httpapi.WithQualificationPlanService(qualificationPlanService), httpapi.WithCoverageService(coverageService), httpapi.WithReliabilityService(reliabilityService), httpapi.WithFailureTaxonomyService(failureTaxonomyService), httpapi.WithEvidenceService(evidenceService), httpapi.WithVerificationService(verificationService), httpapi.WithTrajectoryService(trajectoryService), httpapi.WithExecutionService(executionService), httpapi.WithCertificationService(certificationService), httpapi.WithEnvironmentService(environmentService), httpapi.WithBillingService(billingService), httpapi.WithCustomerCollectorService(customerCollectorService), httpapi.WithCustomerAttributionService(customerAttributionService), httpapi.WithOperationalService(operationalService), httpapi.WithOperationalMemoryService(operationalService)),
		ReadHeaderTimeout: readHeaderTimeout,
		ReadTimeout:       readTimeout,
		WriteTimeout:      writeTimeout,
		IdleTimeout:       idleTimeout,
	}

	serverErrors := make(chan error, 1)
	go func() {
		logger.Info("API server starting", "environment", cfg.Environment, "port", cfg.Port)
		serverErrors <- server.ListenAndServe()
	}()

	select {
	case <-rootCtx.Done():
		logger.Info("shutdown signal received")
	case err := <-serverErrors:
		if !errors.Is(err, http.ErrServerClosed) {
			return fmt.Errorf("serve HTTP: %w", err)
		}
	}

	shutdownCtx, cancel := context.WithTimeout(context.Background(), shutdownTimeout)
	defer cancel()
	if err := server.Shutdown(shutdownCtx); err != nil {
		return fmt.Errorf("graceful HTTP shutdown: %w", err)
	}
	logger.Info("HTTP server stopped")
	return nil
}

// agentClaimAdapter maps agent claims to the minimal coverage ClaimReader
// without creating an import cycle.
type agentClaimAdapter struct {
	service *agent.Service
}

func (a *agentClaimAdapter) ListVersionClaims(ctx context.Context, principal auth.Principal, workspaceID, agentID, versionID string) ([]coverage.ClaimSummary, error) {
	items, err := a.service.ListVersionClaims(ctx, principal, workspaceID, agentID, versionID)
	if err != nil {
		return nil, err
	}
	out := make([]coverage.ClaimSummary, 0, len(items))
	for _, item := range items {
		out = append(out, coverage.ClaimSummary{CapabilityID: item.CapabilityID})
	}
	return out, nil
}

// reliabilityClaimAdapter maps agent claims to the minimal reliability
// ClaimReader without creating an import cycle.
type reliabilityClaimAdapter struct {
	service *agent.Service
}

func (a *reliabilityClaimAdapter) ListVersionClaims(ctx context.Context, principal auth.Principal, workspaceID, agentID, versionID string) ([]reliability.ClaimSummary, error) {
	items, err := a.service.ListVersionClaims(ctx, principal, workspaceID, agentID, versionID)
	if err != nil {
		return nil, err
	}
	out := make([]reliability.ClaimSummary, 0, len(items))
	for _, item := range items {
		out = append(out, reliability.ClaimSummary{CapabilityID: item.CapabilityID})
	}
	return out, nil
}

// executionMembership proves workspace-scoped execution membership for
// coverage assessment without coupling the coverage package to suite SQL.
type executionMembership struct {
	pool *pgxpool.Pool
}

func (m *executionMembership) AgentInExecution(ctx context.Context, workspaceID, executionID, agentVersionID string) (string, bool, error) {
	var agentID string
	err := m.pool.QueryRow(ctx, `SELECT av.agent_id FROM suite_execution_agents sea
		JOIN suite_executions se ON se.id=sea.suite_execution_id
		JOIN agent_versions av ON av.id=sea.agent_version_id
		JOIN agents a ON a.id=av.agent_id
		WHERE sea.suite_execution_id=$1 AND sea.agent_version_id=$2 AND se.workspace_id=$3 AND a.owner_workspace_id=$3`, executionID, agentVersionID, workspaceID).Scan(&agentID)
	if err != nil {
		return "", false, nil
	}
	return agentID, true, nil
}

func newLogger(levelName string) *slog.Logger {
	levels := map[string]slog.Level{"debug": slog.LevelDebug, "info": slog.LevelInfo, "warn": slog.LevelWarn, "error": slog.LevelError}
	return slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: levels[levelName]}))
}
