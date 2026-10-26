package httpapi

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/evalplane/evalplane/backend/internal/auth"
	"github.com/evalplane/evalplane/backend/internal/identity"
	"github.com/evalplane/evalplane/backend/internal/operational"
	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
)

// Exercises the real handlers and repository together. TEST_DATABASE_URL must
// point at an isolated, migrated database; this is not a hosted source proof.
type memoryIntegrationAuth struct {
	fakeAuth
	ownerID    string
	strangerID string
}

func (a memoryIntegrationAuth) Authenticate(_ context.Context, token string) (auth.Principal, error) {
	switch token {
	case "owner-session":
		return auth.Principal{UserID: a.ownerID}, nil
	case "stranger-session":
		return auth.Principal{UserID: a.strangerID}, nil
	default:
		return auth.Principal{}, auth.ErrUnauthenticated
	}
}

func TestOperationalGuidanceHTTPPostgres(t *testing.T) {
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
	org, err := repo.CreateOrganization(ctx, pool, identity.OrganizationInput{Name: "Guidance HTTP " + suffix, Slug: "guidance-http-" + suffix})
	if err != nil {
		t.Fatal(err)
	}
	workspace, err := repo.CreateWorkspace(ctx, pool, org.ID, identity.WorkspaceInput{Name: "Guidance HTTP", Slug: "guidance-http"})
	if err != nil {
		t.Fatal(err)
	}
	owner, err := repo.CreateUser(ctx, pool, identity.UserInput{Email: "guidance-http-" + suffix + "@example.test", FullName: "Guidance HTTP Owner"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = repo.CreateMembership(ctx, pool, workspace.ID, owner.ID, "owner"); err != nil {
		t.Fatal(err)
	}
	stranger, err := repo.CreateUser(ctx, pool, identity.UserInput{Email: "guidance-stranger-" + suffix + "@example.test", FullName: "Other Tenant"})
	if err != nil {
		t.Fatal(err)
	}
	var agentID, versionID, environmentID string
	if err = pool.QueryRow(ctx, `INSERT INTO agents(owner_workspace_id,slug,name) VALUES($1,$2,'Guidance HTTP agent') RETURNING id`, workspace.ID, "guidance-http-"+suffix).Scan(&agentID); err != nil {
		t.Fatal(err)
	}
	if err = pool.QueryRow(ctx, `INSERT INTO agent_versions(agent_id,version_label,implementation_identity) VALUES($1,'v1','{"contract_version":"agent-implementation-identity:v1","kind":"declared","identifier":"guidance-http-test","provenance_level":"declared"}'::jsonb) RETURNING id`, agentID).Scan(&versionID); err != nil {
		t.Fatal(err)
	}
	if err = pool.QueryRow(ctx, `INSERT INTO customer_kubernetes_environments(workspace_id,name,cluster_identifier,namespaces,resource_kinds,environment_tier,created_by) VALUES($1,'Guidance HTTP test','cluster-guidance-http',ARRAY['checkout'],ARRAY['Deployment'],'staging',$2) RETURNING id`, workspace.ID, owner.ID).Scan(&environmentID); err != nil {
		t.Fatal(err)
	}
	h := memoryHandlers{service: operational.NewService(pool, auth.NewRepository(), nil), logger: slog.New(slog.NewTextHandler(io.Discard, nil))}
	request := func(method, body, userID string) *http.Request {
		route := chi.NewRouteContext()
		route.URLParams.Add("workspaceID", workspace.ID)
		route.URLParams.Add("environmentID", environmentID)
		route.URLParams.Add("credentialID", "")
		r := httptest.NewRequest(method, "/", strings.NewReader(body))
		return r.WithContext(auth.ContextWithPrincipal(context.WithValue(r.Context(), chi.RouteCtxKey, route), auth.Principal{UserID: userID}))
	}
	issueBody, _ := json.Marshal(operational.GuidanceCredentialInput{AgentVersionID: versionID, Namespaces: []string{"checkout"}, ResourceKinds: []string{"Deployment"}, ExpiresAt: time.Now().Add(time.Hour)})
	response := httptest.NewRecorder()
	h.issueCredential(response, request(http.MethodPost, string(issueBody), owner.ID))
	if response.Code != http.StatusCreated {
		t.Fatalf("credential issue status: %d %s", response.Code, response.Body.String())
	}
	var credential operational.GuidanceCredential
	if err = json.Unmarshal(response.Body.Bytes(), &credential); err != nil || credential.Token == "" || credential.AgentVersionID != versionID {
		t.Fatal("credential issue did not bind agent version")
	}
	proposal := `{"namespace":"checkout","resource_kind":"Deployment","resource_name":"checkout","operation":"set_image"}`
	agentRequest := func(token string) *http.Request {
		r := httptest.NewRequest(http.MethodPost, "/v1/agent-guidance", strings.NewReader(proposal))
		r.Header.Set("Authorization", "Bearer "+token)
		return r
	}
	response = httptest.NewRecorder()
	h.agentGuidance(response, agentRequest(credential.Token))
	if response.Code != http.StatusOK {
		t.Fatalf("agent guidance status: %d %s", response.Code, response.Body.String())
	}
	var guidance operational.Guidance
	if err = json.Unmarshal(response.Body.Bytes(), &guidance); err != nil || guidance.AgentVersionID != versionID || guidance.EnvironmentID != environmentID || guidance.TrustworthyHistoryAvailable {
		t.Fatalf("unbound or falsely trusted guidance: %+v %v", guidance, err)
	}
	response = httptest.NewRecorder()
	h.history(response, request(http.MethodGet, "", owner.ID))
	if response.Code != http.StatusOK || !strings.Contains(response.Body.String(), guidance.ID) {
		t.Fatalf("guidance history status: %d %s", response.Code, response.Body.String())
	}
	response = httptest.NewRecorder()
	h.credentials(response, request(http.MethodGet, "", owner.ID))
	if response.Code != http.StatusOK || strings.Contains(response.Body.String(), credential.Token) {
		t.Fatal("credential metadata endpoint exposed the bearer token")
	}
	response = httptest.NewRecorder()
	h.history(response, request(http.MethodGet, "", stranger.ID))
	if response.Code != http.StatusForbidden {
		t.Fatalf("cross-tenant history status: %d", response.Code)
	}
	response = httptest.NewRecorder()
	h.episodes(response, request(http.MethodGet, "", stranger.ID))
	if response.Code != http.StatusForbidden {
		t.Fatalf("cross-tenant episode status: %d", response.Code)
	}
	// Exercise actual routes and middleware as well as the direct handlers.
	login := memoryIntegrationAuth{fakeAuth: fakeAuth{allow: true}, ownerID: owner.ID, strangerID: stranger.ID}
	router := NewRouter(healthyDatabase{}, h.logger, WithIdentityService(fakeIdentity{}), WithAuthentication(login, login, false, time.Hour, "http://localhost:3000"), WithOperationalMemoryService(h.service))
	base := "/v1/workspaces/" + workspace.ID + "/customer-kubernetes-environments/" + environmentID
	routed := func(method, path, body, sessionToken string) *httptest.ResponseRecorder {
		r := httptest.NewRequest(method, path, strings.NewReader(body))
		r.Header.Set("Content-Type", "application/json")
		if method != http.MethodGet {
			r.Header.Set("Origin", "http://localhost:3000")
		}
		if sessionToken != "" {
			r.AddCookie(&http.Cookie{Name: auth.CookieName, Value: sessionToken})
		}
		w := httptest.NewRecorder()
		router.ServeHTTP(w, r)
		return w
	}
	response = routed(http.MethodGet, base+"/guidance-history", "", "owner-session")
	if response.Code != http.StatusOK || !strings.Contains(response.Body.String(), guidance.ID) {
		t.Fatalf("routed owner guidance history: %d %s", response.Code, response.Body.String())
	}
	response = routed(http.MethodGet, base+"/guidance-history", "", "stranger-session")
	if response.Code != http.StatusForbidden {
		t.Fatalf("routed cross-tenant guidance history: %d", response.Code)
	}
	response = routed(http.MethodGet, base+"/guidance-operation-links", "", "owner-session")
	if response.Code != http.StatusOK || !strings.Contains(response.Body.String(), `"items":[]`) {
		t.Fatalf("owner guidance links empty state: %d %s", response.Code, response.Body.String())
	}
	response = routed(http.MethodGet, base+"/guidance-operation-links", "", "stranger-session")
	if response.Code != http.StatusForbidden {
		t.Fatalf("cross-tenant guidance links status: %d", response.Code)
	}
	response = routed(http.MethodGet, base+"/guidance-history", "", "")
	if response.Code != http.StatusUnauthorized {
		t.Fatalf("routed anonymous guidance history: %d", response.Code)
	}
	response = routed(http.MethodPost, "/v1/agent-guidance", proposal, "")
	if response.Code != http.StatusUnauthorized {
		t.Fatalf("routed guidance without bearer: %d", response.Code)
	}
	agentRouteRequest := httptest.NewRequest(http.MethodPost, "/v1/agent-guidance", strings.NewReader(proposal))
	agentRouteRequest.Header.Set("Content-Type", "application/json")
	agentRouteRequest.Header.Set("Authorization", "Bearer "+credential.Token)
	response = httptest.NewRecorder()
	router.ServeHTTP(response, agentRouteRequest)
	if response.Code != http.StatusOK || !strings.Contains(response.Body.String(), environmentID) {
		t.Fatalf("routed agent guidance: %d %s", response.Code, response.Body.String())
	}
	if err = h.service.RevokeGuidanceCredential(ctx, auth.Principal{UserID: owner.ID}, workspace.ID, environmentID, credential.ID); err != nil {
		t.Fatal(err)
	}
	response = httptest.NewRecorder()
	h.agentGuidance(response, agentRequest(credential.Token))
	if response.Code != http.StatusUnauthorized {
		t.Fatalf("revoked bearer status: %d", response.Code)
	}
}
