package operational

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

// Requires a scratch TEST_DATABASE_URL migrated through migration 33.
func TestGuidanceCredentialScopePostgres(t *testing.T) {
	url := os.Getenv("TEST_DATABASE_URL")
	if url == "" {
		t.Skip("TEST_DATABASE_URL is not set")
	}
	config, err := pgxpool.ParseConfig(url)
	if err != nil || !strings.HasSuffix(config.ConnConfig.Database, "_test") && !strings.HasPrefix(config.ConnConfig.Database, "realverax_stage10a_") {
		t.Fatal("TEST_DATABASE_URL must name an explicitly disposable test database")
	}
	ctx := context.Background()
	pool, err := pgxpool.New(ctx, url)
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	suffix := strings.ReplaceAll(uuid.NewString(), "-", "")[:12]
	repo := identity.NewRepository()
	org, err := repo.CreateOrganization(ctx, pool, identity.OrganizationInput{Name: "Guidance " + suffix, Slug: "guidance-" + suffix})
	if err != nil {
		t.Fatal(err)
	}
	workspace, err := repo.CreateWorkspace(ctx, pool, org.ID, identity.WorkspaceInput{Name: "Guidance", Slug: "guidance"})
	if err != nil {
		t.Fatal(err)
	}
	other, err := repo.CreateWorkspace(ctx, pool, org.ID, identity.WorkspaceInput{Name: "Other", Slug: "other-" + suffix})
	if err != nil {
		t.Fatal(err)
	}
	user, err := repo.CreateUser(ctx, pool, identity.UserInput{Email: "guidance-" + suffix + "@example.test", FullName: "Guidance Owner"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = repo.CreateMembership(ctx, pool, workspace.ID, user.ID, "owner"); err != nil {
		t.Fatal(err)
	}
	principal := auth.Principal{UserID: user.ID}
	var agentID, versionID, environmentID string
	if err = pool.QueryRow(ctx, `INSERT INTO agents(owner_workspace_id,slug,name) VALUES($1,$2,'Guidance agent') RETURNING id`, workspace.ID, "guidance-"+suffix).Scan(&agentID); err != nil {
		t.Fatal(err)
	}
	if err = pool.QueryRow(ctx, `INSERT INTO agent_versions(agent_id,version_label,implementation_identity) VALUES($1,'v1','{"contract_version":"agent-implementation-identity:v1","kind":"declared","identifier":"guidance-test-v1","provenance_level":"declared"}'::jsonb) RETURNING id`, agentID).Scan(&versionID); err != nil {
		t.Fatal(err)
	}
	if err = pool.QueryRow(ctx, `INSERT INTO customer_kubernetes_environments(workspace_id,name,cluster_identifier,namespaces,resource_kinds,environment_tier,created_by) VALUES($1,'Guidance test','cluster-guidance',ARRAY['checkout'],ARRAY['Deployment'],'staging',$2) RETURNING id`, workspace.ID, user.ID).Scan(&environmentID); err != nil {
		t.Fatal(err)
	}
	service := NewService(pool, auth.NewRepository(), nil)
	if lessons, listErr := service.ListLessons(ctx, principal, workspace.ID, environmentID); listErr != nil || len(lessons) != 0 {
		t.Fatalf("empty scoped lesson history: %+v %v", lessons, listErr)
	}
	if _, listErr := service.ListGuidanceOperationLinks(ctx, principal, other.ID, environmentID); !errors.Is(listErr, auth.ErrForbidden) {
		t.Fatalf("cross-tenant guidance links accepted: %v", listErr)
	}
	var otherAgentID, otherVersionID string
	if err = pool.QueryRow(ctx, `INSERT INTO agents(owner_workspace_id,slug,name) VALUES($1,$2,'Other guidance agent') RETURNING id`, other.ID, "other-guidance-"+suffix).Scan(&otherAgentID); err != nil {
		t.Fatal(err)
	}
	if err = pool.QueryRow(ctx, `INSERT INTO agent_versions(agent_id,version_label,implementation_identity) VALUES($1,'v1','{"contract_version":"agent-implementation-identity:v1","kind":"declared","identifier":"other-guidance-test-v1","provenance_level":"declared"}'::jsonb) RETURNING id`, otherAgentID).Scan(&otherVersionID); err != nil {
		t.Fatal(err)
	}
	if _, err = service.IssueGuidanceCredential(ctx, principal, workspace.ID, environmentID, GuidanceCredentialInput{AgentVersionID: otherVersionID, Namespaces: []string{"checkout"}, ResourceKinds: []string{"Deployment"}, ExpiresAt: time.Now().Add(time.Hour)}); !errors.Is(err, auth.ErrForbidden) {
		t.Fatalf("cross-workspace agent version accepted: %v", err)
	}
	issued, err := service.IssueGuidanceCredential(ctx, principal, workspace.ID, environmentID, GuidanceCredentialInput{AgentVersionID: versionID, Namespaces: []string{"checkout"}, ResourceKinds: []string{"Deployment"}, ExpiresAt: time.Now().Add(time.Hour)})
	if err != nil {
		t.Fatal(err)
	}
	op := ProposedOperation{Namespace: "checkout", ResourceKind: "Deployment", ResourceName: "checkout", Operation: "set_image"}
	guidance, err := service.GuidanceForAgent(ctx, issued.Token, op)
	if err != nil {
		t.Fatal(err)
	}
	if guidance.TrustworthyHistoryAvailable || guidance.AgentVersionID != versionID || guidance.ID == "" {
		t.Fatalf("untrusted history promoted: %+v", guidance)
	}
	metadata, err := service.ListGuidanceCredentials(ctx, principal, workspace.ID, environmentID)
	if err != nil || len(metadata) != 1 || metadata[0].Token != "" {
		t.Fatalf("credential metadata leaked a token: %+v %v", metadata, err)
	}
	if _, err = service.GuidanceForAgent(ctx, "rvg_invalid", op); !errors.Is(err, auth.ErrForbidden) {
		t.Fatalf("invalid credential accepted: %v", err)
	}
	op.Namespace = "database"
	if _, err = service.GuidanceForAgent(ctx, issued.Token, op); !errors.Is(err, auth.ErrForbidden) {
		t.Fatalf("wrong namespace accepted: %v", err)
	}
	op.Namespace = "checkout"
	op.ResourceKind = "Pod"
	if _, err = service.GuidanceForAgent(ctx, issued.Token, op); !errors.Is(err, auth.ErrForbidden) {
		t.Fatalf("wrong resource kind accepted: %v", err)
	}
	op.ResourceKind = "Deployment"
	if _, err = service.ListGuidanceHistory(ctx, principal, other.ID, environmentID); !errors.Is(err, auth.ErrForbidden) {
		t.Fatalf("cross tenant history accepted: %v", err)
	}
	if err = service.RevokeGuidanceCredential(ctx, principal, workspace.ID, environmentID, issued.ID); err != nil {
		t.Fatal(err)
	}
	op.Namespace = "checkout"
	if _, err = service.GuidanceForAgent(ctx, issued.Token, op); !errors.Is(err, auth.ErrUnauthenticated) {
		t.Fatalf("revoked credential accepted: %v", err)
	}
	expiring, err := service.IssueGuidanceCredential(ctx, principal, workspace.ID, environmentID, GuidanceCredentialInput{AgentVersionID: versionID, Namespaces: []string{"checkout"}, ResourceKinds: []string{"Deployment"}, ExpiresAt: time.Now().Add(time.Hour)})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = pool.Exec(ctx, `UPDATE operational_guidance_credentials SET expires_at=now()-interval '1 second' WHERE id=$1`, expiring.ID); err != nil {
		t.Fatal(err)
	}
	if _, err = service.GuidanceForAgent(ctx, expiring.Token, op); !errors.Is(err, auth.ErrUnauthenticated) {
		t.Fatalf("expired credential accepted: %v", err)
	}
	archived, err := service.IssueGuidanceCredential(ctx, principal, workspace.ID, environmentID, GuidanceCredentialInput{AgentVersionID: versionID, Namespaces: []string{"checkout"}, ResourceKinds: []string{"Deployment"}, ExpiresAt: time.Now().Add(time.Hour)})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = pool.Exec(ctx, `UPDATE agents SET status='archived' WHERE id=$1`, agentID); err != nil {
		t.Fatal(err)
	}
	if _, err = service.GuidanceForAgent(ctx, archived.Token, op); !errors.Is(err, auth.ErrUnauthenticated) {
		t.Fatalf("archived agent credential accepted: %v", err)
	}
}
