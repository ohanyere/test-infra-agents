package trajectory

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/evalplane/evalplane/backend/internal/evidence"
)

func testRun() RunInfo {
	now := time.Date(2026, 9, 1, 12, 0, 0, 0, time.UTC)
	completed := now.Add(5 * time.Minute)
	return RunInfo{
		ID: "11111111-1111-1111-1111-111111111111", BatchID: "22222222-2222-2222-2222-222222222222",
		WorkspaceID: "33333333-3333-3333-3333-333333333333", AgentVersionID: "44444444-4444-4444-4444-444444444444",
		ScenarioVersionID: "55555555-5555-5555-5555-555555555555", AttemptNumber: 1, Status: "completed",
		CreatedAt: now, StartedAt: &now, CompletedAt: &completed,
	}
}

func TestContractPinned(t *testing.T) {
	got := Project(testRun(), nil, nil, EvaluationInfo{}, TrialMeta{})
	if got.Contract != ContractVersion {
		t.Fatalf("contract=%q", got.Contract)
	}
	if ContractVersion != "trajectory-evidence-contract:v1" {
		t.Fatalf("contract constant=%q", ContractVersion)
	}
	if got.Pins.TrajectoryContract != ContractVersion {
		t.Fatalf("pins contract=%q", got.Pins.TrajectoryContract)
	}
	if got.OrderingRule == "" {
		t.Fatal("ordering rule must be documented")
	}
}

func TestDeterministicOrderingAndTieBreak(t *testing.T) {
	run := testRun()
	at := run.CreatedAt.Add(time.Minute)
	events := []evidence.RunEvent{
		{ID: 2, RunID: run.ID, Sequence: 2, EventType: "observation", OccurredAt: at, Summary: "b", Payload: json.RawMessage(`{}`)},
		{ID: 1, RunID: run.ID, Sequence: 1, EventType: "observation", OccurredAt: at, Summary: "a", Payload: json.RawMessage(`{}`)},
	}
	first := Project(run, events, nil, EvaluationInfo{}, TrialMeta{})
	second := Project(run, []evidence.RunEvent{events[1], events[0]}, nil, EvaluationInfo{}, TrialMeta{})
	if len(first.Events) != len(second.Events) {
		t.Fatal("event counts differ")
	}
	for i := range first.Events {
		if first.Events[i].EventID != second.Events[i].EventID || first.Events[i].Sequence != second.Events[i].Sequence {
			t.Fatalf("nondeterministic order at %d", i)
		}
	}
	// Tie-break: same timestamp orders by source priority then sequence then ID.
	if !strings.Contains(first.Events[1].EventID, "run_event:1") {
		t.Fatalf("tie-break wrong: %+v", first.Events[1])
	}
}

func TestRequestedVsExecutedDistinction(t *testing.T) {
	run := testRun()
	at := run.CreatedAt.Add(time.Minute)
	actions := []evidence.AgentAction{
		{ID: "66666666-6666-6666-6666-666666666666", RunID: run.ID, ActionType: "scale_workload", Status: "proposed", RequestPayload: json.RawMessage(`{"type":"scale_workload"}`), ResultPayload: json.RawMessage(`{}`), CreatedAt: at},
	}
	got := Project(run, nil, actions, EvaluationInfo{}, TrialMeta{})
	foundRequested, foundExecuted := false, false
	for _, event := range got.Events {
		if event.EventType == EventAgentActionRequested {
			foundRequested = true
		}
		if event.EventType == EventAgentActionExecuted {
			foundExecuted = true
		}
		for _, key := range []string{"chain-of-thought", "chain_of_thought", "reasoning"} {
			if strings.Contains(strings.ToLower(string(event.SafeMetadata)), key) {
				t.Fatalf("leak in metadata: %s", event.SafeMetadata)
			}
		}
	}
	if !foundRequested || foundExecuted {
		t.Fatalf("proposed must project requested-only (requested=%v executed=%v)", foundRequested, foundExecuted)
	}
	if got.Summary.ActionsRequested != 1 || got.Summary.ActionsExecuted != 0 {
		t.Fatalf("summary=%+v", got.Summary)
	}
}

func TestDeniedActionProjection(t *testing.T) {
	run := testRun()
	at := run.CreatedAt.Add(time.Minute)
	target := "Deployment/ns/checkout"
	actions := []evidence.AgentAction{
		{ID: "77777777-7777-7777-7777-777777777777", RunID: run.ID, ActionType: "scale_workload", TargetRef: &target, Status: "denied", Authorized: false, RequestPayload: json.RawMessage(`{"type":"scale_workload"}`), ResultPayload: json.RawMessage(`{"code":"disallowed_resource"}`), CreatedAt: at},
	}
	got := Project(run, nil, actions, EvaluationInfo{}, TrialMeta{})
	denied := false
	for _, event := range got.Events {
		if event.EventType == EventAgentActionDenied {
			denied = true
			if event.Status != StatusDenied || !strings.Contains(event.Summary, "DENIED") {
				t.Fatalf("denied event wrong: %+v", event)
			}
			if !strings.Contains(strings.ToLower(event.Summary), "scope") {
				t.Fatalf("denied reason missing: %q", event.Summary)
			}
		}
	}
	if !denied || got.Summary.ActionsDenied != 1 {
		t.Fatalf("denied missing: %+v", got.Summary)
	}
}

func TestToolCallAndSecretRedaction(t *testing.T) {
	redacted := RedactObject(map[string]any{
		"tool": "evalplane_scale_workload", "target": "checkout",
		"authorization": "Bearer secret-token", "api_key": "key-123",
		"nested": map[string]any{"kubeconfig": "secret-data", "replicas": 4},
	})
	raw, _ := json.Marshal(redacted)
	text := string(raw)
	if strings.Contains(text, "secret-token") || strings.Contains(text, "key-123") || strings.Contains(text, "secret-data") {
		t.Fatalf("secrets leaked: %s", text)
	}
	if !strings.Contains(text, "[REDACTED]") {
		t.Fatalf("redaction marker missing: %s", text)
	}
	if !strings.Contains(text, "checkout") || !strings.Contains(text, "4") {
		t.Fatalf("operational context destroyed: %s", text)
	}
	// Bearer literals in free strings.
	if got := redactValue("Bearer abc123"); got != "[REDACTED]" {
		t.Fatalf("bearer not redacted: %v", got)
	}
}

func TestAgentVsEvaluatorSeparation(t *testing.T) {
	run := testRun()
	at := run.CreatedAt.Add(time.Minute)
	events := []evidence.RunEvent{
		{ID: 1, RunID: run.ID, Sequence: 1, EventType: "observation", OccurredAt: at, Summary: "obs", Payload: json.RawMessage(`{"observation":{"metrics":{"error_rate_pct":12.4}}}`)},
	}
	evaluation := EvaluationInfo{Found: true, ID: "88888888-8888-8888-8888-888888888888", Result: "fail", EvaluatorVersion: "deterministic-evaluator:v1",
		Criteria: []evidence.CriterionResult{{ID: "c1", ScenarioCriterionID: "55555555-5555-5555-5555-555555555555", Key: "recovery", Passed: false}},
		Findings: []evidence.Finding{{ID: "f1", Kind: "safety", Summary: "Unauthorized action attempted", Evidence: json.RawMessage(`{"code":"unauthorized_action_attempt"}`)}},
	}
	got := Project(run, events, nil, evaluation, TrialMeta{})
	views := map[string]string{}
	for _, event := range got.Events {
		views[event.EventType] = event.View
	}
	if views[EventObservation] != "agent_visible" {
		t.Fatalf("observation view=%q", views[EventObservation])
	}
	if views[EventSafetyFinding] != "evaluator" || views[EventEvaluationFinding] != "evaluator" {
		t.Fatalf("evaluator views wrong: %v", views)
	}
}

func TestStateChangeOnlyWhenEvidenced(t *testing.T) {
	run := testRun()
	at := run.CreatedAt.Add(time.Minute)
	plain := []evidence.RunEvent{{ID: 1, RunID: run.ID, Sequence: 1, EventType: "observation", OccurredAt: at, Summary: "obs", Payload: json.RawMessage(`{"observation":{"metrics":{"error_rate_pct":5}}}`)}}
	if got := Project(run, plain, nil, EvaluationInfo{}, TrialMeta{}); countType(got, EventStateChange) != 0 {
		t.Fatal("speculative state change derived")
	}
	withChange := []evidence.RunEvent{{ID: 1, RunID: run.ID, Sequence: 1, EventType: "observation", OccurredAt: at, Summary: "obs",
		Payload: json.RawMessage(`{"observation":{"metrics":{"error_rate_pct":5},"state_transition":{"name":"replicas","from":"2","to":"4"}}}`)}}
	got := Project(run, withChange, nil, EvaluationInfo{}, TrialMeta{})
	if countType(got, EventStateChange) != 1 {
		t.Fatal("evidenced state change missing")
	}
	for _, event := range got.Events {
		if event.EventType == EventStateChange && len(event.EvidenceRefs) == 0 {
			t.Fatal("state change must link underlying evidence")
		}
	}
}

func TestRecoveryAndActionsAfter(t *testing.T) {
	run := testRun()
	detected := run.CreatedAt.Add(2 * time.Minute)
	verified := run.CreatedAt.Add(3 * time.Minute)
	events := []evidence.RunEvent{
		{ID: 1, RunID: run.ID, Sequence: 1, EventType: "recovery", OccurredAt: detected, Summary: "recovery_detected", Payload: json.RawMessage(`{"code":"recovery_detected"}`)},
		{ID: 2, RunID: run.ID, Sequence: 2, EventType: "recovery", OccurredAt: verified, Summary: "recovery_verified", Payload: json.RawMessage(`{"code":"recovery_verified"}`)},
	}
	before := run.CreatedAt.Add(time.Minute)
	after := run.CreatedAt.Add(4 * time.Minute)
	actions := []evidence.AgentAction{
		{ID: "aaaaaaaa-aaaa-aaaa-aaaa-aaaaaaaaaaaa", RunID: run.ID, ActionType: "scale_workload", Status: "executed", Authorized: true, RequestPayload: json.RawMessage(`{}`), ResultPayload: json.RawMessage(`{}`), CreatedAt: before},
		{ID: "bbbbbbbb-bbbb-bbbb-bbbb-bbbbbbbbbbbb", RunID: run.ID, ActionType: "restart_pod", Status: "executed", Authorized: true, RequestPayload: json.RawMessage(`{}`), ResultPayload: json.RawMessage(`{}`), CreatedAt: after},
	}
	got := Project(run, events, actions, EvaluationInfo{}, TrialMeta{})
	if got.RecoveryDetectedAt == nil || !got.RecoveryDetectedAt.Equal(detected) {
		t.Fatalf("recovery_detected=%v", got.RecoveryDetectedAt)
	}
	if got.RecoveryVerifiedAt == nil || !got.RecoveryVerifiedAt.Equal(verified) {
		t.Fatalf("recovery_verified=%v", got.RecoveryVerifiedAt)
	}
	if !got.Summary.RecoveryVerified || got.Summary.ActionsAfterRecovery != 1 || got.Summary.ActionsBeforeRecovery != 1 {
		t.Fatalf("after-recovery wrong: %+v", got.Summary)
	}
	if got.Summary.TimeToRecoveryMS == nil || *got.Summary.TimeToRecoveryMS != 120000 {
		t.Fatalf("time-to-recovery=%v", got.Summary.TimeToRecoveryMS)
	}
}

func TestNoInventedRecovery(t *testing.T) {
	got := Project(testRun(), nil, nil, EvaluationInfo{}, TrialMeta{})
	if got.RecoveryDetectedAt != nil || got.Summary.TimeToRecoveryMS != nil {
		t.Fatal("recovery invented without evidence")
	}
}

func TestSafetyAndPlatformInvalid(t *testing.T) {
	run := testRun()
	evaluation := EvaluationInfo{Found: true, ID: "99999999-9999-9999-9999-999999999999", Result: "fail", EvaluatorVersion: "deterministic-evaluator:v1",
		Findings: []evidence.Finding{
			{ID: "f1", Kind: "safety", Summary: "denied", Evidence: json.RawMessage(`{"code":"cross_namespace_action_attempt"}`)},
			{ID: "f2", Kind: "limitation", Summary: "telemetry gone", Evidence: json.RawMessage(`{"code":"telemetry_unavailable"}`)},
		}}
	got := Project(run, nil, nil, evaluation, TrialMeta{})
	if countType(got, EventSafetyFinding) != 1 || countType(got, EventPlatformInvalid) != 1 {
		t.Fatalf("safety/platform projection wrong: %+v", got.Events)
	}
	if got.Summary.Validity != "invalid_platform" {
		t.Fatalf("validity=%q", got.Summary.Validity)
	}
	found := false
	for _, class := range got.Summary.FailureClasses {
		if class == "safety" {
			found = true
		}
	}
	if !found {
		t.Fatalf("failure taxonomy not reused: %v", got.Summary.FailureClasses)
	}
}

func TestFailureSummaryCountsAndTTR(t *testing.T) {
	run := testRun()
	at := run.CreatedAt.Add(time.Minute)
	actions := []evidence.AgentAction{
		{ID: "aaaaaaaa-aaaa-aaaa-aaaa-aaaaaaaaaaaa", RunID: run.ID, ActionType: "scale_workload", Status: "executed", Authorized: true, RequestPayload: json.RawMessage(`{}`), ResultPayload: json.RawMessage(`{}`), CreatedAt: at},
	}
	evaluation := EvaluationInfo{Found: true, ID: "99999999-9999-9999-9999-999999999999", Result: "pass", EvaluatorVersion: "deterministic-evaluator:v1",
		Criteria: []evidence.CriterionResult{{ID: "c1", ScenarioCriterionID: "55555555-5555-5555-5555-555555555555", Key: "recovery", Passed: true}},
		Findings: []evidence.Finding{{ID: "f1", Kind: "strength", Summary: "fast", Evidence: json.RawMessage(`{"code":"fast_recovery"}`)}},
	}
	got := Project(run, nil, actions, evaluation, TrialMeta{})
	if got.Summary.PassFailOutcome != "pass" || got.Summary.Validity != "valid" {
		t.Fatalf("summary=%+v", got.Summary)
	}
	if got.Summary.FindingCount != 2 || got.Durations.TrialDurationMS == nil {
		t.Fatalf("counts/durations=%+v %+v", got.Summary, got.Durations)
	}
}

func TestMissingDurationsExplicit(t *testing.T) {
	run := RunInfo{ID: "11111111-1111-1111-1111-111111111111", Status: "running"}
	got := Project(run, nil, nil, EvaluationInfo{}, TrialMeta{})
	if got.Durations.TrialDurationMS != nil || got.Durations.TimeToRecoveryMS != nil {
		t.Fatalf("missing durations must be nil: %+v", got.Durations)
	}
}

func TestBoundedPreview(t *testing.T) {
	big := strings.Repeat("x", MaxPreviewBytes+100)
	preview, truncated, size, hash := BoundedPreview(big)
	if !truncated || len(preview) != MaxPreviewBytes || size != len(big) || hash == "" {
		t.Fatalf("preview wrong: %d %v %d %q", len(preview), truncated, size, hash)
	}
	small := "ok"
	preview, truncated, _, _ = BoundedPreview(small)
	if truncated || preview != small {
		t.Fatal("small preview must pass through")
	}
}

func TestProvenanceAndHiddenLeakage(t *testing.T) {
	run := testRun()
	at := run.CreatedAt.Add(time.Minute)
	events := []evidence.RunEvent{{ID: 7, RunID: run.ID, Sequence: 1, EventType: "observation", OccurredAt: at, Summary: "obs", Payload: json.RawMessage(`{}`)}}
	got := Project(run, events, nil, EvaluationInfo{}, TrialMeta{VariantID: strPtr("opaque-variant-1")})
	for _, event := range got.Events {
		if len(event.EvidenceRefs) == 0 {
			t.Fatalf("event %s lacks provenance", event.EventID)
		}
		blob := strings.ToLower(event.EventID + " " + string(event.SafeMetadata) + " " + strings.Join(event.EvidenceRefs, " "))
		for _, forbidden := range []string{"hidden_config", "reference_remediation", "root-cause", "root_cause", "chain-of-thought", "scratchpad"} {
			if strings.Contains(blob, forbidden) {
				t.Fatalf("leak %q in %s", forbidden, event.EventID)
			}
		}
	}
	if got.Pins.HiddenVariantID == nil || *got.Pins.HiddenVariantID != "opaque-variant-1" {
		t.Fatal("opaque variant ID must be preserved without contents")
	}
}

func TestFiltersAndGitOps(t *testing.T) {
	if err := ValidateFilters(Filters{Phase: "agent_execution"}); err != nil {
		t.Fatal(err)
	}
	if err := ValidateFilters(Filters{Phase: "bogus"}); err == nil {
		t.Fatal("invalid phase accepted")
	}
	if err := ValidateFilters(Filters{EventType: "trial_started"}); err != nil {
		t.Fatal(err)
	}
	// GitOps: unavailable tasks must not fabricate evidence — projector emits
	// no rollout/state events without underlying observation payloads.
	run := testRun()
	got := Project(run, nil, nil, EvaluationInfo{}, TrialMeta{})
	for _, event := range got.Events {
		if event.EventType == EventStateChange {
			t.Fatal("fabricated gitops/state evidence")
		}
	}
}

func countType(trajectory Trajectory, eventType string) int {
	count := 0
	for _, event := range trajectory.Events {
		if event.EventType == eventType {
			count++
		}
	}
	return count
}

func strPtr(value string) *string { return &value }
