package evidence

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"sync"
	"testing"
	"time"

	"github.com/evalplane/evalplane/backend/internal/auth"
	"github.com/evalplane/evalplane/backend/internal/identity"
	"github.com/jackc/pgx/v5/pgxpool"
)

func TestEvidenceIntegration(t *testing.T) {
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
	workspaceOne, workspaceTwo, principal := evidenceWorkspaces(t, ctx, pool)
	scenarioOne, criterionOne := evidenceScenario(t, ctx, pool, workspaceOne, "scenario-one")
	scenarioTwo, criterionTwo := evidenceScenario(t, ctx, pool, workspaceOne, "scenario-two")
	agentVersionID := evidenceAgent(t, ctx, pool, workspaceOne)
	runOne := evidenceRun(t, ctx, pool, workspaceOne, scenarioOne, agentVersionID, "completed")
	runTwo := evidenceRun(t, ctx, pool, workspaceOne, scenarioTwo, agentVersionID, "completed")
	runThree := evidenceRun(t, ctx, pool, workspaceOne, scenarioOne, agentVersionID, "completed")
	runFour := evidenceRun(t, ctx, pool, workspaceOne, scenarioOne, agentVersionID, "completed")
	service := NewService(pool, NewRepository(), auth.NewRepository())

	now := time.Now().UTC()
	first, err := service.AppendRunEvent(ctx, AppendRunEventInput{RunID: runOne, EventType: "observation", OccurredAt: now, Summary: "First", Payload: json.RawMessage(`{}`)})
	if err != nil {
		t.Fatal(err)
	}
	second, err := service.AppendRunEvent(ctx, AppendRunEventInput{RunID: runOne, EventType: "decision", OccurredAt: now.Add(time.Second), Summary: "Second", Payload: json.RawMessage(`{}`)})
	if err != nil || first.Sequence != 1 || second.Sequence != 2 {
		t.Fatalf("event sequences=%d,%d error=%v", first.Sequence, second.Sequence, err)
	}
	events, err := service.ListRunEvents(ctx, principal, workspaceOne, runOne, EventFilters{Pagination: identity.Pagination{Limit: 50}})
	if err != nil || len(events) != 2 || events[0].Sequence != 1 || events[1].Sequence != 2 {
		t.Fatalf("events=%+v error=%v", events, err)
	}

	otherEvent, err := service.AppendRunEvent(ctx, AppendRunEventInput{RunID: runTwo, EventType: "system", OccurredAt: now, Summary: "Other", Payload: json.RawMessage(`{}`)})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = service.RecordAgentAction(ctx, RecordAgentActionInput{RunID: runOne, RunEventID: &first.ID, ActionType: "restart", Status: "executed", Authorized: true}); err != nil {
		t.Fatalf("same-run action: %v", err)
	}
	if _, err = service.RecordAgentAction(ctx, RecordAgentActionInput{RunID: runOne, RunEventID: &otherEvent.ID, ActionType: "restart", Status: "denied"}); !errors.Is(err, identity.ErrValidation) {
		t.Fatalf("cross-run event error=%v", err)
	}

	evaluationInput := CreateEvaluationInput{
		RunID:       runOne,
		Result:      "pass",
		Summary:     "Recovered",
		CompletedAt: &now,
		Criteria:    []CriterionResultInput{{ScenarioCriterionID: criterionOne, Passed: true, ObservedValue: json.RawMessage(`42`), Evidence: json.RawMessage(`{"source":"event"}`)}},
		Findings:    []FindingInput{{Kind: "strength", Summary: "Recovered safely", Evidence: json.RawMessage(`{}`), SortOrder: 1}},
	}
	evaluation, err := service.CreateEvaluation(ctx, evaluationInput)
	if err != nil || len(evaluation.Criteria) != 1 || len(evaluation.Findings) != 1 {
		t.Fatalf("evaluation=%+v error=%v", evaluation, err)
	}
	stored, err := service.GetEvaluation(ctx, principal, workspaceOne, runOne)
	if err != nil || len(stored.Criteria) != 1 || len(stored.Findings) != 1 || stored.Criteria[0].ScenarioCriterionID != criterionOne {
		t.Fatalf("stored=%+v error=%v", stored, err)
	}
	if _, err = service.CreateEvaluation(ctx, evaluationInput); !errors.Is(err, identity.ErrConflict) {
		t.Fatalf("duplicate evaluation error=%v", err)
	}

	wrongCriterion := evaluationInput
	wrongCriterion.RunID = runThree
	wrongCriterion.Criteria = []CriterionResultInput{{ScenarioCriterionID: criterionTwo, ObservedValue: json.RawMessage(`null`)}}
	if _, err = service.CreateEvaluation(ctx, wrongCriterion); !errors.Is(err, identity.ErrValidation) {
		t.Fatalf("wrong criterion error=%v", err)
	}
	missingCriterion := evaluationInput
	missingCriterion.RunID = runFour
	missingCriterion.Criteria = nil
	if _, err = service.CreateEvaluation(ctx, missingCriterion); !errors.Is(err, identity.ErrValidation) {
		t.Fatalf("missing criterion error=%v", err)
	}
	assertNoEvaluation(t, ctx, pool, runThree)
	assertNoEvaluation(t, ctx, pool, runFour)

	if _, err = service.ListRunEvents(ctx, principal, workspaceTwo, runOne, EventFilters{Pagination: identity.Pagination{Limit: 50}}); !errors.Is(err, identity.ErrNotFound) {
		t.Fatalf("workspace isolation error=%v", err)
	}

	sequences := make(chan int64, 2)
	errorsOut := make(chan error, 2)
	var wait sync.WaitGroup
	for i := 0; i < 2; i++ {
		wait.Add(1)
		go func() {
			defer wait.Done()
			event, appendErr := service.AppendRunEvent(ctx, AppendRunEventInput{RunID: runTwo, EventType: "system", OccurredAt: now, Summary: "Concurrent", Payload: json.RawMessage(`{}`)})
			if appendErr != nil {
				errorsOut <- appendErr
				return
			}
			sequences <- event.Sequence
		}()
	}
	wait.Wait()
	close(errorsOut)
	close(sequences)
	for appendErr := range errorsOut {
		t.Fatal(appendErr)
	}
	seen := map[int64]bool{}
	for sequence := range sequences {
		seen[sequence] = true
	}
	if !seen[2] || !seen[3] || len(seen) != 2 {
		t.Fatalf("concurrent sequences=%v", seen)
	}
}

func evidenceWorkspaces(t *testing.T, ctx context.Context, pool *pgxpool.Pool) (string, string, auth.Principal) {
	t.Helper()
	repo := identity.NewRepository()
	organization, err := repo.CreateOrganization(ctx, pool, identity.OrganizationInput{Name: "Evidence", Slug: "evidence-test"})
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
	user, err := repo.CreateUser(ctx, pool, identity.UserInput{Email: "evidence@example.com", FullName: "Evidence Test"})
	if err != nil {
		t.Fatal(err)
	}
	for _, workspaceID := range []string{one.ID, two.ID} {
		if _, err = repo.CreateMembership(ctx, pool, workspaceID, user.ID, "viewer"); err != nil {
			t.Fatal(err)
		}
	}
	return one.ID, two.ID, auth.Principal{UserID: user.ID}
}

func evidenceScenario(t *testing.T, ctx context.Context, pool *pgxpool.Pool, workspaceID, slug string) (string, string) {
	t.Helper()
	var scenarioID, versionID, criterionID string
	if err := pool.QueryRow(ctx, `INSERT INTO scenarios(owner_workspace_id,slug,name,kind,visibility,status) VALUES($1,$2,$2,'custom','workspace','active') RETURNING id`, workspaceID, slug).Scan(&scenarioID); err != nil {
		t.Fatal(err)
	}
	if err := pool.QueryRow(ctx, `INSERT INTO scenario_versions(scenario_id,version,category,difficulty,duration_seconds) VALUES($1,1,'traffic','medium',60) RETURNING id`, scenarioID).Scan(&versionID); err != nil {
		t.Fatal(err)
	}
	if err := pool.QueryRow(ctx, `INSERT INTO scenario_criteria(scenario_version_id,key,name,metric_key,operator,target_value) VALUES($1,'availability','Availability','availability_pct','gte','99') RETURNING id`, versionID).Scan(&criterionID); err != nil {
		t.Fatal(err)
	}
	return versionID, criterionID
}

func evidenceAgent(t *testing.T, ctx context.Context, pool *pgxpool.Pool, workspaceID string) string {
	t.Helper()
	var agentID, versionID string
	if err := pool.QueryRow(ctx, `INSERT INTO agents(owner_workspace_id,slug,name,visibility,status) VALUES($1,'evidence-agent','Evidence Agent','workspace','active') RETURNING id`, workspaceID).Scan(&agentID); err != nil {
		t.Fatal(err)
	}
	if err := pool.QueryRow(ctx, `INSERT INTO agent_versions(agent_id,version_label) VALUES($1,'v1') RETURNING id`, agentID).Scan(&versionID); err != nil {
		t.Fatal(err)
	}
	return versionID
}

func evidenceRun(t *testing.T, ctx context.Context, pool *pgxpool.Pool, workspaceID, scenarioVersionID, agentVersionID, status string) string {
	t.Helper()
	var batchID, runID string
	if err := pool.QueryRow(ctx, `INSERT INTO evaluation_batches(workspace_id,scenario_version_id,status) VALUES($1,$2,'draft') RETURNING id`, workspaceID, scenarioVersionID).Scan(&batchID); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `INSERT INTO evaluation_batch_agents(batch_id,agent_version_id) VALUES($1,$2)`, batchID, agentVersionID); err != nil {
		t.Fatal(err)
	}
	if err := pool.QueryRow(ctx, `INSERT INTO runs(batch_id,agent_version_id,attempt_number,status) VALUES($1,$2,1,$3) RETURNING id`, batchID, agentVersionID, status).Scan(&runID); err != nil {
		t.Fatal(err)
	}
	return runID
}

func assertNoEvaluation(t *testing.T, ctx context.Context, pool *pgxpool.Pool, runID string) {
	t.Helper()
	var count int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM evaluations WHERE run_id=$1`, runID).Scan(&count); err != nil || count != 0 {
		t.Fatalf("evaluation count=%d error=%v", count, err)
	}
}
