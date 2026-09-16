package agent

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"testing"

	"github.com/evalplane/evalplane/backend/internal/auth"
	"github.com/evalplane/evalplane/backend/internal/identity"
	"github.com/golang-migrate/migrate/v4"
	_ "github.com/golang-migrate/migrate/v4/database/postgres"
	_ "github.com/golang-migrate/migrate/v4/source/file"
	"github.com/jackc/pgx/v5/pgxpool"
)

func TestAgentIntegration(t *testing.T) {
	url := os.Getenv("TEST_DATABASE_URL")
	if url == "" {
		t.Skip("TEST_DATABASE_URL is not set")
	}
	m, err := migrate.New("file://../../migrations", url)
	if err != nil {
		t.Fatal(err)
	}
	defer m.Close()
	if err = m.Up(); err != nil && !errors.Is(err, migrate.ErrNoChange) {
		t.Fatal(err)
	}
	ctx := context.Background()
	pool, err := pgxpool.New(ctx, url)
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	if _, err = pool.Exec(ctx, `TRUNCATE organizations,users RESTART IDENTITY CASCADE`); err != nil {
		t.Fatal(err)
	}
	idRepo := identity.NewRepository()
	org, err := idRepo.CreateOrganization(ctx, pool, identity.OrganizationInput{Name: "Org", Slug: "agent-test"})
	if err != nil {
		t.Fatal(err)
	}
	workspace, err := idRepo.CreateWorkspace(ctx, pool, org.ID, identity.WorkspaceInput{Name: "One", Slug: "one"})
	if err != nil {
		t.Fatal(err)
	}
	other, err := idRepo.CreateWorkspace(ctx, pool, org.ID, identity.WorkspaceInput{Name: "Two", Slug: "two"})
	if err != nil {
		t.Fatal(err)
	}
	user, err := idRepo.CreateUser(ctx, pool, identity.UserInput{Email: "agent-test@example.com", FullName: "Agent Test"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = idRepo.CreateMembership(ctx, pool, workspace.ID, user.ID, "member"); err != nil {
		t.Fatal(err)
	}
	if _, err = idRepo.CreateMembership(ctx, pool, other.ID, user.ID, "member"); err != nil {
		t.Fatal(err)
	}
	service := NewService(pool, NewRepository(), auth.NewRepository())
	p := auth.Principal{UserID: user.ID}
	if _, err = pool.Exec(ctx, `INSERT INTO agents (slug,name,visibility,status) VALUES ('catalog-active','Catalog','public','active'),('catalog-archived','Archived','public','archived'),('catalog-private','Private','private','active')`); err != nil {
		t.Fatal(err)
	}
	public, err := service.ListPublic(ctx, Filters{Pagination: identity.Pagination{Limit: 50}})
	if err != nil || len(public) != 1 || public[0].Slug != "catalog-active" {
		t.Fatalf("public=%+v err=%v", public, err)
	}
	if _, err = service.GetPublic(ctx, "catalog-private"); !errors.Is(err, identity.ErrNotFound) {
		t.Fatalf("private public-detail error=%v", err)
	}
	if _, err = service.GetPublic(ctx, "catalog-archived"); !errors.Is(err, identity.ErrNotFound) {
		t.Fatalf("archived public-detail error=%v", err)
	}
	input := CreateInput{Name: "Checkout", Slug: "checkout", InitialVersion: VersionInput{VersionLabel: "v1", InterfaceKind: "http", EndpointURL: "https://agent.example/v1/decide", Capabilities: []string{"kubernetes"}, Metadata: json.RawMessage(`{}`)}}
	created, err := service.Create(ctx, p, workspace.ID, input)
	if err != nil || created.LatestVersion == nil {
		t.Fatalf("create=%+v err=%v", created, err)
	}
	if _, err = service.Create(ctx, p, workspace.ID, input); !errors.Is(err, identity.ErrConflict) {
		t.Fatalf("duplicate slug error=%v", err)
	}
	if _, err = service.CreateVersion(ctx, p, workspace.ID, created.ID, input.InitialVersion); !errors.Is(err, identity.ErrConflict) {
		t.Fatalf("duplicate version error=%v", err)
	}
	second, err := service.CreateVersion(ctx, p, workspace.ID, created.ID, VersionInput{VersionLabel: "v2", Metadata: json.RawMessage(`{}`)})
	if err != nil {
		t.Fatal(err)
	}
	latest, err := service.GetWorkspace(ctx, p, workspace.ID, created.ID)
	if err != nil || latest.LatestVersion == nil || latest.LatestVersion.ID != second.ID {
		t.Fatalf("latest=%+v err=%v", latest.LatestVersion, err)
	}
	archived := "archived"
	if _, err = service.Update(ctx, p, workspace.ID, created.ID, UpdateInput{Status: &archived}); err != nil {
		t.Fatal(err)
	}
	if _, err = service.CreateVersion(ctx, p, workspace.ID, created.ID, VersionInput{VersionLabel: "v3", Metadata: json.RawMessage(`{}`)}); !errors.Is(err, identity.ErrConflict) {
		t.Fatalf("archived version error=%v", err)
	}
	if _, err = service.GetWorkspace(ctx, p, other.ID, created.ID); !errors.Is(err, identity.ErrNotFound) {
		t.Fatalf("cross-workspace error=%v", err)
	}
}
