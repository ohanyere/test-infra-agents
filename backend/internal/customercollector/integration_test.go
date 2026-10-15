package customercollector

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/evalplane/evalplane/backend/internal/agentgateway"
	"github.com/evalplane/evalplane/backend/internal/auth"
	"github.com/evalplane/evalplane/backend/internal/evaluator"
	"github.com/evalplane/evalplane/backend/internal/identity"
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
	principal := auth.Principal{UserID: user.ID}
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
	err = pool.QueryRow(ctx, `INSERT INTO scenario_versions(scenario_id,version,description,category,difficulty,duration_seconds,agent_task,evidence_requirements,source_metadata) VALUES($1,1,'Observe external state','traffic','medium',1,'Observe whether the customer workload is healthy.','{"contract_version":"evidence-requirements:v1","workload_state":"REQUIRED","telemetry":"REQUIRED","git_desired_state":"NOT_APPLICABLE","argo_state":"NOT_APPLICABLE","stability":"REQUIRED"}'::jsonb,'{}'::jsonb) RETURNING id`, scenarioID).Scan(&scenarioVersionID)
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
	registered, err := service.Register(ctx, principal, workspace.ID, CreateEnvironmentInput{Name: "Prod checkout", ClusterIdentifier: "customer-cluster-1", Namespaces: []string{"checkout"}, Tier: "production"})
	if err != nil {
		t.Fatal(err)
	}
	if registered.CollectorCredential == "" {
		t.Fatal("one-time collector credential missing")
	}
	if _, err = service.List(ctx, principal, otherWorkspace.ID); !errors.Is(err, auth.ErrForbidden) {
		t.Fatalf("cross-workspace environment list error=%v", err)
	}
	secondEnvironment, err := service.Register(ctx, principal, workspace.ID, CreateEnvironmentInput{Name: "Staging checkout", ClusterIdentifier: "customer-cluster-2", Namespaces: []string{"checkout"}, Tier: "staging"})
	if err != nil {
		t.Fatal(err)
	}
	session, err := service.StartSession(ctx, principal, workspace.ID, registered.Environment.ID, CreateSessionInput{AgentVersionID: agentVersionID, ScenarioVersionID: scenarioVersionID})
	if err != nil {
		t.Fatal(err)
	}
	rotated, err := service.Rotate(ctx, principal, workspace.ID, registered.Environment.ID)
	if err != nil || rotated.CollectorCredential == registered.CollectorCredential || rotated.Environment.ConnectionStatus != "pending" {
		t.Fatalf("credential rotation failed: err=%v", err)
	}
	base := time.Now().UTC().Add(-time.Second)
	phases := []string{"baseline", "stability", "stability", "final"}
	var finalBatch EvidenceBatch
	for index, phase := range phases {
		captured := base.Add(time.Duration(index+1) * time.Second)
		obs := agentgateway.Observation{Timestamp: captured, Workloads: []agentgateway.WorkloadObservation{{Kind: "Deployment", Name: "checkout", Namespace: "checkout", DesiredReplicas: 2, ReadyReplicas: 2, AvailableReplicas: 2, UpdatedReplicas: 2, UID: "uid-checkout", ResourceVersion: time.Now().Format("150405")}}, Pods: []agentgateway.PodObservation{}, Metrics: map[string]any{}, RecentEvents: []agentgateway.RecentEvent{}}
		batch := EvidenceBatch{BatchID: uuid.NewString(), Sequence: int64(index + 1), SchemaVersion: ContractVersion, Phase: phase, CapturedAt: captured, Observation: obs}
		if phase == "final" {
			finalBatch = batch
		}
		if index == 0 {
			if _, ingestErr := service.Ingest(ctx, session.ID, registered.CollectorCredential, batch); !errors.Is(ingestErr, auth.ErrForbidden) {
				t.Fatalf("rotated credential remained valid: %v", ingestErr)
			}
			if _, ingestErr := service.Ingest(ctx, session.ID, secondEnvironment.CollectorCredential, batch); !errors.Is(ingestErr, auth.ErrForbidden) {
				t.Fatalf("wrong-environment credential accepted: %v", ingestErr)
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
			claimInput := SubmitActionClaimInput{
				ContractVersion: ActionClaimContractVersion,
				AgentVersionID:  agentVersionID,
				ClaimID:         uuid.NewString(), IdempotencyKey: uuid.NewString(),
				ActionType: "set_workload_image",
				Resource:   ClaimedResource{Kind: "Deployment", Namespace: "checkout", Name: "checkout", UID: "uid-checkout"},
				StartedAt:  captured, CompletedAt: captured.Add(100 * time.Millisecond),
				Outcome: "executed", ExternalTrace: "abcdef0123456789",
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
			time.Sleep(1100 * time.Millisecond)
		}
	}
	result, err := verify.Get(ctx, principal, workspace.ID, session.RunID)
	if err != nil {
		t.Fatal(err)
	}
	if result.OverallStatus != verification.StatusInconclusive || !result.ActionInspection.Attribution.Required || result.ActionInspection.Attribution.Status != verification.AttributionInconclusive || len(result.ActionInspection.Actions) != 0 {
		t.Fatalf("unjustified status/attribution: %s %+v", result.OverallStatus, result.ActionInspection.Attribution)
	}
	if result.Baseline.SourceProvider != ProviderID || result.Final.SourceProvider != ProviderID {
		t.Fatalf("collector provenance missing: baseline=%s final=%s", result.Baseline.SourceProvider, result.Final.SourceProvider)
	}
	var persisted int
	if err = pool.QueryRow(ctx, `SELECT count(*) FROM customer_observation_batches WHERE session_id=$1`, session.ID).Scan(&persisted); err != nil || persisted != 4 {
		t.Fatalf("persisted collector evidence=%d error=%v", persisted, err)
	}
	last := EvidenceBatch{BatchID: uuid.NewString(), Sequence: 5, SchemaVersion: ContractVersion, Phase: "stability", CapturedAt: base.Add(6 * time.Second), Observation: agentgateway.Observation{Timestamp: base.Add(6 * time.Second)}}
	if _, err = service.Ingest(ctx, session.ID, registered.CollectorCredential, last); !errors.Is(err, identity.ErrConflict) {
		t.Fatalf("post-final evidence error=%v", err)
	}
	var serialized json.RawMessage
	if err = pool.QueryRow(ctx, `SELECT source_metadata FROM verification_results WHERE run_id=$1`, session.RunID).Scan(&serialized); err != nil {
		t.Fatal(err)
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
	if err = service.Revoke(ctx, principal, workspace.ID, registered.Environment.ID); err != nil {
		t.Fatal(err)
	}
	if _, err = service.Ingest(ctx, session.ID, rotated.CollectorCredential, last); !errors.Is(err, auth.ErrForbidden) {
		t.Fatalf("revoked collector accepted evidence: %v", err)
	}
}
