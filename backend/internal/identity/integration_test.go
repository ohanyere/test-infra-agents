package identity

import (
	"context"
	"errors"
	"os"
	"testing"

	"github.com/golang-migrate/migrate/v4"
	_ "github.com/golang-migrate/migrate/v4/database/postgres"
	_ "github.com/golang-migrate/migrate/v4/source/file"
	"github.com/jackc/pgx/v5/pgxpool"
)

func TestIdentityLifecycleIntegration(t *testing.T) {
	databaseURL := os.Getenv("TEST_DATABASE_URL")
	if databaseURL == "" {
		t.Skip("TEST_DATABASE_URL is not set")
	}
	migrations, err := migrate.New("file://../../migrations", databaseURL)
	if err != nil {
		t.Fatal(err)
	}
	defer migrations.Close()
	if err = migrations.Up(); err != nil && !errors.Is(err, migrate.ErrNoChange) {
		t.Fatal(err)
	}
	ctx := context.Background()
	pool, err := pgxpool.New(ctx, databaseURL)
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	if _, err = pool.Exec(ctx, `TRUNCATE organizations, users RESTART IDENTITY CASCADE`); err != nil {
		t.Fatal(err)
	}
	service := NewService(pool, NewRepository())
	organization, err := service.CreateOrganization(ctx, OrganizationInput{Name: "Acme", Slug: "acme"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = service.CreateOrganization(ctx, OrganizationInput{Name: "Duplicate", Slug: "acme"}); !errors.Is(err, ErrConflict) {
		t.Fatalf("duplicate organization error=%v", err)
	}
	workspace, err := service.CreateWorkspace(ctx, organization.ID, WorkspaceInput{Name: "Infrastructure", Slug: "infrastructure"})
	if err != nil {
		t.Fatal(err)
	}
	user, err := service.CreateUser(ctx, UserInput{Email: " FRANCIS@ACME.COM ", FullName: "Francis"})
	if err != nil {
		t.Fatal(err)
	}
	if user.Email != "francis@acme.com" {
		t.Fatalf("email not normalized: %q", user.Email)
	}
	if _, err = service.CreateUser(ctx, UserInput{Email: "francis@acme.com", FullName: "Other"}); !errors.Is(err, ErrConflict) {
		t.Fatalf("duplicate user error=%v", err)
	}
	membership, err := service.CreateMembership(ctx, workspace.ID, user.ID, "member")
	if err != nil {
		t.Fatal(err)
	}
	if membership.Role != "member" {
		t.Fatalf("role=%q", membership.Role)
	}
	membership, err = service.UpdateMembership(ctx, workspace.ID, user.ID, "admin")
	if err != nil || membership.Role != "admin" {
		t.Fatalf("update membership: role=%q err=%v", membership.Role, err)
	}
	if err = service.DeleteMembership(ctx, workspace.ID, user.ID); err != nil {
		t.Fatal(err)
	}
	bootstrap, err := service.Bootstrap(ctx, BootstrapInput{Organization: OrganizationInput{Name: "Second", Slug: "second"}, Workspace: WorkspaceInput{Name: "Second Workspace", Slug: "main"}, User: UserInput{Email: "owner@second.example", FullName: "Owner"}})
	if err != nil {
		t.Fatal(err)
	}
	if bootstrap.Membership.Role != "owner" {
		t.Fatalf("bootstrap role=%q", bootstrap.Membership.Role)
	}
	_, err = service.Bootstrap(ctx, BootstrapInput{Organization: OrganizationInput{Name: "Rolled Back", Slug: "rolled-back"}, Workspace: WorkspaceInput{Name: "Invalid", Slug: "main"}, User: UserInput{Email: "owner@second.example", FullName: "Duplicate"}})
	if !errors.Is(err, ErrConflict) {
		t.Fatalf("bootstrap conflict error=%v", err)
	}
	var count int
	if err = pool.QueryRow(ctx, `SELECT count(*) FROM organizations WHERE slug='rolled-back'`).Scan(&count); err != nil || count != 0 {
		t.Fatalf("bootstrap rollback count=%d err=%v", count, err)
	}
}
