package customercollector

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"encoding/pem"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/evalplane/evalplane/backend/internal/agentgateway"
	"github.com/evalplane/evalplane/backend/internal/auth"
	"github.com/evalplane/evalplane/backend/internal/evaluator"
	"github.com/evalplane/evalplane/backend/internal/identity"
	"github.com/evalplane/evalplane/backend/internal/kubeaudit"
	"github.com/evalplane/evalplane/backend/internal/operational"
	"github.com/evalplane/evalplane/backend/internal/verification"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
)

type canonicalFinalizer struct {
	eval   *evaluator.Service
	verify *verification.Service
}

func (f canonicalFinalizer) FinalizeRun(ctx context.Context, w, run string) (string, error) {
	if _, err := f.eval.EvaluateRun(ctx, w, run); err != nil {
		return "", err
	}
	v, err := f.verify.DeriveRun(ctx, w, run)
	return v.OverallStatus, err
}

func TestCustomerCollectorCanonicalIngestionIntegration(t *testing.T) {
	databaseURL := os.Getenv("TEST_DATABASE_URL")
	if databaseURL == "" {
		t.Skip("TEST_DATABASE_URL is not set")
	}
	config, err := pgxpool.ParseConfig(databaseURL)
	if err != nil || !strings.HasSuffix(config.ConnConfig.Database, "_test") && !strings.HasPrefix(config.ConnConfig.Database, "realverax_stage10a_") {
		t.Fatal("TEST_DATABASE_URL must name an explicitly disposable test database")
	}
	ctx := context.Background()
	pool, err := pgxpool.New(ctx, databaseURL)
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	suffix := strings.ReplaceAll(uuid.NewString(), "-", "")[:12]
	ids := identity.NewRepository()
	org, err := ids.CreateOrganization(ctx, pool, identity.OrganizationInput{Name: "Collector test " + suffix, Slug: "collector-test-" + suffix})
	if err != nil {
		t.Fatal(err)
	}
	workspace, err := ids.CreateWorkspace(ctx, pool, org.ID, identity.WorkspaceInput{Name: "Customer", Slug: "customer"})
	if err != nil {
		t.Fatal(err)
	}
	otherWorkspace, err := ids.CreateWorkspace(ctx, pool, org.ID, identity.WorkspaceInput{Name: "Other customer", Slug: "other-" + suffix})
	if err != nil {
		t.Fatal(err)
	}
	user, err := ids.CreateUser(ctx, pool, identity.UserInput{Email: "collector-" + suffix + "@example.test", FullName: "Collector Test"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = ids.CreateMembership(ctx, pool, workspace.ID, user.ID, "owner"); err != nil {
		t.Fatal(err)
	}
	otherWorkspaceUser, err := ids.CreateUser(ctx, pool, identity.UserInput{Email: "other-collector-" + suffix + "@example.test", FullName: "Other Workspace Collector"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = ids.CreateMembership(ctx, pool, otherWorkspace.ID, otherWorkspaceUser.ID, "owner"); err != nil {
		t.Fatal(err)
	}
	principal := auth.Principal{UserID: user.ID}
	otherWorkspacePrincipal := auth.Principal{UserID: otherWorkspaceUser.ID}
	viewer, err := ids.CreateUser(ctx, pool, identity.UserInput{Email: "collector-viewer-" + suffix + "@example.test", FullName: "Collector Viewer"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = ids.CreateMembership(ctx, pool, workspace.ID, viewer.ID, "viewer"); err != nil {
		t.Fatal(err)
	}
	var scenarioID, scenarioVersionID, agentID, agentVersionID string
	err = pool.QueryRow(ctx, `INSERT INTO scenarios(owner_workspace_id,slug,name,kind,visibility,status) VALUES($1,$2,'Observed','custom','workspace','active') RETURNING id`, workspace.ID, "observed-"+suffix).Scan(&scenarioID)
	if err != nil {
		t.Fatal(err)
	}
	err = pool.QueryRow(ctx, `INSERT INTO scenario_versions(scenario_id,version,description,category,difficulty,duration_seconds,agent_task,evidence_requirements,source_metadata) VALUES($1,1,'Observe external state','traffic','medium',8,'Observe whether the customer workload is healthy.','{"contract_version":"evidence-requirements:v1","workload_state":"REQUIRED","telemetry":"REQUIRED","git_desired_state":"NOT_APPLICABLE","argo_state":"NOT_APPLICABLE","stability":"REQUIRED"}'::jsonb,'{}'::jsonb) RETURNING id`, scenarioID).Scan(&scenarioVersionID)
	if err != nil {
		t.Fatal(err)
	}
	err = pool.QueryRow(ctx, `INSERT INTO agents(owner_workspace_id,slug,name,visibility,status) VALUES($1,$2,'External','workspace','active') RETURNING id`, workspace.ID, "external-"+suffix).Scan(&agentID)
	if err != nil {
		t.Fatal(err)
	}
	err = pool.QueryRow(ctx, `INSERT INTO agent_versions(agent_id,version_label,interface_kind,implementation_identity) VALUES($1,'v1','http','{"contract_version":"agent-implementation-identity:v1","kind":"declared","identifier":"external-agent-v1","provenance_level":"declared"}') RETURNING id`, agentID).Scan(&agentVersionID)
	if err != nil {
		t.Fatal(err)
	}
	verify := verification.NewService(pool, verification.NewRepository(), auth.NewRepository())
	eval := evaluator.NewService(evaluator.NewRepository(pool))
	service, err := NewService(pool, auth.NewRepository(), canonicalFinalizer{eval: eval, verify: verify})
	if err != nil {
		t.Fatal(err)
	}
	registered, err := service.Register(ctx, principal, workspace.ID, CreateEnvironmentInput{Name: "Prod checkout", ClusterIdentifier: "customer-cluster-1", Namespaces: []string{"checkout"}, Tier: "production", AllowForwardedAudit: true})
	if err != nil {
		t.Fatal(err)
	}
	if registered.CollectorCredential == "" {
		t.Fatal("one-time collector credential missing")
	}
	otherEnvironments, err := service.List(ctx, principal, otherWorkspace.ID)
	if !errors.Is(err, auth.ErrForbidden) || len(otherEnvironments) != 0 {
		t.Fatalf("cross-workspace environment list returned %d environments, error=%v; want no data and forbidden", len(otherEnvironments), err)
	}
	workspaceEnvironments, err := service.List(ctx, principal, workspace.ID)
	if err != nil || len(workspaceEnvironments) != 1 || workspaceEnvironments[0].ID != registered.Environment.ID {
		t.Fatalf("authorized environment list=%+v error=%v", workspaceEnvironments, err)
	}
	viewerEnvironments, err := service.List(ctx, auth.Principal{UserID: viewer.ID}, workspace.ID)
	if err != nil || len(viewerEnvironments) != 1 || viewerEnvironments[0].ID != registered.Environment.ID {
		t.Fatalf("authorized viewer environment list=%+v error=%v", viewerEnvironments, err)
	}
	secondEnvironment, err := service.Register(ctx, principal, workspace.ID, CreateEnvironmentInput{Name: "Staging checkout", ClusterIdentifier: "customer-cluster-2", Namespaces: []string{"checkout"}, Tier: "staging"})
	if err != nil {
		t.Fatal(err)
	}
	otherWorkspaceEnvironment, err := service.Register(ctx, otherWorkspacePrincipal, otherWorkspace.ID, CreateEnvironmentInput{Name: "Other workspace", ClusterIdentifier: "other-customer-cluster", Namespaces: []string{"checkout"}, Tier: "staging"})
	if err != nil {
		t.Fatal(err)
	}
	assurance := operational.NewService(pool, auth.NewRepository(), verify)
	initialSources, err := assurance.EnvironmentStatus(ctx, principal, workspace.ID, registered.Environment.ID)
	if err != nil || len(initialSources.EvidenceSources) != 5 || !initialSources.EvidenceSources[1].Enabled || initialSources.EvidenceSources[1].EvidenceCount != 0 || initialSources.EvidenceSources[1].OriginAuthenticated {
		t.Fatalf("registered audit intake was misreported: %+v %v", initialSources.EvidenceSources, err)
	}
	otherSources, err := assurance.EnvironmentStatus(ctx, principal, workspace.ID, secondEnvironment.Environment.ID)
	if err != nil || otherSources.EvidenceSources[1].Enabled {
		t.Fatalf("audit intake enabled without opt-in: %+v %v", otherSources.EvidenceSources, err)
	}
	if _, err = assurance.EnvironmentStatus(ctx, principal, otherWorkspace.ID, otherWorkspaceEnvironment.Environment.ID); !errors.Is(err, auth.ErrForbidden) {
		t.Fatalf("cross-workspace source status disclosed: %v", err)
	}
	floor := float64(3)
	maxError := float64(1)
	contract, contractErr := assurance.SaveContract(ctx, principal, workspace.ID, registered.Environment.ID, operational.ContractInput{Active: true, Service: "checkout/checkout", Rules: []operational.Rule{{Type: "min_available_replicas", Namespace: "checkout", ResourceName: "checkout", Minimum: &floor}, {Type: "max_http_5xx_pct", Maximum: &maxError}}})
	if contractErr != nil || contract.Version != 1 {
		t.Fatalf("save operational contract: %+v %v", contract, contractErr)
	}
	session, err := service.StartSession(ctx, principal, workspace.ID, registered.Environment.ID, CreateSessionInput{AgentVersionID: agentVersionID, ScenarioVersionID: scenarioVersionID})
	if err != nil {
		t.Fatal(err)
	}
	otherSession, err := service.StartSession(ctx, principal, workspace.ID, registered.Environment.ID, CreateSessionInput{AgentVersionID: agentVersionID, ScenarioVersionID: scenarioVersionID})
	if err != nil {
		t.Fatal(err)
	}
	processOwnReconciliation := func(want string, minRevisions int) []ReconciliationRevision {
		t.Helper()
		// The worker consumes a shared queue, which can include cases left by
		// other test fixtures. Wait for this session's revision, not one dequeue.
		for attempt := 0; attempt < 64; attempt++ {
			if _, processed, processErr := ProcessNextAuditReconciliation(ctx, pool); processErr != nil || !processed {
				t.Fatalf("audit reconciliation: processed=%v err=%v", processed, processErr)
			}
			history, historyErr := service.ListAuditReconciliation(ctx, principal, workspace.ID, registered.Environment.ID, session.ID)
			if historyErr != nil {
				t.Fatal(historyErr)
			}
			if len(history) >= minRevisions && history[len(history)-1].Result == want {
				return history
			}
		}
		t.Fatalf("audit reconciliation did not reach %s after 64 worker cycles", want)
		return nil
	}
	approval, approvalErr := assurance.RecordApproval(ctx, principal, workspace.ID, registered.Environment.ID, session.ID, operational.ApprovalInput{ActionID: uuid.NewString(), Resource: "checkout/checkout", Decision: "approved", Reason: "Customer change window", ValidUntil: time.Now().Add(time.Hour)})
	if approvalErr != nil || approval.ID == "" {
		t.Fatalf("customer approval record: %+v %v", approval, approvalErr)
	}
	if _, approvalErr = assurance.RecordApproval(ctx, auth.Principal{UserID: viewer.ID}, workspace.ID, registered.Environment.ID, session.ID, operational.ApprovalInput{ActionID: uuid.NewString(), Resource: "checkout/checkout", Decision: "approved", ValidUntil: time.Now().Add(time.Hour)}); !errors.Is(approvalErr, auth.ErrForbidden) {
		t.Fatalf("viewer recorded approval: %v", approvalErr)
	}
	rotated, err := service.Rotate(ctx, principal, workspace.ID, registered.Environment.ID)
	if err != nil || rotated.CollectorCredential == registered.CollectorCredential || rotated.Environment.ConnectionStatus != "pending" {
		t.Fatalf("credential rotation failed: err=%v", err)
	}
	auditAt := time.Now().UTC().Truncate(time.Microsecond)
	auditEvent := kubeaudit.Event{APIVersion: kubeaudit.AuditAPIVersion, Kind: "Event", AuditID: uuid.NewString(), Stage: "ResponseComplete", Verb: "patch", RequestAt: auditAt, StageAt: auditAt,
		User: kubeaudit.User{Username: "system:serviceaccount:agents:shared"}, ObjectRef: kubeaudit.ObjectRef{APIGroup: "apps", APIVersion: "v1", Resource: "deployments", Namespace: "checkout", Name: "checkout", UID: "uid-checkout"}, Response: kubeaudit.Response{Code: 200}}
	auditJSON, _ := json.Marshal(auditEvent)
	auditInput := ForwardedAuditInput{ClaimedClusterIdentity: registered.Environment.ClusterIdentifier, Event: auditJSON}
	forwarded, err := service.IngestForwardedAudit(ctx, session.ID, rotated.CollectorCredential, auditInput)
	if err != nil || forwarded.TrustClassification != AuditCandidateTrust || forwarded.AuditOriginStatus != "unverified" || forwarded.CollectorAuthenticationStatus != "verified" {
		t.Fatalf("forwarded audit trust: %+v %v", forwarded, err)
	}
	auditSources, statusErr := assurance.EnvironmentStatus(ctx, principal, workspace.ID, registered.Environment.ID)
	if statusErr != nil || auditSources.EvidenceSources[1].EvidenceCount != 1 || auditSources.EvidenceSources[1].LastReceivedAt == nil || auditSources.EvidenceSources[1].OriginAuthenticated {
		t.Fatalf("received audit candidate was misreported: %+v %v", auditSources.EvidenceSources, statusErr)
	}
	duplicateAudit, err := service.IngestForwardedAudit(ctx, session.ID, rotated.CollectorCredential, auditInput)
	if err != nil || !duplicateAudit.Duplicate || duplicateAudit.ID != forwarded.ID {
		t.Fatalf("audit retry: %+v %v", duplicateAudit, err)
	}
	if _, err = service.IngestForwardedAudit(ctx, otherSession.ID, rotated.CollectorCredential, auditInput); !errors.Is(err, identity.ErrConflict) {
		t.Fatalf("cross-session source event replay: %v", err)
	}
	wrongClusterAudit := auditInput
	wrongClusterAudit.ClaimedClusterIdentity = "wrong-cluster"
	if _, err = service.IngestForwardedAudit(ctx, session.ID, rotated.CollectorCredential, wrongClusterAudit); !errors.Is(err, auth.ErrForbidden) {
		t.Fatalf("wrong audit cluster: %v", err)
	}
	if _, err = service.IngestForwardedAudit(ctx, session.ID, registered.CollectorCredential, auditInput); !errors.Is(err, auth.ErrForbidden) {
		t.Fatalf("rotated audit credential: %v", err)
	}
	modifiedAudit := auditEvent
	modifiedAudit.ObjectRef.Name = "different"
	modifiedJSON, _ := json.Marshal(modifiedAudit)
	if _, err = service.IngestForwardedAudit(ctx, session.ID, rotated.CollectorCredential, ForwardedAuditInput{ClaimedClusterIdentity: registered.Environment.ClusterIdentifier, Event: modifiedJSON}); !errors.Is(err, identity.ErrConflict) {
		t.Fatalf("contradictory audit retry: %v", err)
	}
	auditList, err := service.ListForwardedAudits(ctx, principal, workspace.ID, registered.Environment.ID, session.ID)
	if err != nil || len(auditList) != 1 || auditList[0].ID != forwarded.ID {
		t.Fatalf("audit retrieval: %+v %v", auditList, err)
	}
	if _, err = service.ListForwardedAudits(ctx, principal, otherWorkspace.ID, registered.Environment.ID, session.ID); !errors.Is(err, auth.ErrForbidden) {
		t.Fatalf("cross-tenant audit retrieval: %v", err)
	}
	// Keep synthetic observations safely behind server receipt time. Claims
	// ending after the current server time are correctly rejected, even when
	// their client-side timestamps are otherwise close to the baseline.
	base := time.Now().UTC().Add(-2 * time.Second).Truncate(time.Microsecond)
	phases := []string{"baseline", "stability", "stability", "final"}
	var finalBatch EvidenceBatch
	var baselineReceived time.Time
	for index, phase := range phases {
		captured := base.Add(time.Duration(index+1) * time.Second).Truncate(time.Microsecond)
		// Keep the final sample behind the server receipt clock so a metric
		// sampled at that instant remains valid after session completion.
		if phase == "final" {
			if wait := time.Until(captured); wait > 0 {
				time.Sleep(wait + time.Millisecond)
			}
		}
		obs := agentgateway.Observation{Timestamp: captured, Workloads: []agentgateway.WorkloadObservation{{Kind: "Deployment", Name: "checkout", Namespace: "checkout", DesiredReplicas: 2, ReadyReplicas: 2, AvailableReplicas: 2, UpdatedReplicas: 2, UID: "uid-checkout", ResourceVersion: time.Now().Format("150405")}}, Pods: []agentgateway.PodObservation{}, Metrics: map[string]any{}, RecentEvents: []agentgateway.RecentEvent{}}
		batch := EvidenceBatch{BatchID: uuid.NewString(), Sequence: int64(index + 1), SchemaVersion: ContractVersion, Phase: phase, CapturedAt: captured, Observation: obs}
		if phase == "final" {
			finalBatch = batch
		}
		if index == 0 {
			if _, sessionErr := service.StartSession(ctx, principal, otherWorkspace.ID, otherWorkspaceEnvironment.Environment.ID, CreateSessionInput{AgentVersionID: agentVersionID, ScenarioVersionID: scenarioVersionID}); !errors.Is(sessionErr, auth.ErrForbidden) {
				t.Fatalf("cross-workspace session creation error=%v, want forbidden", sessionErr)
			}
			if _, sessionErr := service.StartSession(ctx, auth.Principal{UserID: viewer.ID}, workspace.ID, registered.Environment.ID, CreateSessionInput{AgentVersionID: agentVersionID, ScenarioVersionID: scenarioVersionID}); !errors.Is(sessionErr, auth.ErrForbidden) {
				t.Fatalf("viewer session creation error=%v, want forbidden", sessionErr)
			}
			if _, ingestErr := service.Ingest(ctx, session.ID, registered.CollectorCredential, batch); !errors.Is(ingestErr, auth.ErrForbidden) {
				t.Fatalf("rotated credential remained valid: %v", ingestErr)
			}
			if _, ingestErr := service.Ingest(ctx, session.ID, secondEnvironment.CollectorCredential, batch); !errors.Is(ingestErr, auth.ErrForbidden) {
				t.Fatalf("wrong-environment credential accepted: %v", ingestErr)
			}
			if _, ingestErr := service.Ingest(ctx, session.ID, otherWorkspaceEnvironment.CollectorCredential, batch); !errors.Is(ingestErr, auth.ErrForbidden) {
				t.Fatalf("cross-workspace credential accepted: %v", ingestErr)
			}
			wrongScope := batch
			wrongScope.Observation.Workloads = []agentgateway.WorkloadObservation{{Kind: "Deployment", Name: "checkout", Namespace: "default", DesiredReplicas: 1}}
			if _, ingestErr := service.Ingest(ctx, session.ID, rotated.CollectorCredential, wrongScope); !errors.Is(ingestErr, auth.ErrForbidden) {
				t.Fatalf("out-of-scope resource accepted: %v", ingestErr)
			}
			wrongOrder := batch
			wrongOrder.Sequence = 2
			if _, ingestErr := service.Ingest(ctx, session.ID, rotated.CollectorCredential, wrongOrder); !errors.Is(ingestErr, identity.ErrConflict) {
				t.Fatalf("out-of-order evidence accepted: %v", ingestErr)
			}
		}
		result, ingestErr := service.Ingest(ctx, session.ID, rotated.CollectorCredential, batch)
		if ingestErr != nil {
			t.Fatalf("phase %s: %v", phase, ingestErr)
		}
		if index < 3 && !result.Accepted {
			t.Fatalf("phase %s not accepted", phase)
		}
		if index == 0 {
			baselineReceived = result.ReceivedAt
			status, statusErr := assurance.EnvironmentStatus(ctx, principal, workspace.ID, registered.Environment.ID)
			if statusErr != nil || status.EvidenceSources[0].EvidenceCount != 1 || status.EvidenceSources[0].Reachability != "recently_seen" || status.EvidenceSources[0].OriginAuthenticated || len(status.Sessions) != 2 || status.Sessions[0].ObservationBatches+status.Sessions[1].ObservationBatches != 1 {
				t.Fatalf("observation intake status was misreported: %+v %v", status, statusErr)
			}
			claimInput := SubmitActionClaimInput{
				ContractVersion: ActionClaimContractVersion,
				AgentVersionID:  agentVersionID,
				ClaimID:         uuid.NewString(), IdempotencyKey: uuid.NewString(),
				ActionType: "set_workload_image",
				Resource:   ClaimedResource{Kind: "Deployment", Namespace: "checkout", Name: "checkout", UID: "uid-checkout"},
				StartedAt:  captured, CompletedAt: captured.Add(100 * time.Millisecond),
				Outcome: "executed", ExternalTrace: "abcdef0123456789",
			}
			beforeWindow := claimInput
			beforeWindow.ClaimID = uuid.NewString()
			beforeWindow.IdempotencyKey = uuid.NewString()
			beforeWindow.StartedAt = captured.Add(-time.Microsecond)
			if _, claimErr := service.SubmitActionClaim(ctx, principal, workspace.ID, registered.Environment.ID, session.ID, beforeWindow); !errors.Is(claimErr, identity.ErrValidation) {
				t.Fatalf("claim starting before baseline error=%v, want validation error", claimErr)
			}
			afterWindow := claimInput
			afterWindow.ClaimID = uuid.NewString()
			afterWindow.IdempotencyKey = uuid.NewString()
			afterWindow.CompletedAt = time.Now().UTC().Add(time.Second)
			if _, claimErr := service.SubmitActionClaim(ctx, principal, workspace.ID, registered.Environment.ID, session.ID, afterWindow); !errors.Is(claimErr, identity.ErrValidation) {
				t.Fatalf("claim ending after server receipt time error=%v, want validation error", claimErr)
			}
			var rejectedClaims int
			if err := pool.QueryRow(ctx, `SELECT count(*) FROM customer_agent_action_claims WHERE session_id=$1`, session.ID).Scan(&rejectedClaims); err != nil || rejectedClaims != 0 {
				t.Fatalf("rejected claims persisted: count=%d error=%v", rejectedClaims, err)
			}
			if _, claimErr := service.SubmitActionClaim(ctx, auth.Principal{UserID: viewer.ID}, workspace.ID, registered.Environment.ID, session.ID, claimInput); !errors.Is(claimErr, auth.ErrForbidden) {
				t.Fatalf("viewer action claim error=%v, want forbidden", claimErr)
			}
			if _, claimErr := service.SubmitActionClaim(ctx, principal, otherWorkspace.ID, registered.Environment.ID, session.ID, claimInput); !errors.Is(claimErr, auth.ErrForbidden) {
				t.Fatalf("cross-workspace action claim error=%v, want forbidden", claimErr)
			}
			wrongAgent := claimInput
			wrongAgent.AgentVersionID = uuid.NewString()
			if _, claimErr := service.SubmitActionClaim(ctx, principal, workspace.ID, registered.Environment.ID, session.ID, wrongAgent); !errors.Is(claimErr, auth.ErrForbidden) {
				t.Fatalf("wrong agent version error=%v, want forbidden", claimErr)
			}
			if _, claimErr := service.SubmitActionClaim(ctx, principal, workspace.ID, secondEnvironment.Environment.ID, session.ID, claimInput); !errors.Is(claimErr, auth.ErrForbidden) {
				t.Fatalf("wrong environment error=%v, want forbidden", claimErr)
			}
			if _, claimErr := service.SubmitActionClaim(ctx, principal, workspace.ID, registered.Environment.ID, uuid.NewString(), claimInput); !errors.Is(claimErr, auth.ErrForbidden) {
				t.Fatalf("wrong session error=%v, want forbidden", claimErr)
			}
			claim, claimErr := service.SubmitActionClaim(ctx, principal, workspace.ID, registered.Environment.ID, session.ID, claimInput)
			if claimErr != nil || claim.Trust != ActionClaimTrust || claim.WorkspaceID != workspace.ID || claim.RunID != session.RunID || claim.SubmitterUserID != user.ID {
				t.Fatalf("action claim was not persisted with server-derived provenance: claim=%+v error=%v", claim, claimErr)
			}
			retry, claimErr := service.SubmitActionClaim(ctx, principal, workspace.ID, registered.Environment.ID, session.ID, claimInput)
			if claimErr != nil || !retry.Duplicate || retry.ClaimID != claim.ClaimID {
				t.Fatalf("identical action claim retry was not idempotent: claim=%+v error=%v", retry, claimErr)
			}
			conflict := claimInput
			conflict.Outcome = "failed"
			if _, claimErr := service.SubmitActionClaim(ctx, principal, workspace.ID, registered.Environment.ID, session.ID, conflict); !errors.Is(claimErr, identity.ErrConflict) {
				t.Fatalf("conflicting duplicate action claim error=%v, want conflict", claimErr)
			}
			claims, claimErr := service.ListActionClaims(ctx, principal, workspace.ID, registered.Environment.ID, session.ID)
			if claimErr != nil || len(claims) != 1 || claims[0].Trust != ActionClaimTrust {
				t.Fatalf("persisted action claims=%+v error=%v", claims, claimErr)
			}
			foreignClaims, claimErr := service.ListActionClaims(ctx, principal, otherWorkspace.ID, otherWorkspaceEnvironment.Environment.ID, session.ID)
			if !errors.Is(claimErr, auth.ErrForbidden) || len(foreignClaims) != 0 {
				t.Fatalf("cross-workspace action-claim list returned %d claims, error=%v; want no data and forbidden", len(foreignClaims), claimErr)
			}
			foreignClaims, claimErr = service.ListActionClaims(ctx, otherWorkspacePrincipal, workspace.ID, registered.Environment.ID, session.ID)
			if !errors.Is(claimErr, auth.ErrForbidden) || len(foreignClaims) != 0 {
				t.Fatalf("other-workspace member accessed session claims: returned %d claims, error=%v", len(foreignClaims), claimErr)
			}
			if _, err = pool.Exec(ctx, `UPDATE customer_audit_reconciliation_cases SET next_attempt_at=now() WHERE session_id=$1 AND claim_id=$2`, session.ID, claim.ClaimID); err != nil {
				t.Fatal(err)
			}
			history := processOwnReconciliation("insufficient", 3)
			if _, historyErr := service.ListAuditReconciliation(ctx, principal, otherWorkspace.ID, registered.Environment.ID, session.ID); !errors.Is(historyErr, auth.ErrForbidden) {
				t.Fatalf("cross-tenant reconciliation retrieval: %v", historyErr)
			}
			lateEvent := auditEvent
			lateEvent.AuditID = uuid.NewString()
			lateEvent.StageAt = time.Now().UTC().Truncate(time.Microsecond)
			lateEvent.RequestAt = lateEvent.StageAt
			lateJSON, _ := json.Marshal(lateEvent)
			if _, err = service.IngestForwardedAudit(ctx, session.ID, rotated.CollectorCredential, ForwardedAuditInput{ClaimedClusterIdentity: registered.Environment.ClusterIdentifier, Event: lateJSON}); err != nil {
				t.Fatalf("late audit candidate: %v", err)
			}
			lateHistory := processOwnReconciliation("conflicting", len(history)+1)
			if len(lateHistory) <= len(history) || history[len(history)-1].Result != "insufficient" {
				t.Fatalf("late reconciliation history=%+v", lateHistory)
			}
			viewerClaims, claimErr := service.ListActionClaims(ctx, auth.Principal{UserID: viewer.ID}, workspace.ID, registered.Environment.ID, session.ID)
			if claimErr != nil || len(viewerClaims) != 1 || viewerClaims[0].ClaimID != claim.ClaimID {
				t.Fatalf("authorized viewer action-claim list=%+v error=%v", viewerClaims, claimErr)
			}
		}
		if index == 3 && (result.VerificationStatus != verification.StatusInconclusive) {
			t.Fatalf("observation-only result=%s, want INCONCLUSIVE", result.VerificationStatus)
		}
		if index == 2 {
			captured := base.Add(4 * time.Second)
			prematureFinal := EvidenceBatch{BatchID: uuid.NewString(), Sequence: 4, SchemaVersion: ContractVersion, Phase: "final", CapturedAt: captured, Observation: agentgateway.Observation{Timestamp: captured, Workloads: []agentgateway.WorkloadObservation{}}}
			if _, ingestErr := service.Ingest(ctx, session.ID, rotated.CollectorCredential, prematureFinal); !errors.Is(ingestErr, identity.ErrConflict) {
				t.Fatalf("final evidence accepted before configured observation window: %v", ingestErr)
			}
			if wait := time.Until(baselineReceived.Add(time.Duration(session.ObservationWindowSeconds)*time.Second + 10*time.Millisecond)); wait > 0 {
				time.Sleep(wait)
			}
		}
	}
	result, err := verify.Get(ctx, principal, workspace.ID, session.RunID)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = verify.Get(ctx, principal, otherWorkspace.ID, session.RunID); !errors.Is(err, auth.ErrForbidden) {
		t.Fatalf("cross-workspace verification report access error=%v, want forbidden", err)
	}
	if result.OverallStatus != verification.StatusInconclusive || !result.ActionInspection.Attribution.Required || result.ActionInspection.Attribution.Status != verification.AttributionInconclusive || len(result.ActionInspection.Actions) != 0 {
		t.Fatalf("unjustified status/attribution: %s %+v", result.OverallStatus, result.ActionInspection.Attribution)
	}
	errorPct := float64(5)
	metricInput := MetricCandidateInput{SampleID: uuid.NewString(), ClaimedClusterIdentity: registered.Environment.ClusterIdentifier, Namespace: "checkout", Service: "checkout", SampledAt: finalBatch.CapturedAt, ErrorRatePct: &errorPct}
	metricCandidate, metricErr := service.IngestMetricCandidate(ctx, session.ID, rotated.CollectorCredential, metricInput)
	if metricErr != nil || metricCandidate.Trust != MetricCandidateTrust {
		t.Fatalf("metric candidate: %+v %v", metricCandidate, metricErr)
	}
	if retry, metricErr := service.IngestMetricCandidate(ctx, session.ID, rotated.CollectorCredential, metricInput); metricErr != nil || !retry.Duplicate {
		t.Fatalf("metric retry: %+v %v", retry, metricErr)
	}
	if _, metricErr = service.IngestMetricCandidate(ctx, otherSession.ID, rotated.CollectorCredential, metricInput); !errors.Is(metricErr, identity.ErrConflict) {
		t.Fatalf("cross-session metric replay: %v", metricErr)
	}
	wrongMetric := metricInput
	wrongMetric.ClaimedClusterIdentity = "wrong-cluster"
	if _, metricErr = service.IngestMetricCandidate(ctx, session.ID, rotated.CollectorCredential, wrongMetric); !errors.Is(metricErr, auth.ErrForbidden) {
		t.Fatalf("wrong metric cluster: %v", metricErr)
	}
	changedMetric := metricInput
	changedValue := float64(9)
	changedMetric.ErrorRatePct = &changedValue
	if _, metricErr = service.IngestMetricCandidate(ctx, session.ID, rotated.CollectorCredential, changedMetric); !errors.Is(metricErr, identity.ErrConflict) {
		t.Fatalf("contradictory metric retry: %v", metricErr)
	}
	operationalReport, reportErr := assurance.AssessRun(ctx, principal, workspace.ID, registered.Environment.ID, session.ID)
	if reportErr != nil || operationalReport.CanonicalOverallStatus != verification.StatusInconclusive || len(operationalReport.AgentClaims) != 1 || len(operationalReport.Contracts) != 1 || len(operationalReport.ServiceMetricSamples) != 1 || len(operationalReport.ApprovalRecords) != 1 {
		t.Fatalf("operational report: %+v %v", operationalReport, reportErr)
	}
	if _, reportErr = assurance.AssessRun(ctx, principal, workspace.ID, registered.Environment.ID, session.ID); reportErr != nil {
		t.Fatalf("idempotent operational assessment: %v", reportErr)
	}
	reports, reportErr := assurance.ListReports(ctx, principal, workspace.ID, registered.Environment.ID, session.ID)
	if reportErr != nil || len(reports) != 1 {
		t.Fatalf("persisted operational reports: %d %v", len(reports), reportErr)
	}
	if len(operationalReport.OperationExplanations) == 0 || len(reports[0].OperationExplanations) != len(operationalReport.OperationExplanations) {
		t.Fatalf("operation explanations did not persist: %+v", reports)
	}
	var claimOnly, forwardedOnly bool
	for _, explanation := range reports[0].OperationExplanations {
		if explanation.AttributionStatus == "attributed" || explanation.OutcomeStatus != "not established" {
			t.Fatalf("provisional evidence promoted to agent or outcome proof: %+v", explanation)
		}
		if strings.HasPrefix(explanation.Key, "claim:") && explanation.ObservationStatus == "not linked" {
			claimOnly = true
		}
		if strings.HasPrefix(explanation.Key, "audit:") && strings.Contains(explanation.ObservationStatus, "origin unverified") {
			forwardedOnly = true
		}
	}
	if !claimOnly || !forwardedOnly {
		t.Fatalf("claim and forwarded audit were not kept separate: %+v", reports[0].OperationExplanations)
	}
	for index, finding := range reports[0].Findings {
		if finding.ContractID == contract.ID && finding.RuleID == "" {
			t.Fatalf("contract finding %d lacks stable rule identity: %+v", index, finding)
		}
	}
	var firstReportJSON []byte
	if err = pool.QueryRow(ctx, `SELECT report FROM operational_assessment_reports WHERE workspace_id=$1 AND session_id=$2 AND revision=1`, workspace.ID, session.ID).Scan(&firstReportJSON); err != nil {
		t.Fatal(err)
	}
	for _, reference := range []string{"audit:" + auditEvent.AuditID, "approval:" + approval.ID} {
		detail, detailErr := assurance.ResolveEvidence(ctx, principal, workspace.ID, registered.Environment.ID, session.ID, reference)
		if detailErr != nil || detail.Reference != reference || detail.Summary == "" {
			t.Fatalf("scoped evidence %s: %+v %v", reference, detail, detailErr)
		}
		if _, detailErr = assurance.ResolveEvidence(ctx, otherWorkspacePrincipal, workspace.ID, registered.Environment.ID, session.ID, reference); !errors.Is(detailErr, auth.ErrForbidden) {
			t.Fatalf("cross-tenant evidence %s: %v", reference, detailErr)
		}
		if _, detailErr = assurance.ResolveEvidence(ctx, principal, workspace.ID, secondEnvironment.Environment.ID, session.ID, reference); !errors.Is(detailErr, auth.ErrForbidden) {
			t.Fatalf("cross-environment evidence %s: %v", reference, detailErr)
		}
	}
	episodes, episodeErr := assurance.ListEpisodes(ctx, principal, workspace.ID, registered.Environment.ID, true)
	if episodeErr != nil || len(episodes) != 1 || episodes[0].Eligibility == "verified" || episodes[0].ReportRevision != operationalReport.Revision || episodes[0].Report.RunID != session.RunID {
		t.Fatalf("provisional episode persistence: %+v %v", episodes, episodeErr)
	}
	var episodeBytes []byte
	var storedDigest string
	if err = pool.QueryRow(ctx, `SELECT payload_bytes,content_digest FROM operational_episodes WHERE id=$1`, episodes[0].ID).Scan(&episodeBytes, &storedDigest); err != nil {
		t.Fatal(err)
	}
	digest := sha256.Sum256(episodeBytes)
	if storedDigest != "sha256:"+hex.EncodeToString(digest[:]) || storedDigest != episodes[0].ContentDigest {
		t.Fatal("persisted episode bytes and digest disagree")
	}
	reportBytes, err := json.Marshal(operationalReport)
	if err != nil || !bytes.Equal(episodeBytes, reportBytes) {
		t.Fatalf("episode does not retain exact assessment revision bytes: %v", err)
	}
	if _, err = pool.Exec(ctx, `UPDATE operational_episodes SET eligibility='verified' WHERE id=$1`, episodes[0].ID); err == nil {
		t.Fatal("provisional episode could be promoted by mutation")
	}
	materialized, episodeErr := assurance.MaterializeSession(ctx, principal, workspace.ID, registered.Environment.ID, session.ID)
	if episodeErr != nil || len(materialized) != 1 || materialized[0].ID != episodes[0].ID {
		t.Fatalf("episode materialization retry: %+v %v", materialized, episodeErr)
	}
	if episodeErr = assurance.DecideEpisode(ctx, principal, workspace.ID, registered.Environment.ID, episodes[0].ID, "revoked", "Customer review"); episodeErr != nil {
		t.Fatal(episodeErr)
	}
	decided, episodeErr := assurance.ListEpisodes(ctx, principal, workspace.ID, registered.Environment.ID, true)
	if episodeErr != nil || len(decided) != 1 || !decided[0].Revoked || decided[0].ContentDigest != episodes[0].ContentDigest {
		t.Fatalf("append-only episode revocation: %+v %v", decided, episodeErr)
	}
	if _, err = pool.Exec(ctx, `UPDATE operational_episode_decisions SET reason='changed' WHERE episode_id=$1`, episodes[0].ID); err == nil {
		t.Fatal("episode decision history was mutable")
	}
	if episodeErr = assurance.DecideEpisode(ctx, principal, workspace.ID, registered.Environment.ID, episodes[0].ID, "restored", "Customer review complete"); episodeErr != nil {
		t.Fatal(episodeErr)
	}
	var decisionCount int
	if err = pool.QueryRow(ctx, `SELECT count(*) FROM operational_episode_decisions WHERE episode_id=$1`, episodes[0].ID).Scan(&decisionCount); err != nil || decisionCount != 2 {
		t.Fatalf("append-only episode decision count: %d %v", decisionCount, err)
	}
	trusted, episodeErr := assurance.ListEpisodes(ctx, principal, workspace.ID, registered.Environment.ID, false)
	if episodeErr != nil || len(trusted) != 0 {
		t.Fatalf("provisional history entered trusted list: %+v %v", trusted, episodeErr)
	}
	// A database-only synthetic lesson exercises review and correction without
	// presenting provisional collector evidence as trusted operational advice.
	var lessonID string
	if err = pool.QueryRow(ctx, `INSERT INTO operational_lessons(workspace_id,environment_id,episode_id,lesson_kind,condition,outcome,evidence,limitations)
	  VALUES($1,$2,$3,'observed_failure','{"resource_kind":"Deployment","namespace":"checkout"}'::jsonb,'{"code":"fixture_only"}'::jsonb,'[{"reference":"fixture","provenance":"unverified"}]'::jsonb,ARRAY['synthetic fixture']) RETURNING id`, workspace.ID, registered.Environment.ID, episodes[0].ID).Scan(&lessonID); err != nil {
		t.Fatal(err)
	}
	if err = assurance.ReviewLesson(ctx, principal, workspace.ID, registered.Environment.ID, lessonID, "approved", "Fixture must remain provisional"); !errors.Is(err, auth.ErrForbidden) {
		t.Fatalf("provisional lesson approved: %v", err)
	}
	if err = assurance.ReviewLesson(ctx, principal, workspace.ID, registered.Environment.ID, lessonID, "rejected", "Unsupported source"); err != nil {
		t.Fatal(err)
	}
	successorID, correctionErr := assurance.SupersedeLesson(ctx, principal, workspace.ID, registered.Environment.ID, lessonID, "Only under a narrower checkout rollout condition")
	if correctionErr != nil || successorID == lessonID {
		t.Fatalf("lesson correction: %s %v", successorID, correctionErr)
	}
	lessons, lessonErr := assurance.ListLessons(ctx, principal, workspace.ID, registered.Environment.ID)
	if lessonErr != nil || len(lessons) != 2 || lessons[0].SupersedesLessonID != lessonID || lessons[0].ReviewState != "pending" || lessons[1].ReviewState != "retired" || lessons[0].SessionID != session.ID {
		t.Fatalf("lesson correction history: %+v %v", lessons, lessonErr)
	}
	if err = assurance.ReviewLesson(ctx, principal, workspace.ID, registered.Environment.ID, successorID, "approved", "Fixture must remain provisional"); !errors.Is(err, auth.ErrForbidden) {
		t.Fatalf("corrected provisional lesson approved: %v", err)
	}
	lessonDecisions, lessonErr := assurance.ListLessonDecisions(ctx, principal, workspace.ID, registered.Environment.ID, lessonID)
	if lessonErr != nil || len(lessonDecisions) != 2 || lessonDecisions[0].Decision != "rejected" || lessonDecisions[1].Decision != "retired" {
		t.Fatalf("append-only lesson decisions: %+v %v", lessonDecisions, lessonErr)
	}
	if _, episodeErr = assurance.ListEpisodes(ctx, otherWorkspacePrincipal, workspace.ID, registered.Environment.ID, true); !errors.Is(episodeErr, auth.ErrForbidden) {
		t.Fatalf("cross-tenant episode retrieval: %v", episodeErr)
	}
	if _, episodeErr = assurance.MaterializeSession(ctx, principal, workspace.ID, secondEnvironment.Environment.ID, session.ID); !errors.Is(episodeErr, auth.ErrForbidden) {
		t.Fatalf("cross-environment materialization: %v", episodeErr)
	}
	relationships, episodeErr := assurance.ListEpisodeRelationships(ctx, principal, workspace.ID, registered.Environment.ID, episodes[0].ID)
	if episodeErr != nil || len(relationships) == 0 {
		t.Fatalf("episode relationships: %+v %v", relationships, episodeErr)
	}
	for _, relationship := range relationships {
		if relationship.Relation == "associated_service" && relationship.Observed {
			t.Fatal("inferred service relationship was marked observed")
		}
	}
	if _, episodeErr = assurance.ListEpisodeRelationships(ctx, otherWorkspacePrincipal, workspace.ID, registered.Environment.ID, episodes[0].ID); !errors.Is(episodeErr, auth.ErrForbidden) {
		t.Fatalf("cross-tenant relationship retrieval: %v", episodeErr)
	}
	guidance, episodeErr := assurance.PreviewGuidance(ctx, principal, workspace.ID, registered.Environment.ID, operational.ProposedOperation{Namespace: "checkout", ResourceKind: "Deployment", ResourceName: "checkout", Operation: "set_image"})
	if episodeErr != nil || guidance.TrustworthyHistoryAvailable || len(guidance.Episodes) != 0 || len(guidance.Requirements) != 1 {
		t.Fatalf("provisional evidence entered guidance: %+v %v", guidance, episodeErr)
	}
	history, episodeErr := assurance.ListGuidanceHistory(ctx, principal, workspace.ID, registered.Environment.ID)
	if episodeErr != nil || len(history) != 1 || history[0].ID != guidance.ID {
		t.Fatalf("persisted guidance history: %+v %v", history, episodeErr)
	}
	findings, findingErr := assurance.ListFindings(ctx, principal, workspace.ID, registered.Environment.ID, session.ID)
	if findingErr != nil || len(findings) != len(operationalReport.Findings) {
		t.Fatalf("persisted operational findings: %d %v", len(findings), findingErr)
	}
	for _, finding := range findings {
		if finding.Disposition == "violation" {
			t.Fatalf("unverified collector evidence promoted: %+v", finding)
		}
	}
	if _, findingErr = assurance.ListFindings(ctx, otherWorkspacePrincipal, workspace.ID, registered.Environment.ID, session.ID); !errors.Is(findingErr, auth.ErrForbidden) {
		t.Fatalf("cross-tenant finding retrieval: %v", findingErr)
	}
	notifications, notificationErr := assurance.ListNotifications(ctx, principal, workspace.ID, registered.Environment.ID)
	if notificationErr != nil || len(notifications) == 0 || notifications[0].DeliveryState != "pending" {
		t.Fatalf("notification outbox: %+v %v", notifications, notificationErr)
	}
	if _, approvalErr = pool.Exec(ctx, `UPDATE operational_approval_records SET decision='denied' WHERE id=$1`, approval.ID); approvalErr == nil {
		t.Fatal("approval record history was mutable")
	}
	if _, findingErr = pool.Exec(ctx, `DELETE FROM operational_finding_history WHERE session_id=$1`, session.ID); findingErr == nil {
		t.Fatal("finding history was mutable")
	}
	if _, err = pool.Exec(ctx, `UPDATE operational_assessment_reports SET report='{}'::jsonb WHERE session_id=$1`, session.ID); err == nil {
		t.Fatal("operational report history was mutable")
	}
	if _, err = pool.Exec(ctx, `DELETE FROM operational_contract_versions WHERE contract_id=$1`, contract.ID); err == nil {
		t.Fatal("contract version history was mutable")
	}
	deactivated, contractErr := assurance.SaveContract(ctx, principal, workspace.ID, registered.Environment.ID, operational.ContractInput{ContractID: contract.ID, Service: contract.Service, Active: false, Rules: contract.Rules})
	if contractErr != nil || deactivated.Version != 2 {
		t.Fatalf("contract deactivation revision: %+v %v", deactivated, contractErr)
	}
	lateAudit := auditEvent
	lateAudit.AuditID = uuid.NewString()
	lateJSON, _ := json.Marshal(lateAudit)
	if _, err = service.IngestForwardedAudit(ctx, session.ID, rotated.CollectorCredential, ForwardedAuditInput{ClaimedClusterIdentity: registered.Environment.ClusterIdentifier, Event: lateJSON}); err != nil {
		t.Fatalf("late audit evidence: %v", err)
	}
	revised, reportErr := assurance.AssessRun(ctx, principal, workspace.ID, registered.Environment.ID, session.ID)
	if reportErr != nil || revised.Revision != 2 || len(revised.Contracts) != 1 || revised.Contracts[0].Version != 1 || len(revised.ObservedActions) <= len(operationalReport.ObservedActions) {
		t.Fatalf("late-evidence revision retained action-time contract: %+v %v", revised, reportErr)
	}
	var firstReportAfter []byte
	if err = pool.QueryRow(ctx, `SELECT report FROM operational_assessment_reports WHERE workspace_id=$1 AND session_id=$2 AND revision=1`, workspace.ID, session.ID).Scan(&firstReportAfter); err != nil || !bytes.Equal(firstReportJSON, firstReportAfter) || len(revised.OperationExplanations) <= len(operationalReport.OperationExplanations) {
		t.Fatalf("explanation revision rewrote history or ignored late audit: before=%d after=%d err=%v", len(operationalReport.OperationExplanations), len(revised.OperationExplanations), err)
	}
	revisedEpisodes, episodeErr := assurance.ListEpisodes(ctx, principal, workspace.ID, registered.Environment.ID, true)
	if episodeErr != nil || len(revisedEpisodes) != 2 {
		t.Fatalf("append-only episode revisions: %+v %v", revisedEpisodes, episodeErr)
	}
	var firstRevisionFound, secondRevisionFound bool
	for _, episode := range revisedEpisodes {
		switch episode.ReportRevision {
		case 1:
			firstRevisionFound = episode.ID == episodes[0].ID && episode.ContentDigest == episodes[0].ContentDigest
		case 2:
			var persistedBytes []byte
			if err = pool.QueryRow(ctx, `SELECT payload_bytes FROM operational_episodes WHERE id=$1`, episode.ID).Scan(&persistedBytes); err != nil {
				t.Fatal(err)
			}
			revisedBytes, marshalErr := json.Marshal(revised)
			secondRevisionFound = marshalErr == nil && bytes.Equal(persistedBytes, revisedBytes) && episode.ContentDigest != episodes[0].ContentDigest
		}
	}
	if !firstRevisionFound || !secondRevisionFound {
		t.Fatal("late evidence did not retain distinct immutable episode revisions")
	}
	if _, reportErr = assurance.ListReports(ctx, otherWorkspacePrincipal, workspace.ID, registered.Environment.ID, session.ID); !errors.Is(reportErr, auth.ErrForbidden) {
		t.Fatalf("cross-tenant operational reports: %v", reportErr)
	}
	if result.Baseline.SourceProvider != ProviderID || result.Final.SourceProvider != ProviderID {
		t.Fatalf("collector provenance missing: baseline=%s final=%s", result.Baseline.SourceProvider, result.Final.SourceProvider)
	}
	var persisted int
	if err = pool.QueryRow(ctx, `SELECT count(*) FROM customer_observation_batches WHERE session_id=$1`, session.ID).Scan(&persisted); err != nil || persisted != 4 {
		t.Fatalf("persisted collector evidence=%d error=%v", persisted, err)
	}
	var reportBefore json.RawMessage
	if err = pool.QueryRow(ctx, `SELECT to_jsonb(v) FROM verification_results v WHERE run_id=$1`, session.RunID).Scan(&reportBefore); err != nil {
		t.Fatal(err)
	}
	last := EvidenceBatch{BatchID: uuid.NewString(), Sequence: 5, SchemaVersion: ContractVersion, Phase: "stability", CapturedAt: base.Add(6 * time.Second), Observation: agentgateway.Observation{Timestamp: base.Add(6 * time.Second)}}
	if _, err = service.Ingest(ctx, session.ID, rotated.CollectorCredential, last); !errors.Is(err, identity.ErrConflict) {
		t.Fatalf("post-final evidence with active credential error=%v, want conflict", err)
	}
	var persistedAfter int
	if err = pool.QueryRow(ctx, `SELECT count(*) FROM customer_observation_batches WHERE session_id=$1`, session.ID).Scan(&persistedAfter); err != nil || persistedAfter != persisted {
		t.Fatalf("rejected post-final evidence changed persisted batches: before=%d after=%d error=%v", persisted, persistedAfter, err)
	}
	var reportAfter json.RawMessage
	if err = pool.QueryRow(ctx, `SELECT to_jsonb(v) FROM verification_results v WHERE run_id=$1`, session.RunID).Scan(&reportAfter); err != nil {
		t.Fatal(err)
	}
	if string(reportBefore) != string(reportAfter) {
		t.Fatal("rejected post-final evidence changed the canonical report")
	}
	duplicate, err := service.Ingest(ctx, session.ID, rotated.CollectorCredential, finalBatch)
	if err != nil || !duplicate.Duplicate || duplicate.VerificationStatus != verification.StatusInconclusive {
		t.Fatalf("exact final retry was not idempotent: result=%+v error=%v", duplicate, err)
	}
	changedDuplicate := finalBatch
	changedDuplicate.Observation.Workloads = []agentgateway.WorkloadObservation{{Kind: "Deployment", Name: "checkout", Namespace: "checkout", DesiredReplicas: 2, ReadyReplicas: 0}}
	if _, err = service.Ingest(ctx, session.ID, rotated.CollectorCredential, changedDuplicate); !errors.Is(err, identity.ErrConflict) {
		t.Fatalf("same batch identity with different evidence accepted: %v", err)
	}
	// A completed session can be monitored without reopening immutable intake.
	// An unavailable source creates an assessment gap and never a service failure.
	var regressed atomic.Bool
	monitorServer := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/apis/apps/v1/namespaces/checkout/deployments/checkout" || r.Header.Get("Authorization") != "Bearer monitor-read-only" {
			http.Error(w, "forbidden", http.StatusForbidden)
			return
		}
		available := `3`
		if regressed.Load() {
			available = `1`
		}
		_, _ = w.Write([]byte(`{"metadata":{"name":"checkout","namespace":"checkout","uid":"uid-checkout","resourceVersion":"12","generation":2},"spec":{"replicas":3,"template":{"spec":{"containers":[{"name":"checkout","image":"checkout:v2"}]}}},"status":{"observedGeneration":2,"availableReplicas":` + available + `,"readyReplicas":3,"updatedReplicas":3}}`))
	}))
	defer monitorServer.Close()
	monitorPin := sha256.Sum256(monitorServer.Certificate().Raw)
	monitorSource, err := assurance.RegisterDirectSource(ctx, principal, workspace.ID, registered.Environment.ID, operational.DirectSource{Kind: "kubernetes_state", EndpointURL: monitorServer.URL, ServerCertificateSHA256: hex.EncodeToString(monitorPin[:]), Namespace: "checkout", ResourceName: "checkout"})
	if err != nil {
		t.Fatalf("register monitor source: %v", err)
	}
	plan, err := assurance.ConfigureMonitoring(ctx, principal, workspace.ID, registered.Environment.ID, session.ID, operational.MonitoringInput{SourceIDs: []string{monitorSource.ID}, CadenceSeconds: 60, DurationSeconds: 180})
	if err != nil || plan.State != "active" {
		t.Fatalf("configure monitoring: %+v %v", plan, err)
	}
	if _, err = assurance.Monitoring(ctx, otherWorkspacePrincipal, workspace.ID, registered.Environment.ID, session.ID); !errors.Is(err, auth.ErrForbidden) {
		t.Fatalf("cross-tenant monitor disclosed: %v", err)
	}
	// Simulate a process dying after claiming the first cycle, before it
	// acquired any source. Recovery must retain this cycle identity.
	leasedCycle := uuid.NewString()
	if _, err = pool.Exec(ctx, `UPDATE assurance_monitoring_plans SET state='running',current_cycle_id=$2,cycle_number=1,attempts=1,lease_until=now()-interval '1 second',next_due_at=now()-interval '1 second' WHERE session_id=$1`, session.ID, leasedCycle); err != nil {
		t.Fatal(err)
	}
	var concurrent sync.WaitGroup
	for i := 0; i < 2; i++ {
		concurrent.Add(1)
		go func() {
			defer concurrent.Done()
			_, _ = operational.ProcessNextMonitoring(ctx, pool, assurance, operational.FileMonitorCredentials{})
		}()
	}
	concurrent.Wait()
	var concurrentCount int
	if err = pool.QueryRow(ctx, `SELECT count(*) FROM assurance_monitoring_events WHERE session_id=$1`, session.ID).Scan(&concurrentCount); err != nil || concurrentCount > 1 {
		t.Fatalf("concurrent workers duplicated a logical cycle: count=%d error=%v", concurrentCount, err)
	}
	monitorProcessed := concurrentCount == 1
	for i := 0; i < 16; i++ {
		if monitorProcessed {
			break
		}
		var processed bool
		processed, err = operational.ProcessNextMonitoring(ctx, pool, assurance, operational.FileMonitorCredentials{})
		if !processed {
			break
		}
		var count int
		if qerr := pool.QueryRow(ctx, `SELECT count(*) FROM assurance_monitoring_events WHERE session_id=$1`, session.ID).Scan(&count); qerr != nil {
			t.Fatal(qerr)
		}
		if count > 0 {
			monitorProcessed = true
			break
		}
	}
	if !monitorProcessed {
		t.Fatal("scheduled monitor did not execute")
	}
	view, err := assurance.Monitoring(ctx, principal, workspace.ID, registered.Environment.ID, session.ID)
	if err != nil || len(view.Events) != 1 || view.Events[0].Status != "unavailable" || view.Events[0].CycleID != leasedCycle || view.Plan.Attempts != 2 {
		t.Fatalf("source outage monitoring history: %+v %v", view, err)
	}
	monitorReports, err := assurance.ListReports(ctx, principal, workspace.ID, registered.Environment.ID, session.ID)
	if err != nil || len(monitorReports) != 3 || monitorReports[2].Findings[len(monitorReports[2].Findings)-1].Code != "SOURCE_ACQUISITION_UNAVAILABLE" {
		t.Fatalf("source gap report: revisions=%d err=%v", len(monitorReports), err)
	}
	monitorEpisodes, err := assurance.ListEpisodes(ctx, principal, workspace.ID, registered.Environment.ID, true)
	if err != nil || len(monitorEpisodes) != 2 {
		t.Fatalf("source outage promoted episode: %d %v", len(monitorEpisodes), err)
	}
	manualEpisodes, err := assurance.MaterializeSession(ctx, principal, workspace.ID, registered.Environment.ID, session.ID)
	if err != nil || len(manualEpisodes) != 2 {
		t.Fatalf("manual materialization promoted a source outage: %d %v", len(manualEpisodes), err)
	}
	if _, err = pool.Exec(ctx, `UPDATE assurance_monitoring_plans SET next_due_at=now()-interval '1 second' WHERE session_id=$1`, session.ID); err != nil {
		t.Fatal(err)
	}
	if processed, retryErr := operational.ProcessNextMonitoring(ctx, pool, assurance, operational.FileMonitorCredentials{}); !processed || retryErr == nil {
		t.Fatalf("expected bounded source retry: processed=%v error=%v", processed, retryErr)
	}
	var sourceNotifications int
	if err = pool.QueryRow(ctx, `SELECT count(*) FROM assurance_notification_events n JOIN operational_finding_history f ON f.id=n.finding_id WHERE f.session_id=$1 AND f.finding->>'code'='SOURCE_ACQUISITION_UNAVAILABLE'`, session.ID).Scan(&sourceNotifications); err != nil || sourceNotifications != 1 {
		t.Fatalf("source outage notification dedup=%d error=%v", sourceNotifications, err)
	}
	credentialsDir := t.TempDir()
	if err = os.WriteFile(filepath.Join(credentialsDir, monitorSource.ID+".ca.pem"), pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: monitorServer.Certificate().Raw}), 0600); err != nil {
		t.Fatal(err)
	}
	if err = os.WriteFile(filepath.Join(credentialsDir, monitorSource.ID+".token"), []byte("monitor-read-only"), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err = pool.Exec(ctx, `UPDATE assurance_monitoring_plans SET next_due_at=now()-interval '1 second' WHERE session_id=$1`, session.ID); err != nil {
		t.Fatal(err)
	}
	monitorProcessed = false
	for i := 0; i < 16; i++ {
		var processed bool
		processed, err = operational.ProcessNextMonitoring(ctx, pool, assurance, operational.FileMonitorCredentials{Directory: credentialsDir})
		if !processed {
			break
		}
		var count int
		if qerr := pool.QueryRow(ctx, `SELECT count(*) FROM assurance_monitoring_events WHERE session_id=$1`, session.ID).Scan(&count); qerr != nil {
			t.Fatal(qerr)
		}
		if count > 2 {
			monitorProcessed = true
			break
		}
	}
	if !monitorProcessed {
		t.Fatal("repaired monitor did not execute")
	}
	view, err = assurance.Monitoring(ctx, principal, workspace.ID, registered.Environment.ID, session.ID)
	if err != nil || len(view.Events) != 3 || view.Events[2].Status != "acquired" || view.Events[0].CycleID != view.Events[2].CycleID || view.Plan.State != "active" || view.Plan.Attempts != 0 {
		t.Fatalf("retry did not preserve cycle and acquire source: %+v %v", view, err)
	}
	observations, err := assurance.ListDirectObservations(ctx, principal, workspace.ID, registered.Environment.ID, session.ID)
	if err != nil || len(observations) != 1 || observations[0].MonitoringCycleID != view.Events[0].CycleID {
		t.Fatalf("cycle observation binding: %+v %v", observations, err)
	}
	if _, err = pool.Exec(ctx, `UPDATE customer_direct_outcome_observations SET payload='{}'::jsonb WHERE id=$1`, observations[0].ID); err == nil {
		t.Fatal("monitor observation history was mutable")
	}
	monitorReports, err = assurance.ListReports(ctx, principal, workspace.ID, registered.Environment.ID, session.ID)
	if err != nil || len(monitorReports) != 5 {
		t.Fatalf("repaired source assessment revisions=%d error=%v", len(monitorReports), err)
	}
	var recoveredState bool
	for _, finding := range monitorReports[4].Findings {
		if finding.Dimension == "infrastructure_state" && finding.Status == operational.Pass {
			recoveredState = true
		}
	}
	if !recoveredState {
		t.Fatal("fresh direct Deployment evidence did not establish the customer replica floor")
	}
	regressed.Store(true)
	if _, err = pool.Exec(ctx, `UPDATE assurance_monitoring_plans SET next_due_at=now()-interval '1 second' WHERE session_id=$1`, session.ID); err != nil {
		t.Fatal(err)
	}
	monitorProcessed = false
	for i := 0; i < 16; i++ {
		var processed bool
		processed, err = operational.ProcessNextMonitoring(ctx, pool, assurance, operational.FileMonitorCredentials{Directory: credentialsDir})
		if !processed {
			break
		}
		var count int
		if qerr := pool.QueryRow(ctx, `SELECT count(*) FROM assurance_monitoring_events WHERE session_id=$1`, session.ID).Scan(&count); qerr != nil {
			t.Fatal(qerr)
		}
		if count > 3 {
			monitorProcessed = true
			break
		}
	}
	if !monitorProcessed {
		t.Fatal("second monitoring cycle did not execute")
	}
	monitorReports, err = assurance.ListReports(ctx, principal, workspace.ID, registered.Environment.ID, session.ID)
	if err != nil || len(monitorReports) != 6 {
		t.Fatalf("regression report revisions=%d error=%v", len(monitorReports), err)
	}
	var regression bool
	for _, finding := range monitorReports[5].Findings {
		if finding.Code == "ASSURANCE_infrastructure_state_REGRESSION" && finding.Status == operational.Fail {
			regression = true
		}
	}
	if !regression {
		t.Fatal("fresh Deployment replica regression did not append a transition")
	}
	if err = assurance.RevokeDirectSource(ctx, principal, workspace.ID, registered.Environment.ID, monitorSource.ID); err != nil {
		t.Fatalf("revoke monitor source: %v", err)
	}
	if _, err = pool.Exec(ctx, `UPDATE assurance_monitoring_plans SET next_due_at=now()-interval '1 second' WHERE session_id=$1`, session.ID); err != nil {
		t.Fatal(err)
	}
	monitorProcessed = false
	for i := 0; i < 16; i++ {
		var processed bool
		processed, err = operational.ProcessNextMonitoring(ctx, pool, assurance, operational.FileMonitorCredentials{Directory: credentialsDir})
		if !processed {
			break
		}
		var count int
		if qerr := pool.QueryRow(ctx, `SELECT count(*) FROM assurance_monitoring_events WHERE session_id=$1`, session.ID).Scan(&count); qerr != nil {
			t.Fatal(qerr)
		}
		if count > 4 {
			monitorProcessed = true
			break
		}
	}
	if !monitorProcessed {
		t.Fatal("revoked source was not recorded as unavailable")
	}
	view, err = assurance.Monitoring(ctx, principal, workspace.ID, registered.Environment.ID, session.ID)
	if err != nil || view.Events[len(view.Events)-1].Status != "unavailable" {
		t.Fatalf("revoked source incorrectly acquired: %+v %v", view, err)
	}
	for attempt := 2; attempt <= 5; attempt++ {
		if _, err = pool.Exec(ctx, `UPDATE assurance_monitoring_plans SET next_due_at=now()-interval '1 second' WHERE session_id=$1`, session.ID); err != nil {
			t.Fatal(err)
		}
		if processed, retryErr := operational.ProcessNextMonitoring(ctx, pool, assurance, operational.FileMonitorCredentials{Directory: credentialsDir}); !processed || retryErr == nil {
			t.Fatalf("revoked source retry %d: processed=%v error=%v", attempt, processed, retryErr)
		}
	}
	view, err = assurance.Monitoring(ctx, principal, workspace.ID, registered.Environment.ID, session.ID)
	if err != nil || view.Plan.State != "failed" || view.Plan.Attempts != 5 || view.Plan.LastError == "" {
		t.Fatalf("bounded terminal failure: %+v %v", view, err)
	}
	if _, err = assurance.ChangeMonitoring(ctx, otherWorkspacePrincipal, workspace.ID, registered.Environment.ID, session.ID, "retry"); !errors.Is(err, auth.ErrForbidden) {
		t.Fatalf("cross-tenant monitoring repair allowed: %v", err)
	}
	if repaired, repairErr := assurance.ChangeMonitoring(ctx, principal, workspace.ID, registered.Environment.ID, session.ID, "retry"); repairErr != nil || repaired.State != "active" || repaired.Attempts != 0 {
		t.Fatalf("authorized monitoring repair: %+v %v", repaired, repairErr)
	}
	if err = pool.QueryRow(ctx, `SELECT count(*) FROM assurance_notification_events n JOIN operational_finding_history f ON f.id=n.finding_id WHERE f.session_id=$1 AND f.finding->>'code'='SOURCE_ACQUISITION_UNAVAILABLE'`, session.ID).Scan(&sourceNotifications); err != nil || sourceNotifications != 2 {
		t.Fatalf("resolved and recurring source outage notifications=%d error=%v", sourceNotifications, err)
	}
	if _, err = assurance.ChangeMonitoring(ctx, principal, workspace.ID, registered.Environment.ID, session.ID, "cancel"); err != nil {
		t.Fatalf("cancel failed monitor: %v", err)
	}
	if err = service.Revoke(ctx, principal, workspace.ID, registered.Environment.ID); err != nil {
		t.Fatal(err)
	}
	if _, err = service.Ingest(ctx, session.ID, rotated.CollectorCredential, last); !errors.Is(err, auth.ErrForbidden) {
		t.Fatalf("revoked collector accepted evidence: %v", err)
	}
	if _, err = service.IngestForwardedAudit(ctx, session.ID, rotated.CollectorCredential, auditInput); !errors.Is(err, auth.ErrForbidden) {
		t.Fatalf("revoked collector accepted audit candidate: %v", err)
	}
}
