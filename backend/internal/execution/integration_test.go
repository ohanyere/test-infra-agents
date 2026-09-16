package execution

import (
	"context"
	"errors"
	"os"
	"sync"
	"testing"

	"github.com/evalplane/evalplane/backend/internal/auth"
	"github.com/evalplane/evalplane/backend/internal/identity"
	"github.com/jackc/pgx/v5/pgxpool"
)

func TestExecutionIntegration(t *testing.T) {
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
	if _, err = pool.Exec(ctx, `TRUNCATE organizations,users RESTART IDENTITY CASCADE`); err != nil {
		t.Fatal(err)
	}
	workspaceOne, workspaceTwo, principal := executionWorkspaces(t, ctx, pool)
	scenarioID, scenarioVersionID := executionScenario(t, ctx, pool, workspaceOne)
	agentID, agentVersionID := executionAgent(t, ctx, pool, workspaceOne)
	service := NewService(pool, NewRepository(), auth.NewRepository())

	completedBatch, _ := executionBatch(t, ctx, pool, workspaceOne, scenarioVersionID, agentVersionID, 1)
	queued, err := service.StartBatch(ctx, principal, workspaceOne, completedBatch)
	if err != nil || queued.Status != "queued" || queued.StartedAt != nil {
		t.Fatalf("start=%+v error=%v", queued, err)
	}
	if _, err = service.StartBatch(ctx, principal, workspaceOne, completedBatch); !errors.Is(err, identity.ErrConflict) {
		t.Fatalf("second start error=%v", err)
	}
	if _, err = service.StartBatch(ctx, principal, workspaceTwo, completedBatch); !errors.Is(err, identity.ErrNotFound) {
		t.Fatalf("workspace isolation error=%v", err)
	}
	claimed, err := service.ClaimNextRun(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if claimed.BatchID != completedBatch || claimed.WorkspaceID != workspaceOne || claimed.Scenario.ID != scenarioID || claimed.Scenario.VersionID != scenarioVersionID || claimed.Scenario.Version != 1 || claimed.Agent.ID != agentID || claimed.Agent.VersionID != agentVersionID || claimed.Agent.VersionLabel != "v1" || claimed.Agent.InterfaceKind != "http" || claimed.AttemptNumber != 1 {
		t.Fatalf("claim=%+v", claimed)
	}
	assertRunState(t, ctx, pool, claimed.RunID, "provisioning", true, false)
	assertBatchState(t, ctx, pool, completedBatch, "running", true, false)
	if err = service.MarkRunRunning(ctx, claimed.RunID, "opaque://environment/one"); err != nil {
		t.Fatal(err)
	}
	if err = service.CompleteRun(ctx, claimed.RunID); err != nil {
		t.Fatal(err)
	}
	assertBatchState(t, ctx, pool, completedBatch, "completed", true, true)

	failedBatch, _ := executionBatch(t, ctx, pool, workspaceOne, scenarioVersionID, agentVersionID, 2)
	mustStart(t, ctx, service, principal, workspaceOne, failedBatch)
	failedRun, err := service.ClaimNextRun(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if err = service.FailRun(ctx, failedRun.RunID); err != nil {
		t.Fatal(err)
	}
	assertBatchState(t, ctx, pool, failedBatch, "running", true, false)
	remainingRun, err := service.ClaimNextRun(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if err = service.MarkRunRunning(ctx, remainingRun.RunID, "opaque://environment/two"); err != nil {
		t.Fatal(err)
	}
	if err = service.CompleteRun(ctx, remainingRun.RunID); err != nil {
		t.Fatal(err)
	}
	assertBatchState(t, ctx, pool, failedBatch, "failed", true, true)

	stoppedBatch, _ := executionBatch(t, ctx, pool, workspaceOne, scenarioVersionID, agentVersionID, 2)
	mustStart(t, ctx, service, principal, workspaceOne, stoppedBatch)
	for i := 0; i < 2; i++ {
		run, claimErr := service.ClaimNextRun(ctx)
		if claimErr != nil {
			t.Fatal(claimErr)
		}
		if claimErr = service.StopRun(ctx, run.RunID); claimErr != nil {
			t.Fatal(claimErr)
		}
	}
	assertBatchState(t, ctx, pool, stoppedBatch, "stopped", true, true)

	for _, initialStatus := range []string{"draft", "queued"} {
		batchID, runIDs := executionBatch(t, ctx, pool, workspaceOne, scenarioVersionID, agentVersionID, 1)
		if initialStatus == "queued" {
			mustStart(t, ctx, service, principal, workspaceOne, batchID)
		}
		state, stopErr := service.StopBatch(ctx, principal, workspaceOne, batchID)
		if stopErr != nil || state.Status != "stopped" {
			t.Fatalf("stop from %s: state=%+v error=%v", initialStatus, state, stopErr)
		}
		assertRunState(t, ctx, pool, runIDs[0], "stopped", false, true)
	}

	runningBatch, _ := executionBatch(t, ctx, pool, workspaceOne, scenarioVersionID, agentVersionID, 1)
	mustStart(t, ctx, service, principal, workspaceOne, runningBatch)
	if _, err = service.ClaimNextRun(ctx); err != nil {
		t.Fatal(err)
	}
	if _, err = service.StopBatch(ctx, principal, workspaceOne, runningBatch); err != nil {
		t.Fatalf("stop running error=%v", err)
	}

	concurrentBatch, _ := executionBatch(t, ctx, pool, workspaceOne, scenarioVersionID, agentVersionID, 1)
	mustStart(t, ctx, service, principal, workspaceOne, concurrentBatch)
	var wait sync.WaitGroup
	results := make(chan error, 2)
	for i := 0; i < 2; i++ {
		wait.Add(1)
		go func() {
			defer wait.Done()
			_, claimErr := service.ClaimNextRun(ctx)
			results <- claimErr
		}()
	}
	wait.Wait()
	close(results)
	claimedCount, noWorkCount := 0, 0
	for claimErr := range results {
		switch {
		case claimErr == nil:
			claimedCount++
		case errors.Is(claimErr, ErrNoRunAvailable):
			noWorkCount++
		default:
			t.Fatalf("concurrent claim error=%v", claimErr)
		}
	}
	if claimedCount != 1 || noWorkCount != 1 {
		t.Fatalf("claims=%d no-work=%d", claimedCount, noWorkCount)
	}
	if _, err = service.ClaimNextRun(ctx); !errors.Is(err, ErrNoRunAvailable) {
		t.Fatalf("no-work error=%v", err)
	}
}

func executionWorkspaces(t *testing.T, ctx context.Context, pool *pgxpool.Pool) (string, string, auth.Principal) {
	t.Helper()
	repo := identity.NewRepository()
	organization, err := repo.CreateOrganization(ctx, pool, identity.OrganizationInput{Name: "Execution", Slug: "execution-test"})
	if err != nil {
		t.Fatal(err)
	}
	one, err := repo.CreateWorkspace(ctx, pool, organization.ID, identity.WorkspaceInput{Name: "One", Slug: "one"})
	if err != nil {
		t.Fatal(err)
	}
	two, err := repo.CreateWorkspace(ctx, pool, organization.ID, identity.WorkspaceInput{Name: "Two", Slug: "two"})
	if err != nil {
		t.Fatal(err)
	}
	user, err := repo.CreateUser(ctx, pool, identity.UserInput{Email: "execution@example.com", FullName: "Execution Test"})
	if err != nil {
		t.Fatal(err)
	}
	for _, workspaceID := range []string{one.ID, two.ID} {
		if _, err = repo.CreateMembership(ctx, pool, workspaceID, user.ID, "member"); err != nil {
			t.Fatal(err)
		}
	}
	return one.ID, two.ID, auth.Principal{UserID: user.ID}
}

func executionScenario(t *testing.T, ctx context.Context, pool *pgxpool.Pool, workspaceID string) (string, string) {
	t.Helper()
	var scenarioID, versionID string
	if err := pool.QueryRow(ctx, `INSERT INTO scenarios(owner_workspace_id,slug,name,kind,visibility,status) VALUES($1,'execution','Execution','custom','workspace','active') RETURNING id`, workspaceID).Scan(&scenarioID); err != nil {
		t.Fatal(err)
	}
	if err := pool.QueryRow(ctx, `INSERT INTO scenario_versions(scenario_id,version,category,difficulty,duration_seconds) VALUES($1,1,'traffic','medium',60) RETURNING id`, scenarioID).Scan(&versionID); err != nil {
		t.Fatal(err)
	}
	return scenarioID, versionID
}

func executionAgent(t *testing.T, ctx context.Context, pool *pgxpool.Pool, workspaceID string) (string, string) {
	t.Helper()
	var agentID, versionID string
	if err := pool.QueryRow(ctx, `INSERT INTO agents(owner_workspace_id,slug,name,visibility,status) VALUES($1,'execution-agent','Execution Agent','workspace','active') RETURNING id`, workspaceID).Scan(&agentID); err != nil {
		t.Fatal(err)
	}
	if err := pool.QueryRow(ctx, `INSERT INTO agent_versions(agent_id,version_label,interface_kind,capabilities,metadata) VALUES($1,'v1','http',ARRAY['execute'],'{"mode":"test"}') RETURNING id`, agentID).Scan(&versionID); err != nil {
		t.Fatal(err)
	}
	return agentID, versionID
}

func executionBatch(t *testing.T, ctx context.Context, pool *pgxpool.Pool, workspaceID, scenarioVersionID, agentVersionID string, runCount int) (string, []string) {
	t.Helper()
	var batchID string
	if err := pool.QueryRow(ctx, `INSERT INTO evaluation_batches(workspace_id,scenario_version_id,status,environment_mode,config) VALUES($1,$2,'draft','isolated','{"checkpoint":true}') RETURNING id`, workspaceID, scenarioVersionID).Scan(&batchID); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `INSERT INTO evaluation_batch_agents(batch_id,agent_version_id) VALUES($1,$2)`, batchID, agentVersionID); err != nil {
		t.Fatal(err)
	}
	runIDs := make([]string, 0, runCount)
	for attempt := 1; attempt <= runCount; attempt++ {
		var runID string
		if err := pool.QueryRow(ctx, `INSERT INTO runs(batch_id,agent_version_id,attempt_number,status) VALUES($1,$2,$3,'queued') RETURNING id`, batchID, agentVersionID, attempt).Scan(&runID); err != nil {
			t.Fatal(err)
		}
		runIDs = append(runIDs, runID)
	}
	return batchID, runIDs
}

func mustStart(t *testing.T, ctx context.Context, service *Service, principal auth.Principal, workspaceID, batchID string) {
	t.Helper()
	if _, err := service.StartBatch(ctx, principal, workspaceID, batchID); err != nil {
		t.Fatal(err)
	}
}

func assertRunState(t *testing.T, ctx context.Context, pool *pgxpool.Pool, runID, wantStatus string, wantStarted, wantCompleted bool) {
	t.Helper()
	var status string
	var started, completed any
	if err := pool.QueryRow(ctx, `SELECT status,started_at,completed_at FROM runs WHERE id=$1`, runID).Scan(&status, &started, &completed); err != nil {
		t.Fatal(err)
	}
	if status != wantStatus || (started != nil) != wantStarted || (completed != nil) != wantCompleted {
		t.Fatalf("run status=%s started=%v completed=%v", status, started, completed)
	}
}

func assertBatchState(t *testing.T, ctx context.Context, pool *pgxpool.Pool, batchID, wantStatus string, wantStarted, wantCompleted bool) {
	t.Helper()
	var status string
	var started, completed any
	if err := pool.QueryRow(ctx, `SELECT status,started_at,completed_at FROM evaluation_batches WHERE id=$1`, batchID).Scan(&status, &started, &completed); err != nil {
		t.Fatal(err)
	}
	if status != wantStatus || (started != nil) != wantStarted || (completed != nil) != wantCompleted {
		t.Fatalf("batch status=%s started=%v completed=%v", status, started, completed)
	}
}
