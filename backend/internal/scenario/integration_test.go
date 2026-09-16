package scenario

import (
	"context"
	"encoding/json"
	"errors"
	"github.com/evalplane/evalplane/backend/internal/auth"
	"github.com/evalplane/evalplane/backend/internal/identity"
	"github.com/golang-migrate/migrate/v4"
	_ "github.com/golang-migrate/migrate/v4/database/postgres"
	_ "github.com/golang-migrate/migrate/v4/source/file"
	"github.com/jackc/pgx/v5/pgxpool"
	"os"
	"testing"
)

func TestScenarioIntegration(t *testing.T) {
	url := os.Getenv("TEST_DATABASE_URL")
	if url == "" {
		t.Skip("TEST_DATABASE_URL is not set")
	}
	m, e := migrate.New("file://../../migrations", url)
	if e != nil {
		t.Fatal(e)
	}
	defer m.Close()
	if e = m.Up(); e != nil && !errors.Is(e, migrate.ErrNoChange) {
		t.Fatal(e)
	}
	ctx := context.Background()
	pool, e := pgxpool.New(ctx, url)
	if e != nil {
		t.Fatal(e)
	}
	defer pool.Close()
	if _, e = pool.Exec(ctx, `TRUNCATE organizations,users RESTART IDENTITY CASCADE`); e != nil {
		t.Fatal(e)
	}
	ids := identity.NewRepository()
	org, e := ids.CreateOrganization(ctx, pool, identity.OrganizationInput{Name: "Org", Slug: "scenario-test"})
	if e != nil {
		t.Fatal(e)
	}
	one, e := ids.CreateWorkspace(ctx, pool, org.ID, identity.WorkspaceInput{Name: "One", Slug: "one"})
	if e != nil {
		t.Fatal(e)
	}
	two, e := ids.CreateWorkspace(ctx, pool, org.ID, identity.WorkspaceInput{Name: "Two", Slug: "two"})
	if e != nil {
		t.Fatal(e)
	}
	user, e := ids.CreateUser(ctx, pool, identity.UserInput{Email: "scenario-test@example.com", FullName: "Test"})
	if e != nil {
		t.Fatal(e)
	}
	for _, w := range []string{one.ID, two.ID} {
		if _, e = ids.CreateMembership(ctx, pool, w, user.ID, "member"); e != nil {
			t.Fatal(e)
		}
	}
	svc := NewService(pool, NewRepository(), auth.NewRepository())
	p := auth.Principal{UserID: user.ID}
	criterion := CriterionInput{Key: "availability", Name: "Availability", MetricKey: "application.availability_pct", Operator: "gte", TargetValue: json.RawMessage(`99.9`)}
	input := CreateInput{Name: "Traffic", Slug: "traffic", InitialVersion: VersionInput{Description: "Initial", Category: "traffic", Difficulty: "medium", DurationSeconds: 600, Environment: json.RawMessage(`{}`), Conditions: json.RawMessage(`[]`), Criteria: []CriterionInput{criterion}}}
	created, e := svc.Create(ctx, p, one.ID, input)
	if e != nil || created.LatestVersion == nil || created.LatestVersion.Version != 1 || len(created.LatestVersion.Criteria) != 1 {
		t.Fatalf("create=%+v error=%v", created, e)
	}
	if _, e = svc.Create(ctx, p, one.ID, input); !errors.Is(e, identity.ErrConflict) {
		t.Fatalf("duplicate=%v", e)
	}
	v2, e := svc.CreateVersion(ctx, p, one.ID, created.ID, VersionInput{Description: "Second", Category: "traffic", Difficulty: "hard", DurationSeconds: 700, Environment: json.RawMessage(`{}`), Conditions: json.RawMessage(`[]`), Criteria: []CriterionInput{{Key: "slo", Name: "SLO", MetricKey: "application.slo_restored", Operator: "required", TargetValue: json.RawMessage(`true`)}}})
	if e != nil || v2.Version != 2 || len(v2.Criteria) != 1 {
		t.Fatalf("v2=%+v error=%v", v2, e)
	}
	latest, e := svc.GetWorkspace(ctx, p, one.ID, created.ID)
	if e != nil || latest.LatestVersion.Version != 2 || latest.LatestVersion.Criteria[0].Key != "slo" {
		t.Fatalf("latest=%+v error=%v", latest.LatestVersion, e)
	}
	archived := "archived"
	if _, e = svc.Update(ctx, p, one.ID, created.ID, UpdateInput{Status: &archived}); e != nil {
		t.Fatal(e)
	}
	if _, e = svc.CreateVersion(ctx, p, one.ID, created.ID, input.InitialVersion); !errors.Is(e, identity.ErrConflict) {
		t.Fatalf("archive=%v", e)
	}
	if _, e = svc.GetWorkspace(ctx, p, two.ID, created.ID); !errors.Is(e, identity.ErrNotFound) {
		t.Fatalf("isolation=%v", e)
	}
	if _, e = pool.Exec(ctx, `INSERT INTO scenarios(slug,name,kind,visibility,status)VALUES('catalog','Catalog','built_in','public','active'),('custom-public','Custom','custom','public','active'),('archived','Archived','built_in','public','archived')`); e != nil {
		t.Fatal(e)
	}
	pub, e := svc.ListPublic(ctx, Filters{Pagination: identity.Pagination{Limit: 50}})
	if e != nil || len(pub) != 1 || pub[0].Slug != "catalog" {
		t.Fatalf("public=%+v error=%v", pub, e)
	}
}
