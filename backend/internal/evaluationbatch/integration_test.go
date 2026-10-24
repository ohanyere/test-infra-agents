package evaluationbatch

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"testing"

	"github.com/evalplane/evalplane/backend/internal/auth"
	"github.com/evalplane/evalplane/backend/internal/identity"
	"github.com/jackc/pgx/v5/pgxpool"
)

func TestEvaluationBatchIntegration(t *testing.T) {
	url := os.Getenv("TEST_DATABASE_URL")
	if url == "" {
		t.Skip("TEST_DATABASE_URL is not set")
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
	org, e := ids.CreateOrganization(ctx, pool, identity.OrganizationInput{Name: "Org", Slug: "batch-test"})
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
	user, e := ids.CreateUser(ctx, pool, identity.UserInput{Email: "batch-test@example.com", FullName: "Test"})
	if e != nil {
		t.Fatal(e)
	}
	if _, e = ids.CreateMembership(ctx, pool, one.ID, user.ID, "member"); e != nil {
		t.Fatal(e)
	}
	if _, e = ids.CreateMembership(ctx, pool, two.ID, user.ID, "member"); e != nil {
		t.Fatal(e)
	}
	p := auth.Principal{UserID: user.ID}
	scenarioVersionID := insertScenarioVersion(t, ctx, pool, &one.ID, "owned", "custom", "workspace", "active")
	otherScenarioVersionID := insertScenarioVersion(t, ctx, pool, &two.ID, "other", "custom", "private", "active")
	publicScenarioVersionID := insertScenarioVersion(t, ctx, pool, nil, "public", "built_in", "public", "active")
	archivedScenarioVersionID := insertScenarioVersion(t, ctx, pool, &one.ID, "archived", "custom", "workspace", "archived")
	agent1 := insertAgentVersion(t, ctx, pool, &one.ID, "agent-a", "workspace", "active")
	agent2 := insertAgentVersion(t, ctx, pool, &one.ID, "agent-b", "workspace", "active")
	otherAgent := insertAgentVersion(t, ctx, pool, &two.ID, "other-agent", "private", "active")
	publicAgent := insertAgentVersion(t, ctx, pool, nil, "public-agent", "public", "active")
	archivedAgent := insertAgentVersion(t, ctx, pool, &one.ID, "archived-agent", "workspace", "archived")
	svc := NewService(pool, NewRepository(), auth.NewRepository())
	makeBatch := func(agentIDs []string, runs int) Batch {
		v, e := svc.Create(ctx, p, one.ID, CreateInput{ScenarioVersionID: scenarioVersionID, AgentVersionIDs: agentIDs, RunsPerAgent: runs, EnvironmentMode: "isolated", Config: json.RawMessage(`{}`)})
		if e != nil {
			t.Fatal(e)
		}
		return v
	}
	oneBatch := makeBatch([]string{agent1}, 3)
	if oneBatch.RunCount != 3 || oneBatch.Scenario.ScenarioVersionID != scenarioVersionID {
		t.Fatalf("one=%+v", oneBatch)
	}
	twoBatch := makeBatch([]string{agent1, agent2}, 3)
	if twoBatch.RunCount != 6 {
		t.Fatalf("two=%+v", twoBatch)
	}
	assertExactVersions(t, ctx, pool, twoBatch.ID, scenarioVersionID, []string{agent1, agent2})
	var pinnedIdentity json.RawMessage
	if e := pool.QueryRow(ctx, `SELECT agent_implementation_identity FROM runs WHERE batch_id=$1 AND agent_version_id=$2 ORDER BY attempt_number LIMIT 1`, twoBatch.ID, agent1).Scan(&pinnedIdentity); e != nil {
		t.Fatal(e)
	}
	var pinned map[string]any
	if e := json.Unmarshal(pinnedIdentity, &pinned); e != nil || pinned["identifier"] != "agent-a-revision" {
		t.Fatalf("run identity was not pinned: identity=%s error=%v", pinnedIdentity, e)
	}
	if _, e := pool.Exec(ctx, `UPDATE agent_versions SET implementation_identity=jsonb_set(implementation_identity,'{identifier}','"replacement"') WHERE id=$1`, agent1); e == nil {
		t.Fatal("agent version implementation identity mutation must be rejected")
	}
	var stillPinned json.RawMessage
	if e := pool.QueryRow(ctx, `SELECT agent_implementation_identity FROM runs WHERE batch_id=$1 AND agent_version_id=$2 ORDER BY attempt_number LIMIT 1`, twoBatch.ID, agent1).Scan(&stillPinned); e != nil || string(stillPinned) != string(pinnedIdentity) {
		t.Fatalf("historical run identity changed: before=%s after=%s error=%v", pinnedIdentity, stillPinned, e)
	}
	ciInput := CreateInput{ScenarioVersionID: scenarioVersionID, AgentVersionIDs: []string{agent1}, RunsPerAgent: 1, EnvironmentMode: "isolated", IdempotencyKey: "github:org/repo:abc123:suite", SourceMetadata: SourceMetadata{Provider: "github", Repository: "org/repo", CommitSHA: "abc1234", PullRequest: "248", Workflow: "verify"}}
	ciFirst, e := svc.Create(ctx, p, one.ID, ciInput)
	if e != nil {
		t.Fatal(e)
	}
	ciAgain, e := svc.Create(ctx, p, one.ID, ciInput)
	if e != nil || ciAgain.ID != ciFirst.ID || ciAgain.SourceMetadata.CommitSHA != "abc1234" {
		t.Fatalf("idempotent CI batch first=%+v again=%+v error=%v", ciFirst, ciAgain, e)
	}
	runs, e := svc.ListRuns(ctx, p, one.ID, twoBatch.ID, Filters{Pagination: identity.Pagination{Limit: 50}})
	if e != nil || len(runs) != 6 {
		t.Fatalf("runs=%d error=%v", len(runs), e)
	}
	before := countBatches(t, ctx, pool)
	_, e = svc.Create(ctx, p, one.ID, CreateInput{ScenarioVersionID: scenarioVersionID, AgentVersionIDs: []string{agent1, "33333333-3333-4333-8333-333333333333"}, RunsPerAgent: 3})
	if !errors.Is(e, identity.ErrNotFound) || countBatches(t, ctx, pool) != before {
		t.Fatalf("rollback error=%v", e)
	}
	for name, input := range map[string]CreateInput{
		"cross-workspace agent":    {ScenarioVersionID: scenarioVersionID, AgentVersionIDs: []string{otherAgent}},
		"cross-workspace scenario": {ScenarioVersionID: otherScenarioVersionID, AgentVersionIDs: []string{agent1}},
		"archived agent":           {ScenarioVersionID: scenarioVersionID, AgentVersionIDs: []string{archivedAgent}},
		"archived scenario":        {ScenarioVersionID: archivedScenarioVersionID, AgentVersionIDs: []string{agent1}},
	} {
		t.Run(name, func(t *testing.T) {
			if _, err := svc.Create(ctx, p, one.ID, input); !errors.Is(err, identity.ErrNotFound) {
				t.Fatalf("error=%v", err)
			}
		})
	}
	publicBatch, e := svc.Create(ctx, p, one.ID, CreateInput{ScenarioVersionID: publicScenarioVersionID, AgentVersionIDs: []string{publicAgent}})
	if e != nil {
		t.Fatalf("public resources: %v", e)
	}
	assertExactVersions(t, ctx, pool, publicBatch.ID, publicScenarioVersionID, []string{publicAgent})
	if _, e = svc.Get(ctx, p, two.ID, twoBatch.ID); !errors.Is(e, identity.ErrNotFound) {
		t.Fatalf("cross-workspace batch=%v", e)
	}
	if _, e = svc.GetRun(ctx, p, two.ID, runs[0].ID); !errors.Is(e, identity.ErrNotFound) {
		t.Fatalf("cross-workspace run=%v", e)
	}
}

func insertScenarioVersion(t *testing.T, ctx context.Context, pool *pgxpool.Pool, workspaceID *string, slug, kind, visibility, status string) string {
	t.Helper()
	var scenarioID, versionID string
	if err := pool.QueryRow(ctx, `INSERT INTO scenarios(owner_workspace_id,slug,name,kind,visibility,status) VALUES($1,$2,$2,$3,$4,$5) RETURNING id`, workspaceID, slug, kind, visibility, status).Scan(&scenarioID); err != nil {
		t.Fatal(err)
	}
	if err := pool.QueryRow(ctx, `INSERT INTO scenario_versions(scenario_id,version,category,difficulty,duration_seconds) VALUES($1,1,'traffic','medium',60) RETURNING id`, scenarioID).Scan(&versionID); err != nil {
		t.Fatal(err)
	}
	return versionID
}

func insertAgentVersion(t *testing.T, ctx context.Context, pool *pgxpool.Pool, workspaceID *string, slug, visibility, status string) string {
	t.Helper()
	var agentID, versionID string
	if err := pool.QueryRow(ctx, `INSERT INTO agents(owner_workspace_id,slug,name,visibility,status) VALUES($1,$2,$2,$3,$4) RETURNING id`, workspaceID, slug, visibility, status).Scan(&agentID); err != nil {
		t.Fatal(err)
	}
	if err := pool.QueryRow(ctx, `INSERT INTO agent_versions(agent_id,version_label,implementation_identity) VALUES($1,'v1',jsonb_build_object('contract_version','agent-implementation-identity:v1','kind','source_revision','identifier',$2 || '-revision','provenance_level','declared')) RETURNING id`, agentID, slug).Scan(&versionID); err != nil {
		t.Fatal(err)
	}
	return versionID
}

func assertExactVersions(t *testing.T, ctx context.Context, pool *pgxpool.Pool, batchID, scenarioVersionID string, wantAgents []string) {
	t.Helper()
	var gotScenario string
	if err := pool.QueryRow(ctx, `SELECT scenario_version_id FROM evaluation_batches WHERE id=$1`, batchID).Scan(&gotScenario); err != nil || gotScenario != scenarioVersionID {
		t.Fatalf("scenario version=%q want=%q error=%v", gotScenario, scenarioVersionID, err)
	}
	rows, err := pool.Query(ctx, `SELECT agent_version_id FROM evaluation_batch_agents WHERE batch_id=$1 ORDER BY agent_version_id`, batchID)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	got := make(map[string]bool, len(wantAgents))
	for rows.Next() {
		var id string
		if err = rows.Scan(&id); err != nil {
			t.Fatal(err)
		}
		got[id] = true
	}
	if err = rows.Err(); err != nil {
		t.Fatal(err)
	}
	for _, id := range wantAgents {
		if !got[id] {
			t.Fatalf("agent version %s was not preserved: got=%v", id, got)
		}
	}
	if len(got) != len(wantAgents) {
		t.Fatalf("agent versions=%v want=%v", got, wantAgents)
	}
}
func countBatches(t *testing.T, ctx context.Context, p *pgxpool.Pool) int {
	t.Helper()
	var n int
	if e := p.QueryRow(ctx, `SELECT count(*) FROM evaluation_batches`).Scan(&n); e != nil {
		t.Fatal(e)
	}
	return n
}
