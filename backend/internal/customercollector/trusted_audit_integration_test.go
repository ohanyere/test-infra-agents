package customercollector

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/sha256"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"encoding/pem"
	"errors"
	"fmt"
	"math/big"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/evalplane/evalplane/backend/internal/agentgateway"
	"github.com/evalplane/evalplane/backend/internal/auth"
	"github.com/evalplane/evalplane/backend/internal/customerattribution"
	"github.com/evalplane/evalplane/backend/internal/identity"
	"github.com/evalplane/evalplane/backend/internal/kubeaudit"
	"github.com/evalplane/evalplane/backend/internal/operational"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
)

func TestAuthenticatedAuditAndExecutionReceiptIntegration(t *testing.T) {
	url := os.Getenv("TEST_DATABASE_URL")
	if url == "" {
		t.Skip("TEST_DATABASE_URL is not set")
	}
	config, err := pgxpool.ParseConfig(url)
	if err != nil || config.ConnConfig.Database != "infra_agents_test" && config.ConnConfig.Database != "infra_agents_stage10h_fresh_test" {
		t.Fatal("requires identity-verified disposable infra_agents_test or infra_agents_stage10h_fresh_test")
	}
	ctx := context.Background()
	pool, err := pgxpool.New(ctx, url)
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	var actual string
	if err = pool.QueryRow(ctx, "SELECT current_database()").Scan(&actual); err != nil || actual != config.ConnConfig.Database {
		t.Fatal("wrong test database", actual, err)
	}
	suffix := strings.ReplaceAll(uuid.NewString(), "-", "")[:12]
	ids := identity.NewRepository()
	org, err := ids.CreateOrganization(ctx, pool, identity.OrganizationInput{Name: "Trusted source " + suffix, Slug: "trust-" + suffix})
	if err != nil {
		t.Fatal(err)
	}
	w, err := ids.CreateWorkspace(ctx, pool, org.ID, identity.WorkspaceInput{Name: "Customer", Slug: "customer"})
	if err != nil {
		t.Fatal(err)
	}
	other, err := ids.CreateWorkspace(ctx, pool, org.ID, identity.WorkspaceInput{Name: "Other", Slug: "other"})
	if err != nil {
		t.Fatal(err)
	}
	user, err := ids.CreateUser(ctx, pool, identity.UserInput{Email: "trust-" + suffix + "@example.test", FullName: "Trusted Source"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = ids.CreateMembership(ctx, pool, w.ID, user.ID, "owner"); err != nil {
		t.Fatal(err)
	}
	p := auth.Principal{UserID: user.ID}
	var scenario, scenarioVersion, agent, agentVersion string
	err = pool.QueryRow(ctx, `INSERT INTO scenarios(owner_workspace_id,slug,name,kind,visibility,status) VALUES($1,$2,'Audit','custom','workspace','active') RETURNING id`, w.ID, "trust-"+suffix).Scan(&scenario)
	if err != nil {
		t.Fatal(err)
	}
	err = pool.QueryRow(ctx, `INSERT INTO scenario_versions(scenario_id,version,description,category,difficulty,duration_seconds,agent_task,evidence_requirements,source_metadata) VALUES($1,1,'Trusted audit','traffic','medium',8,'Observe','{"contract_version":"evidence-requirements:v1","workload_state":"REQUIRED","telemetry":"REQUIRED","git_desired_state":"NOT_APPLICABLE","argo_state":"NOT_APPLICABLE","stability":"REQUIRED"}'::jsonb,'{}'::jsonb) RETURNING id`, scenario).Scan(&scenarioVersion)
	if err != nil {
		t.Fatal(err)
	}
	err = pool.QueryRow(ctx, `INSERT INTO agents(owner_workspace_id,slug,name,visibility,status) VALUES($1,$2,'Agent','workspace','active') RETURNING id`, w.ID, "trust-"+suffix).Scan(&agent)
	if err != nil {
		t.Fatal(err)
	}
	err = pool.QueryRow(ctx, `INSERT INTO agent_versions(agent_id,version_label,interface_kind,implementation_identity) VALUES($1,'v1','http','{"contract_version":"agent-implementation-identity:v1","kind":"declared","identifier":"trust-fixture","provenance_level":"declared"}') RETURNING id`, agent).Scan(&agentVersion)
	if err != nil {
		t.Fatal(err)
	}
	collector, err := NewService(pool, auth.NewRepository(), canonicalFinalizer{})
	if err != nil {
		t.Fatal(err)
	}
	registered, err := collector.Register(ctx, p, w.ID, CreateEnvironmentInput{Name: "Checkout", ClusterIdentifier: "cluster-one", Namespaces: []string{"checkout"}, ResourceKinds: []string{"Deployment", "Pod", "Service", "Ingress", "NetworkPolicy", "Namespace"}, Tier: "staging"})
	if err != nil {
		t.Fatal(err)
	}
	session, err := collector.StartSession(ctx, p, w.ID, registered.Environment.ID, CreateSessionInput{AgentVersionID: agentVersion, ScenarioVersionID: scenarioVersion})
	if err != nil {
		t.Fatal(err)
	}
	baseline := time.Now().UTC().Truncate(time.Microsecond)
	batch := EvidenceBatch{BatchID: uuid.NewString(), Sequence: 1, SchemaVersion: ContractVersion, Phase: "baseline", CapturedAt: baseline, Observation: agentgateway.Observation{Timestamp: baseline, Workloads: []agentgateway.WorkloadObservation{{Kind: "Deployment", Name: "checkout", Namespace: "checkout", UID: "uid-checkout", DesiredReplicas: 1, ReadyReplicas: 1, AvailableReplicas: 1, UpdatedReplicas: 1}}, Pods: []agentgateway.PodObservation{}, Metrics: map[string]any{}, RecentEvents: []agentgateway.RecentEvent{}}}
	if _, err = collector.Ingest(ctx, session.ID, registered.CollectorCredential, batch); err != nil {
		t.Fatal(err)
	}
	_, clientKey, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	certTemplate := &x509.Certificate{SerialNumber: big.NewInt(1), Subject: pkix.Name{CommonName: "customer-apiserver-audit"}, NotBefore: time.Now().Add(-time.Hour), NotAfter: time.Now().Add(time.Hour), KeyUsage: x509.KeyUsageDigitalSignature, ExtKeyUsage: []x509.ExtKeyUsage{x509.ExtKeyUsageClientAuth}}
	certDER, err := x509.CreateCertificate(rand.Reader, certTemplate, certTemplate, clientKey.Public(), clientKey)
	if err != nil {
		t.Fatal(err)
	}
	cert, err := x509.ParseCertificate(certDER)
	if err != nil {
		t.Fatal(err)
	}
	agentCertDigest := sha256.Sum256([]byte("separate registered agent client certificate fixture"))
	agentCertFingerprint := hex.EncodeToString(agentCertDigest[:])
	registeredSource, err := collector.RegisterAuditMTLSSource(ctx, p, w.ID, registered.Environment.ID, string(pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: certDER})))
	if err != nil {
		t.Fatal(err)
	}
	if _, err = collector.RegisterAuditMTLSSource(ctx, p, other.ID, registered.Environment.ID, string(pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: certDER}))); !errors.Is(err, auth.ErrForbidden) {
		t.Fatalf("cross-tenant source accepted: %v", err)
	}
	eventTime := time.Now().UTC().Truncate(time.Microsecond)
	event := kubeaudit.Event{APIVersion: kubeaudit.AuditAPIVersion, Kind: "Event", AuditID: uuid.NewString(), Stage: "ResponseComplete", Verb: "patch", RequestAt: eventTime, StageAt: eventTime, User: kubeaudit.User{Username: "system:serviceaccount:checkout:gateway"}, ObjectRef: kubeaudit.ObjectRef{APIGroup: "apps", APIVersion: "v1", Resource: "deployments", Namespace: "checkout", Name: "checkout", UID: "uid-checkout"}, Response: kubeaudit.Response{Code: 200}}
	raw, _ := json.Marshal(event)
	intake := NewAuditWebhookIntake(pool)
	if _, err = intake.IngestAuthenticatedAuditEvent(ctx, nil, raw); !errors.Is(err, auth.ErrForbidden) {
		t.Fatalf("forged source accepted: %v", err)
	}
	otherCertTemplate := *certTemplate
	otherCertTemplate.SerialNumber = big.NewInt(2)
	otherCertDER, err := x509.CreateCertificate(rand.Reader, &otherCertTemplate, &otherCertTemplate, clientKey.Public(), clientKey)
	if err != nil {
		t.Fatal(err)
	}
	otherCert, err := x509.ParseCertificate(otherCertDER)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = intake.IngestAuthenticatedAuditEvent(ctx, otherCert, raw); !errors.Is(err, auth.ErrForbidden) {
		t.Fatalf("unregistered client certificate accepted: %v", err)
	}
	result, err := intake.IngestAuthenticatedAuditEvent(ctx, cert, raw)
	if err != nil {
		t.Fatal(err)
	}
	if result.SourceID != registeredSource.ID || result.SessionID != session.ID || result.EvidenceID == "" {
		t.Fatalf("incorrect source binding: %+v", result)
	}
	serviceDeletion := event
	serviceDeletion.AuditID = uuid.NewString()
	serviceDeletion.Verb = "delete"
	serviceDeletion.ObjectRef = kubeaudit.ObjectRef{APIGroup: "", APIVersion: "v1", Resource: "services", Namespace: "checkout", Name: "checkout-public", UID: "uid-public-service"}
	serviceRaw, _ := json.Marshal(serviceDeletion)
	serviceResult, err := intake.IngestAuthenticatedAuditEvent(ctx, cert, serviceRaw)
	if err != nil {
		t.Fatalf("protected Service delete audit rejected: %v", err)
	}
	var storedKind, storedOperation string
	if err = pool.QueryRow(ctx, `SELECT target_kind,operation FROM customer_action_evidence WHERE id=$1`, serviceResult.EvidenceID).Scan(&storedKind, &storedOperation); err != nil || storedKind != "Service" || storedOperation != "delete" {
		t.Fatalf("protected delete lost audited identity: kind=%s operation=%s err=%v", storedKind, storedOperation, err)
	}
	assurance := operational.NewService(pool, auth.NewRepository(), nil)
	insufficientRequests := false
	staleMetrics := false
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer read-only-test-token" {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		switch {
		case strings.Contains(r.URL.Path, "/deployments/checkout"):
			_, _ = w.Write([]byte(`{"metadata":{"name":"checkout","namespace":"checkout","uid":"uid-checkout","resourceVersion":"12","generation":2},"spec":{"replicas":1,"template":{"spec":{"containers":[{"name":"checkout","image":"checkout:v2"}]}}},"status":{"observedGeneration":2,"availableReplicas":1,"readyReplicas":1,"updatedReplicas":1}}`))
		case strings.Contains(r.URL.Path, "/services/checkout-public"):
			w.WriteHeader(http.StatusNotFound)
			_, _ = w.Write([]byte(`{"kind":"Status","reason":"NotFound","code":404}`))
		case strings.Contains(r.URL.Path, "/services/checkout"):
			_, _ = w.Write([]byte(`{"kind":"Service","metadata":{"name":"checkout","namespace":"checkout","uid":"service-uid","resourceVersion":"8"},"spec":{"type":"LoadBalancer"},"status":{"loadBalancer":{"ingress":[{"ip":"192.0.2.1"}]}}}`))
		case strings.Contains(r.URL.Path, "/ingresses/checkout"):
			_, _ = w.Write([]byte(`{"kind":"Ingress","metadata":{"name":"checkout","namespace":"checkout","uid":"ingress-uid","resourceVersion":"8"},"spec":{"rules":[{"host":"checkout.example"}]}}`))
		case strings.Contains(r.URL.Path, "/networkpolicies/checkout"):
			_, _ = w.Write([]byte(`{"kind":"NetworkPolicy","metadata":{"name":"checkout","namespace":"checkout","uid":"policy-uid","resourceVersion":"8"},"spec":{"policyTypes":["Ingress"],"ingress":[{}]}}`))
		case strings.Contains(r.URL.Path, "/applications/checkout"):
			_, _ = w.Write([]byte(`{"metadata":{"name":"checkout"},"spec":{"source":{"repoURL":"https://git.example/checkout","path":"apps/checkout","targetRevision":"abcdef"}},"status":{"sync":{"status":"Synced","revision":"abcdef"},"health":{"status":"Healthy"}}}`))
		case r.URL.Path == "/api/v1/query":
			value := "100"
			if insufficientRequests && r.URL.Query().Get("query") == "checkout_requests" {
				value = "2"
			}
			if r.URL.Query().Get("query") == "checkout_5xx_pct" {
				value = "0.5"
			}
			sampleAt := time.Now()
			if staleMetrics {
				sampleAt = sampleAt.Add(-5 * time.Minute)
			}
			_, _ = fmt.Fprintf(w, `{"status":"success","data":{"resultType":"vector","result":[{"value":[%d,%q]}]}}`, sampleAt.Unix(), value)
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()
	serverFingerprint := sha256.Sum256(server.Certificate().Raw)
	serverPin := hex.EncodeToString(serverFingerprint[:])
	serverCA := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: server.Certificate().Raw})
	postContract, err := assurance.SaveContract(ctx, p, w.ID, registered.Environment.ID, operational.ContractInput{State: "active", Service: "checkout/checkout", Rules: []operational.Rule{{Type: "post_image", Namespace: "checkout", ResourceName: "checkout", Value: "checkout:v2"}, {Type: "approved_image", Namespace: "checkout", ResourceName: "checkout", Value: "checkout:v2"}, {Type: "allowed_mutation_scope", Namespace: "checkout", ResourceKind: "Deployment", ResourceName: "checkout"}, {Type: "require_approval", Namespace: "checkout", ResourceName: "checkout", ApprovalRequired: true}}})
	if err != nil {
		t.Fatal(err)
	}
	_ = postContract
	registerDirect := func(kind string, config operational.DirectSourceConfig) operational.DirectSource {
		source, err := assurance.RegisterDirectSource(ctx, p, w.ID, registered.Environment.ID, operational.DirectSource{Kind: kind, EndpointURL: server.URL, ServerCertificateSHA256: serverPin, Namespace: "checkout", ResourceName: "checkout", Configuration: config})
		if err != nil {
			t.Fatal(err)
		}
		return source
	}
	stateSource := registerDirect("kubernetes_state", operational.DirectSourceConfig{})
	stateObservation, err := assurance.AcquireDirect(ctx, stateSource.ID, session.ID, serverCA, "read-only-test-token")
	if err != nil || stateObservation.Kind != "kubernetes_state" {
		t.Fatalf("direct state acquisition: %+v %v", stateObservation, err)
	}
	probeSource, err := assurance.RegisterDirectSource(ctx, p, w.ID, registered.Environment.ID, operational.DirectSource{Kind: "external_reachability", EndpointURL: "https://checkout.example.org", ServerCertificateSHA256: serverPin, Namespace: "checkout", ResourceName: "checkout", Configuration: operational.DirectSourceConfig{ProbeVantage: "external-fixture"}})
	if err != nil {
		t.Fatalf("scoped probe registration: %v", err)
	}
	if _, err = assurance.RegisterDirectSource(ctx, p, other.ID, registered.Environment.ID, operational.DirectSource{Kind: "external_reachability", EndpointURL: "https://checkout.example.org", ServerCertificateSHA256: serverPin, Namespace: "checkout", ResourceName: "checkout", Configuration: operational.DirectSourceConfig{ProbeVantage: "external-fixture"}}); !errors.Is(err, auth.ErrForbidden) {
		t.Fatalf("cross-tenant probe accepted: %v", err)
	}
	if _, err = assurance.RegisterDirectSource(ctx, p, w.ID, registered.Environment.ID, operational.DirectSource{Kind: "external_reachability", EndpointURL: "https://checkout.example.org", ServerCertificateSHA256: serverPin, Namespace: "other", ResourceName: "checkout", Configuration: operational.DirectSourceConfig{ProbeVantage: "external-fixture"}}); !errors.Is(err, auth.ErrForbidden) {
		t.Fatalf("wrong-namespace probe accepted: %v", err)
	}
	if _, err = assurance.RegisterDirectSource(ctx, p, w.ID, registered.Environment.ID, operational.DirectSource{Kind: "external_reachability", EndpointURL: "https://checkout.example.org", ServerCertificateSHA256: serverPin, Namespace: "checkout", ResourceName: "checkout"}); !errors.Is(err, identity.ErrValidation) {
		t.Fatalf("missing vantage accepted: %v", err)
	}
	if _, err = assurance.AcquireDirect(ctx, probeSource.ID, session.ID, nil, ""); !errors.Is(err, auth.ErrForbidden) {
		t.Fatalf("ordinary source credential accepted for probe: %v", err)
	}
	if _, err = assurance.AcquireReachability(ctx, probeSource.ID, session.ID, "wrong-vantage"); !errors.Is(err, auth.ErrForbidden) {
		t.Fatalf("wrong probe vantage accepted: %v", err)
	}
	if _, err = assurance.AcquireReachability(ctx, probeSource.ID, session.ID, "external-fixture"); !errors.Is(err, auth.ErrForbidden) {
		t.Fatalf("unbound Ingress target probed: %v", err)
	}
	for _, kind := range []string{"kubernetes_service", "kubernetes_ingress", "kubernetes_networkpolicy"} {
		source := registerDirect(kind, operational.DirectSourceConfig{})
		observation, acquireErr := assurance.AcquireDirect(ctx, source.ID, session.ID, serverCA, "read-only-test-token")
		if acquireErr != nil || observation.Kind != kind {
			t.Fatalf("direct %s acquisition: %+v %v", kind, observation, acquireErr)
		}
	}
	deletedSource, err := assurance.RegisterDirectSource(ctx, p, w.ID, registered.Environment.ID, operational.DirectSource{Kind: "kubernetes_service", EndpointURL: server.URL, ServerCertificateSHA256: serverPin, Namespace: "checkout", ResourceName: "checkout-public"})
	if err != nil {
		t.Fatal(err)
	}
	deletedObservation, err := assurance.AcquireDirect(ctx, deletedSource.ID, session.ID, serverCA, "read-only-test-token")
	if err != nil {
		t.Fatalf("direct deleted Service acquisition: %v", err)
	}
	var deletedState struct {
		ResourceState operational.KubernetesResourceState `json:"resource_state"`
	}
	if json.Unmarshal(deletedObservation.Payload, &deletedState) != nil || deletedState.ResourceState.Present {
		t.Fatalf("deleted Service state: %+v", deletedObservation)
	}
	argoSource := registerDirect("argocd", operational.DirectSourceConfig{Repository: "https://git.example/checkout", Path: "apps/checkout", Revision: "abcdef"})
	argoObservation, err := assurance.AcquireDirect(ctx, argoSource.ID, session.ID, serverCA, "read-only-test-token")
	if err != nil || argoObservation.Kind != "argocd" {
		t.Fatalf("direct Argo acquisition: %+v %v", argoObservation, err)
	}
	metricSource := registerDirect("prometheus", operational.DirectSourceConfig{Queries: map[string]string{"request_count": "checkout_requests", "error_rate_pct": "checkout_5xx_pct"}, MinRequests: 20, WindowSeconds: 60})
	metricObservation, err := assurance.AcquireDirect(ctx, metricSource.ID, session.ID, serverCA, "read-only-test-token")
	if err != nil || metricObservation.Kind != "prometheus" {
		t.Fatalf("direct metrics acquisition: %+v %v", metricObservation, err)
	}
	insufficientRequests = true
	if _, err = assurance.AcquireDirect(ctx, metricSource.ID, session.ID, serverCA, "read-only-test-token"); !errors.Is(err, identity.ErrValidation) {
		t.Fatalf("insufficient request volume was accepted: %v", err)
	}
	insufficientRequests = false
	staleMetrics = true
	if _, err = assurance.AcquireDirect(ctx, metricSource.ID, session.ID, serverCA, "read-only-test-token"); !errors.Is(err, identity.ErrValidation) {
		t.Fatalf("stale metric was accepted: %v", err)
	}
	staleMetrics = false
	var rawDigest []byte
	var rawEvidence []byte
	if err = pool.QueryRow(ctx, `SELECT source_digest,source_raw FROM customer_direct_outcome_observations WHERE id=$1`, stateObservation.ID).Scan(&rawDigest, &rawEvidence); err != nil || sha256.Sum256(rawEvidence) != ([32]byte)(rawDigest) {
		t.Fatalf("direct source bytes or digest changed: %v", err)
	}
	wrongPin := sha256.Sum256([]byte("wrong pinned server"))
	wrongSource, err := assurance.RegisterDirectSource(ctx, p, w.ID, registered.Environment.ID, operational.DirectSource{Kind: "kubernetes_state", EndpointURL: server.URL, ServerCertificateSHA256: hex.EncodeToString(wrongPin[:]), Namespace: "checkout", ResourceName: "checkout"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = assurance.AcquireDirect(ctx, wrongSource.ID, session.ID, serverCA, "read-only-test-token"); err == nil {
		t.Fatal("mismatched source server certificate was accepted")
	}
	if _, err = assurance.RegisterDirectSource(ctx, p, other.ID, registered.Environment.ID, operational.DirectSource{Kind: "kubernetes_state", EndpointURL: server.URL, ServerCertificateSHA256: serverPin, Namespace: "checkout", ResourceName: "checkout"}); !errors.Is(err, auth.ErrForbidden) {
		t.Fatalf("cross-tenant source registration accepted: %v", err)
	}
	beforeBaseline := event
	beforeBaseline.AuditID = uuid.NewString()
	beforeBaseline.RequestAt = baseline.Add(-time.Second)
	beforeBaseline.StageAt = beforeBaseline.RequestAt
	beforeRaw, _ := json.Marshal(beforeBaseline)
	if _, err = intake.IngestAuthenticatedAuditEvent(ctx, cert, beforeRaw); !errors.Is(err, ErrNoMatchingAuditSession) {
		t.Fatalf("out-of-window audit accepted: %v", err)
	}
	beforeBinding, err := customerattribution.NewService(pool, auth.NewRepository()).ListDecisions(ctx, p, w.ID, registered.Environment.ID, session.ID)
	if err != nil || len(beforeBinding) != 0 {
		t.Fatalf("service account alone attributed the agent: %v %+v", err, beforeBinding)
	}
	retry, err := intake.IngestAuthenticatedAuditEvent(ctx, cert, raw)
	if err != nil || !retry.Duplicate || retry.ID != result.ID {
		t.Fatalf("idempotent retry failed: %+v %v", retry, err)
	}
	tampered := event
	tampered.ObjectRef.Name = "other"
	changed, _ := json.Marshal(tampered)
	if _, err = intake.IngestAuthenticatedAuditEvent(ctx, cert, changed); !errors.Is(err, identity.ErrConflict) {
		t.Fatalf("tampered replay accepted: %v", err)
	}
	issuerPublic, issuerPrivate, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	attribution := customerattribution.NewService(pool, auth.NewRepository())
	issuer, err := attribution.RegisterIssuer(ctx, p, w.ID, registered.Environment.ID, agentVersion, agentCertFingerprint, base64.RawURLEncoding.EncodeToString(issuerPublic))
	if err != nil {
		t.Fatal(err)
	}
	preflightInput := operational.GatewayPreflightInput{IssuerID: issuer.ID, WorkspaceID: w.ID, EnvironmentID: registered.Environment.ID, SessionID: session.ID, RunID: session.RunID, AgentVersionID: agentVersion, Namespace: "checkout", ResourceName: "checkout", Operation: "patch", Image: "checkout:v2"}
	preflightInput.ActionID = uuid.NewString()
	missingApproval, err := assurance.PreflightGateway(ctx, issuer.Credential, preflightInput)
	if err != nil || missingApproval.Decision != "denied" || missingApproval.ReasonCode != "APPROVAL_REQUIRED_OR_DENIED" {
		t.Fatalf("missing approval was not blocked: %+v %v", missingApproval, err)
	}
	preflightInput.ActionID = uuid.NewString()
	approval, err := assurance.RecordApproval(ctx, p, w.ID, registered.Environment.ID, session.ID, operational.ApprovalInput{ActionID: preflightInput.ActionID, Resource: "checkout/checkout", Decision: "approved", ValidUntil: time.Now().Add(5 * time.Minute)})
	if err != nil {
		t.Fatal(err)
	}
	allowed, err := assurance.PreflightGateway(ctx, issuer.Credential, preflightInput)
	if err != nil || allowed.Decision != "allowed" || len(allowed.ApprovalReferences) != 1 {
		t.Fatalf("valid scoped approval failed: %+v %v", allowed, err)
	}
	if _, err = assurance.PreflightGateway(ctx, issuer.Credential, preflightInput); !errors.Is(err, identity.ErrConflict) {
		t.Fatalf("replayed preflight accepted: %v", err)
	}
	if err = assurance.RevokeApproval(ctx, p, w.ID, registered.Environment.ID, session.ID, approval.ID, "customer revoked"); err != nil {
		t.Fatal(err)
	}
	preflightInput.ActionID = uuid.NewString()
	revokedApproval, err := assurance.RecordApproval(ctx, p, w.ID, registered.Environment.ID, session.ID, operational.ApprovalInput{ActionID: preflightInput.ActionID, Resource: "checkout/checkout", Decision: "approved", ValidUntil: time.Now().Add(5 * time.Minute)})
	if err != nil {
		t.Fatal(err)
	}
	if err = assurance.RevokeApproval(ctx, p, w.ID, registered.Environment.ID, session.ID, revokedApproval.ID, "customer revoked before execution"); err != nil {
		t.Fatal(err)
	}
	revoked, err := assurance.PreflightGateway(ctx, issuer.Credential, preflightInput)
	if err != nil || revoked.Decision != "denied" {
		t.Fatalf("revoked approval accepted: %+v %v", revoked, err)
	}
	preflightInput.ActionID = uuid.NewString()
	if _, err = assurance.RecordApproval(ctx, p, w.ID, registered.Environment.ID, session.ID, operational.ApprovalInput{ActionID: preflightInput.ActionID, Resource: "checkout/checkout", Decision: "approved", ValidUntil: time.Now().Add(600 * time.Millisecond)}); err != nil {
		t.Fatal(err)
	}
	time.Sleep(700 * time.Millisecond)
	expiredApproval, err := assurance.PreflightGateway(ctx, issuer.Credential, preflightInput)
	if err != nil || expiredApproval.Decision != "denied" {
		t.Fatalf("expired approval accepted: %+v %v", expiredApproval, err)
	}
	preflightInput.ActionID = uuid.NewString()
	preflightInput.Image = "checkout:unapproved"
	wrongImage, err := assurance.PreflightGateway(ctx, issuer.Credential, preflightInput)
	if err != nil || wrongImage.Decision != "denied" {
		t.Fatalf("wrong image accepted: %+v %v", wrongImage, err)
	}
	preflightInput.Image = "checkout:v2"
	preflightInput.ActionID = uuid.NewString()
	preflightInput.Namespace = "other"
	if _, err = assurance.PreflightGateway(ctx, issuer.Credential, preflightInput); !errors.Is(err, auth.ErrForbidden) {
		t.Fatalf("wrong namespace accepted: %v", err)
	}
	preflightInput.Namespace = "checkout"
	preflightInput.WorkspaceID = other.ID
	if _, err = assurance.PreflightGateway(ctx, issuer.Credential, preflightInput); !errors.Is(err, auth.ErrForbidden) {
		t.Fatalf("wrong tenant accepted: %v", err)
	}
	payload := customerattribution.ReceiptPayload{IssuerID: issuer.ID, WorkspaceID: w.ID, EnvironmentID: registered.Environment.ID, ClusterIdentity: registered.Environment.ClusterIdentifier, SessionID: session.ID, RunID: session.RunID, AgentVersionID: agentVersion, AgentCertificateSHA256: agentCertFingerprint, ExecutionSessionID: uuid.NewString(), ActionID: uuid.NewString(), AuditID: event.AuditID, Principal: event.User.Username, Operation: "patch", Namespace: "checkout", ResourceName: "checkout", ResourceUID: "uid-checkout", IssuedAt: time.Now().UTC(), ExpiresAt: time.Now().UTC().Add(2 * time.Minute)}
	sign := func(v customerattribution.ReceiptPayload) customerattribution.SignedReceipt {
		encoded, _ := json.Marshal(v)
		return customerattribution.SignedReceipt{Payload: v, Signature: base64.RawURLEncoding.EncodeToString(ed25519.Sign(issuerPrivate, encoded))}
	}
	if _, err = attribution.IngestGatewayReceipt(ctx, "invalid", sign(payload)); !errors.Is(err, auth.ErrForbidden) {
		t.Fatalf("invalid gateway credential accepted: %v", err)
	}
	decision, err := attribution.IngestGatewayReceipt(ctx, issuer.Credential, sign(payload))
	if err != nil || decision.Assessment.Status != customerattribution.StatusAttributed {
		t.Fatalf("matching independent bindings: %+v %v", decision, err)
	}
	duplicateDecision, err := attribution.IngestGatewayReceipt(ctx, issuer.Credential, sign(payload))
	if err != nil || duplicateDecision.ID != decision.ID {
		t.Fatalf("receipt retry was not idempotent: %+v %v", duplicateDecision, err)
	}
	guidanceCredential, err := assurance.IssueGuidanceCredential(ctx, p, w.ID, registered.Environment.ID, operational.GuidanceCredentialInput{AgentVersionID: agentVersion, Namespaces: []string{"checkout"}, ResourceKinds: []string{"Deployment"}, ExpiresAt: time.Now().Add(time.Hour)})
	if err != nil {
		t.Fatal(err)
	}
	guidance, err := assurance.GuidanceForAgent(ctx, guidanceCredential.Token, operational.ProposedOperation{RunID: session.RunID, SessionID: session.ID, Namespace: "checkout", ResourceKind: "Deployment", ResourceName: "checkout", Operation: "set_image"})
	if err != nil {
		t.Fatal(err)
	}
	pendingPayload := payload
	pendingPayload.ActionID = uuid.NewString()
	pendingPayload.AuditID = uuid.NewString()
	pendingPayload.GuidanceRequestID = guidance.ID
	pendingPayload.StartedAt = time.Now().UTC()
	pendingPayload.IssuedAt = time.Now().UTC()
	pendingPayload.ExpiresAt = pendingPayload.IssuedAt.Add(2 * time.Minute)
	pendingResult, err := attribution.IngestGatewayReceipt(ctx, issuer.Credential, sign(pendingPayload))
	if err != nil || pendingResult.Assessment.ReasonCode != "MISSING_AUDIT_EVIDENCE" {
		t.Fatalf("pre-audit receipt was not retained provisionally: %+v %v", pendingResult, err)
	}
	links, err := assurance.ListGuidanceOperationLinks(ctx, p, w.ID, registered.Environment.ID)
	if err != nil || len(links) == 0 || links[0].GuidanceRequestID != guidance.ID || links[0].AttributionStatus != "INCONCLUSIVE" {
		t.Fatalf("gateway guidance reference was promoted or lost: %+v %v", links, err)
	}
	wrongGuidance := pendingPayload
	wrongGuidance.ActionID = uuid.NewString()
	wrongGuidance.AuditID = uuid.NewString()
	wrongGuidance.GuidanceRequestID = uuid.NewString()
	if _, err = attribution.IngestGatewayReceipt(ctx, issuer.Credential, sign(wrongGuidance)); !errors.Is(err, auth.ErrForbidden) {
		t.Fatalf("unknown guidance reference accepted: %v", err)
	}
	pendingView, err := attribution.ListDecisions(ctx, p, w.ID, registered.Environment.ID, session.ID)
	if err != nil {
		t.Fatal(err)
	}
	visiblePending := false
	for _, item := range pendingView {
		if item.ExecutionReceiptID == pendingResult.ExecutionReceiptID && item.Assessment.ReasonCode == "MISSING_AUDIT_EVIDENCE" {
			visiblePending = true
		}
	}
	if !visiblePending {
		t.Fatal("customer could not inspect pending signed receipt")
	}
	pendingEvent := event
	pendingEvent.AuditID = pendingPayload.AuditID
	pendingEvent.RequestAt = pendingPayload.IssuedAt.Add(-time.Millisecond)
	pendingEvent.StageAt = pendingEvent.RequestAt
	pendingRaw, _ := json.Marshal(pendingEvent)
	if _, err = intake.IngestAuthenticatedAuditEvent(ctx, cert, pendingRaw); err != nil {
		t.Fatalf("late audit did not reconcile pending receipt: %v", err)
	}
	resolved, err := attribution.ListDecisions(ctx, p, w.ID, registered.Environment.ID, session.ID)
	if err != nil {
		t.Fatal(err)
	}
	matchedPending := false
	for _, item := range resolved {
		if item.ExecutionReceiptID == pendingResult.ExecutionReceiptID && item.Assessment.Status == customerattribution.StatusAttributed {
			matchedPending = true
		}
	}
	if !matchedPending {
		t.Fatalf("pending receipt lacked append-only matching decision: %+v", resolved)
	}
	var actorStatus, causalityStatus string
	if err = pool.QueryRow(ctx, `SELECT actor_identity_status,causality_status FROM customer_action_evidence WHERE id=$1`, result.EvidenceID).Scan(&actorStatus, &causalityStatus); err != nil || actorStatus != "unknown" || causalityStatus != "unproven" {
		t.Fatalf("audit principal was promoted to agent actor: actor=%s causality=%s err=%v", actorStatus, causalityStatus, err)
	}
	wrongVersion := payload
	wrongVersion.ActionID = uuid.NewString()
	wrongVersion.AgentVersionID = uuid.NewString()
	if _, err = attribution.IngestReceipt(ctx, p, w.ID, registered.Environment.ID, sign(wrongVersion)); !errors.Is(err, auth.ErrForbidden) {
		t.Fatalf("wrong agent version accepted: %v", err)
	}
	wrongAgentCertificate := payload
	wrongAgentCertificate.ActionID = uuid.NewString()
	otherFingerprint := sha256.Sum256([]byte("different agent client certificate"))
	wrongAgentCertificate.AgentCertificateSHA256 = hex.EncodeToString(otherFingerprint[:])
	if _, err = attribution.IngestReceipt(ctx, p, w.ID, registered.Environment.ID, sign(wrongAgentCertificate)); !errors.Is(err, auth.ErrForbidden) {
		t.Fatalf("wrong agent client certificate accepted: %v", err)
	}
	wrongCluster := payload
	wrongCluster.ActionID = uuid.NewString()
	wrongCluster.ClusterIdentity = "wrong-cluster"
	if _, err = attribution.IngestReceipt(ctx, p, w.ID, registered.Environment.ID, sign(wrongCluster)); !errors.Is(err, auth.ErrForbidden) {
		t.Fatalf("wrong cluster accepted: %v", err)
	}
	expired := payload
	expired.ExpiresAt = time.Now().Add(-time.Second)
	if _, err = attribution.IngestReceipt(ctx, p, w.ID, registered.Environment.ID, sign(expired)); !errors.Is(err, identity.ErrValidation) {
		t.Fatalf("expired receipt accepted: %v", err)
	}
	otherPublic, otherPrivate, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	otherIssuer, err := attribution.RegisterIssuer(ctx, p, w.ID, registered.Environment.ID, agentVersion, agentCertFingerprint, base64.RawURLEncoding.EncodeToString(otherPublic))
	if err != nil {
		t.Fatal(err)
	}
	conflictPayload := payload
	conflictPayload.IssuerID = otherIssuer.ID
	conflictPayload.ActionID = uuid.NewString()
	conflictPayload.ExecutionSessionID = uuid.NewString()
	conflictBytes, _ := json.Marshal(conflictPayload)
	conflict, err := attribution.IngestReceipt(ctx, p, w.ID, registered.Environment.ID, customerattribution.SignedReceipt{Payload: conflictPayload, Signature: base64.RawURLEncoding.EncodeToString(ed25519.Sign(otherPrivate, conflictBytes))})
	if err != nil || conflict.Assessment.ReasonCode != "CONFLICTING_EXECUTION_BINDINGS" || conflict.Assessment.Status != customerattribution.StatusInconclusive {
		t.Fatalf("competing issuer was not marked conflicting: %+v %v", conflict, err)
	}
	if _, err = attribution.IngestReceipt(ctx, p, other.ID, registered.Environment.ID, sign(payload)); !errors.Is(err, auth.ErrForbidden) {
		t.Fatalf("cross-tenant receipt accepted: %v", err)
	}
	wrong := payload
	wrong.ActionID = uuid.NewString()
	wrong.Namespace = "other"
	if _, err = attribution.IngestReceipt(ctx, p, w.ID, registered.Environment.ID, sign(wrong)); !errors.Is(err, auth.ErrForbidden) {
		t.Fatalf("wrong resource accepted: %v", err)
	}
	forged := sign(payload)
	forged.Signature = base64.RawURLEncoding.EncodeToString(make([]byte, ed25519.SignatureSize))
	if _, err = attribution.IngestReceipt(ctx, p, w.ID, registered.Environment.ID, forged); !errors.Is(err, auth.ErrForbidden) {
		t.Fatalf("forged signature accepted: %v", err)
	}
	decisions, err := attribution.ListDecisions(ctx, p, w.ID, registered.Environment.ID, session.ID)
	if err != nil || len(decisions) != 4 {
		t.Fatalf("persisted decision: %d %v", len(decisions), err)
	}
	for _, reference := range []string{"audit:" + event.AuditID, "receipt:" + decision.ExecutionReceiptID, "gateway_preflight:" + allowed.ID, "attribution:" + decision.ID} {
		detail, detailErr := assurance.ResolveEvidence(ctx, p, w.ID, registered.Environment.ID, session.ID, reference)
		if detailErr != nil || detail.Reference != reference || detail.Summary == "" {
			t.Fatalf("scoped evidence %s: %+v %v", reference, detail, detailErr)
		}
		if _, detailErr = assurance.ResolveEvidence(ctx, p, other.ID, registered.Environment.ID, session.ID, reference); !errors.Is(detailErr, auth.ErrForbidden) {
			t.Fatalf("cross-tenant evidence %s: %v", reference, detailErr)
		}
		if _, detailErr = assurance.ResolveEvidence(ctx, p, w.ID, registered.Environment.ID, uuid.NewString(), reference); !errors.Is(detailErr, auth.ErrForbidden) {
			t.Fatalf("cross-session evidence %s: %v", reference, detailErr)
		}
	}
	status, err := operational.NewService(pool, auth.NewRepository(), nil).EnvironmentStatus(ctx, p, w.ID, registered.Environment.ID)
	if err != nil || status.EvidenceSources[2].EvidenceCount != 3 || !status.EvidenceSources[2].OriginAuthenticated || status.EvidenceSources[4].EvidenceCount != 1 {
		t.Fatalf("source and current attribution status lost conflict: %+v %v", status.EvidenceSources, err)
	}
	if _, err = pool.Exec(ctx, `UPDATE customer_execution_issuers SET expires_at=now()-interval '1 second' WHERE id=$1`, issuer.ID); err != nil {
		t.Fatal(err)
	}
	if _, err = attribution.IngestGatewayReceipt(ctx, issuer.Credential, sign(payload)); !errors.Is(err, auth.ErrForbidden) {
		t.Fatalf("expired issuer credential accepted: %v", err)
	}
	if err = attribution.RevokeIssuer(ctx, p, w.ID, registered.Environment.ID, otherIssuer.ID); err != nil {
		t.Fatal(err)
	}
	if _, err = attribution.IngestGatewayReceipt(ctx, otherIssuer.Credential, customerattribution.SignedReceipt{Payload: conflictPayload, Signature: base64.RawURLEncoding.EncodeToString(ed25519.Sign(otherPrivate, conflictBytes))}); !errors.Is(err, auth.ErrForbidden) {
		t.Fatalf("revoked issuer credential accepted: %v", err)
	}
	if _, err = pool.Exec(ctx, `UPDATE customer_audit_mtls_sources SET expires_at=now()-interval '1 second' WHERE id=$1`, registeredSource.ID); err != nil {
		t.Fatal(err)
	}
	newEvent := event
	newEvent.AuditID = uuid.NewString()
	newRaw, _ := json.Marshal(newEvent)
	if _, err = intake.IngestAuthenticatedAuditEvent(ctx, cert, newRaw); !errors.Is(err, auth.ErrForbidden) {
		t.Fatalf("expired source credential accepted: %v", err)
	}
}
