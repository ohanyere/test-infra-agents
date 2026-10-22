package httpapi

import (
	"log/slog"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
)

type routerConfig struct {
	identity            IdentityService
	auth                AuthService
	authorization       WorkspaceAuthorization
	cookieSecure        bool
	sessionTTL          time.Duration
	appOrigin           string
	agents              AgentService
	agentConnections    AgentConnectionService
	scenarios           ScenarioService
	scenarioAuthoring   ScenarioAuthoringService
	incidentSources     IncidentSourceService
	evaluationBatches   EvaluationBatchService
	evaluationSuites    EvaluationSuiteService
	agentRegressions    AgentRegressionService
	qualification       QualificationService
	qualificationPlans  QualificationPlanService
	coverage            CoverageService
	reliability         ReliabilityService
	failureTaxonomy     FailureTaxonomyService
	evidence            EvidenceService
	execution           ExecutionService
	trajectory          TrajectoryService
	certification       CertificationService
	environment         EnvironmentService
	billing             BillingService
	verification        VerificationService
	customerCollector   CustomerCollectorService
	customerAttribution customerAttributionService
	operational         OperationalService
	memory              OperationalMemoryService
}
type RouterOption func(*routerConfig)

func WithIdentityService(service IdentityService) RouterOption {
	return func(config *routerConfig) { config.identity = service }
}
func WithAgentService(service AgentService) RouterOption {
	return func(config *routerConfig) { config.agents = service }
}
func WithAgentConnectionService(service AgentConnectionService) RouterOption {
	return func(config *routerConfig) { config.agentConnections = service }
}
func WithScenarioService(service ScenarioService) RouterOption {
	return func(config *routerConfig) { config.scenarios = service }
}
func WithScenarioAuthoringService(service ScenarioAuthoringService) RouterOption {
	return func(config *routerConfig) { config.scenarioAuthoring = service }
}
func WithIncidentSourceService(service IncidentSourceService) RouterOption {
	return func(config *routerConfig) { config.incidentSources = service }
}
func WithEvaluationBatchService(service EvaluationBatchService) RouterOption {
	return func(config *routerConfig) { config.evaluationBatches = service }
}
func WithEvaluationSuiteService(service EvaluationSuiteService) RouterOption {
	return func(config *routerConfig) { config.evaluationSuites = service }
}
func WithAgentRegressionService(service AgentRegressionService) RouterOption {
	return func(config *routerConfig) { config.agentRegressions = service }
}
func WithQualificationService(service QualificationService) RouterOption {
	return func(config *routerConfig) { config.qualification = service }
}
func WithQualificationPlanService(service QualificationPlanService) RouterOption {
	return func(config *routerConfig) { config.qualificationPlans = service }
}
func WithCoverageService(service CoverageService) RouterOption {
	return func(config *routerConfig) { config.coverage = service }
}
func WithReliabilityService(service ReliabilityService) RouterOption {
	return func(config *routerConfig) { config.reliability = service }
}
func WithFailureTaxonomyService(service FailureTaxonomyService) RouterOption {
	return func(config *routerConfig) { config.failureTaxonomy = service }
}
func WithEvidenceService(service EvidenceService) RouterOption {
	return func(config *routerConfig) { config.evidence = service }
}
func WithExecutionService(service ExecutionService) RouterOption {
	return func(config *routerConfig) { config.execution = service }
}
func WithCertificationService(service CertificationService) RouterOption {
	return func(config *routerConfig) { config.certification = service }
}
func WithEnvironmentService(service EnvironmentService) RouterOption {
	return func(config *routerConfig) { config.environment = service }
}
func WithTrajectoryService(service TrajectoryService) RouterOption {
	return func(config *routerConfig) { config.trajectory = service }
}
func WithBillingService(service BillingService) RouterOption {
	return func(config *routerConfig) { config.billing = service }
}
func WithVerificationService(service VerificationService) RouterOption {
	return func(config *routerConfig) { config.verification = service }
}
func WithCustomerCollectorService(service CustomerCollectorService) RouterOption {
	return func(config *routerConfig) { config.customerCollector = service }
}
func WithCustomerAttributionService(service customerAttributionService) RouterOption {
	return func(config *routerConfig) { config.customerAttribution = service }
}
func WithOperationalService(service OperationalService) RouterOption {
	return func(config *routerConfig) { config.operational = service }
}
func WithOperationalMemoryService(service OperationalMemoryService) RouterOption {
	return func(config *routerConfig) { config.memory = service }
}
func WithAuthentication(service AuthService, authorization WorkspaceAuthorization, cookieSecure bool, sessionTTL time.Duration, appOrigin string) RouterOption {
	return func(config *routerConfig) {
		config.auth = service
		config.authorization = authorization
		config.cookieSecure = cookieSecure
		config.sessionTTL = sessionTTL
		config.appOrigin = appOrigin
	}
}

func NewRouter(database DatabasePinger, logger *slog.Logger, options ...RouterOption) http.Handler {
	config := routerConfig{}
	for _, option := range options {
		option(&config)
	}
	handlers := healthHandlers{database: database, logger: logger}
	router := chi.NewRouter()
	router.Use(requestIDMiddleware)
	router.Use(requestLoggingMiddleware(logger))
	router.Use(middleware.Recoverer)
	router.Use(securityHeaders)
	if config.appOrigin != "" {
		router.Use(corsMiddleware(config.appOrigin))
	}
	router.Get("/healthz", handlers.healthz)
	router.Get("/readyz", handlers.readyz)
	if config.identity != nil && config.auth != nil && config.authorization != nil {
		identityAPI := identityHandlers{service: config.identity, authorization: config.authorization, logger: logger}
		authAPI := authHandlers{service: config.auth, cookieSecure: config.cookieSecure, ttl: config.sessionTTL}
		router.Route("/v1", func(v1 chi.Router) {
			v1.Use(middleware.Timeout(10 * time.Second))
			v1.Use(requireJSON)
			v1.Post("/auth/signup", authAPI.signup)
			v1.Post("/auth/login", authAPI.login)
			if config.agents != nil {
				agentAPI := agentHandlers{service: config.agents, logger: logger}
				v1.Get("/agents", agentAPI.listPublic)
				v1.Get("/agents/{slug}", agentAPI.getPublic)
			}
			v1.Get("/agent-capabilities", (agentConnectionHandlers{}).catalog)
			if config.memory != nil {
				v1.Post("/agent-guidance", (memoryHandlers{service: config.memory, logger: logger}).agentGuidance)
			}
			if config.scenarios != nil {
				scenarioAPI := scenarioHandlers{service: config.scenarios, logger: logger}
				v1.Get("/scenarios", scenarioAPI.listPublic)
				v1.Get("/scenarios/{slug}", scenarioAPI.getPublic)
			}
			qualificationTasksAPI := qualificationTasksHandlers{logger: logger}
			v1.Get("/qualification-tasks", qualificationTasksAPI.listTasks)
			v1.Get("/qualification-tasks/{taskID}", qualificationTasksAPI.getTask)
			if config.billing != nil {
				v1.Post("/billing/webhooks/{provider}", billingHandlers{service: config.billing, logger: logger}.webhook)
			}
			if config.customerCollector != nil {
				v1.Post("/collector/observation-sessions/{sessionID}/evidence", customerCollectorHandlers{service: config.customerCollector, logger: logger}.ingest)
				v1.Post("/collector/observation-sessions/{sessionID}/audit-candidates", customerCollectorHandlers{service: config.customerCollector, logger: logger}.ingestForwardedAudit)
				v1.Post("/collector/observation-sessions/{sessionID}/metric-candidates", customerCollectorHandlers{service: config.customerCollector, logger: logger}.ingestMetricCandidate)
			}
			if config.customerAttribution != nil {
				v1.Post("/execution-issuers/receipts", (customerAttributionHandlers{service: config.customerAttribution, logger: logger}).ingestGatewayReceipt)
			}
			if preflight, ok := config.operational.(gatewayPreflightService); ok {
				v1.Post("/execution-issuers/preflight", (gatewayPreflightHandlers{service: preflight, operationalHandlers: operationalHandlers{logger: logger}}).preflight)
			}
			v1.Group(func(protected chi.Router) {
				protected.Use(authenticationMiddleware(config.auth))
				protected.Use(originMiddleware(config.appOrigin))
				protected.Post("/auth/logout", authAPI.logout)
				protected.Get("/auth/me", authAPI.me)
				protected.Get("/organizations", identityAPI.listOrganizations)
				protected.Get("/organizations/{organizationID}", identityAPI.getOrganization)
				protected.Get("/organizations/{organizationID}/workspaces", identityAPI.listWorkspaces)
				protected.Get("/workspaces/{workspaceID}", identityAPI.getWorkspace)
				if config.billing != nil {
					billingAPI := billingHandlers{service: config.billing, logger: logger}
					protected.Get("/workspaces/{workspaceID}/billing", billingAPI.account)
					protected.Get("/workspaces/{workspaceID}/entitlements", billingAPI.entitlements)
					protected.Get("/workspaces/{workspaceID}/usage", billingAPI.usage)
					protected.Post("/workspaces/{workspaceID}/billing/checkout", billingAPI.checkout)
					protected.Post("/workspaces/{workspaceID}/billing/portal", billingAPI.portal)
				}
				protected.Patch("/workspaces/{workspaceID}", identityAPI.updateWorkspace)
				protected.Get("/workspaces/{workspaceID}/members", identityAPI.listMembers)
				protected.Post("/workspaces/{workspaceID}/members", identityAPI.createMembership)
				protected.Patch("/workspaces/{workspaceID}/members/{userID}", identityAPI.updateMembership)
				protected.Delete("/workspaces/{workspaceID}/members/{userID}", identityAPI.deleteMembership)
				if config.agents != nil {
					agentAPI := agentHandlers{service: config.agents, logger: logger}
					protected.Get("/workspaces/{workspaceID}/agents", agentAPI.listWorkspace)
					protected.Post("/workspaces/{workspaceID}/agents", agentAPI.create)
					protected.Get("/workspaces/{workspaceID}/agents/{agentID}", agentAPI.getWorkspace)
					protected.Patch("/workspaces/{workspaceID}/agents/{agentID}", agentAPI.update)
					protected.Post("/workspaces/{workspaceID}/agents/{agentID}/versions", agentAPI.createVersion)
					protected.Get("/workspaces/{workspaceID}/agents/{agentID}/versions", agentAPI.listVersions)
					protected.Get("/workspaces/{workspaceID}/agents/{agentID}/versions/{versionID}", agentAPI.getVersion)
				}
				if config.agentConnections != nil {
					connectionAPI := agentConnectionHandlers{service: config.agentConnections, logger: logger}
					protected.Post("/workspaces/{workspaceID}/agent-connections/test", connectionAPI.testConfiguration)
					protected.Get("/workspaces/{workspaceID}/agent-versions/{versionID}/connection", connectionAPI.get)
					protected.Put("/workspaces/{workspaceID}/agent-versions/{versionID}/credential", connectionAPI.credential)
					protected.Post("/workspaces/{workspaceID}/agent-versions/{versionID}/connection/test", connectionAPI.test)
					protected.Get("/workspaces/{workspaceID}/agent-versions/{versionID}/compatibility/scenario-versions/{scenarioVersionID}", connectionAPI.compatibility)
				}
				catalogAPI := capabilityCatalogHandlers{logger: logger}
				if config.agents != nil {
					catalogAPI.claims = config.agents
				}
				if config.coverage != nil {
					catalogAPI.coverage = config.coverage
				}
				protected.Get("/capability-catalog", catalogAPI.list)
				protected.Get("/capability-catalog/{capabilityID}", catalogAPI.get)
				protected.Get("/task-family-catalog", coverageHandlers{service: config.coverage, logger: logger}.taskFamilies)
				protected.Get("/capability-coverage-requirements", coverageHandlers{service: config.coverage, logger: logger}.coverageRequirements)
				if config.agents != nil {
					protected.Get("/workspaces/{workspaceID}/agents/{agentID}/versions/{versionID}/capability-claims", catalogAPI.listClaims)
				}
				if config.coverage != nil {
					coverageAPI := coverageHandlers{service: config.coverage, logger: logger}
					protected.Get("/workspaces/{workspaceID}/agents/{agentID}/versions/{versionID}/capability-coverage", coverageAPI.versionCoverage)
					protected.Get("/workspaces/{workspaceID}/suite-executions/{executionID}/agents/{agentVersionID}/capability-coverage", coverageAPI.executionCoverage)
					protected.Post("/workspaces/{workspaceID}/suite-executions/{executionID}/agents/{agentVersionID}/capability-coverage/assessments", coverageAPI.persistExecutionCoverage)
				}
				if config.reliability != nil {
					reliabilityAPI := reliabilityHandlers{service: config.reliability, logger: logger}
					protected.Get("/workspaces/{workspaceID}/agents/{agentID}/versions/{versionID}/reliability", reliabilityAPI.agentVersionReliability)
					protected.Get("/workspaces/{workspaceID}/suite-executions/{executionID}/reliability", reliabilityAPI.executionReliability)
					protected.Get("/workspaces/{workspaceID}/scenarios/{scenarioID}/versions/{versionID}/reliability", reliabilityAPI.scenarioVersionReliability)
				}
				if config.scenarios != nil {
					scenarioAPI := scenarioHandlers{service: config.scenarios, logger: logger}
					protected.Get("/workspaces/{workspaceID}/scenarios", scenarioAPI.listWorkspace)
					protected.Post("/workspaces/{workspaceID}/scenarios", scenarioAPI.create)
					protected.Get("/workspaces/{workspaceID}/scenarios/{scenarioID}", scenarioAPI.getWorkspace)
					protected.Patch("/workspaces/{workspaceID}/scenarios/{scenarioID}", scenarioAPI.update)
					protected.Post("/workspaces/{workspaceID}/scenarios/{scenarioID}/versions", scenarioAPI.createVersion)
					protected.Get("/workspaces/{workspaceID}/scenarios/{scenarioID}/versions", scenarioAPI.listVersions)
					protected.Get("/workspaces/{workspaceID}/scenarios/{scenarioID}/versions/{versionID}", scenarioAPI.getVersion)
					if config.coverage != nil {
						coverageAPI := coverageHandlers{service: config.coverage, logger: logger}
						protected.Post("/workspaces/{workspaceID}/scenarios/{scenarioID}/versions/{versionID}/task-family", coverageAPI.assignFamily)
						protected.Get("/workspaces/{workspaceID}/scenarios/{scenarioID}/versions/{versionID}/task-family", coverageAPI.getAssignment)
					}
				}
				if config.scenarioAuthoring != nil {
					authoringAPI := scenarioAuthoringHandlers{service: config.scenarioAuthoring, logger: logger}
					protected.Post("/workspaces/{workspaceID}/scenario-drafts/describe", authoringAPI.describe)
					protected.Post("/workspaces/{workspaceID}/scenario-drafts/import-incident", authoringAPI.importIncident)
				}
				if config.incidentSources != nil {
					sourceAPI := incidentSourceHandlers{service: config.incidentSources, logger: logger}
					protected.Post("/workspaces/{workspaceID}/incident-sources", sourceAPI.create)
					protected.Get("/workspaces/{workspaceID}/incident-sources", sourceAPI.list)
					protected.Get("/workspaces/{workspaceID}/incident-sources/{connectionID}", sourceAPI.get)
					protected.Post("/workspaces/{workspaceID}/incident-sources/{connectionID}/rotate", sourceAPI.rotate)
					protected.Post("/workspaces/{workspaceID}/incident-sources/{connectionID}/test", sourceAPI.test)
					protected.Get("/workspaces/{workspaceID}/incident-sources/{connectionID}/incidents", sourceAPI.browse)
					protected.Get("/workspaces/{workspaceID}/incident-sources/{connectionID}/incidents/{externalID}", sourceAPI.fetch)
					protected.Post("/workspaces/{workspaceID}/incident-sources/{connectionID}/incidents/{externalID}/import", sourceAPI.importIncident)
				}
				if config.evaluationBatches != nil {
					batchAPI := evaluationBatchHandlers{service: config.evaluationBatches, logger: logger}
					protected.Get("/workspaces/{workspaceID}/evaluation-batches", batchAPI.list)
					protected.Post("/workspaces/{workspaceID}/evaluation-batches", batchAPI.create)
					protected.Get("/workspaces/{workspaceID}/evaluation-batches/{batchID}", batchAPI.get)
					protected.Get("/workspaces/{workspaceID}/evaluation-batches/{batchID}/runs", batchAPI.listRuns)
					protected.Get("/workspaces/{workspaceID}/runs/{runID}", batchAPI.getRun)
				}
				if config.evaluationSuites != nil {
					suiteAPI := evaluationSuiteHandlers{service: config.evaluationSuites, logger: logger}
					protected.Get("/workspaces/{workspaceID}/evaluation-suites", suiteAPI.listSuites)
					protected.Post("/workspaces/{workspaceID}/evaluation-suites", suiteAPI.createSuite)
					protected.Get("/workspaces/{workspaceID}/evaluation-suites/{suiteID}", suiteAPI.getSuite)
					protected.Post("/workspaces/{workspaceID}/evaluation-suites/{suiteID}/versions", suiteAPI.createVersion)
					protected.Get("/workspaces/{workspaceID}/evaluation-suite-versions/{versionID}", suiteAPI.getVersion)
					protected.Post("/workspaces/{workspaceID}/suite-executions", suiteAPI.startExecution)
					protected.Get("/workspaces/{workspaceID}/suite-executions/{executionID}", suiteAPI.getExecution)
					protected.Get("/workspaces/{workspaceID}/suite-executions/{executionID}/results", suiteAPI.getResults)
				}
				if config.agentRegressions != nil {
					regressionAPI := agentRegressionHandlers{service: config.agentRegressions, logger: logger}
					protected.Post("/workspaces/{workspaceID}/agent-regressions", regressionAPI.create)
					protected.Get("/workspaces/{workspaceID}/agent-regressions/{comparisonID}", regressionAPI.get)
					protected.Get("/workspaces/{workspaceID}/agent-regressions/{comparisonID}/results", regressionAPI.results)
				}
				if config.qualification != nil {
					qualificationAPI := qualificationHandlers{service: config.qualification, logger: logger}
					protected.Post("/workspaces/{workspaceID}/qualification-policies", qualificationAPI.createPolicy)
					protected.Get("/workspaces/{workspaceID}/qualification-policies", qualificationAPI.listPolicies)
					protected.Get("/workspaces/{workspaceID}/qualification-policies/{policyID}", qualificationAPI.getPolicy)
					protected.Post("/workspaces/{workspaceID}/qualification-policies/{policyID}/versions", qualificationAPI.createVersion)
					protected.Get("/workspaces/{workspaceID}/qualification-policy-versions/{versionID}", qualificationAPI.getVersion)
					protected.Post("/workspaces/{workspaceID}/qualification-assessments", qualificationAPI.createAssessment)
					protected.Get("/workspaces/{workspaceID}/qualification-assessments/{assessmentID}", qualificationAPI.getAssessment)
					protected.Get("/workspaces/{workspaceID}/suite-executions/{executionID}/comparison", qualificationAPI.comparison)
				}
				if config.qualificationPlans != nil {
					planAPI := qualificationPlanHandlers{service: config.qualificationPlans, logger: logger}
					protected.Post("/workspaces/{workspaceID}/agents/{agentID}/versions/{versionID}/qualification-plans", planAPI.buildPlan)
					protected.Get("/workspaces/{workspaceID}/qualification-plans/{planID}", planAPI.getPlan)
					protected.Post("/workspaces/{workspaceID}/qualification-plans/{planID}/suite", planAPI.buildSuite)
				}
				if config.failureTaxonomy != nil {
					failureAPI := failureTaxonomyHandlers{service: config.failureTaxonomy, logger: logger}
					protected.Get("/workspaces/{workspaceID}/suite-executions/{executionID}/failures", failureAPI.get)
				}
				if config.evidence != nil {
					evidenceAPI := evidenceHandlers{service: config.evidence, logger: logger}
					protected.Get("/workspaces/{workspaceID}/runs/{runID}/events", evidenceAPI.listEvents)
					protected.Get("/workspaces/{workspaceID}/runs/{runID}/actions", evidenceAPI.listActions)
					protected.Get("/workspaces/{workspaceID}/runs/{runID}/evaluation", evidenceAPI.getEvaluation)
				}
				if config.verification != nil {
					verificationAPI := verificationHandlers{service: config.verification, logger: logger}
					protected.Get("/workspaces/{workspaceID}/runs/{runID}/verification", verificationAPI.get)
					protected.Get("/workspaces/{workspaceID}/runs/{runID}/verification/actions", verificationAPI.actions)
					protected.Get("/workspaces/{workspaceID}/runs/{runID}/verification/snapshots", verificationAPI.snapshots)
					protected.Get("/workspaces/{workspaceID}/runs/{runID}/verification/forward-risks", verificationAPI.risks)
				}
				if config.trajectory != nil {
					trajectoryAPI := trajectoryHandlers{service: config.trajectory, logger: logger}
					protected.Get("/workspaces/{workspaceID}/runs/{runID}/trajectory", trajectoryAPI.getTrajectory)
					protected.Get("/workspaces/{workspaceID}/runs/{runID}/evidence/{evidenceID}", trajectoryAPI.getEvidence)
					protected.Get("/workspaces/{workspaceID}/suite-executions/{executionID}/trajectory-summary", trajectoryAPI.getSuiteSummary)
				}
				if config.execution != nil {
					executionAPI := executionHandlers{service: config.execution, logger: logger}
					protected.Post("/workspaces/{workspaceID}/evaluation-batches/{batchID}/start", executionAPI.startBatch)
					protected.Post("/workspaces/{workspaceID}/evaluation-batches/{batchID}/stop", executionAPI.stopBatch)
				}
				if config.certification != nil {
					certificationAPI := certificationHandlers{service: config.certification, logger: logger}
					protected.Get("/workspaces/{workspaceID}/scenarios/{scenarioID}/versions/{versionID}/certification", certificationAPI.getCertification)
					protected.Post("/workspaces/{workspaceID}/scenarios/{scenarioID}/versions/{versionID}/certification", certificationAPI.triggerCertification)
					protected.Put("/workspaces/{workspaceID}/scenarios/{scenarioID}/versions/{versionID}/reference-remediation", certificationAPI.putReference)
					protected.Get("/workspaces/{workspaceID}/scenarios/{scenarioID}/versions/{versionID}/certification/eligibility", certificationAPI.getEligibility)
				}
				if config.environment != nil {
					environmentAPI := environmentHandlers{service: config.environment, logger: logger}
					protected.Get("/environment-components", environmentAPI.listComponents)
					protected.Get("/environment-profiles", environmentAPI.listProfiles)
					protected.Get("/environment-providers", environmentAPI.listProviders)
					protected.Get("/workspaces/{workspaceID}/environments", environmentAPI.listInstances)
					protected.Get("/workspaces/{workspaceID}/environments/{environmentID}", environmentAPI.getInstance)
					protected.Get("/workspaces/{workspaceID}/environments/{environmentID}/components", environmentAPI.listInstanceComponents)
					protected.Post("/workspaces/{workspaceID}/environment-selection/resolve", environmentAPI.resolveSelection)
					protected.Get("/workspaces/{workspaceID}/scenarios/{scenarioID}/versions/{versionID}/environment", environmentAPI.getVersionEnvironment)
					protected.Put("/workspaces/{workspaceID}/scenarios/{scenarioID}/versions/{versionID}/environment", environmentAPI.putVersionRequirements)
				}
				if config.customerCollector != nil {
					collectorAPI := customerCollectorHandlers{service: config.customerCollector, logger: logger}
					if trusted, ok := config.customerCollector.(customerTrustedSourceService); ok {
						trustedAPI := customerTrustedSourceHandlers{service: trusted, logger: logger}
						protected.Post("/workspaces/{workspaceID}/customer-kubernetes-environments/{environmentID}/authenticated-audit-sources", trustedAPI.registerAudit)
						protected.Get("/workspaces/{workspaceID}/customer-kubernetes-environments/{environmentID}/authenticated-audit-sources", trustedAPI.listAudit)
						protected.Delete("/workspaces/{workspaceID}/customer-kubernetes-environments/{environmentID}/authenticated-audit-sources/{sourceID}", trustedAPI.revokeAudit)
					}
					protected.Post("/workspaces/{workspaceID}/customer-kubernetes-environments", collectorAPI.register)
					protected.Get("/workspaces/{workspaceID}/customer-kubernetes-environments", collectorAPI.list)
					protected.Post("/workspaces/{workspaceID}/customer-kubernetes-environments/{environmentID}/rotate", collectorAPI.rotate)
					protected.Delete("/workspaces/{workspaceID}/customer-kubernetes-environments/{environmentID}", collectorAPI.revoke)
					protected.Post("/workspaces/{workspaceID}/customer-kubernetes-environments/{environmentID}/observation-sessions", collectorAPI.startSession)
					protected.Post("/workspaces/{workspaceID}/customer-kubernetes-environments/{environmentID}/observation-sessions/{sessionID}/action-claims", collectorAPI.submitActionClaim)
					protected.Get("/workspaces/{workspaceID}/customer-kubernetes-environments/{environmentID}/observation-sessions/{sessionID}/action-claims", collectorAPI.listActionClaims)
					protected.Get("/workspaces/{workspaceID}/customer-kubernetes-environments/{environmentID}/observation-sessions/{sessionID}/audit-candidates", collectorAPI.listForwardedAudits)
					protected.Get("/workspaces/{workspaceID}/customer-kubernetes-environments/{environmentID}/observation-sessions/{sessionID}/reconciliations", collectorAPI.listAuditReconciliation)
				}
				if config.customerAttribution != nil {
					attributionAPI := customerAttributionHandlers{service: config.customerAttribution, logger: logger}
					base := "/workspaces/{workspaceID}/customer-kubernetes-environments/{environmentID}"
					protected.Post(base+"/execution-issuers", attributionAPI.registerIssuer)
					protected.Get(base+"/execution-issuers", attributionAPI.listIssuers)
					protected.Delete(base+"/execution-issuers/{issuerID}", attributionAPI.revokeIssuer)
					protected.Post(base+"/execution-receipts", attributionAPI.ingestReceipt)
					protected.Get(base+"/observation-sessions/{sessionID}/attribution-decisions", attributionAPI.listDecisions)
				}
				if config.memory != nil {
					memoryAPI := memoryHandlers{service: config.memory, logger: logger}
					protected.Get("/workspaces/{workspaceID}/guidance-experiments", memoryAPI.experiments)
					protected.Post("/workspaces/{workspaceID}/guidance-experiments", memoryAPI.recordExperiment)
					base := "/workspaces/{workspaceID}/customer-kubernetes-environments/{environmentID}"
					protected.Get(base+"/operational-episodes", memoryAPI.episodes)
					protected.Get(base+"/operational-episodes/{episodeID}/relationships", memoryAPI.relationships)
					protected.Post(base+"/observation-sessions/{sessionID}/operational-episodes/materialize", memoryAPI.materialize)
					protected.Post(base+"/operational-episodes/{episodeID}/decisions", memoryAPI.episodeDecision)
					protected.Get(base+"/operational-lessons", memoryAPI.lessons)
					protected.Post(base+"/operational-lessons/{lessonID}/decisions", memoryAPI.lessonDecision)
					protected.Get(base+"/operational-lessons/{lessonID}/decisions", memoryAPI.lessonDecisions)
					protected.Post(base+"/operational-lessons/{lessonID}/supersede", memoryAPI.supersedeLesson)
					protected.Post(base+"/guidance-credentials", memoryAPI.issueCredential)
					protected.Get(base+"/guidance-credentials", memoryAPI.credentials)
					protected.Post(base+"/guidance-credentials/{credentialID}/revoke", memoryAPI.revokeCredential)
					protected.Post(base+"/guidance-preview", memoryAPI.preview)
					protected.Get(base+"/guidance-history", memoryAPI.history)
					protected.Get(base+"/guidance-operation-links", memoryAPI.guidanceLinks)
				}
				if config.operational != nil {
					operationalAPI := operationalHandlers{service: config.operational, logger: logger}
					base := "/workspaces/{workspaceID}/customer-kubernetes-environments/{environmentID}"
					if monitoring, ok := config.operational.(monitoringService); ok {
						api := monitoringHandlers{service: monitoring, operationalHandlers: operationalAPI}
						protected.Post(base+"/observation-sessions/{sessionID}/monitoring", api.configure)
						protected.Get(base+"/observation-sessions/{sessionID}/monitoring", api.get)
						protected.Post(base+"/observation-sessions/{sessionID}/monitoring/actions", api.action)
					}
					if direct, ok := config.operational.(directSourceService); ok {
						directAPI := directSourceHandlers{service: direct, operationalHandlers: operationalAPI}
						protected.Post(base+"/direct-outcome-sources", directAPI.register)
						protected.Get(base+"/direct-outcome-sources", directAPI.list)
						protected.Delete(base+"/direct-outcome-sources/{sourceID}", directAPI.revoke)
						protected.Get(base+"/observation-sessions/{sessionID}/direct-outcome-observations", directAPI.observations)
					}
					if preflight, ok := config.operational.(gatewayPreflightService); ok {
						protected.Post("/workspaces/{workspaceID}/customer-kubernetes-environments/{environmentID}/observation-sessions/{sessionID}/approvals/{approvalID}/revoke", (gatewayPreflightHandlers{service: preflight, operationalHandlers: operationalAPI}).revokeApproval)
					}
					protected.Post("/workspaces/{workspaceID}/customer-kubernetes-environments/{environmentID}/operational-contracts", operationalAPI.saveContract)
					protected.Get("/workspaces/{workspaceID}/customer-kubernetes-environments/{environmentID}/operational-contracts", operationalAPI.listContracts)
					protected.Get("/workspaces/{workspaceID}/customer-kubernetes-environments/{environmentID}/assurance-status", operationalAPI.status)
					protected.Get("/workspaces/{workspaceID}/customer-kubernetes-environments/{environmentID}/assurance-notifications", operationalAPI.notifications)
					protected.Get("/workspaces/{workspaceID}/customer-kubernetes-environments/{environmentID}/observation-sessions", operationalAPI.sessions)
					protected.Post("/workspaces/{workspaceID}/customer-kubernetes-environments/{environmentID}/observation-sessions/{sessionID}/approvals", operationalAPI.approval)
					protected.Get("/workspaces/{workspaceID}/customer-kubernetes-environments/{environmentID}/observation-sessions/{sessionID}/approvals", operationalAPI.approvals)
					protected.Get("/workspaces/{workspaceID}/customer-kubernetes-environments/{environmentID}/observation-sessions/{sessionID}/findings", operationalAPI.findings)
					protected.Get("/workspaces/{workspaceID}/customer-kubernetes-environments/{environmentID}/observation-sessions/{sessionID}/stability-assessments", operationalAPI.stability)
					protected.Get("/workspaces/{workspaceID}/customer-kubernetes-environments/{environmentID}/observation-sessions/{sessionID}/evidence-references", operationalAPI.evidenceReferences)
					protected.Post("/workspaces/{workspaceID}/customer-kubernetes-environments/{environmentID}/observation-sessions/{sessionID}/operational-assessments", operationalAPI.assess)
					protected.Get("/workspaces/{workspaceID}/customer-kubernetes-environments/{environmentID}/observation-sessions/{sessionID}/operational-assessments", operationalAPI.reports)
					protected.Get("/workspaces/{workspaceID}/customer-kubernetes-environments/{environmentID}/observation-sessions/{sessionID}/evidence-detail", operationalAPI.evidenceDetail)
				}
			})
		})
	}
	return router
}
