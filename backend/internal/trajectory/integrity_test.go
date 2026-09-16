package trajectory

import (
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/evalplane/evalplane/backend/internal/evidence"
	"github.com/evalplane/evalplane/backend/internal/identity"
)

func workerRecoveryEvents(run RunInfo, at time.Time) []evidence.RunEvent {
	return []evidence.RunEvent{
		{ID: 1, RunID: run.ID, Sequence: 1, EventType: "recovery", OccurredAt: at, Summary: "worker_recovery_completed", Payload: json.RawMessage(`{"code":"worker_recovery_completed","classification":"platform"}`)},
	}
}

func applicationRecoveryEvents(run RunInfo, detected, verified time.Time) []evidence.RunEvent {
	return []evidence.RunEvent{
		{ID: 1, RunID: run.ID, Sequence: 1, EventType: "recovery", OccurredAt: detected, Summary: "recovery_detected", Payload: json.RawMessage(`{"code":"recovery_detected"}`)},
		{ID: 2, RunID: run.ID, Sequence: 2, EventType: "recovery", OccurredAt: verified, Summary: "recovery_verified", Payload: json.RawMessage(`{"code":"recovery_verified"}`)},
	}
}

// 1. worker_recovery_completed alone does not set recovery_detected_at.
func TestWorkerRecoveryAloneSetsNoDetectedAt(t *testing.T) {
	run := testRun()
	at := run.CreatedAt.Add(time.Minute)
	got := Project(run, workerRecoveryEvents(run, at), nil, EvaluationInfo{}, TrialMeta{})
	if got.RecoveryDetectedAt != nil {
		t.Fatalf("worker recovery set recovery_detected_at=%v", got.RecoveryDetectedAt)
	}
}

// 2. worker_recovery_completed alone does not set recovery_verified_at.
func TestWorkerRecoveryAloneSetsNoVerifiedAt(t *testing.T) {
	run := testRun()
	at := run.CreatedAt.Add(time.Minute)
	got := Project(run, workerRecoveryEvents(run, at), nil, EvaluationInfo{}, TrialMeta{})
	if got.RecoveryVerifiedAt != nil {
		t.Fatalf("worker recovery set recovery_verified_at=%v", got.RecoveryVerifiedAt)
	}
	if got.Summary.RecoveryVerified {
		t.Fatal("worker recovery set recovery_verified=true")
	}
}

// 3. worker_recovery_completed alone does not create time_to_recovery.
func TestWorkerRecoveryAloneCreatesNoTTR(t *testing.T) {
	run := testRun()
	at := run.CreatedAt.Add(time.Minute)
	got := Project(run, workerRecoveryEvents(run, at), nil, EvaluationInfo{}, TrialMeta{})
	if got.Summary.TimeToRecoveryMS != nil || got.Durations.TimeToRecoveryMS != nil {
		t.Fatalf("worker recovery created time_to_recovery=%v/%v", got.Summary.TimeToRecoveryMS, got.Durations.TimeToRecoveryMS)
	}
}

// 4. worker_recovery_completed alone does not create actions_after_recovery.
func TestWorkerRecoveryAloneCreatesNoPostRecoveryActions(t *testing.T) {
	run := testRun()
	at := run.CreatedAt.Add(2 * time.Minute)
	actions := []evidence.AgentAction{
		{ID: "aaaaaaaa-aaaa-aaaa-aaaa-aaaaaaaaaaaa", RunID: run.ID, ActionType: "scale_workload", Status: "executed", Authorized: true, RequestPayload: json.RawMessage(`{}`), ResultPayload: json.RawMessage(`{}`), CreatedAt: run.CreatedAt.Add(time.Minute)},
		{ID: "bbbbbbbb-bbbb-bbbb-bbbb-bbbbbbbbbbbb", RunID: run.ID, ActionType: "restart_pod", Status: "executed", Authorized: true, RequestPayload: json.RawMessage(`{}`), ResultPayload: json.RawMessage(`{}`), CreatedAt: run.CreatedAt.Add(3 * time.Minute)},
	}
	got := Project(run, workerRecoveryEvents(run, at), actions, EvaluationInfo{}, TrialMeta{})
	if got.RecoveryDetectedAt != nil || got.Summary.TimeToRecoveryMS != nil {
		t.Fatalf("worker recovery invented detection: %+v", got.Summary)
	}
	if got.Summary.ActionsAfterRecovery != 0 {
		t.Fatalf("worker lease recovery labeled post-service-recovery actions=%d", got.Summary.ActionsAfterRecovery)
	}
	if got.Summary.ActionsBeforeRecovery != len(actions) {
		t.Fatalf("before=%d want %d", got.Summary.ActionsBeforeRecovery, len(actions))
	}
	if got.Summary.RecoveryVerified {
		t.Fatal("worker recovery set verified=true")
	}
}

// 5. worker recovery remains visible as platform/execution evidence.
func TestWorkerRecoveryRemainsVisibleAsPlatformEvidence(t *testing.T) {
	run := testRun()
	at := run.CreatedAt.Add(time.Minute)
	got := Project(run, workerRecoveryEvents(run, at), nil, EvaluationInfo{}, TrialMeta{})
	foundPlatform := false
	for _, event := range got.Events {
		if event.EventType == EventRecoveryDetected || event.EventType == EventRecoveryVerified {
			t.Fatalf("worker recovery projected as service recovery: %+v", event)
		}
		if event.EventType == EventPlatformInvalid {
			foundPlatform = true
			if event.Actor != ActorPlatform {
				t.Fatalf("worker recovery actor=%q want platform", event.Actor)
			}
			if len(event.EvidenceRefs) == 0 {
				t.Fatal("worker recovery platform event lacks provenance")
			}
		}
	}
	if !foundPlatform {
		t.Fatalf("worker recovery evidence removed; events=%+v", got.Events)
	}
}

// 6. explicit application recovery still sets recovery_detected_at.
func TestExplicitApplicationRecoverySetsDetected(t *testing.T) {
	run := testRun()
	detected := run.CreatedAt.Add(2 * time.Minute)
	events := []evidence.RunEvent{
		{ID: 1, RunID: run.ID, Sequence: 1, EventType: "recovery", OccurredAt: detected, Summary: "recovery_detected", Payload: json.RawMessage(`{"code":"recovery_detected"}`)},
	}
	got := Project(run, events, nil, EvaluationInfo{}, TrialMeta{})
	if got.RecoveryDetectedAt == nil || !got.RecoveryDetectedAt.Equal(detected) {
		t.Fatalf("application recovery not detected: %v", got.RecoveryDetectedAt)
	}
	if got.Summary.TimeToRecoveryMS == nil {
		t.Fatal("application recovery missing time_to_recovery")
	}
}

// 7. explicit recovery verification still sets recovery_verified_at.
func TestExplicitVerificationSetsVerified(t *testing.T) {
	run := testRun()
	detected := run.CreatedAt.Add(2 * time.Minute)
	verified := run.CreatedAt.Add(3 * time.Minute)
	got := Project(run, applicationRecoveryEvents(run, detected, verified), nil, EvaluationInfo{}, TrialMeta{})
	if got.RecoveryVerifiedAt == nil || !got.RecoveryVerifiedAt.Equal(verified) {
		t.Fatalf("verification missing: %v", got.RecoveryVerifiedAt)
	}
	if !got.Summary.RecoveryVerified {
		t.Fatal("summary verified=false")
	}
}

// 8. application recovery + later action correctly counts post-recovery action.
func TestApplicationRecoveryLaterActionCountsPostRecovery(t *testing.T) {
	run := testRun()
	detected := run.CreatedAt.Add(2 * time.Minute)
	verified := run.CreatedAt.Add(3 * time.Minute)
	actions := []evidence.AgentAction{
		{ID: "aaaaaaaa-aaaa-aaaa-aaaa-aaaaaaaaaaaa", RunID: run.ID, ActionType: "scale_workload", Status: "executed", Authorized: true, RequestPayload: json.RawMessage(`{}`), ResultPayload: json.RawMessage(`{}`), CreatedAt: run.CreatedAt.Add(time.Minute)},
		{ID: "bbbbbbbb-bbbb-bbbb-bbbb-bbbbbbbbbbbb", RunID: run.ID, ActionType: "restart_pod", Status: "executed", Authorized: true, RequestPayload: json.RawMessage(`{}`), ResultPayload: json.RawMessage(`{}`), CreatedAt: run.CreatedAt.Add(4 * time.Minute)},
	}
	got := Project(run, applicationRecoveryEvents(run, detected, verified), actions, EvaluationInfo{}, TrialMeta{})
	if got.Summary.ActionsBeforeRecovery != 1 || got.Summary.ActionsAfterRecovery != 1 {
		t.Fatalf("split wrong: %+v", got.Summary)
	}
}

// 9. no substring-based generic "recovery" event verifies service recovery.
func TestGenericRecoveryStringDoesNotVerify(t *testing.T) {
	run := testRun()
	at := run.CreatedAt.Add(time.Minute)
	events := []evidence.RunEvent{
		{ID: 1, RunID: run.ID, Sequence: 1, EventType: "recovery", OccurredAt: at, Summary: "something recovered-ish", Payload: json.RawMessage(`{"code":"my_recovery_thing_in_progress"}`)},
		{ID: 2, RunID: run.ID, Sequence: 2, EventType: "system", OccurredAt: at.Add(time.Minute), Summary: "recovery-ish system note", Payload: json.RawMessage(`{"code":"service_recovery_observed"}`)},
		{ID: 3, RunID: run.ID, Sequence: 3, EventType: "observation", OccurredAt: at.Add(2 * time.Minute), Summary: "recovery mentioned", Payload: json.RawMessage(`{"observation":{"metrics":{"error_rate_pct":5}}}`)},
	}
	got := Project(run, events, nil, EvaluationInfo{}, TrialMeta{})
	if got.RecoveryDetectedAt != nil || got.RecoveryVerifiedAt != nil {
		t.Fatalf("generic recovery string verified service recovery: %v %v", got.RecoveryDetectedAt, got.RecoveryVerifiedAt)
	}
	if got.Summary.RecoveryVerified || got.Summary.TimeToRecoveryMS != nil {
		t.Fatalf("generic recovery invented fields: %+v", got.Summary)
	}
	for _, event := range got.Events {
		if event.EventType == EventRecoveryVerified {
			t.Fatalf("generic event projected as verified: %+v", event)
		}
	}
}

func testSuiteRows(count int) []SuiteRow {
	rows := make([]SuiteRow, 0, count)
	for i := 0; i < count; i++ {
		rows = append(rows, SuiteRow{
			RunID:             "11111111-1111-1111-1111-11111111111" + string(rune('0'+i)),
			TrialOrdinal:      i + 1,
			AgentVersionID:    "44444444-4444-4444-4444-444444444444",
			ScenarioVersionID: "55555555-5555-5555-5555-555555555555",
			RunStatus:         "completed",
		})
	}
	return rows
}

func okLoader(run RunInfo) func(string) (LoadedTrial, error) {
	return func(runID string) (LoadedTrial, error) {
		now := time.Date(2026, 9, 1, 12, 0, 0, 0, time.UTC)
		return LoadedTrial{Run: RunInfo{ID: runID, WorkspaceID: "ws", AgentVersionID: "av", ScenarioVersionID: "sv", AttemptNumber: 1, Status: "completed", CreatedAt: now}}, nil
	}
}

func neverExists(string) (bool, error) { return false, nil }

// 10. complete suite summary reports expected == loaded.
func TestCompleteSuiteSummaryReportsExpectedEqualsLoaded(t *testing.T) {
	rows := testSuiteRows(3)
	got, err := summarizeSuiteTrials("exec-1", "ws-1", rows, okLoader(testRun()), neverExists)
	if err != nil {
		t.Fatal(err)
	}
	if !got.Complete || got.ExpectedTrials != 3 || got.LoadedTrials != 3 || got.TotalTrials != 3 {
		t.Fatalf("complete summary wrong: %+v", got)
	}
	if len(got.MissingTrials) != 0 || len(got.Issues) != 0 {
		t.Fatalf("complete summary must have no missing/issues: %+v", got)
	}
}

// 11. missing trial can never be silently omitted.
func TestMissingTrialNeverSilentlyOmitted(t *testing.T) {
	rows := testSuiteRows(2)
	load := func(runID string) (LoadedTrial, error) {
		if strings.HasSuffix(runID, "1") {
			return LoadedTrial{}, identity.ErrNotFound
		}
		return okLoader(testRun())(runID)
	}
	got, err := summarizeSuiteTrials("exec-1", "ws-1", rows, load, neverExists)
	if err != nil {
		t.Fatal(err)
	}
	if got.Complete {
		t.Fatal("partial summary reported complete=true")
	}
	if got.ExpectedTrials != 2 || got.LoadedTrials != 1 || got.TotalTrials != 2 {
		t.Fatalf("counts wrong: %+v", got)
	}
	if len(got.MissingTrials) != 1 || len(got.Issues) != 1 {
		t.Fatalf("missing/issues not explicit: %+v", got)
	}
	if got.ExpectedTrials == got.LoadedTrials {
		t.Fatal("expected==loaded despite missing trial (silent omission)")
	}
}

// 12. partial summary explicitly reports complete=false and counts.
func TestPartialSummaryReportsCompleteFalseAndCounts(t *testing.T) {
	rows := testSuiteRows(10)
	load := func(runID string) (LoadedTrial, error) {
		if strings.HasSuffix(runID, "9") {
			return LoadedTrial{}, identity.ErrNotFound
		}
		return okLoader(testRun())(runID)
	}
	got, err := summarizeSuiteTrials("exec-1", "ws-1", rows, load, neverExists)
	if err != nil {
		t.Fatal(err)
	}
	if got.Complete {
		t.Fatal("expected complete=false")
	}
	if got.ExpectedTrials != 10 || got.LoadedTrials != 9 {
		t.Fatalf("9-of-10 counts wrong: %+v", got)
	}
	if len(got.MissingTrials) != 1 || got.Issues[0].Code == "" || got.Issues[0].RunID == nil {
		t.Fatalf("partial reasons not explicit: %+v", got)
	}
}

// 13. ownership/integrity mismatch does not leak or silently skip.
func TestOwnershipMismatchFailsInsteadOfSkipping(t *testing.T) {
	rows := testSuiteRows(2)
	// Conflict from loader must fail the request.
	_, err := summarizeSuiteTrials("exec-1", "ws-1", rows, func(runID string) (LoadedTrial, error) {
		return LoadedTrial{}, identity.ErrConflict
	}, neverExists)
	if !errors.Is(err, identity.ErrConflict) {
		t.Fatalf("conflict must fail request, got %v", err)
	}
	// NotFound but row still exists globally => ownership mismatch => fail.
	_, err = summarizeSuiteTrials("exec-1", "ws-1", rows, func(runID string) (LoadedTrial, error) {
		return LoadedTrial{}, identity.ErrNotFound
	}, func(string) (bool, error) { return true, nil })
	if !errors.Is(err, identity.ErrConflict) {
		t.Fatalf("ownership mismatch must fail with conflict, got %v", err)
	}
}

// 14. frontend never invents an outcome for a missing trial: backend emits no
// fabricated trial row for missing IDs and frontend renders only loaded rows.
func TestMissingTrialHasNoFabricatedOutcome(t *testing.T) {
	rows := testSuiteRows(2)
	missingID := rows[0].RunID
	load := func(runID string) (LoadedTrial, error) {
		if runID == missingID {
			return LoadedTrial{}, identity.ErrNotFound
		}
		return okLoader(testRun())(runID)
	}
	got, err := summarizeSuiteTrials("exec-1", "ws-1", rows, load, neverExists)
	if err != nil {
		t.Fatal(err)
	}
	for _, trial := range got.Trials {
		if trial.RunID == missingID {
			t.Fatal("fabricated outcome for missing trial")
		}
		if trial.Outcome == "" || trial.Validity == "" {
			t.Fatalf("loaded trial lacks outcome/validity: %+v", trial)
		}
	}
	if len(got.Trials) != 1 {
		t.Fatalf("trials=%d want 1 loaded", len(got.Trials))
	}
}

// 17. no evaluator-only leakage in worker-recovery projection.
func TestWorkerRecoveryProjectionLeaksNoEvaluatorOnly(t *testing.T) {
	run := testRun()
	at := run.CreatedAt.Add(time.Minute)
	got := Project(run, workerRecoveryEvents(run, at), nil, EvaluationInfo{}, TrialMeta{})
	for _, event := range got.Events {
		blob := strings.ToLower(event.EventID + " " + string(event.SafeMetadata) + " " + strings.Join(event.EvidenceRefs, " "))
		for _, forbidden := range []string{"hidden_config", "reference_remediation", "root_cause", "scratchpad", "chain-of-thought", "chain_of_thought", "reasoning"} {
			if strings.Contains(blob, forbidden) {
				t.Fatalf("leak %q in %s", forbidden, event.EventID)
			}
		}
	}
}

// 18. no LLM logic: projection is deterministic and pure.
func TestProjectionDeterministicNoLLM(t *testing.T) {
	run := testRun()
	at := run.CreatedAt.Add(time.Minute)
	events := []evidence.RunEvent{
		{ID: 1, RunID: run.ID, Sequence: 1, EventType: "observation", OccurredAt: at, Summary: "obs", Payload: json.RawMessage(`{"observation":{"metrics":{"error_rate_pct":5}}}`)},
	}
	first := Project(run, events, nil, EvaluationInfo{}, TrialMeta{})
	second := Project(run, events, nil, EvaluationInfo{}, TrialMeta{})
	if len(first.Events) != len(second.Events) {
		t.Fatal("nondeterministic projection")
	}
	for i := range first.Events {
		if first.Events[i].EventID != second.Events[i].EventID || first.Events[i].Summary != second.Events[i].Summary {
			t.Fatal("nondeterministic projection output")
		}
	}
}

// Stage-34 correction: known worker recovery + persisted platform
// classification still projects as platform-invalid evidence.
func TestKnownWorkerRecoveryWithPlatformClassificationProjectsPlatformInvalid(t *testing.T) {
	run := testRun()
	at := run.CreatedAt.Add(time.Minute)
	got := Project(run, workerRecoveryEvents(run, at), nil, EvaluationInfo{}, TrialMeta{})
	found := false
	for _, event := range got.Events {
		if event.EventType == EventRecoveryDetected || event.EventType == EventRecoveryVerified {
			t.Fatalf("worker recovery projected as service recovery: %+v", event)
		}
		if event.EventType == EventPlatformInvalid {
			found = true
			if event.Actor != ActorPlatform {
				t.Fatalf("actor=%q want platform", event.Actor)
			}
			if event.Status != StatusInvalid {
				t.Fatalf("status=%q want invalid", event.Status)
			}
			if len(event.EvidenceRefs) == 0 {
				t.Fatal("platform event lacks provenance")
			}
			if !strings.Contains(event.Summary, "worker_recovery_completed") {
				t.Fatalf("worker code not recognizable in summary: %q", event.Summary)
			}
		}
	}
	if !found {
		t.Fatalf("platform-classified worker recovery must project platform-invalid; events=%+v", got.Events)
	}
	if got.Summary.Validity != "invalid_platform" {
		t.Fatalf("validity=%q want invalid_platform (persisted classification authoritative)", got.Summary.Validity)
	}
}

// Stage-34 correction: unknown/future worker_* without platform
// classification must NOT project as platform-invalid. It stays visible as
// safe worker lifecycle evidence.
func TestUnknownWorkerEventWithoutPlatformClassificationNotPlatformInvalid(t *testing.T) {
	run := testRun()
	at := run.CreatedAt.Add(time.Minute)
	events := []evidence.RunEvent{
		{ID: 1, RunID: run.ID, Sequence: 1, EventType: "recovery", OccurredAt: at, Summary: "worker_future_reclaim_v2", Payload: json.RawMessage(`{"code":"worker_future_reclaim_v2"}`)},
		{ID: 2, RunID: run.ID, Sequence: 2, EventType: "system", OccurredAt: at.Add(time.Minute), Summary: "worker_future_reclaim_v2", Payload: json.RawMessage(`{"code":"worker_future_reclaim_v2"}`)},
	}
	got := Project(run, events, nil, EvaluationInfo{}, TrialMeta{})
	for _, event := range got.Events {
		if event.EventType == EventPlatformInvalid {
			t.Fatalf("unclassified future worker_* manufactured platform_invalid: %+v", event)
		}
		if event.Status == StatusInvalid {
			t.Fatalf("unclassified future worker_* manufactured invalid status: %+v", event)
		}
		if event.EventType == EventRecoveryDetected || event.EventType == EventRecoveryVerified {
			t.Fatalf("future worker_* manufactured service recovery: %+v", event)
		}
		if event.EventType == EventAgentActionFailed {
			t.Fatalf("future worker_* manufactured agent failure: %+v", event)
		}
	}
	// Still visible as worker/platform lifecycle evidence (display-only prefix).
	visible := false
	for _, event := range got.Events {
		if strings.Contains(event.Summary, "worker_future_reclaim_v2") && event.Actor == ActorPlatform {
			visible = true
			if event.Status == StatusInvalid {
				t.Fatalf("visible worker lifecycle must not carry invalid status: %+v", event)
			}
		}
	}
	if !visible {
		t.Fatalf("future worker_* evidence removed; events=%+v", got.Events)
	}
	// Prefix alone is not a failure classifier.
	if IsWorkerRecoveryCode("worker_future_reclaim_v2") {
		t.Fatal("IsWorkerRecoveryCode must not match unknown worker_* by prefix")
	}
	if !IsWorkerRecoveryCode("worker_recovery_completed") {
		t.Fatal("known Stage-18 code must still be recognized")
	}
}

// Stage-34 correction: unknown worker_* still cannot imply service recovery.
func TestUnknownWorkerEventCannotImplyServiceRecovery(t *testing.T) {
	run := testRun()
	at := run.CreatedAt.Add(2 * time.Minute)
	events := []evidence.RunEvent{
		{ID: 1, RunID: run.ID, Sequence: 1, EventType: "recovery", OccurredAt: at, Summary: "worker_future_reclaim_v2", Payload: json.RawMessage(`{"code":"worker_future_reclaim_v2"}`)},
	}
	actions := []evidence.AgentAction{
		{ID: "aaaaaaaa-aaaa-aaaa-aaaa-aaaaaaaaaaaa", RunID: run.ID, ActionType: "scale_workload", Status: "executed", Authorized: true, RequestPayload: json.RawMessage(`{}`), ResultPayload: json.RawMessage(`{}`), CreatedAt: run.CreatedAt.Add(time.Minute)},
		{ID: "bbbbbbbb-bbbb-bbbb-bbbb-bbbbbbbbbbbb", RunID: run.ID, ActionType: "restart_pod", Status: "executed", Authorized: true, RequestPayload: json.RawMessage(`{}`), ResultPayload: json.RawMessage(`{}`), CreatedAt: run.CreatedAt.Add(3 * time.Minute)},
	}
	got := Project(run, events, actions, EvaluationInfo{}, TrialMeta{})
	if got.RecoveryDetectedAt != nil {
		t.Fatalf("unknown worker set recovery_detected_at=%v", got.RecoveryDetectedAt)
	}
	if got.RecoveryVerifiedAt != nil {
		t.Fatalf("unknown worker set recovery_verified_at=%v", got.RecoveryVerifiedAt)
	}
	if got.Summary.TimeToRecoveryMS != nil || got.Durations.TimeToRecoveryMS != nil {
		t.Fatalf("unknown worker created time_to_recovery=%v/%v", got.Summary.TimeToRecoveryMS, got.Durations.TimeToRecoveryMS)
	}
	if got.Summary.RecoveryVerified {
		t.Fatal("unknown worker set recovery_verified=true")
	}
	if got.Summary.ActionsAfterRecovery != 0 || got.Summary.ActionsBeforeRecovery != len(actions) {
		t.Fatalf("unknown worker split actions: %+v", got.Summary)
	}
}

// Stage-34 correction: trial validity remains driven by persisted
// classification, not by the worker_* prefix.
func TestTrialValidityDrivenByPersistedClassification(t *testing.T) {
	run := testRun()
	at := run.CreatedAt.Add(time.Minute)
	unclassified := []evidence.RunEvent{
		{ID: 1, RunID: run.ID, Sequence: 1, EventType: "recovery", OccurredAt: at, Summary: "worker_future_reclaim_v2", Payload: json.RawMessage(`{"code":"worker_future_reclaim_v2"}`)},
	}
	got := Project(run, unclassified, nil, EvaluationInfo{}, TrialMeta{})
	if got.Summary.Validity == "invalid_platform" {
		t.Fatalf("unclassified worker_* manufactured invalid_platform validity: %+v", got.Summary)
	}
	classified := []evidence.RunEvent{
		{ID: 1, RunID: run.ID, Sequence: 1, EventType: "recovery", OccurredAt: at, Summary: "worker_recovery_completed", Payload: json.RawMessage(`{"code":"worker_recovery_completed","classification":"platform"}`)},
	}
	got = Project(run, classified, nil, EvaluationInfo{}, TrialMeta{})
	if got.Summary.Validity != "invalid_platform" {
		t.Fatalf("platform-classified worker recovery must drive invalid_platform, got %q", got.Summary.Validity)
	}
}

// Stage-34 correction: requested-vs-executed stays green with unknown worker
// events present.
func TestRequestedVsExecutedRemainsGreenWithUnknownWorker(t *testing.T) {
	run := testRun()
	at := run.CreatedAt.Add(time.Minute)
	events := []evidence.RunEvent{
		{ID: 1, RunID: run.ID, Sequence: 1, EventType: "recovery", OccurredAt: at, Summary: "worker_future_reclaim_v2", Payload: json.RawMessage(`{"code":"worker_future_reclaim_v2"}`)},
	}
	actions := []evidence.AgentAction{
		{ID: "66666666-6666-6666-6666-666666666666", RunID: run.ID, ActionType: "scale_workload", Status: "proposed", RequestPayload: json.RawMessage(`{"type":"scale_workload"}`), ResultPayload: json.RawMessage(`{}`), CreatedAt: at},
	}
	got := Project(run, events, actions, EvaluationInfo{}, TrialMeta{})
	foundRequested, foundExecuted := false, false
	for _, event := range got.Events {
		if event.EventType == EventAgentActionRequested {
			foundRequested = true
		}
		if event.EventType == EventAgentActionExecuted {
			foundExecuted = true
		}
	}
	if !foundRequested || foundExecuted {
		t.Fatalf("proposed must stay requested-only with unknown worker present (requested=%v executed=%v)", foundRequested, foundExecuted)
	}
	if got.Summary.ActionsRequested != 1 || got.Summary.ActionsExecuted != 0 {
		t.Fatalf("summary=%+v", got.Summary)
	}
}

// Stage-34 correction: no evaluator-only leakage in unknown worker projection.
func TestUnknownWorkerProjectionLeaksNoEvaluatorOnly(t *testing.T) {
	run := testRun()
	at := run.CreatedAt.Add(time.Minute)
	events := []evidence.RunEvent{
		{ID: 1, RunID: run.ID, Sequence: 1, EventType: "recovery", OccurredAt: at, Summary: "worker_future_reclaim_v2", Payload: json.RawMessage(`{"code":"worker_future_reclaim_v2"}`)},
	}
	got := Project(run, events, nil, EvaluationInfo{}, TrialMeta{})
	for _, event := range got.Events {
		blob := strings.ToLower(event.EventID + " " + string(event.SafeMetadata) + " " + strings.Join(event.EvidenceRefs, " "))
		for _, forbidden := range []string{"hidden_config", "reference_remediation", "root_cause", "scratchpad", "chain-of-thought", "chain_of_thought", "reasoning"} {
			if strings.Contains(blob, forbidden) {
				t.Fatalf("leak %q in %s", forbidden, event.EventID)
			}
		}
	}
}

// Final correction: unknown worker_* without platform classification stays
// visible as safe platform lifecycle evidence on the evaluator side — never
// agent_visible, never platform_invalid, never service recovery.
func TestUnknownWorkerLifecycleUsesEvaluatorView(t *testing.T) {
	run := testRun()
	at := run.CreatedAt.Add(time.Minute)
	events := []evidence.RunEvent{
		{ID: 1, RunID: run.ID, Sequence: 1, EventType: "recovery", OccurredAt: at, Summary: "worker_future_reclaim_v2", Payload: json.RawMessage(`{"code":"worker_future_reclaim_v2"}`)},
		{ID: 2, RunID: run.ID, Sequence: 2, EventType: "system", OccurredAt: at.Add(time.Minute), Summary: "worker_future_reclaim_v2", Payload: json.RawMessage(`{"code":"worker_future_reclaim_v2"}`)},
	}
	got := Project(run, events, nil, EvaluationInfo{}, TrialMeta{})
	matched := 0
	for _, event := range got.Events {
		if !strings.Contains(event.Summary, "worker_future_reclaim_v2") {
			continue
		}
		matched++
		if event.Actor != ActorPlatform {
			t.Fatalf("unknown worker actor=%q want platform: %+v", event.Actor, event)
		}
		if event.View == "agent_visible" {
			t.Fatalf("unknown worker must not be agent_visible: %+v", event)
		}
		if event.View != "evaluator" {
			t.Fatalf("unknown worker view=%q want evaluator: %+v", event.View, event)
		}
		if event.EventType == EventPlatformInvalid {
			t.Fatalf("unknown worker manufactured platform_invalid: %+v", event)
		}
		if event.Status == StatusInvalid {
			t.Fatalf("unknown worker manufactured invalid status: %+v", event)
		}
		if event.EventType == EventRecoveryDetected || event.EventType == EventRecoveryVerified {
			t.Fatalf("unknown worker manufactured service recovery: %+v", event)
		}
		if event.EventType == EventAgentActionFailed {
			t.Fatalf("unknown worker manufactured agent failure: %+v", event)
		}
		if len(event.EvidenceRefs) == 0 {
			t.Fatalf("worker lifecycle must keep provenance: %+v", event)
		}
		blob := strings.ToLower(event.EventID + " " + string(event.SafeMetadata) + " " + strings.Join(event.EvidenceRefs, " "))
		for _, forbidden := range []string{"hidden_config", "reference_remediation", "root_cause", "scratchpad", "chain-of-thought", "chain_of_thought", "reasoning"} {
			if strings.Contains(blob, forbidden) {
				t.Fatalf("leak %q in %s", forbidden, event.EventID)
			}
		}
	}
	if matched == 0 {
		t.Fatalf("unknown worker evidence removed; events=%+v", got.Events)
	}
	if got.RecoveryDetectedAt != nil || got.RecoveryVerifiedAt != nil {
		t.Fatalf("unknown worker implied service recovery: %v %v", got.RecoveryDetectedAt, got.RecoveryVerifiedAt)
	}
	if got.Summary.RecoveryVerified || got.Summary.TimeToRecoveryMS != nil {
		t.Fatalf("unknown worker invented recovery fields: %+v", got.Summary)
	}
}

// Final correction: known worker codes keep prior behavior — platform-classified
// stays platform-invalid/evaluator, unclassified stays safe lifecycle/evaluator.
func TestKnownWorkerViewBehaviorPreservedByClassification(t *testing.T) {
	run := testRun()
	at := run.CreatedAt.Add(time.Minute)
	// Known + platform classification: still platform-invalid, evaluator, platform.
	classified := []evidence.RunEvent{
		{ID: 1, RunID: run.ID, Sequence: 1, EventType: "recovery", OccurredAt: at, Summary: "worker_recovery_completed", Payload: json.RawMessage(`{"code":"worker_recovery_completed","classification":"platform"}`)},
		{ID: 2, RunID: run.ID, Sequence: 2, EventType: "system", OccurredAt: at.Add(time.Minute), Summary: "worker_recovery_completed", Payload: json.RawMessage(`{"code":"worker_recovery_completed","classification":"platform"}`)},
	}
	got := Project(run, classified, nil, EvaluationInfo{}, TrialMeta{})
	foundInvalid := false
	for _, event := range got.Events {
		if !strings.Contains(event.Summary, "worker_recovery_completed") {
			continue
		}
		if event.Actor != ActorPlatform {
			t.Fatalf("classified worker actor=%q want platform: %+v", event.Actor, event)
		}
		if event.EventType == EventPlatformInvalid {
			foundInvalid = true
			if event.View != "evaluator" {
				t.Fatalf("classified worker view=%q want evaluator: %+v", event.View, event)
			}
			if event.Status != StatusInvalid {
				t.Fatalf("classified worker status=%q want invalid: %+v", event.Status, event)
			}
		}
		if event.View == "agent_visible" {
			t.Fatalf("classified worker must not be agent_visible: %+v", event)
		}
	}
	if !foundInvalid {
		t.Fatalf("classified worker must still project platform-invalid; events=%+v", got.Events)
	}
	// Known code without classification: safe lifecycle, evaluator, never invalid.
	unclassified := []evidence.RunEvent{
		{ID: 1, RunID: run.ID, Sequence: 1, EventType: "recovery", OccurredAt: at, Summary: "worker_recovery_completed", Payload: json.RawMessage(`{"code":"worker_recovery_completed"}`)},
		{ID: 2, RunID: run.ID, Sequence: 2, EventType: "system", OccurredAt: at.Add(time.Minute), Summary: "worker_recovery_completed", Payload: json.RawMessage(`{"code":"worker_recovery_completed","classification":"ok"}`)},
	}
	got = Project(run, unclassified, nil, EvaluationInfo{}, TrialMeta{})
	matched := 0
	for _, event := range got.Events {
		if !strings.Contains(event.Summary, "worker_recovery_completed") {
			continue
		}
		matched++
		if event.Actor != ActorPlatform {
			t.Fatalf("unclassified worker actor=%q want platform: %+v", event.Actor, event)
		}
		if event.View != "evaluator" {
			t.Fatalf("unclassified worker view=%q want evaluator: %+v", event.View, event)
		}
		if event.EventType == EventPlatformInvalid || event.Status == StatusInvalid {
			t.Fatalf("unclassified worker manufactured invalid: %+v", event)
		}
	}
	if matched == 0 {
		t.Fatalf("unclassified known worker evidence removed; events=%+v", got.Events)
	}
	if got.Summary.Validity == "invalid_platform" {
		t.Fatalf("unclassified worker manufactured invalid_platform validity")
	}
}
