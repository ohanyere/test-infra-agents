package agentregression

import (
	"context"
	"errors"
	"testing"

	"github.com/evalplane/evalplane/backend/internal/auth"
	"github.com/evalplane/evalplane/backend/internal/evaluationsuite"
	"github.com/evalplane/evalplane/backend/internal/identity"
)

const workspaceID = "55555555-5555-4555-8555-555555555555"

type fakeRoles struct {
	role string
	err  error
}

func (f fakeRoles) GetRole(context.Context, identity.DBTX, string, string) (string, error) {
	return f.role, f.err
}

type fakeSuites struct {
	in  evaluationsuite.StartExecutionInput
	err error
}

func (f *fakeSuites) StartSuiteExecutionWithRecorder(ctx context.Context, _ auth.Principal, _ string, in evaluationsuite.StartExecutionInput, recorder evaluationsuite.ExecutionRecorder) (evaluationsuite.Execution, error) {
	f.in = in
	execution := evaluationsuite.Execution{ID: "66666666-6666-4666-8666-666666666666", SuiteVersionID: in.SuiteVersionID, AgentVersionIDs: in.AgentVersionIDs}
	if f.err == nil && recorder != nil {
		f.err = recorder(ctx, nil, execution)
	}
	return execution, f.err
}

type fakeStore struct {
	created    CreateInput
	comparison Comparison
	trials     []TrialRow
	criteria   []CriterionRow
}

func (f *fakeStore) Create(_ context.Context, _ DBTX, workspace, user, execution string, in CreateInput) (Comparison, error) {
	f.created = in
	f.comparison = Comparison{ID: "77777777-7777-4777-8777-777777777777", WorkspaceID: workspace, SuiteExecutionID: execution, SuiteVersionID: in.SuiteVersionID, BaselineAgentVersionID: in.BaselineAgentVersionID, CandidateAgentVersionID: in.CandidateAgentVersionID}
	return f.comparison, nil
}
func (f *fakeStore) Get(context.Context, DBTX, string, string) (Comparison, error) {
	return f.comparison, nil
}
func (f *fakeStore) ListTrials(context.Context, DBTX, string, string) ([]TrialRow, error) {
	return f.trials, nil
}
func (f *fakeStore) ListCriteria(context.Context, DBTX, string, string) ([]CriterionRow, error) {
	return f.criteria, nil
}

func TestCreatePinsExactVersionsInOneExistingSuiteExecution(t *testing.T) {
	store := &fakeStore{}
	suites := &fakeSuites{}
	service := NewService(nil, store, fakeRoles{role: "member"}, suites)
	in := CreateInput{SuiteVersionID: suiteID, BaselineAgentVersionID: baselineID, CandidateAgentVersionID: candidateID}
	got, err := service.Create(context.Background(), auth.Principal{UserID: workspaceID}, workspaceID, in)
	if err != nil {
		t.Fatal(err)
	}
	if suites.in.SuiteVersionID != suiteID || len(suites.in.AgentVersionIDs) != 2 || suites.in.AgentVersionIDs[0] != baselineID || suites.in.AgentVersionIDs[1] != candidateID || got.BaselineAgentVersionID != baselineID {
		t.Fatalf("versions not pinned: %#v %#v", suites.in, got)
	}
}
func TestCreateRejectsSameVersionAndViewer(t *testing.T) {
	service := NewService(nil, &fakeStore{}, fakeRoles{role: "member"}, &fakeSuites{})
	_, err := service.Create(context.Background(), auth.Principal{UserID: workspaceID}, workspaceID, CreateInput{SuiteVersionID: suiteID, BaselineAgentVersionID: baselineID, CandidateAgentVersionID: baselineID})
	if !errors.Is(err, identity.ErrValidation) {
		t.Fatalf("err=%v", err)
	}
	service = NewService(nil, &fakeStore{}, fakeRoles{role: "viewer"}, &fakeSuites{})
	_, err = service.Create(context.Background(), auth.Principal{UserID: workspaceID}, workspaceID, CreateInput{SuiteVersionID: suiteID, BaselineAgentVersionID: baselineID, CandidateAgentVersionID: candidateID})
	if !errors.Is(err, auth.ErrForbidden) {
		t.Fatalf("err=%v", err)
	}
}
func TestWorkspaceIsolationAppliesToReads(t *testing.T) {
	service := NewService(nil, &fakeStore{}, fakeRoles{err: identity.ErrNotFound}, &fakeSuites{})
	_, err := service.Get(context.Background(), auth.Principal{UserID: workspaceID}, workspaceID, "77777777-7777-4777-8777-777777777777")
	if !errors.Is(err, auth.ErrForbidden) {
		t.Fatalf("err=%v", err)
	}
}
