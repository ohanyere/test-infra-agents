package qualification

import (
	"context"
	"errors"
	"fmt"
	"testing"

	"github.com/evalplane/evalplane/backend/internal/agentregression"
	"github.com/evalplane/evalplane/backend/internal/auth"
	"github.com/evalplane/evalplane/backend/internal/evaluationsuite"
	"github.com/evalplane/evalplane/backend/internal/failuretaxonomy"
	"github.com/evalplane/evalplane/backend/internal/identity"
	"github.com/jackc/pgx/v5/pgxpool"
)

const workspaceID = "99999999-9999-4999-8999-999999999999"
const executionID = "88888888-8888-4888-8888-888888888888"
const agentID = "77777777-7777-4777-8777-777777777777"

type fakeRoles struct {
	role string
	err  error
}

func (f fakeRoles) GetRole(context.Context, identity.DBTX, string, string) (string, error) {
	return f.role, f.err
}

type fakeStore struct {
	policy    Policy
	version   PolicyVersion
	saved     Assessment
	saveCalls int
}

func (f *fakeStore) CreatePolicy(_ context.Context, _ DBTX, workspace, user, name string) (Policy, error) {
	f.policy = Policy{ID: policyID, WorkspaceID: workspace, Name: name}
	return f.policy, nil
}
func (f *fakeStore) CreateVersion(_ context.Context, _ *pgxpool.Pool, _ string, policy string, _ string, in CreateVersionInput) (PolicyVersion, error) {
	f.version.Version++
	f.version.ID = fmt.Sprintf("version-%d", f.version.Version)
	f.version.PolicyID = policy
	f.version.Definition = in.Definition
	return f.version, nil
}
func (f *fakeStore) GetPolicy(context.Context, DBTX, string, string) (Policy, error) {
	return f.policy, nil
}
func (f *fakeStore) ListPolicies(context.Context, DBTX, string) ([]Policy, error) {
	return []Policy{f.policy}, nil
}
func (f *fakeStore) GetVersion(context.Context, DBTX, string, string) (PolicyVersion, error) {
	return f.version, nil
}
func (f *fakeStore) SaveAssessment(_ context.Context, _ *pgxpool.Pool, _ string, _ CreateAssessmentInput, out Assessment, _ AssessmentFacts) (Assessment, error) {
	f.saveCalls++
	if f.saved.ID != "" {
		return f.saved, nil
	}
	out.ID = "66666666-6666-4666-8666-666666666666"
	f.saved = out
	return out, nil
}
func (f *fakeStore) GetAssessmentByIdentity(context.Context, DBTX, string, CreateAssessmentInput) (Assessment, error) {
	return f.saved, nil
}
func (f *fakeStore) GetAssessment(context.Context, DBTX, string, string) (Assessment, error) {
	return f.saved, nil
}
func (f *fakeStore) ListAssessments(context.Context, DBTX, string, string, string) ([]Assessment, error) {
	return nil, nil
}

type fakeSuites struct {
	execution evaluationsuite.Execution
	results   evaluationsuite.Results
}

func (f fakeSuites) GetSuiteExecution(context.Context, auth.Principal, string, string) (evaluationsuite.Execution, error) {
	return f.execution, nil
}
func (f fakeSuites) GetSuiteResults(context.Context, auth.Principal, string, string) (evaluationsuite.Results, error) {
	return f.results, nil
}

type fakeRegressions struct{}

func (fakeRegressions) Results(context.Context, auth.Principal, string, string) (agentregression.Results, error) {
	return agentregression.Results{}, nil
}

type fakeFailures struct{ result failuretaxonomy.Results }

func (f fakeFailures) ForExecution(context.Context, auth.Principal, string, string) (failuretaxonomy.Results, error) {
	return f.result, nil
}

func TestPolicyCreatedInWorkspaceAndViewerRejected(t *testing.T) {
	store := &fakeStore{}
	service := NewService(nil, store, fakeRoles{role: "member"}, fakeSuites{}, fakeRegressions{}, fakeFailures{})
	got, err := service.CreatePolicy(context.Background(), auth.Principal{UserID: agentID}, workspaceID, CreatePolicyInput{Name: " Adoption "})
	if err != nil || got.WorkspaceID != workspaceID || got.Name != "Adoption" {
		t.Fatalf("policy=%#v err=%v", got, err)
	}
	service = NewService(nil, store, fakeRoles{role: "viewer"}, fakeSuites{}, fakeRegressions{}, fakeFailures{})
	if _, err = service.CreatePolicy(context.Background(), auth.Principal{UserID: agentID}, workspaceID, CreatePolicyInput{Name: "Denied"}); !errors.Is(err, auth.ErrForbidden) {
		t.Fatalf("err=%v", err)
	}
}

func TestChangingPolicyCreatesANewVersion(t *testing.T) {
	store := &fakeStore{}
	service := NewService(nil, store, fakeRoles{role: "admin"}, fakeSuites{}, fakeRegressions{}, fakeFailures{})
	first, err := service.CreatePolicyVersion(context.Background(), auth.Principal{UserID: agentID}, workspaceID, policyID, CreateVersionInput{Definition: basePolicy().Definition})
	if err != nil {
		t.Fatal(err)
	}
	changed := basePolicy().Definition
	changed.MinimumOverallPassRate = 95
	second, err := service.CreatePolicyVersion(context.Background(), auth.Principal{UserID: agentID}, workspaceID, policyID, CreateVersionInput{Definition: changed})
	if err != nil {
		t.Fatal(err)
	}
	if first.ID == second.ID || first.Version != 1 || second.Version != 2 || first.Definition.MinimumOverallPassRate == second.Definition.MinimumOverallPassRate {
		t.Fatalf("first=%#v second=%#v", first, second)
	}
}
func TestAssessmentPinsPolicyExecutionAgentAndIsIdempotent(t *testing.T) {
	store := &fakeStore{version: basePolicy()}
	suite := fakeSuites{execution: evaluationsuite.Execution{ID: executionID, SuiteVersionID: "22222222-2222-4222-8222-222222222222", Status: "completed"}, results: evaluationsuite.Results{SuiteVersionID: "22222222-2222-4222-8222-222222222222", ByAgent: []evaluationsuite.AgentCounts{{AgentVersionID: agentID, Counts: evaluationsuite.Counts{Scheduled: 3, Valid: 3, Passed: 3, PassRate: pct(100)}}}}}
	service := NewService(nil, store, fakeRoles{role: "member"}, suite, fakeRegressions{}, fakeFailures{})
	in := CreateAssessmentInput{PolicyVersionID: policyID, SuiteExecutionID: executionID, AgentVersionID: agentID}
	first, err := service.CreateAssessment(context.Background(), auth.Principal{UserID: agentID}, workspaceID, in)
	if err != nil {
		t.Fatal(err)
	}
	second, err := service.CreateAssessment(context.Background(), auth.Principal{UserID: agentID}, workspaceID, in)
	if err != nil || first.ID != second.ID || first.PolicyVersionID != policyID || first.SuiteExecutionID != executionID || first.AgentVersionID != agentID {
		t.Fatalf("first=%#v second=%#v err=%v", first, second, err)
	}
}
func TestWorkspaceIsolationIsEnforced(t *testing.T) {
	service := NewService(nil, &fakeStore{}, fakeRoles{err: identity.ErrNotFound}, fakeSuites{}, fakeRegressions{}, fakeFailures{})
	_, err := service.GetPolicy(context.Background(), auth.Principal{UserID: agentID}, workspaceID, policyID)
	if !errors.Is(err, auth.ErrForbidden) {
		t.Fatalf("err=%v", err)
	}
}

func TestNonterminalExecutionCannotBeAssessed(t *testing.T) {
	store := &fakeStore{version: basePolicy()}
	suite := fakeSuites{execution: evaluationsuite.Execution{ID: executionID, Status: "running"}}
	service := NewService(nil, store, fakeRoles{role: "member"}, suite, fakeRegressions{}, fakeFailures{})
	_, err := service.CreateAssessment(context.Background(), auth.Principal{UserID: agentID}, workspaceID, CreateAssessmentInput{PolicyVersionID: policyID, SuiteExecutionID: executionID, AgentVersionID: agentID})
	if !errors.Is(err, identity.ErrConflict) {
		t.Fatalf("err=%v", err)
	}
}

func TestComparativeReportContainsObjectiveSameSuiteSides(t *testing.T) {
	a, b := "aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa", "bbbbbbbb-bbbb-4bbb-8bbb-bbbbbbbbbbbb"
	trials := []evaluationsuite.TrialResult{{AgentVersionID: a, ScenarioVersionID: "s", TrialOrdinal: 1, VariantID: "v", VariantSeed: 1, EvaluatorVersion: "e"}, {AgentVersionID: b, ScenarioVersionID: "s", TrialOrdinal: 1, VariantID: "v", VariantSeed: 1, EvaluatorVersion: "e"}}
	results := evaluationsuite.Results{SuiteVersionID: "suite", ByAgent: []evaluationsuite.AgentCounts{{AgentVersionID: a, Counts: evaluationsuite.Counts{Scheduled: 1, Valid: 1, Passed: 1, PassRate: pct(100)}}, {AgentVersionID: b, Counts: evaluationsuite.Counts{Scheduled: 1, Valid: 1, Failed: 1, PassRate: pct(0)}}}, Trials: trials}
	suite := fakeSuites{execution: evaluationsuite.Execution{ID: executionID, SuiteVersionID: "suite"}, results: results}
	failures := fakeFailures{result: failuretaxonomy.Results{Failures: []failuretaxonomy.FailureGroup{{AgentVersionID: b, Category: failuretaxonomy.CategoryCriterion, Code: "criterion_failed:error_rate"}}}}
	service := NewService(nil, &fakeStore{}, fakeRoles{role: "viewer"}, suite, fakeRegressions{}, failures)
	report, err := service.ComparativeReport(context.Background(), auth.Principal{UserID: agentID}, workspaceID, executionID, "")
	if err != nil {
		t.Fatal(err)
	}
	if report.SuiteVersionID != "suite" || report.Comparability != "comparable" || len(report.Agents) != 2 || len(report.Agents[1].Failures) != 1 {
		t.Fatalf("report=%#v", report)
	}
}
