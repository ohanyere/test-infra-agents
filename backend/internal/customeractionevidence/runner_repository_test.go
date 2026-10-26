package customeractionevidence

import (
	"context"
	"errors"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/evalplane/evalplane/backend/internal/auth"
	"github.com/evalplane/evalplane/backend/internal/identity"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
)

func TestRunnerRepositoryIntegration(t *testing.T) {
	url := os.Getenv("TEST_DATABASE_URL")
	if url == "" {
		t.Skip("TEST_DATABASE_URL is not set")
	}
	ctx := context.Background()
	pool, err := pgxpool.New(ctx, url)
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	tx, err := pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback(ctx)
	suffix := strings.ReplaceAll(uuid.NewString(), "-", "")[:12]
	var workspace, other, registration, session, otherSession string
	err = tx.QueryRow(ctx, `WITH o AS (INSERT INTO organizations(name,slug) VALUES('Audit test',$1) RETURNING id) INSERT INTO workspaces(organization_id,name,slug) SELECT id,'Audit test','audit' FROM o RETURNING id`, "audit-test-"+suffix).Scan(&workspace)
	if err != nil {
		t.Fatal(err)
	}
	err = tx.QueryRow(ctx, `WITH o AS (INSERT INTO organizations(name,slug) VALUES('Other audit test',$1) RETURNING id) INSERT INTO workspaces(organization_id,name,slug) SELECT id,'Other audit test','audit' FROM o RETURNING id`, "other-audit-test-"+suffix).Scan(&other)
	if err != nil {
		t.Fatal(err)
	}
	event, source, _ := validKubernetesAuditFixture()
	source.AcquiredAt = time.Now().UTC().Add(-time.Second)
	event.RequestAt = source.AcquiredAt.Add(-2 * time.Second)
	event.StageAt = source.AcquiredAt.Add(-time.Second)
	source.ProvisioningIdentity = "github-actions:fixture:kind-create-cluster:evalplane-e2e"
	err = tx.QueryRow(ctx, `INSERT INTO runner_audit_test_registrations(workspace_id,cluster_identity,namespaces,resource_kinds,collector_identity) VALUES($1,$2,$3,$4,$5) RETURNING id`, workspace, source.ClusterIdentity, []string{"proof"}, []string{"Deployment"}, source.ProvisioningIdentity).Scan(&registration)
	if err != nil {
		t.Fatal(err)
	}
	for _, target := range []*string{&session, &otherSession} {
		err = tx.QueryRow(ctx, `INSERT INTO runner_audit_test_sessions(workspace_id,registration_id) VALUES($1,$2) RETURNING id`, workspace, registration).Scan(target)
		if err != nil {
			t.Fatal(err)
		}
	}
	binding, err := ResolveRunnerTestBinding(ctx, tx, workspace, session, source.ProvisioningIdentity)
	if err != nil {
		t.Fatal(err)
	}
	e, err := buildFromValidatedKubernetesSnapshot(ctx, event, source, binding, time.Now().UTC())
	if err != nil {
		t.Fatal(err)
	}
	if _, err = Store(ctx, tx, e); !errors.Is(err, ErrUntrustedInput) {
		t.Fatalf("customer Store accepted runner evidence: %v", err)
	}
	first, err := StoreRunnerTestEvidence(ctx, tx, workspace, session, source.ProvisioningIdentity, e)
	if err != nil {
		t.Fatal(err)
	}
	again, err := StoreRunnerTestEvidence(ctx, tx, workspace, session, source.ProvisioningIdentity, e)
	if err != nil || !again.Duplicate || again.ID != first.ID {
		t.Fatalf("retry: %+v %v", again, err)
	}
	immutableCheck, err := tx.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = immutableCheck.Exec(ctx, `UPDATE runner_kubernetes_audit_evidence SET source_event_id='changed' WHERE id=$1`, first.ID); err == nil {
		t.Fatal("persisted runner evidence was mutable")
	}
	if err = immutableCheck.Rollback(ctx); err != nil {
		t.Fatal(err)
	}
	got, err := GetRunnerTestEvidence(ctx, tx, workspace, session, source.ProvisioningIdentity, first.ID)
	if err != nil || got.KubernetesAudit.SourceEventDigest != e.KubernetesAudit.SourceEventDigest || got.Actor.Status != "unknown" || got.Actor.Identity != "" || got.Trust != TrustInsufficientProvenance {
		t.Fatalf("retrieval: %+v %v", got, err)
	}
	if _, err = GetRunnerTestEvidence(ctx, tx, other, session, source.ProvisioningIdentity, first.ID); !errors.Is(err, auth.ErrForbidden) {
		t.Fatalf("wrong tenant: %v", err)
	}
	if _, err = GetRunnerTestEvidence(ctx, tx, workspace, otherSession, source.ProvisioningIdentity, first.ID); !errors.Is(err, auth.ErrForbidden) {
		t.Fatalf("wrong session: %v", err)
	}
	if _, err = ResolveRunnerTestBinding(ctx, tx, workspace, session, "github-actions:other"); !errors.Is(err, auth.ErrForbidden) {
		t.Fatalf("wrong collector: %v", err)
	}
	contradictoryEvent := event
	contradictoryEvent.ObjectRef.Name = "different-deployment"
	contradictory, err := buildFromValidatedKubernetesSnapshot(ctx, contradictoryEvent, source, binding, time.Now().UTC())
	if err != nil {
		t.Fatal(err)
	}
	if _, err = StoreRunnerTestEvidence(ctx, tx, workspace, session, source.ProvisioningIdentity, contradictory); !errors.Is(err, identity.ErrConflict) {
		t.Fatalf("contradictory retry: %v", err)
	}
	otherBinding, err := ResolveRunnerTestBinding(ctx, tx, workspace, otherSession, source.ProvisioningIdentity)
	if err != nil {
		t.Fatal(err)
	}
	replay, err := buildFromValidatedKubernetesSnapshot(ctx, event, source, otherBinding, time.Now().UTC())
	if err != nil {
		t.Fatal(err)
	}
	if _, err = StoreRunnerTestEvidence(ctx, tx, workspace, otherSession, source.ProvisioningIdentity, replay); !errors.Is(err, identity.ErrConflict) {
		t.Fatalf("replay: %v", err)
	}
	wrong := binding
	wrong.ClusterIdentity = "kind:wrong"
	if _, err = buildFromValidatedKubernetesSnapshot(ctx, event, source, wrong, time.Now().UTC()); !errors.Is(err, ErrInvalidKubernetesAuditEvidence) {
		t.Fatalf("wrong cluster: %v", err)
	}
	tampered := e
	tampered.Operation = "update"
	tampered.Integrity.Digest = Digest(tampered)
	if _, err = StoreRunnerTestEvidence(ctx, tx, workspace, session, source.ProvisioningIdentity, tampered); !errors.Is(err, ErrUntrustedInput) {
		t.Fatalf("tamper: %v", err)
	}
	if _, err = tx.Exec(ctx, `UPDATE runner_audit_test_sessions SET status='completed' WHERE id=$1`, session); err != nil {
		t.Fatal(err)
	}
	if _, err = GetRunnerTestEvidence(ctx, tx, workspace, session, source.ProvisioningIdentity, first.ID); err != nil {
		t.Fatalf("completed-session retrieval: %v", err)
	}
	if _, err = StoreRunnerTestEvidence(ctx, tx, workspace, session, source.ProvisioningIdentity, e); !errors.Is(err, auth.ErrForbidden) {
		t.Fatalf("completed-session write: %v", err)
	}
}
