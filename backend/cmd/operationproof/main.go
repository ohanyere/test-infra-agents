// operationproof prepares disposable observation sessions for hosted acceptance.
// It never authenticates Kubernetes audit origin: even a genuine Kind event is
// forwarded through the deliberately provisional collector path.
package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/evalplane/evalplane/backend/internal/agentgateway"
	"github.com/evalplane/evalplane/backend/internal/auth"
	"github.com/evalplane/evalplane/backend/internal/customercollector"
	"github.com/evalplane/evalplane/backend/internal/evaluator"
	"github.com/evalplane/evalplane/backend/internal/identity"
	"github.com/evalplane/evalplane/backend/internal/kubeaudit"
	"github.com/evalplane/evalplane/backend/internal/operational"
	"github.com/evalplane/evalplane/backend/internal/verification"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
)

type fixture struct {
	WorkspaceID         string    `json:"workspace_id"`
	OtherWorkspaceID    string    `json:"other_workspace_id"`
	EnvironmentID       string    `json:"environment_id"`
	SessionID           string    `json:"session_id"`
	AgentVersionID      string    `json:"agent_version_id"`
	ContractID          string    `json:"contract_id"`
	Namespace           string    `json:"namespace"`
	ResourceName        string    `json:"resource_name"`
	Cluster             string    `json:"cluster"`
	Email               string    `json:"email"`
	OtherEmail          string    `json:"other_email"`
	CollectorCredential string    `json:"collector_credential"`
	BaselineAt          time.Time `json:"baseline_at"`
}

type finalizer struct {
	eval   *evaluator.Service
	verify *verification.Service
}

func (f finalizer) FinalizeRun(ctx context.Context, w, run string) (string, error) {
	if _, err := f.eval.EvaluateRun(ctx, w, run); err != nil {
		return "", err
	}
	v, err := f.verify.DeriveRun(ctx, w, run)
	return v.OverallStatus, err
}

func main() {
	phase := flag.String("phase", "", "begin or finish")
	path := flag.String("fixture", "", "private fixture file")
	eventPath := flag.String("event", "", "Kind proof JSON; omit only for the labeled synthetic browser fixture")
	namespace := flag.String("namespace", "realverax-audit-proof", "registered namespace")
	name := flag.String("name", "", "Deployment name")
	cluster := flag.String("cluster", "kind:evalplane-e2e", "registered cluster label")
	flag.Parse()
	if os.Getenv("OPERATION_PROOF_DISPOSABLE") != "1" || *path == "" || (*phase != "begin" && *phase != "finish") {
		fatal("explicit disposable proof fixture authorization required")
	}
	dbURL := os.Getenv("DATABASE_URL")
	config, err := pgxpool.ParseConfig(dbURL)
	if err != nil || (config.ConnConfig.Database != "verification_e2e" && config.ConnConfig.Database != "infra_agents_test") {
		fatal("requires disposable verification_e2e or infra_agents_test database")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	pool, err := pgxpool.New(ctx, dbURL)
	if err != nil {
		fatal(err.Error())
	}
	defer pool.Close()
	var actual string
	if err = pool.QueryRow(ctx, `SELECT current_database()`).Scan(&actual); err != nil || actual != config.ConnConfig.Database {
		fatal("database identity mismatch")
	}
	verify := verification.NewService(pool, verification.NewRepository(), auth.NewRepository())
	collector, err := customercollector.NewService(pool, auth.NewRepository(), finalizer{evaluator.NewService(evaluator.NewRepository(pool)), verify})
	if err != nil {
		fatal(err.Error())
	}
	assurance := operational.NewService(pool, auth.NewRepository(), verify)
	if *phase == "begin" {
		if *name == "" || os.Getenv("OPERATION_PROOF_PASSWORD") == "" {
			fatal("name and OPERATION_PROOF_PASSWORD required")
		}
		begin(ctx, pool, collector, assurance, *path, *namespace, *name, *cluster)
		return
	}
	finish(ctx, collector, assurance, *path, *eventPath)
}

func begin(ctx context.Context, pool *pgxpool.Pool, collector *customercollector.Service, assurance *operational.Service, path, namespace, name, cluster string) {
	suffix := strings.ReplaceAll(uuid.NewString(), "-", "")[:12]
	password := os.Getenv("OPERATION_PROOF_PASSWORD")
	authService, err := auth.NewService(pool, auth.NewRepository(), identity.NewRepository(), time.Hour, auth.DefaultPasswordParams)
	check(err)
	owner, err := authService.Signup(ctx, auth.SignupInput{FullName: "Operation proof owner", Email: "operation-" + suffix + "@example.test", Password: password, Organization: identity.OrganizationInput{Name: "Operation proof " + suffix, Slug: "operation-" + suffix}, Workspace: identity.WorkspaceInput{Name: "Proof", Slug: "proof"}})
	check(err)
	other, err := authService.Signup(ctx, auth.SignupInput{FullName: "Other proof owner", Email: "other-operation-" + suffix + "@example.test", Password: password, Organization: identity.OrganizationInput{Name: "Other proof " + suffix, Slug: "other-operation-" + suffix}, Workspace: identity.WorkspaceInput{Name: "Other", Slug: "other"}})
	check(err)
	p := auth.Principal{UserID: owner.User.ID}
	var scenario, scenarioVersion, agent, agentVersion string
	check(pool.QueryRow(ctx, `INSERT INTO scenarios(owner_workspace_id,slug,name,kind,visibility,status) VALUES($1,$2,'Operation proof','custom','workspace','active') RETURNING id`, owner.Workspace.ID, "operation-"+suffix).Scan(&scenario))
	check(pool.QueryRow(ctx, `INSERT INTO scenario_versions(scenario_id,version,description,category,difficulty,duration_seconds,agent_task,evidence_requirements,source_metadata) VALUES($1,1,'Observe provisional audit','traffic','medium',8,'Observe','{"contract_version":"evidence-requirements:v1","workload_state":"REQUIRED","telemetry":"REQUIRED","git_desired_state":"NOT_APPLICABLE","argo_state":"NOT_APPLICABLE","stability":"REQUIRED"}'::jsonb,'{}'::jsonb) RETURNING id`, scenario).Scan(&scenarioVersion))
	check(pool.QueryRow(ctx, `INSERT INTO agents(owner_workspace_id,slug,name,visibility,status) VALUES($1,$2,'Configured fixture agent','workspace','active') RETURNING id`, owner.Workspace.ID, "operation-"+suffix).Scan(&agent))
	check(pool.QueryRow(ctx, `INSERT INTO agent_versions(agent_id,version_label,interface_kind,implementation_identity) VALUES($1,'v1','http','{"contract_version":"agent-implementation-identity:v1","kind":"declared","identifier":"operation-proof-fixture","provenance_level":"declared"}') RETURNING id`, agent).Scan(&agentVersion))
	registered, err := collector.Register(ctx, p, owner.Workspace.ID, customercollector.CreateEnvironmentInput{Name: "Proof environment", ClusterIdentifier: cluster, Namespaces: []string{namespace}, ResourceKinds: []string{"Deployment", "Pod"}, Tier: "staging", AllowForwardedAudit: true})
	check(err)
	contract, err := assurance.SaveContract(ctx, p, owner.Workspace.ID, registered.Environment.ID, operational.ContractInput{Active: true, Rules: []operational.Rule{{Type: "forbid_operation", Operation: "patch", Namespace: namespace, ResourceKind: "Deployment", ResourceName: name}}})
	check(err)
	session, err := collector.StartSession(ctx, p, owner.Workspace.ID, registered.Environment.ID, customercollector.CreateSessionInput{AgentVersionID: agentVersion, ScenarioVersionID: scenarioVersion})
	check(err)
	baseline := time.Now().UTC().Truncate(time.Microsecond)
	_, err = collector.Ingest(ctx, session.ID, registered.CollectorCredential, batch(1, "baseline", baseline, namespace, name))
	check(err)
	f := fixture{WorkspaceID: owner.Workspace.ID, OtherWorkspaceID: other.Workspace.ID, EnvironmentID: registered.Environment.ID, SessionID: session.ID, AgentVersionID: agentVersion, ContractID: contract.ID, Namespace: namespace, ResourceName: name, Cluster: cluster, Email: owner.User.Email, OtherEmail: other.User.Email, CollectorCredential: registered.CollectorCredential, BaselineAt: baseline}
	data, err := json.Marshal(f)
	check(err)
	file, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
	check(err)
	_, err = file.Write(data)
	check(err)
	check(file.Close())
}

func finish(ctx context.Context, collector *customercollector.Service, assurance *operational.Service, path, eventPath string) {
	private, err := os.ReadFile(path)
	check(err)
	var f fixture
	check(json.Unmarshal(private, &f))
	var event kubeaudit.Event
	if eventPath == "" {
		now := time.Now().UTC().Truncate(time.Microsecond)
		event = kubeaudit.Event{APIVersion: kubeaudit.AuditAPIVersion, Kind: "Event", AuditID: uuid.NewString(), Stage: "ResponseComplete", Verb: "patch", RequestAt: now, StageAt: now, User: kubeaudit.User{Username: "synthetic-browser-fixture"}, ObjectRef: kubeaudit.ObjectRef{APIGroup: "apps", APIVersion: "v1", Resource: "deployments", Namespace: f.Namespace, Name: f.ResourceName}, Response: kubeaudit.Response{Code: 200}}
	} else {
		data, readErr := os.ReadFile(eventPath)
		check(readErr)
		var proof struct {
			Event kubeaudit.Event `json:"event"`
		}
		check(json.Unmarshal(data, &proof))
		event = proof.Event
		if event.AuditID == "" {
			fatal("Kind artifact lacks audit event")
		}
	}
	if event.Verb != "patch" || event.ObjectRef.Namespace != f.Namespace || event.ObjectRef.Name != f.ResourceName {
		fatal("audit event does not match registered operation scope")
	}
	encoded, err := json.Marshal(event)
	check(err)
	audit, err := collector.IngestForwardedAudit(ctx, f.SessionID, f.CollectorCredential, customercollector.ForwardedAuditInput{ClaimedClusterIdentity: f.Cluster, Event: encoded})
	check(err)
	if audit.AuditOriginStatus != "unverified" || audit.TrustClassification != customercollector.AuditCandidateTrust {
		fatal("runner audit candidate was promoted")
	}
	first := time.Now().UTC().Truncate(time.Microsecond)
	if !first.After(f.BaselineAt) {
		first = f.BaselineAt.Add(time.Microsecond)
	}
	for i := int64(2); i <= 4; i++ {
		at := first.Add(time.Duration(i-2) * time.Microsecond)
		if wait := time.Until(at); wait > 0 {
			time.Sleep(wait)
		}
		phase := "stability"
		if i == 4 {
			phase = "final"
			if wait := time.Until(f.BaselineAt.Add(8*time.Second + time.Millisecond)); wait > 0 {
				time.Sleep(wait)
			}
		}
		_, err = collector.Ingest(ctx, f.SessionID, f.CollectorCredential, batch(i, phase, at, f.Namespace, f.ResourceName))
		check(err)
	}
	// Authenticated user credentials are used only to establish fixture tenancy.
	// Assessment itself runs through the same persisted service used by the API.
	report, err := assurance.AssessRunSystem(ctx, f.WorkspaceID, f.EnvironmentID, f.SessionID)
	check(err)
	ruleID := fmt.Sprintf("%s:v1:rule:1", f.ContractID)
	var foundRule, foundAudit, foundAction bool
	for _, finding := range report.Findings {
		if finding.RuleID == ruleID && finding.Code == "UNVERIFIED_FORBIDDEN_OPERATION_CANDIDATE" && finding.Status == operational.Inconclusive && finding.AffectedResource == f.Namespace+"/"+f.ResourceName {
			for _, evidence := range finding.Evidence {
				if evidence.Reference == event.AuditID {
					foundRule = true
				}
			}
		}
	}
	for _, action := range report.ObservedActions {
		if action.ID == event.AuditID && action.Operation == "patch" && action.Kind == "Deployment" && action.Namespace == f.Namespace && action.Name == f.ResourceName && !action.OriginAuthenticated {
			foundAction = true
		}
	}
	for _, op := range report.OperationExplanations {
		if op.Key == "audit:"+event.AuditID && op.Resource == f.Namespace+"/"+f.ResourceName && op.AttributionStatus == "not established" && strings.Contains(op.ObservationStatus, "origin unverified") {
			foundAudit = true
		}
	}
	if !foundRule || !foundAudit || !foundAction || report.CanonicalOverallStatus != verification.StatusInconclusive {
		fatal("provisional operation explanation or exact rule finding missing")
	}
	public := map[string]any{"workspace_id": f.WorkspaceID, "other_workspace_id": f.OtherWorkspaceID, "environment_id": f.EnvironmentID, "session_id": f.SessionID, "agent_version_id": f.AgentVersionID, "contract_id": f.ContractID, "rule_id": ruleID, "namespace": f.Namespace, "resource_name": f.ResourceName, "audit_id": event.AuditID, "source_event_digest": audit.SourceEventDigest, "evidence_reference": "audit:" + event.AuditID, "email": f.Email, "other_email": f.OtherEmail, "source_classification": "INSUFFICIENT_PROVENANCE", "audit_origin_status": "unverified", "fixture_kind": map[bool]string{true: "synthetic_browser_fixture", false: "runner_kind_audit"}[eventPath == ""]}
	check(json.NewEncoder(os.Stdout).Encode(public))
}

func batch(sequence int64, phase string, at time.Time, namespace, name string) customercollector.EvidenceBatch {
	observation := agentgateway.Observation{Timestamp: at, Workloads: []agentgateway.WorkloadObservation{{Kind: "Deployment", Namespace: namespace, Name: name, DesiredReplicas: 1, ReadyReplicas: 1, AvailableReplicas: 1, UpdatedReplicas: 1}}, Pods: []agentgateway.PodObservation{}, Metrics: map[string]any{}, RecentEvents: []agentgateway.RecentEvent{}}
	return customercollector.EvidenceBatch{BatchID: uuid.NewString(), Sequence: sequence, SchemaVersion: customercollector.ContractVersion, Phase: phase, CapturedAt: at, Observation: observation}
}
func check(err error) {
	if err != nil {
		fatal(err.Error())
	}
}
func fatal(message string) { fmt.Fprintln(os.Stderr, message); os.Exit(1) }
