package auth

import (
	"context"
	"errors"
	"github.com/evalplane/evalplane/backend/internal/identity"
	"github.com/golang-migrate/migrate/v4"
	_ "github.com/golang-migrate/migrate/v4/database/postgres"
	_ "github.com/golang-migrate/migrate/v4/source/file"
	"github.com/jackc/pgx/v5/pgxpool"
	"os"
	"testing"
	"time"
)

func TestAuthenticationAndAuthorizationIntegration(t *testing.T) {
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
	idService := identity.NewService(pool, idRepo)
	service, err := NewService(pool, NewRepository(), idRepo, time.Hour, testPasswordParams)
	if err != nil {
		t.Fatal(err)
	}
	signup := func(email, orgSlug, workspaceSlug string) SignupResult {
		result, signupErr := service.Signup(ctx, SignupInput{FullName: "Francis", Email: email, Password: "correct horse battery staple", Organization: identity.OrganizationInput{Name: "Organization", Slug: orgSlug}, Workspace: identity.WorkspaceInput{Name: "Workspace", Slug: workspaceSlug}})
		if signupErr != nil {
			t.Fatal(signupErr)
		}
		return result
	}
	owner := signup("OWNER@EXAMPLE.COM", "primary", "main")
	if owner.RawToken == "" {
		t.Fatal("signup omitted raw token")
	}
	var storedDigest []byte
	var credential string
	if err = pool.QueryRow(ctx, `SELECT s.token_hash,c.password_hash FROM sessions s JOIN password_credentials c ON c.user_id=s.user_id WHERE s.user_id=$1 ORDER BY s.created_at LIMIT 1`, owner.User.ID).Scan(&storedDigest, &credential); err != nil {
		t.Fatal(err)
	}
	if string(storedDigest) == owner.RawToken || credential == owner.PasswordForTest() {
		t.Fatal("raw credential persisted")
	}
	if string(storedDigest) != string(TokenDigest(owner.RawToken)) {
		t.Fatal("stored digest mismatch")
	}
	principal, err := service.Authenticate(ctx, owner.RawToken)
	if err != nil || principal.UserID != owner.User.ID {
		t.Fatalf("authenticate: %+v %v", principal, err)
	}
	login, err := service.Login(ctx, LoginInput{Email: " OWNER@EXAMPLE.COM ", Password: "correct horse battery staple"})
	if err != nil || login.RawToken == "" {
		t.Fatalf("login: %v", err)
	}
	if _, err = service.Login(ctx, LoginInput{Email: "owner@example.com", Password: "wrong password value"}); !errors.Is(err, ErrInvalidCredentials) {
		t.Fatalf("wrong password error=%v", err)
	}
	if _, err = service.Login(ctx, LoginInput{Email: "unknown@example.com", Password: "wrong password value"}); !errors.Is(err, ErrInvalidCredentials) {
		t.Fatalf("unknown email error=%v", err)
	}
	if err = service.Logout(ctx, principal); err != nil {
		t.Fatal(err)
	}
	if _, err = service.Authenticate(ctx, owner.RawToken); !errors.Is(err, ErrUnauthenticated) {
		t.Fatalf("revoked session error=%v", err)
	}
	if _, err = service.Authenticate(ctx, "random-invalid-token"); !errors.Is(err, ErrUnauthenticated) {
		t.Fatalf("invalid session error=%v", err)
	}
	expiredRaw, expiredDigest, _ := GenerateSessionToken()
	if _, err = pool.Exec(ctx, `INSERT INTO sessions(user_id,token_hash,created_at,expires_at) VALUES($1,$2,now()-interval '2 hours',now()-interval '1 hour')`, owner.User.ID, expiredDigest); err != nil {
		t.Fatal(err)
	}
	if _, err = service.Authenticate(ctx, expiredRaw); !errors.Is(err, ErrUnauthenticated) {
		t.Fatalf("expired session error=%v", err)
	}
	countRows := func(query string, args ...any) int {
		var count int
		if scanErr := pool.QueryRow(ctx, query, args...).Scan(&count); scanErr != nil {
			t.Fatal(scanErr)
		}
		return count
	}
	before := countRows(`SELECT count(*) FROM organizations`)
	if _, err = service.Signup(ctx, SignupInput{FullName: "Duplicate", Email: "owner@example.com", Password: "correct horse battery staple", Organization: identity.OrganizationInput{Name: "Rollback", Slug: "rollback-email"}, Workspace: identity.WorkspaceInput{Name: "Rollback", Slug: "main"}}); !errors.Is(err, identity.ErrConflict) {
		t.Fatalf("duplicate email error=%v", err)
	}
	if countRows(`SELECT count(*) FROM organizations`) != before {
		t.Fatal("duplicate email did not roll back")
	}
	if _, err = service.Signup(ctx, SignupInput{FullName: "New", Email: "new@example.com", Password: "correct horse battery staple", Organization: identity.OrganizationInput{Name: "Duplicate", Slug: "primary"}, Workspace: identity.WorkspaceInput{Name: "Rollback", Slug: "main"}}); !errors.Is(err, identity.ErrConflict) {
		t.Fatalf("duplicate slug error=%v", err)
	}
	if countRows(`SELECT count(*) FROM users WHERE email='new@example.com'`) != 0 {
		t.Fatal("duplicate slug left user")
	}
	if _, err = service.Signup(ctx, SignupInput{FullName: "Short", Email: "short@example.com", Password: "short", Organization: identity.OrganizationInput{Name: "Short", Slug: "short"}, Workspace: identity.WorkspaceInput{Name: "Short", Slug: "main"}}); !errors.Is(err, identity.ErrValidation) {
		t.Fatalf("short password error=%v", err)
	}
	if countRows(`SELECT count(*) FROM organizations WHERE slug='short'`) != 0 {
		t.Fatal("invalid password created rows")
	}
	generated, err := service.Signup(ctx, SignupInput{FullName: "Generated", Email: "generated@example.com", Password: "correct horse battery staple", Organization: identity.OrganizationInput{Name: "Acme Team"}, Workspace: identity.WorkspaceInput{Name: "Acme Team Infrastructure"}})
	if err != nil || generated.Organization.Slug != "acme-team" || generated.Workspace.Slug != "acme-team-infrastructure" {
		t.Fatalf("generated slugs: org=%q workspace=%q err=%v", generated.Organization.Slug, generated.Workspace.Slug, err)
	}
	if _, err = service.Signup(ctx, SignupInput{FullName: "Generated Conflict", Email: "generated-conflict@example.com", Password: "correct horse battery staple", Organization: identity.OrganizationInput{Name: "Acme Team"}, Workspace: identity.WorkspaceInput{Name: "Another Workspace"}}); !errors.Is(err, identity.ErrConflict) {
		t.Fatalf("generated slug conflict error=%v", err)
	}
	if _, err = service.Signup(ctx, SignupInput{FullName: "Invalid", Email: "invalid-slug@example.com", Password: "correct horse battery staple", Organization: identity.OrganizationInput{Name: "Invalid", Slug: "Bad Slug"}, Workspace: identity.WorkspaceInput{Name: "Workspace"}}); !errors.Is(err, identity.ErrValidation) {
		t.Fatalf("explicit invalid slug error=%v", err)
	}
	if _, err = service.Signup(ctx, SignupInput{FullName: "Impossible", Email: "impossible@example.com", Password: "correct horse battery staple", Organization: identity.OrganizationInput{Name: "!!!"}, Workspace: identity.WorkspaceInput{Name: "Workspace"}}); !errors.Is(err, identity.ErrValidation) {
		t.Fatalf("impossible generated slug error=%v", err)
	}
	ownerPrincipal := Principal{UserID: owner.User.ID}
	makeUser := func(email, role string) identity.User {
		u, e := idService.CreateUser(ctx, identity.UserInput{Email: email, FullName: role})
		if e != nil {
			t.Fatal(e)
		}
		if _, e = idService.CreateMembership(ctx, owner.Workspace.ID, u.ID, role); e != nil {
			t.Fatal(e)
		}
		return u
	}
	viewer := makeUser("viewer@example.com", "viewer")
	member := makeUser("member@example.com", "member")
	memberOnly := makeUser("member-only@example.com", "member")
	admin := makeUser("admin@example.com", "admin")
	candidate, err := idService.CreateUser(ctx, identity.UserInput{Email: "candidate@example.com", FullName: "Candidate"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = service.AddMember(ctx, Principal{UserID: viewer.ID}, owner.Workspace.ID, candidate.ID, "viewer"); !errors.Is(err, ErrForbidden) {
		t.Fatal("viewer added member")
	}
	if _, err = service.AddMember(ctx, Principal{UserID: member.ID}, owner.Workspace.ID, candidate.ID, "viewer"); !errors.Is(err, ErrForbidden) {
		t.Fatal("member added member")
	}
	if _, err = service.AddMember(ctx, Principal{UserID: admin.ID}, owner.Workspace.ID, candidate.ID, "viewer"); err != nil {
		t.Fatalf("admin add: %v", err)
	}
	if _, err = service.ChangeMemberRole(ctx, Principal{UserID: admin.ID}, owner.Workspace.ID, candidate.ID, "owner"); !errors.Is(err, ErrForbidden) {
		t.Fatal("admin promoted owner")
	}
	if err = service.RemoveMember(ctx, Principal{UserID: admin.ID}, owner.Workspace.ID, owner.User.ID); !errors.Is(err, ErrForbidden) {
		t.Fatal("admin removed owner")
	}
	if _, err = service.ChangeMemberRole(ctx, ownerPrincipal, owner.Workspace.ID, member.ID, "admin"); err != nil {
		t.Fatalf("owner role change: %v", err)
	}
	updated, err := service.UpdateWorkspaceName(ctx, ownerPrincipal, owner.Workspace.ID, "Platform Engineering")
	if err != nil || updated.Name != "Platform Engineering" {
		t.Fatalf("owner workspace update: %+v %v", updated, err)
	}
	if _, err = service.UpdateWorkspaceName(ctx, Principal{UserID: admin.ID}, owner.Workspace.ID, "Infrastructure"); err != nil {
		t.Fatalf("admin workspace update: %v", err)
	}
	if _, err = service.UpdateWorkspaceName(ctx, Principal{UserID: viewer.ID}, owner.Workspace.ID, "Forbidden"); !errors.Is(err, ErrForbidden) {
		t.Fatal("viewer updated workspace")
	}
	if _, err = service.UpdateWorkspaceName(ctx, Principal{UserID: memberOnly.ID}, owner.Workspace.ID, "Forbidden"); !errors.Is(err, ErrForbidden) {
		t.Fatal("member updated workspace")
	}
	if _, err = service.UpdateWorkspaceName(ctx, ownerPrincipal, owner.Workspace.ID, "   "); !errors.Is(err, identity.ErrValidation) {
		t.Fatal("empty workspace name accepted")
	}
	unrelated := signup("other@example.com", "other", "main")
	if err = service.CanViewWorkspace(ctx, Principal{UserID: unrelated.User.ID}, owner.Workspace.ID); !errors.Is(err, ErrForbidden) {
		t.Fatal("unrelated user viewed workspace")
	}
	if _, err = service.UpdateWorkspaceName(ctx, Principal{UserID: unrelated.User.ID}, owner.Workspace.ID, "Forbidden"); !errors.Is(err, ErrForbidden) {
		t.Fatal("unrelated user updated workspace")
	}
	if _, err = service.UpdateWorkspaceName(ctx, ownerPrincipal, "11111111-1111-4111-8111-111111111111", "Missing"); !errors.Is(err, ErrForbidden) {
		t.Fatalf("nonexistent workspace error=%v", err)
	}
	organizations, err := service.ListOrganizations(ctx, Principal{UserID: unrelated.User.ID}, identity.Pagination{Limit: 50})
	if err != nil || len(organizations) != 1 || organizations[0].ID != unrelated.Organization.ID {
		t.Fatalf("organization isolation: %+v %v", organizations, err)
	}
	if _, err = service.ChangeMemberRole(ctx, ownerPrincipal, owner.Workspace.ID, owner.User.ID, "admin"); !errors.Is(err, identity.ErrConflict) {
		t.Fatal("final owner demoted")
	}
	if err = service.RemoveMember(ctx, ownerPrincipal, owner.Workspace.ID, owner.User.ID); !errors.Is(err, identity.ErrConflict) {
		t.Fatal("final owner removed")
	}
}
func (SignupResult) PasswordForTest() string { return "correct horse battery staple" }
