package evaluationsuite

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"reflect"
	"testing"

	"github.com/evalplane/evalplane/backend/internal/auth"
	"github.com/evalplane/evalplane/backend/internal/evaluationbatch"
	"github.com/evalplane/evalplane/backend/internal/identity"
	"github.com/jackc/pgx/v5/pgxpool"
)

func TestEvaluationSuiteRepositoryIntegration(t *testing.T) {
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
	if _, err = pool.Exec(ctx, `TRUNCATE organizations,users RESTART IDENTITY CASCADE`); err != nil {
		t.Fatal(err)
	}

	identities := identity.NewRepository()
	organization, err := identities.CreateOrganization(ctx, pool, identity.OrganizationInput{Name: "Suite Test", Slug: "suite-test"})
	if err != nil {
		t.Fatal(err)
	}
	workspace, err := identities.CreateWorkspace(ctx, pool, organization.ID, identity.WorkspaceInput{Name: "One", Slug: "one"})
	if err != nil {
		t.Fatal(err)
	}
	otherWorkspace, err := identities.CreateWorkspace(ctx, pool, organization.ID, identity.WorkspaceInput{Name: "Two", Slug: "two"})
	if err != nil {
		t.Fatal(err)
	}
	user, err := identities.CreateUser(ctx, pool, identity.UserInput{Email: "suite@example.com", FullName: "Suite User"})
	if err != nil {
		t.Fatal(err)
	}
	viewer, err := identities.CreateUser(ctx, pool, identity.UserInput{Email: "viewer@example.com", FullName: "Viewer"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = identities.CreateMembership(ctx, pool, workspace.ID, user.ID, "member"); err != nil {
		t.Fatal(err)
	}
	if _, err = identities.CreateMembership(ctx, pool, otherWorkspace.ID, user.ID, "member"); err != nil {
		t.Fatal(err)
	}
	if _, err = identities.CreateMembership(ctx, pool, workspace.ID, viewer.ID, "viewer"); err != nil {
		t.Fatal(err)
	}

	scenarioA := insertSuiteScenarioVersion(t, ctx, pool, workspace.ID, "suite-scenario-a")
	scenarioB := insertSuiteScenarioVersion(t, ctx, pool, workspace.ID, "suite-scenario-b")
	otherScenario := insertSuiteScenarioVersion(t, ctx, pool, otherWorkspace.ID, "suite-other-scenario")
	agentA, agentAID := insertSuiteAgentVersion(t, ctx, pool, workspace.ID, "suite-agent-a", "v1")
	agentB, _ := insertSuiteAgentVersion(t, ctx, pool, workspace.ID, "suite-agent-b", "v1")
	otherAgent, _ := insertSuiteAgentVersion(t, ctx, pool, otherWorkspace.ID, "suite-other-agent", "v1")

	service := NewService(pool, NewRepository(), auth.NewRepository(), evaluationbatch.NewRepository())
	principal := auth.Principal{UserID: user.ID}
	suite, err := service.CreateSuite(ctx, principal, workspace.ID, CreateSuiteInput{Name: "Core qualification"})
	if err != nil || suite.WorkspaceID != workspace.ID {
		t.Fatalf("create suite: value=%+v error=%v", suite, err)
	}
	if _, err = service.CreateSuite(ctx, auth.Principal{UserID: viewer.ID}, workspace.ID, CreateSuiteInput{Name: "Denied"}); !errors.Is(err, auth.ErrForbidden) {
		t.Fatalf("viewer create error=%v", err)
	}

	variantTraffic, variantReplicas := 75, 2
	versionOne, err := service.CreateSuiteVersion(ctx, principal, workspace.ID, suite.ID, CreateVersionInput{Description: "first", Metadata: json.RawMessage(`{"owner":"platform"}`), Scenarios: []ScenarioEntry{{ScenarioVersionID: scenarioA, TrialCount: 2, HiddenVariants: []HiddenVariantConfig{{TrafficVUs: &variantTraffic, InitialReplicas: &variantReplicas}}}, {ScenarioVersionID: scenarioB, TrialCount: 1}}})
	if err != nil {
		t.Fatal(err)
	}
	if versionOne.Version != 1 || versionOne.Scenarios[0].ScenarioVersionID != scenarioA || versionOne.Scenarios[1].ScenarioVersionID != scenarioB {
		t.Fatalf("exact scenario versions were not pinned: %+v", versionOne)
	}
	versionTwo, err := service.CreateSuiteVersion(ctx, principal, workspace.ID, suite.ID, CreateVersionInput{Description: "changed", Scenarios: []ScenarioEntry{{ScenarioVersionID: scenarioB, TrialCount: 4}}})
	if err != nil || versionTwo.Version != 2 {
		t.Fatalf("create next immutable version: value=%+v error=%v", versionTwo, err)
	}
	unchanged, err := service.GetSuiteVersion(ctx, principal, workspace.ID, versionOne.ID)
	if err != nil || unchanged.Description != "first" || len(unchanged.Scenarios) != 2 || unchanged.Scenarios[0].TrialCount != 2 {
		t.Fatalf("historical version changed: value=%+v error=%v", unchanged, err)
	}
	if _, err = pool.Exec(ctx, `UPDATE evaluation_suite_versions SET description='mutated' WHERE id=$1`, versionOne.ID); err == nil {
		t.Fatal("database allowed an immutable suite version to be updated")
	}
	if _, err = pool.Exec(ctx, `UPDATE evaluation_suite_version_scenarios SET trial_count=9 WHERE suite_version_id=$1`, versionOne.ID); err == nil {
		t.Fatal("database allowed immutable suite membership to be updated")
	}
	for name, input := range map[string]CreateVersionInput{
		"duplicate":       {Scenarios: []ScenarioEntry{{ScenarioVersionID: scenarioA, TrialCount: 1}, {ScenarioVersionID: scenarioA, TrialCount: 1}}},
		"cross workspace": {Scenarios: []ScenarioEntry{{ScenarioVersionID: otherScenario, TrialCount: 1}}},
	} {
		t.Run(name, func(t *testing.T) {
			if _, createErr := service.CreateSuiteVersion(ctx, principal, workspace.ID, suite.ID, input); createErr == nil {
				t.Fatal("expected version creation to fail")
			}
		})
	}

	// Newer resource versions must not affect execution of the pinned IDs.
	_ = insertNewerSuiteScenarioVersion(t, ctx, pool, scenarioA)
	_ = insertAdditionalSuiteAgentVersion(t, ctx, pool, agentAID)
	beforeExecutions := countSuiteRows(t, ctx, pool, "suite_executions")
	if _, err = service.StartSuiteExecution(ctx, principal, workspace.ID, StartExecutionInput{SuiteVersionID: versionOne.ID, AgentVersionIDs: []string{agentA, otherAgent}}); !errors.Is(err, identity.ErrNotFound) {
		t.Fatalf("cross-workspace agent error=%v", err)
	}
	if countSuiteRows(t, ctx, pool, "suite_executions") != beforeExecutions {
		t.Fatal("failed execution creation left partial suite rows")
	}
	execution, err := service.StartSuiteExecution(ctx, principal, workspace.ID, StartExecutionInput{SuiteVersionID: versionOne.ID, AgentVersionIDs: []string{agentA, agentB}})
	if err != nil {
		t.Fatal(err)
	}
	if execution.SuiteVersionID != versionOne.ID || len(execution.BatchIDs) != 2 {
		t.Fatalf("execution did not pin exact suite version/create batches: %+v", execution)
	}
	var runs int
	if err = pool.QueryRow(ctx, `SELECT count(*) FROM runs r JOIN suite_execution_batches seb ON seb.batch_id=r.batch_id WHERE seb.suite_execution_id=$1`, execution.ID).Scan(&runs); err != nil || runs != 6 {
		t.Fatalf("runs=%d want=6 error=%v", runs, err)
	}
	var assignments int
	if err = pool.QueryRow(ctx, `SELECT count(*) FROM suite_execution_run_variants WHERE suite_execution_id=$1`, execution.ID).Scan(&assignments); err != nil || assignments != 6 {
		t.Fatalf("variant assignments=%d want=6 error=%v", assignments, err)
	}
	if _, err = pool.Exec(ctx, `UPDATE suite_execution_run_variants SET variant_seed=variant_seed+1 WHERE suite_execution_id=$1`, execution.ID); err == nil {
		t.Fatal("database allowed a historical variant assignment to change")
	}
	var unequalDistributions int
	if err = pool.QueryRow(ctx, `SELECT count(*) FROM (SELECT scenario_version_id,trial_ordinal,count(DISTINCT variant_id) variants,count(DISTINCT variant_seed) seeds FROM suite_execution_run_variants WHERE suite_execution_id=$1 GROUP BY scenario_version_id,trial_ordinal HAVING count(DISTINCT variant_id)<>1 OR count(DISTINCT variant_seed)<>1) unequal`, execution.ID).Scan(&unequalDistributions); err != nil || unequalDistributions != 0 {
		t.Fatalf("agents received unequal variant distributions: groups=%d error=%v", unequalDistributions, err)
	}
	var wrongVersions int
	if err = pool.QueryRow(ctx, `SELECT count(*) FROM suite_execution_batches WHERE suite_execution_id=$1 AND scenario_version_id NOT IN ($2,$3)`, execution.ID, scenarioA, scenarioB).Scan(&wrongVersions); err != nil || wrongVersions != 0 {
		t.Fatalf("wrong scenario versions=%d error=%v", wrongVersions, err)
	}

	rows, err := pool.Query(ctx, `SELECT r.id FROM runs r JOIN suite_execution_batches seb ON seb.batch_id=r.batch_id WHERE seb.suite_execution_id=$1 ORDER BY r.id`, execution.ID)
	if err != nil {
		t.Fatal(err)
	}
	var runIDs []string
	for rows.Next() {
		var id string
		if err = rows.Scan(&id); err != nil {
			t.Fatal(err)
		}
		runIDs = append(runIDs, id)
	}
	rows.Close()
	for index, result := range []string{"pass", "fail", "inconclusive"} {
		if _, err = pool.Exec(ctx, `UPDATE runs SET status='completed',completed_at=now() WHERE id=$1`, runIDs[index]); err != nil {
			t.Fatal(err)
		}
		if _, err = pool.Exec(ctx, `INSERT INTO evaluations(run_id,result) VALUES($1,$2)`, runIDs[index], result); err != nil {
			t.Fatal(err)
		}
	}
	if _, err = pool.Exec(ctx, `UPDATE runs SET status='running',started_at=now() WHERE id=$1`, runIDs[3]); err != nil {
		t.Fatal(err)
	}
	results, err := service.GetSuiteResults(ctx, principal, workspace.ID, execution.ID)
	if err != nil {
		t.Fatal(err)
	}
	passRate, safetyRate := 50.0, 0.0
	want := Counts{Total: 6, Scheduled: 6, Completed: 3, Valid: 3, Passed: 1, Failed: 1, Inconclusive: 1, Pending: 2, Running: 1, PassRate: &passRate, SafetyViolationRate: &safetyRate}
	if !reflect.DeepEqual(results.Overall, want) {
		t.Fatalf("aggregate=%+v want=%+v", results.Overall, want)
	}
	if _, err = service.GetSuite(ctx, principal, otherWorkspace.ID, suite.ID); !errors.Is(err, identity.ErrNotFound) {
		t.Fatalf("workspace isolation error=%v", err)
	}
}

func insertSuiteScenarioVersion(t *testing.T, ctx context.Context, pool *pgxpool.Pool, workspaceID, slug string) string {
	t.Helper()
	var scenarioID string
	if err := pool.QueryRow(ctx, `INSERT INTO scenarios(owner_workspace_id,slug,name,kind,visibility,status) VALUES($1,$2,$2,'custom','workspace','active') RETURNING id`, workspaceID, slug).Scan(&scenarioID); err != nil {
		t.Fatal(err)
	}
	return insertAdditionalSuiteScenarioVersion(t, ctx, pool, scenarioID)
}

func insertNewerSuiteScenarioVersion(t *testing.T, ctx context.Context, pool *pgxpool.Pool, versionID string) string {
	t.Helper()
	var scenarioID string
	if err := pool.QueryRow(ctx, `SELECT scenario_id FROM scenario_versions WHERE id=$1`, versionID).Scan(&scenarioID); err != nil {
		t.Fatal(err)
	}
	return insertAdditionalSuiteScenarioVersion(t, ctx, pool, scenarioID)
}

func insertAdditionalSuiteScenarioVersion(t *testing.T, ctx context.Context, pool *pgxpool.Pool, scenarioID string) string {
	t.Helper()
	var id string
	if err := pool.QueryRow(ctx, `INSERT INTO scenario_versions(scenario_id,version,category,difficulty,duration_seconds,execution_spec) SELECT $1,COALESCE(MAX(version),0)+1,'traffic','medium',60,'{"type":"traffic_spike"}'::jsonb FROM scenario_versions WHERE scenario_id=$1 RETURNING id`, scenarioID).Scan(&id); err != nil {
		t.Fatal(err)
	}
	return id
}

func insertSuiteAgentVersion(t *testing.T, ctx context.Context, pool *pgxpool.Pool, workspaceID, slug, label string) (string, string) {
	t.Helper()
	var agentID string
	if err := pool.QueryRow(ctx, `INSERT INTO agents(owner_workspace_id,slug,name,visibility,status) VALUES($1,$2,$2,'workspace','active') RETURNING id`, workspaceID, slug).Scan(&agentID); err != nil {
		t.Fatal(err)
	}
	return insertAdditionalSuiteAgentVersion(t, ctx, pool, agentID), agentID
}

func insertAdditionalSuiteAgentVersion(t *testing.T, ctx context.Context, pool *pgxpool.Pool, agentID string) string {
	t.Helper()
	var id string
	if err := pool.QueryRow(ctx, `INSERT INTO agent_versions(agent_id,version_label) SELECT $1,'v'||(count(*)+1) FROM agent_versions WHERE agent_id=$1 RETURNING id`, agentID).Scan(&id); err != nil {
		t.Fatal(err)
	}
	return id
}

func countSuiteRows(t *testing.T, ctx context.Context, pool *pgxpool.Pool, table string) int {
	t.Helper()
	if table != "suite_executions" {
		t.Fatal("unsupported test table")
	}
	var count int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM suite_executions`).Scan(&count); err != nil {
		t.Fatal(err)
	}
	return count
}
