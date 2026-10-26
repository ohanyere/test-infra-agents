package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/evalplane/evalplane/backend/internal/auth"
	"github.com/evalplane/evalplane/backend/internal/customeractionevidence"
	"github.com/evalplane/evalplane/backend/internal/identity"
	"github.com/evalplane/evalplane/backend/internal/kubeaudit"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
)

func main() {
	namespace := flag.String("namespace", "", "expected target namespace")
	name := flag.String("name", "", "expected Deployment name")
	verb := flag.String("verb", "", "expected API verb")
	principal := flag.String("principal", "", "expected authenticated Kubernetes principal")
	flag.Parse()
	if *namespace != "realverax-audit-proof" || *name == "" || *verb != "patch" || *principal == "" || flag.NArg() != 0 {
		fatal("expected namespace, name, verb, and principal")
	}
	if os.Getenv("GITHUB_ACTIONS") != "true" || os.Getenv("GITHUB_RUN_ID") == "" || os.Getenv("DATABASE_URL") == "" {
		fatal("disposable GitHub Actions runner and database are required")
	}
	acquireCtx, cancelAcquire := context.WithTimeout(context.Background(), 75*time.Second)
	selected, err := kubeaudit.WaitForMutation(acquireCtx, *namespace, *name, *verb, *principal)
	cancelAcquire()
	if err != nil {
		fatal(err.Error())
	}
	event, source, ok := selected.Snapshot()
	if !ok {
		fatal("audit event did not pass runner source binding")
	}
	expectedCollector := "github-actions:" + os.Getenv("GITHUB_RUN_ID") + ":kind-create-cluster:evalplane-e2e"
	if source.ProvisioningIdentity != expectedCollector {
		fatal("unexpected runner provisioning identity")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 75*time.Second)
	defer cancel()
	pool, err := pgxpool.New(ctx, os.Getenv("DATABASE_URL"))
	if err != nil {
		fatal(err.Error())
	}
	defer pool.Close()
	tx, err := pool.Begin(ctx)
	if err != nil {
		fatal(err.Error())
	}
	defer tx.Rollback(ctx)
	suffix := strings.ReplaceAll(uuid.NewString(), "-", "")[:12]
	var workspaceID, otherWorkspaceID, registrationID, sessionID, otherSessionID string
	err = tx.QueryRow(ctx, `WITH o AS (INSERT INTO organizations(name,slug) VALUES('Audit proof',$1) RETURNING id) INSERT INTO workspaces(organization_id,name,slug) SELECT id,'Audit proof','audit' FROM o RETURNING id`, "audit-proof-"+suffix).Scan(&workspaceID)
	if err != nil {
		fatal(err.Error())
	}
	err = tx.QueryRow(ctx, `WITH o AS (INSERT INTO organizations(name,slug) VALUES('Other audit proof',$1) RETURNING id) INSERT INTO workspaces(organization_id,name,slug) SELECT id,'Other audit proof','audit' FROM o RETURNING id`, "other-audit-proof-"+suffix).Scan(&otherWorkspaceID)
	if err != nil {
		fatal(err.Error())
	}
	err = tx.QueryRow(ctx, `INSERT INTO runner_audit_test_registrations(workspace_id,cluster_identity,namespaces,resource_kinds,collector_identity) VALUES($1,$2,$3,$4,$5) RETURNING id`, workspaceID, source.ClusterIdentity, []string{*namespace}, []string{"Deployment"}, expectedCollector).Scan(&registrationID)
	if err != nil {
		fatal(err.Error())
	}
	err = tx.QueryRow(ctx, `INSERT INTO runner_audit_test_sessions(workspace_id,registration_id) VALUES($1,$2) RETURNING id`, workspaceID, registrationID).Scan(&sessionID)
	if err != nil {
		fatal(err.Error())
	}
	err = tx.QueryRow(ctx, `INSERT INTO runner_audit_test_sessions(workspace_id,registration_id) VALUES($1,$2) RETURNING id`, workspaceID, registrationID).Scan(&otherSessionID)
	if err != nil {
		fatal(err.Error())
	}
	binding, err := customeractionevidence.ResolveRunnerTestBinding(ctx, tx, workspaceID, sessionID, expectedCollector)
	if err != nil {
		fatal(err.Error())
	}
	evidence, err := customeractionevidence.BuildFromKubernetesAudit(ctx, selected, binding, time.Now().UTC())
	if err != nil {
		fatal(fmt.Sprintf("build Stage 5A audit evidence: %v", err))
	}
	if _, err = customeractionevidence.Store(ctx, tx, evidence); !errors.Is(err, customeractionevidence.ErrUntrustedInput) {
		fatal("runner evidence reached customer Store")
	}
	persisted, err := customeractionevidence.StoreRunnerTestEvidence(ctx, tx, workspaceID, sessionID, expectedCollector, evidence)
	if err != nil {
		fatal(err.Error())
	}
	duplicate, err := customeractionevidence.StoreRunnerTestEvidence(ctx, tx, workspaceID, sessionID, expectedCollector, evidence)
	if err != nil || !duplicate.Duplicate || duplicate.ID != persisted.ID {
		fatal("idempotent retry failed")
	}
	if err = tx.Commit(ctx); err != nil {
		fatal(err.Error())
	}
	// Read and challenge the committed row in a separate transaction.
	tx, err = pool.Begin(ctx)
	if err != nil {
		fatal(err.Error())
	}
	defer tx.Rollback(ctx)
	retrieved, err := customeractionevidence.GetRunnerTestEvidence(ctx, tx, workspaceID, sessionID, expectedCollector, persisted.ID)
	if err != nil {
		fatal(err.Error())
	}
	if retrieved.Integrity.Digest != evidence.Integrity.Digest || retrieved.Trust != customeractionevidence.TrustInsufficientProvenance || retrieved.Actor.Status != "unknown" || retrieved.Actor.Identity != "" {
		fatal("retrieved evidence changed provenance or attribution")
	}
	otherBinding, err := customeractionevidence.ResolveRunnerTestBinding(ctx, tx, workspaceID, otherSessionID, expectedCollector)
	if err != nil {
		fatal(err.Error())
	}
	replay, err := customeractionevidence.BuildFromKubernetesAudit(ctx, selected, otherBinding, time.Now().UTC())
	if err != nil {
		fatal(err.Error())
	}
	if _, err = customeractionevidence.StoreRunnerTestEvidence(ctx, tx, workspaceID, otherSessionID, expectedCollector, replay); !errors.Is(err, identity.ErrConflict) {
		fatal("cross-session replay accepted")
	}
	if _, err = customeractionevidence.GetRunnerTestEvidence(ctx, tx, workspaceID, otherSessionID, expectedCollector, persisted.ID); !errors.Is(err, auth.ErrForbidden) {
		fatal("cross-session retrieval accepted")
	}
	if _, err = customeractionevidence.GetRunnerTestEvidence(ctx, tx, otherWorkspaceID, sessionID, expectedCollector, persisted.ID); !errors.Is(err, auth.ErrForbidden) {
		fatal("cross-tenant retrieval accepted")
	}
	if _, err = customeractionevidence.ResolveRunnerTestBinding(ctx, tx, workspaceID, sessionID, "github-actions:wrong:kind-create-cluster:evalplane-e2e"); !errors.Is(err, auth.ErrForbidden) {
		fatal("wrong collector accepted")
	}
	wrongCluster := binding
	wrongCluster.ClusterIdentity = "kind:wrong"
	if _, err = customeractionevidence.BuildFromKubernetesAudit(ctx, selected, wrongCluster, time.Now().UTC()); !errors.Is(err, customeractionevidence.ErrInvalidKubernetesAuditEvidence) {
		fatal("wrong cluster accepted")
	}
	tampered := evidence
	tampered.Target.Name = "forged"
	tampered.Integrity.Digest = customeractionevidence.Digest(tampered)
	if _, err = customeractionevidence.StoreRunnerTestEvidence(ctx, tx, workspaceID, sessionID, expectedCollector, tampered); !errors.Is(err, customeractionevidence.ErrUntrustedInput) {
		fatal("tampered evidence accepted")
	}
	if _, err = tx.Exec(ctx, `UPDATE runner_audit_test_sessions SET status='completed' WHERE id=$1`, sessionID); err != nil {
		fatal(err.Error())
	}
	if _, err = customeractionevidence.GetRunnerTestEvidence(ctx, tx, workspaceID, sessionID, expectedCollector, persisted.ID); err != nil {
		fatal("completed-session evidence retrieval failed: " + err.Error())
	}
	if err = tx.Commit(ctx); err != nil {
		fatal(err.Error())
	}
	result := struct {
		Status      string                          `json:"status"`
		Source      kubeaudit.SourceIdentity        `json:"source"`
		Event       kubeaudit.Event                 `json:"event"`
		Evidence    customeractionevidence.Evidence `json:"evidence"`
		PersistedID string                          `json:"persisted_id"`
		Retrieved   customeractionevidence.Evidence `json:"retrieved"`
		Checks      string                          `json:"checks"`
		Notice      string                          `json:"notice"`
	}{"runner_bound_audit_persisted", source, event, evidence, persisted.ID, retrieved, "retry_replay_scope_cluster_collector_tamper_customer_store_rejected", "runner-bound Kind test evidence only; no production customer source authentication"}
	if err = json.NewEncoder(os.Stdout).Encode(result); err != nil {
		fatal(err.Error())
	}
}
func fatal(message string) { fmt.Fprintln(os.Stderr, message); os.Exit(1) }
