package trajectory

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/evalplane/evalplane/backend/internal/evidence"
	"github.com/evalplane/evalplane/backend/internal/failuretaxonomy"
)

// RunInfo is the minimal run row the projector needs.
type RunInfo struct {
	ID                string
	BatchID           string
	WorkspaceID       string
	AgentVersionID    string
	ScenarioVersionID string
	AttemptNumber     int
	Status            string
	EnvironmentRef    *string
	CreatedAt         time.Time
	StartedAt         *time.Time
	CompletedAt       *time.Time
}

// EvaluationInfo is the minimal persisted evaluation the projector needs.
type EvaluationInfo struct {
	Found                bool
	ID                   string
	Result               string
	EvaluatorVersion     string
	RecoveryDurationMS   *int64
	AvailabilityPct      *float64
	PeakErrorRatePct     *float64
	SafetyViolationCount int
	Criteria             []evidence.CriterionResult
	Findings             []evidence.Finding
	CompletedAt          *time.Time
}

// TrialMeta carries suite/variant/environment pins without hidden contents.
type TrialMeta struct {
	SuiteExecutionID         *string
	SuiteVersionID           *string
	VariantID                *string
	VariantSeed              *int64
	TrialOrdinal             int
	EnvironmentProfileID     *string
	EnvironmentProviderID    *string
	EnvironmentIsolationTier *string
	RuntimeContract          string
}

// candidate is an internal merge row before deterministic sorting.
type candidate struct {
	occurredAt time.Time
	priority   int
	sequence   int64
	stableID   string
	event      TrajectoryEvent
}

// Project builds a deterministic safe trajectory from persisted evidence.
// It performs no I/O, runs no LLM, and fabricates nothing: recovery and
// state-change events appear only when evidenced.
func Project(run RunInfo, events []evidence.RunEvent, actions []evidence.AgentAction, evaluation EvaluationInfo, meta TrialMeta) Trajectory {
	start := run.CreatedAt
	if run.StartedAt != nil && !run.StartedAt.IsZero() {
		start = *run.StartedAt
	}
	var rows []candidate

	rows = append(rows, candidate{
		occurredAt: start, priority: SourcePriority("run"), sequence: 0, stableID: "run:" + run.ID,
		event: TrajectoryEvent{
			EventID: "run:" + run.ID + ":started", OccurredAt: start,
			EventType: EventTrialStarted, Phase: PhaseProvisioning, Actor: ActorPlatform,
			Summary: fmt.Sprintf("Trial started (attempt %d, status %s)", run.AttemptNumber, run.Status),
			Status:  StatusInfo, SourceType: "run", SourceID: run.ID,
			EvidenceRefs: []string{"run:" + run.ID}, SafeMetadata: SafeMetadata(map[string]any{
				"attempt_number": run.AttemptNumber, "status": run.Status,
			}), View: "agent_visible",
		},
	})

	byEventID := map[int64][]evidence.AgentAction{}
	for _, action := range actions {
		if action.RunEventID != nil {
			byEventID[*action.RunEventID] = append(byEventID[*action.RunEventID], action)
		}
	}

	observations := 0
	for _, event := range events {
		rows = append(rows, projectRunEvent(run, event, byEventID[event.ID])...)
		if event.EventType == "observation" {
			observations++
		}
	}

	for _, action := range actions {
		rows = append(rows, projectAction(run, action)...)
	}

	if evaluation.Found {
		rows = append(rows, projectEvaluation(run, evaluation)...)
	}

	sort.SliceStable(rows, func(i, j int) bool {
		if !rows[i].occurredAt.Equal(rows[j].occurredAt) {
			return rows[i].occurredAt.Before(rows[j].occurredAt)
		}
		if rows[i].priority != rows[j].priority {
			return rows[i].priority < rows[j].priority
		}
		if rows[i].sequence != rows[j].sequence {
			return rows[i].sequence < rows[j].sequence
		}
		return rows[i].stableID < rows[j].stableID
	})

	out := make([]TrajectoryEvent, 0, len(rows)+1)
	var firstActionAt, firstObservationAt *time.Time
	for _, event := range events {
		if event.EventType == "observation" && firstObservationAt == nil {
			at := event.OccurredAt
			firstObservationAt = &at
		}
	}
	for index, row := range rows {
		row.event.Sequence = int64(index)
		if !start.IsZero() {
			relative := row.event.OccurredAt.Sub(start).Milliseconds()
			row.event.RelativeMS = &relative
		}
		out = append(out, row.event)
	}

	summary, recoveryDetectedAt, recoveryVerifiedAt := buildSummary(run, events, actions, evaluation, observations)
	if len(actions) > 0 {
		var earliest *time.Time
		for _, action := range actions {
			at := action.CreatedAt
			if action.StartedAt != nil && !action.StartedAt.IsZero() {
				at = *action.StartedAt
			}
			if earliest == nil || at.Before(*earliest) {
				copy := at
				earliest = &copy
			}
		}
		firstActionAt = earliest
	}
	durations := buildDurations(run, events, firstActionAt, summary.TimeToRecoveryMS)

	if recoveryDetectedAt == nil {
		recoveryDetectedAt = detectRecovery(run, events, evaluation)
	}
	if recoveryDetectedAt != nil {
		summary.TimeToRecoveryMS = diffMS(start, *recoveryDetectedAt)
		summary.ActionsBeforeRecovery, summary.ActionsAfterRecovery = splitActionsByRecovery(actions, *recoveryDetectedAt)
		recoveryVerifiedAt = detectRecoveryVerified(events, *recoveryDetectedAt)
		summary.RecoveryVerified = recoveryVerifiedAt != nil
	} else {
		summary.ActionsBeforeRecovery = len(actions)
	}

	out = append(out, TrajectoryEvent{
		EventID: "run:" + run.ID + ":completed", Sequence: int64(len(rows)),
		OccurredAt: valueOrNow(run.CompletedAt), EventType: EventTrialCompleted,
		Phase: PhaseCleanup, Actor: ActorPlatform,
		Summary: fmt.Sprintf("Trial completed with status %s", run.Status),
		Status:  StatusInfo, SourceType: "run", SourceID: run.ID,
		EvidenceRefs: []string{"run:" + run.ID},
		SafeMetadata: SafeMetadata(map[string]any{"status": run.Status}),
		View:         "agent_visible",
	})
	if !start.IsZero() {
		relative := out[len(out)-1].OccurredAt.Sub(start).Milliseconds()
		out[len(out)-1].RelativeMS = &relative
	}

	ordinal := meta.TrialOrdinal
	if ordinal == 0 {
		ordinal = run.AttemptNumber
	}
	runtimeContract := meta.RuntimeContract
	if runtimeContract == "" {
		runtimeContract = "runtime-contract:v1"
	}
	evaluatorContract := "deterministic-evaluator:v1"
	if evaluation.Found && evaluation.EvaluatorVersion != "" {
		evaluatorContract = evaluation.EvaluatorVersion
	}
	return Trajectory{
		Contract: ContractVersion, RunID: run.ID, WorkspaceID: run.WorkspaceID,
		OrderingRule: OrderingRule, Events: out, Summary: summary, Durations: durations,
		Pins: ReproPins{
			AgentVersionID: run.AgentVersionID, ScenarioVersionID: run.ScenarioVersionID,
			SuiteExecutionID: meta.SuiteExecutionID, SuiteVersionID: meta.SuiteVersionID,
			HiddenVariantID: meta.VariantID, RuntimeContract: runtimeContract,
			EvaluatorContract: evaluatorContract, TrajectoryContract: ContractVersion,
			EnvironmentProfileID: meta.EnvironmentProfileID, EnvironmentProviderID: meta.EnvironmentProviderID,
			EnvironmentIsolationTier: meta.EnvironmentIsolationTier, TrialOrdinal: ordinal,
		},
		RecoveryDetectedAt: recoveryDetectedAt, RecoveryVerifiedAt: recoveryVerifiedAt,
		TotalEvents: len(out),
	}
}

func projectRunEvent(run RunInfo, event evidence.RunEvent, linked []evidence.AgentAction) []candidate {
	var payload map[string]any
	_ = json.Unmarshal(nullObject(event.Payload), &payload)
	if payload == nil {
		payload = map[string]any{}
	}
	code, _ := payload["code"].(string)
	if code == "" {
		code = event.Summary
	}
	ref := "run_event:" + strconv.FormatInt(event.ID, 10)
	safe := map[string]any{"code": code}
	for _, key := range []string{"step", "action_id", "status", "denial_code"} {
		if value, ok := payload[key]; ok {
			safe[key] = value
		}
	}

	switch event.EventType {
	case "scenario":
		return []candidate{{occurredAt: event.OccurredAt, priority: SourcePriority("run"), sequence: event.Sequence, stableID: ref, event: TrajectoryEvent{
			EventID: ref, OccurredAt: event.OccurredAt, EventType: EventEnvironmentSetup,
			Phase: PhaseSetup, Actor: ActorPlatform, Summary: safeSummary("Scenario setup: "+code, event.Summary),
			Status: StatusInfo, SourceType: "observation", SourceID: ref,
			EvidenceRefs: []string{ref}, SafeMetadata: SafeMetadata(safe), View: "agent_visible",
		}}}
	case "observation":
		rows := []candidate{{occurredAt: event.OccurredAt, priority: SourcePriority("observation"), sequence: event.Sequence, stableID: ref, event: TrajectoryEvent{
			EventID: ref, OccurredAt: event.OccurredAt, EventType: EventObservation,
			Phase: PhaseAgentExecution, Actor: ActorEnvironment, Summary: observationSummary(payload, event.Summary),
			Status: StatusSuccess, SourceType: "observation", SourceID: ref,
			EvidenceRefs: []string{ref}, SafeMetadata: observationMetadata(payload), View: "agent_visible",
		}}}
		if hasTelemetry(payload) {
			rows = append(rows, candidate{occurredAt: event.OccurredAt, priority: SourcePriority("observation"), sequence: event.Sequence, stableID: ref + ":telemetry", event: TrajectoryEvent{
				EventID: ref + ":telemetry", OccurredAt: event.OccurredAt, EventType: EventTelemetryObservation,
				Phase: PhaseAgentExecution, Actor: ActorEnvironment, Summary: telemetrySummary(payload),
				Status: StatusSuccess, SourceType: "observation", SourceID: ref,
				EvidenceRefs: []string{ref}, SafeMetadata: telemetryMetadata(payload), View: "agent_visible",
			}})
		}
		if change, ok := deriveStateChange(payload, ref); ok {
			rows = append(rows, candidate{occurredAt: event.OccurredAt, priority: SourcePriority("observation"), sequence: event.Sequence, stableID: ref + ":state", event: change})
		}
		return rows
	case "action":
		// Raw action events are superseded by the action-lifecycle projection
		// below; keep a requested marker only when no action row links here.
		if len(linked) > 0 {
			return nil
		}
		return []candidate{{occurredAt: event.OccurredAt, priority: SourcePriority("action"), sequence: event.Sequence, stableID: ref, event: TrajectoryEvent{
			EventID: ref, OccurredAt: event.OccurredAt, EventType: EventAgentActionRequested,
			Phase: PhaseAgentExecution, Actor: ActorAgent, Summary: safeSummary("Agent action requested: "+code, event.Summary),
			Status: StatusInfo, SourceType: "action", SourceID: ref,
			EvidenceRefs: []string{ref}, SafeMetadata: SafeMetadata(safe), View: "agent_visible",
		}}}
	case "decision":
		return []candidate{{occurredAt: event.OccurredAt, priority: SourcePriority("action"), sequence: event.Sequence, stableID: ref, event: TrajectoryEvent{
			EventID: ref, OccurredAt: event.OccurredAt, EventType: EventAgentActionRequested,
			Phase: PhaseAgentExecution, Actor: ActorAgent, Summary: safeSummary("Agent decision: "+code, event.Summary),
			Status: StatusInfo, SourceType: "action", SourceID: ref,
			EvidenceRefs: []string{ref}, SafeMetadata: SafeMetadata(safe), View: "agent_visible",
		}}}
	case "safety":
		return []candidate{{occurredAt: event.OccurredAt, priority: SourcePriority("safety"), sequence: event.Sequence, stableID: ref, event: TrajectoryEvent{
			EventID: ref, OccurredAt: event.OccurredAt, EventType: EventSafetyFinding,
			Phase: PhaseAgentExecution, Actor: ActorEvaluator, Summary: safetySummary(payload, event.Summary),
			Status: StatusDenied, SourceType: "safety", SourceID: ref,
			EvidenceRefs: append([]string{ref}, actionRef(payload)...), SafeMetadata: SafeMetadata(safe), View: "evaluator",
		}}}
	case "recovery":
		// Stage-34 integrity: worker/execution lifecycle recovery must never
		// project as service recovery. Only closed application-recovery codes
		// project as recovery_detected/verified (actor evaluator). All other
		// recovery-type evidence (worker_* lifecycle, unknown) remains visible
		// as platform/execution evidence, never as service recovery.
		if IsApplicationRecoveryVerifiedCode(code) {
			return []candidate{{occurredAt: event.OccurredAt, priority: SourcePriority("recovery"), sequence: event.Sequence, stableID: ref, event: TrajectoryEvent{
				EventID: ref, OccurredAt: event.OccurredAt, EventType: EventRecoveryVerified,
				Phase: PhaseVerification, Actor: ActorEvaluator, Summary: safeSummary("Recovery: "+code, event.Summary),
				Status: StatusSuccess, SourceType: "recovery", SourceID: ref,
				EvidenceRefs: []string{ref}, SafeMetadata: SafeMetadata(safe), View: "evaluator",
			}}}
		}
		if IsApplicationRecoveryDetectedCode(code) {
			return []candidate{{occurredAt: event.OccurredAt, priority: SourcePriority("recovery"), sequence: event.Sequence, stableID: ref, event: TrajectoryEvent{
				EventID: ref, OccurredAt: event.OccurredAt, EventType: EventRecoveryDetected,
				Phase: PhaseVerification, Actor: ActorEvaluator, Summary: safeSummary("Recovery: "+code, event.Summary),
				Status: StatusSuccess, SourceType: "recovery", SourceID: ref,
				EvidenceRefs: []string{ref}, SafeMetadata: SafeMetadata(safe), View: "evaluator",
			}}}
		}
		// Stage-34 correction: never infer platform-invalid from the worker_*
		// prefix or the recovery event_type alone. The persisted
		// execution/platform classification remains authoritative: only
		// platform-classified evidence projects as platform-invalid.
		// Unknown/future worker_* without platform classification stays
		// visible as safe worker lifecycle evidence (info, no invalid
		// status, no agent failure, no service recovery).
		if isPlatformClassifiedEvent(payload, code) {
			summary := "Recovery: " + code
			if isWorkerLifecycleCode(code) {
				summary = "Worker recovery: " + code
			}
			return []candidate{{occurredAt: event.OccurredAt, priority: SourcePriority("platform"), sequence: event.Sequence, stableID: ref, event: TrajectoryEvent{
				EventID: ref, OccurredAt: event.OccurredAt, EventType: EventPlatformInvalid,
				Phase: PhaseEvaluation, Actor: ActorPlatform, Summary: safeSummary(summary, event.Summary),
				Status: StatusInvalid, SourceType: "platform", SourceID: ref,
				EvidenceRefs: []string{ref}, SafeMetadata: SafeMetadata(safe), View: "evaluator",
			}}}
		}
		if isWorkerLifecycleCode(code) {
			// Final correction: worker/execution lifecycle is internal
			// platform evidence. Without persisted evidence the agent
			// received it, it must not be labeled agent_visible. Keep it
			// visible as safe lifecycle evidence on the evaluator side.
			return []candidate{{occurredAt: event.OccurredAt, priority: SourcePriority("cleanup"), sequence: event.Sequence, stableID: ref, event: TrajectoryEvent{
				EventID: ref, OccurredAt: event.OccurredAt, EventType: EventCleanupStarted,
				Phase: PhaseCleanup, Actor: ActorPlatform, Summary: safeSummary("Worker lifecycle: "+code, event.Summary),
				Status: StatusInfo, SourceType: "cleanup", SourceID: ref,
				EvidenceRefs: []string{ref}, SafeMetadata: SafeMetadata(safe), View: "evaluator",
			}}}
		}
		return []candidate{{occurredAt: event.OccurredAt, priority: SourcePriority("cleanup"), sequence: event.Sequence, stableID: ref, event: TrajectoryEvent{
			EventID: ref, OccurredAt: event.OccurredAt, EventType: EventCleanupStarted,
			Phase: PhaseCleanup, Actor: ActorPlatform, Summary: safeSummary("Recovery: "+code, event.Summary),
			Status: StatusInfo, SourceType: "cleanup", SourceID: ref,
			EvidenceRefs: []string{ref}, SafeMetadata: SafeMetadata(safe), View: "agent_visible",
		}}}
	case "evaluation":
		return []candidate{{occurredAt: event.OccurredAt, priority: SourcePriority("evaluation"), sequence: event.Sequence, stableID: ref, event: TrajectoryEvent{
			EventID: ref, OccurredAt: event.OccurredAt, EventType: EventEvaluationCompleted,
			Phase: PhaseEvaluation, Actor: ActorEvaluator, Summary: safeSummary("Evaluation: "+code, event.Summary),
			Status: StatusInfo, SourceType: "evaluation", SourceID: ref,
			EvidenceRefs: []string{ref}, SafeMetadata: SafeMetadata(safe), View: "evaluator",
		}}}
	case "system":
		return []candidate{projectSystemEvent(run, event, ref, code, safe)}
	default:
		// Unknown persisted types (e.g. mcp tool records) project as safe
		// structured tool events without bearer material.
		actor := ActorTool
		if event.EventType == "mcp" {
			actor = ActorTool
		} else {
			actor = ActorPlatform
		}
		return []candidate{{occurredAt: event.OccurredAt, priority: SourcePriority("tool"), sequence: event.Sequence, stableID: ref, event: TrajectoryEvent{
			EventID: ref, OccurredAt: event.OccurredAt, EventType: toolEventType(code),
			Phase: toolPhase(code), Actor: actor, Summary: safeSummary("Tool event: "+code, event.Summary),
			Status: toolStatus(code), SourceType: "tool", SourceID: ref,
			EvidenceRefs: []string{ref}, SafeMetadata: SafeMetadata(safe), View: "agent_visible",
		}}}
	}
}

func projectSystemEvent(run RunInfo, event evidence.RunEvent, ref, code string, safe map[string]any) candidate {
	eventType := EventTrialStarted
	phase := PhaseProvisioning
	actor := ActorPlatform
	status := StatusInfo
	summaryPrefix := "Platform: "
	// Final correction: worker/execution lifecycle without platform
	// classification stays visible but must not claim agent_visible.
	// Explicit override to the existing evaluator side; empty means
	// fall back to viewFor(eventType).
	viewOverride := ""
	switch code {
	case "run_claimed":
		eventType = EventTrialStarted
		phase = PhaseProvisioning
	case "environment_provisioning_started":
		eventType = EventEnvironmentProvisioningStarted
		phase = PhaseProvisioning
	case "environment_ready":
		eventType = EventEnvironmentReady
		phase = PhaseProvisioning
	case "scenario_started":
		eventType = EventEnvironmentSetup
		phase = PhaseSetup
	case "agent_step_started", "agent_step_completed", "observation_collected", "post_action_observation_collected", "final_observation_collected":
		eventType = EventObservation
		phase = PhaseAgentExecution
		actor = ActorEnvironment
		status = StatusSuccess
	case "evaluation_completed":
		eventType = EventEvaluationCompleted
		phase = PhaseEvaluation
		actor = ActorEvaluator
	case "cleanup_started":
		eventType = EventCleanupStarted
		phase = PhaseCleanup
	case "cleanup_completed", "cleanup_failed":
		eventType = EventCleanupCompleted
		phase = PhaseCleanup
		if code == "cleanup_failed" {
			status = StatusFailed
		} else {
			status = StatusSuccess
		}
	case "runtime_failure", "platform_failure", "environment_failure", "environment_provisioning_failed":
		eventType = EventPlatformInvalid
		phase = PhaseEvaluation
		actor = ActorEvaluator
		status = StatusInvalid
	case "worker_lease_expired", "run_abandoned", "worker_recovery_started", "worker_recovery_completed", "worker_recovery_cleanup_failed":
		// Stage-34 correction: never infer platform-invalid solely from a
		// worker lifecycle code. Persisted classification is authoritative.
		var payload map[string]any
		_ = json.Unmarshal(nullObject(event.Payload), &payload)
		if payload == nil {
			payload = map[string]any{}
		}
		if isPlatformClassifiedEvent(payload, code) {
			eventType = EventPlatformInvalid
			phase = PhaseEvaluation
			actor = ActorPlatform
			status = StatusInvalid
			summaryPrefix = "Worker recovery: "
		} else {
			eventType = EventCleanupStarted
			phase = PhaseCleanup
			actor = ActorPlatform
			status = StatusInfo
			summaryPrefix = "Worker lifecycle: "
			viewOverride = "evaluator"
		}
	default:
		if isWorkerLifecycleCode(code) {
			// Future/unknown worker_* without platform classification stays
			// visible as safe lifecycle evidence: no platform_invalid, no
			// invalid status, no failure. Display-only prefix use. Internal
			// platform evidence: evaluator view, never agent_visible.
			eventType = EventCleanupStarted
			phase = PhaseCleanup
			actor = ActorPlatform
			status = StatusInfo
			summaryPrefix = "Worker lifecycle: "
			viewOverride = "evaluator"
		} else if strings.Contains(code, "cleanup") {
			eventType = EventCleanupStarted
			phase = PhaseCleanup
		} else if strings.Contains(code, "environment") {
			eventType = EventEnvironmentSetup
			phase = PhaseSetup
		} else {
			eventType = EventTrialStarted
			phase = PhaseProvisioning
		}
	}
	_ = run
	view := viewFor(eventType)
	if viewOverride != "" {
		view = viewOverride
	}
	return candidate{occurredAt: event.OccurredAt, priority: SourcePriority(sourceFor(eventType)), sequence: event.Sequence, stableID: ref, event: TrajectoryEvent{
		EventID: ref, OccurredAt: event.OccurredAt, EventType: eventType,
		Phase: phase, Actor: actor, Summary: safeSummary(summaryPrefix+code, event.Summary),
		Status: status, SourceType: sourceFor(eventType), SourceID: ref,
		EvidenceRefs: []string{ref}, SafeMetadata: SafeMetadata(safe), View: view,
	}}
}

func projectAction(run RunInfo, action evidence.AgentAction) []candidate {
	ref := "agent_action:" + action.ID
	target := ""
	if action.TargetRef != nil && *action.TargetRef != "" {
		target = " → " + *action.TargetRef
	}
	base := TrajectoryEvent{
		SourceType: "action", SourceID: ref,
		EvidenceRefs: []string{ref},
		View:         "agent_visible",
	}
	var request map[string]any
	_ = json.Unmarshal(nullObject(action.RequestPayload), &request)
	safeRequest := RedactObject(orEmpty(request))
	safeMeta := map[string]any{"action_type": action.ActionType, "authorized": action.Authorized, "status": action.Status}
	if target != "" {
		safeMeta["target"] = strings.TrimPrefix(target, " → ")
	}
	if actionType, ok := safeRequest["type"]; ok {
		safeMeta["requested_type"] = actionType
	}
	at := action.CreatedAt
	if action.StartedAt != nil && !action.StartedAt.IsZero() {
		at = *action.StartedAt
	}
	_ = run
	requested := candidate{occurredAt: at, priority: SourcePriority("action"), sequence: 0, stableID: ref + ":requested", event: TrajectoryEvent{
		EventID: ref + ":requested", OccurredAt: at, EventType: EventAgentActionRequested,
		Phase: PhaseAgentExecution, Actor: ActorAgent,
		Summary: fmt.Sprintf("Agent requested: %s%s", action.ActionType, target),
		Status:  StatusInfo, SourceType: base.SourceType, SourceID: base.SourceID,
		EvidenceRefs: base.EvidenceRefs, SafeMetadata: SafeMetadata(safeMeta), View: "agent_visible",
	}}
	switch action.Status {
	case "denied":
		reason := denialReason(action.ResultPayload)
		return []candidate{requested, {occurredAt: completedOr(at, action.CompletedAt), priority: SourcePriority("safety"), sequence: 0, stableID: ref + ":denied", event: TrajectoryEvent{
			EventID: ref + ":denied", OccurredAt: completedOr(at, action.CompletedAt), EventType: EventAgentActionDenied,
			Phase: PhaseAgentExecution, Actor: ActorPlatform,
			Summary: fmt.Sprintf("DENIED: %s%s — %s", action.ActionType, target, reason),
			Status:  StatusDenied, SourceType: "safety", SourceID: ref,
			EvidenceRefs: base.EvidenceRefs, SafeMetadata: SafeMetadata(safeMeta), View: "agent_visible",
		}}}
	case "executed", "allowed":
		eventType := EventAgentActionExecuted
		if action.Status == "allowed" {
			eventType = EventAgentActionRequested
		}
		status := StatusSuccess
		summary := fmt.Sprintf("Executed: %s%s", action.ActionType, target)
		if action.Status == "allowed" {
			summary = fmt.Sprintf("Authorized: %s%s (awaiting execution record)", action.ActionType, target)
			status = StatusInfo
		}
		return []candidate{requested, {occurredAt: completedOr(at, action.CompletedAt), priority: SourcePriority("action"), sequence: 0, stableID: ref + ":executed", event: TrajectoryEvent{
			EventID: ref + ":executed", OccurredAt: completedOr(at, action.CompletedAt), EventType: eventType,
			Phase: PhaseAgentExecution, Actor: ActorEnvironment,
			Summary: summary, Status: status, SourceType: "action", SourceID: ref,
			EvidenceRefs: base.EvidenceRefs, SafeMetadata: SafeMetadata(safeMeta), View: "agent_visible",
		}}}
	case "failed":
		return []candidate{requested, {occurredAt: completedOr(at, action.CompletedAt), priority: SourcePriority("action"), sequence: 0, stableID: ref + ":failed", event: TrajectoryEvent{
			EventID: ref + ":failed", OccurredAt: completedOr(at, action.CompletedAt), EventType: EventAgentActionFailed,
			Phase: PhaseAgentExecution, Actor: ActorEnvironment,
			Summary: fmt.Sprintf("FAILED: %s%s", action.ActionType, target),
			Status:  StatusFailed, SourceType: "action", SourceID: ref,
			EvidenceRefs: base.EvidenceRefs, SafeMetadata: SafeMetadata(safeMeta), View: "agent_visible",
		}}}
	default: // proposed and any future status: requested only, never rendered as executed.
		return []candidate{requested}
	}
}

func projectEvaluation(run RunInfo, evaluation EvaluationInfo) []candidate {
	_ = run
	var rows []candidate
	now := valueOrNow(evaluation.CompletedAt)
	rows = append(rows, candidate{occurredAt: now, priority: SourcePriority("evaluation"), sequence: 1 << 60, stableID: "evaluation:" + evaluation.ID + ":started", event: TrajectoryEvent{
		EventID: "evaluation:" + evaluation.ID + ":started", OccurredAt: now, EventType: EventEvaluationStarted,
		Phase: PhaseEvaluation, Actor: ActorEvaluator, Summary: "Deterministic evaluation started",
		Status: StatusInfo, SourceType: "evaluation", SourceID: "evaluation:" + evaluation.ID,
		EvidenceRefs: []string{"evaluation:" + evaluation.ID}, SafeMetadata: SafeMetadata(map[string]any{"evaluator_version": evaluation.EvaluatorVersion}), View: "evaluator",
	}})
	for _, criterion := range evaluation.Criteria {
		id := "criterion:" + criterion.ID
		status := StatusSuccess
		if !criterion.Passed {
			status = StatusFailed
		}
		rows = append(rows, candidate{occurredAt: now, priority: SourcePriority("evaluation"), sequence: 1<<60 + 1, stableID: id, event: TrajectoryEvent{
			EventID: id, OccurredAt: now, EventType: EventEvaluationFinding,
			Phase: PhaseEvaluation, Actor: ActorEvaluator,
			Summary: fmt.Sprintf("Criterion %s: %s", criterion.Key, passWord(criterion.Passed)),
			Status:  status, SourceType: "evaluation", SourceID: "evaluation:" + evaluation.ID,
			EvidenceRefs: []string{"evaluation:" + evaluation.ID, id},
			SafeMetadata: SafeMetadata(map[string]any{"criterion_key": criterion.Key, "passed": criterion.Passed}),
			View:         "evaluator",
		}})
	}
	for _, finding := range evaluation.Findings {
		eventType := EventEvaluationFinding
		if finding.Kind == "safety" {
			eventType = EventSafetyFinding
		}
		code := findingCode(finding.Evidence)
		if isPlatformCode(code) {
			eventType = EventPlatformInvalid
		}
		rows = append(rows, candidate{occurredAt: now, priority: SourcePriority(sourceFor(eventType)), sequence: 1<<60 + 2, stableID: "finding:" + finding.ID, event: TrajectoryEvent{
			EventID: "finding:" + finding.ID, OccurredAt: now, EventType: eventType,
			Phase: PhaseEvaluation, Actor: ActorEvaluator,
			Summary: safeSummary(finding.Summary, finding.Summary),
			Status:  findingStatus(finding.Kind, code), SourceType: sourceFor(eventType), SourceID: "finding:" + finding.ID,
			EvidenceRefs: []string{"evaluation:" + evaluation.ID, "finding:" + finding.ID},
			SafeMetadata: SafeMetadata(map[string]any{"kind": finding.Kind, "code": code}),
			View:         "evaluator",
		}})
	}
	status := StatusSuccess
	if evaluation.Result == "fail" {
		status = StatusFailed
	} else if evaluation.Result == "inconclusive" || evaluation.Result == "stopped" {
		status = StatusInvalid
	}
	rows = append(rows, candidate{occurredAt: now.Add(time.Millisecond), priority: SourcePriority("evaluation"), sequence: 1<<60 + 3, stableID: "evaluation:" + evaluation.ID + ":completed", event: TrajectoryEvent{
		EventID: "evaluation:" + evaluation.ID + ":completed", OccurredAt: now.Add(time.Millisecond), EventType: EventEvaluationCompleted,
		Phase: PhaseEvaluation, Actor: ActorEvaluator,
		Summary: fmt.Sprintf("Evaluation completed: %s", strings.ToUpper(evaluation.Result)),
		Status:  status, SourceType: "evaluation", SourceID: "evaluation:" + evaluation.ID,
		EvidenceRefs: []string{"evaluation:" + evaluation.ID}, SafeMetadata: SafeMetadata(map[string]any{"result": evaluation.Result}), View: "evaluator",
	}})
	return rows
}

func buildSummary(run RunInfo, events []evidence.RunEvent, actions []evidence.AgentAction, evaluation EvaluationInfo, observations int) (OutcomeSummary, *time.Time, *time.Time) {
	requested := len(actions)
	executed, denied := 0, 0
	for _, action := range actions {
		switch action.Status {
		case "executed":
			executed++
		case "denied":
			denied++
		}
	}
	summary := OutcomeSummary{
		TrialStatus: run.Status, EvaluationStatus: "", Validity: "pending",
		PassFailOutcome: "unknown", FailureClasses: []string{},
		ActionsRequested: requested, ActionsExecuted: executed, ActionsDenied: denied,
		ObservationsCount: observations,
	}
	if evaluation.Found {
		summary.EvaluationStatus = evaluation.Result
		summary.FindingCount = len(evaluation.Findings) + len(evaluation.Criteria)
		summary.SafetyViolationCount = evaluation.SafetyViolationCount
		switch evaluation.Result {
		case "pass":
			summary.PassFailOutcome = "pass"
		case "fail":
			summary.PassFailOutcome = "fail"
		default:
			summary.PassFailOutcome = evaluation.Result
		}
		classes := map[string]bool{}
		for _, finding := range evaluation.Findings {
			code := findingCode(finding.Evidence)
			if code == "" {
				continue
			}
			classes[failuretaxonomy.Category(code)] = true
		}
		for _, criterion := range evaluation.Criteria {
			if !criterion.Passed {
				classes[failuretaxonomy.CategoryCriterion] = true
			}
		}
		for class := range classes {
			summary.FailureClasses = append(summary.FailureClasses, class)
		}
		sort.Strings(summary.FailureClasses)
		summary.Validity = trialValidity(run, events, evaluation)
	} else {
		summary.Validity = trialValidity(run, events, evaluation)
	}
	return summary, detectRecovery(run, events, evaluation), nil
}

func trialValidity(run RunInfo, events []evidence.RunEvent, evaluation EvaluationInfo) string {
	platform := false
	for _, event := range events {
		var payload map[string]any
		_ = json.Unmarshal(nullObject(event.Payload), &payload)
		classification, _ := payload["classification"].(string)
		code, _ := payload["code"].(string)
		if classification == "platform" && code != "" && code != "cleanup_completed" {
			platform = true
		}
	}
	if evaluation.Found {
		for _, finding := range evaluation.Findings {
			if code := findingCode(finding.Evidence); code == "runtime_failure" || code == "telemetry_unavailable" {
				platform = true
			}
		}
	}
	terminal := run.Status == "completed" || run.Status == "failed" || run.Status == "stopped"
	if platform && (evaluation.Found || terminal) {
		return "invalid_platform"
	}
	if evaluation.Found {
		return "valid"
	}
	return "pending"
}

// Stage-34 integrity: closed application-recovery vocabulary.
//
// Only explicit persisted evidence whose semantics mean task/service recovery
// may satisfy service-recovery fields. Worker/execution lifecycle codes
// (worker_*) must never satisfy them. Matching is exact (closed set), never
// substring-based: a generic string containing "recovery" does not count.

// IsApplicationRecoveryDetectedCode reports whether a persisted code means
// application/service recovery was detected (including explicit verification,
// which implies detection for earliest-time purposes).
func IsApplicationRecoveryDetectedCode(code string) bool {
	switch code {
	case "recovery_detected", "recovery_criteria_satisfied", "recovery_verified":
		return true
	}
	return false
}

// IsApplicationRecoveryVerifiedCode reports whether a persisted code means
// application/service recovery was explicitly verified.
func IsApplicationRecoveryVerifiedCode(code string) bool {
	return code == "recovery_verified"
}

// IsWorkerRecoveryCode reports whether a persisted code is one of the known
// Stage-18 worker/execution lifecycle recovery codes. These events remain
// visible as worker lifecycle evidence but must never satisfy service-recovery
// fields. Exact matching only: never use prefix matching as a
// failure-classification rule.
func IsWorkerRecoveryCode(code string) bool {
	switch code {
	case "worker_lease_expired", "run_abandoned", "worker_recovery_started", "worker_recovery_completed", "worker_recovery_cleanup_failed":
		return true
	}
	return false
}

// isWorkerLifecycleCode identifies an event as belonging to the
// worker/platform lifecycle for display purposes only (e.g. "Worker
// lifecycle: <code>" summaries). Prefix matching is allowed here solely as a
// display hint for unknown/future worker_* codes; it must never drive
// failure classification (platform_invalid/invalid status), service-recovery
// fields, or trial validity. Classification remains authoritative via
// isPlatformClassifiedEvent.
func isWorkerLifecycleCode(code string) bool {
	if IsWorkerRecoveryCode(code) {
		return true
	}
	return strings.HasPrefix(code, "worker_")
}

// isPlatformClassifiedEvent reports whether persisted evidence says the event
// is platform-class per current validity semantics (mirrors trialValidity:
// payload classification=platform with a non-empty, non-cleanup_completed
// code). Only platform-classified events may project as platform-invalid.
func isPlatformClassifiedEvent(payload map[string]any, code string) bool {
	if code == "" || code == "cleanup_completed" {
		return false
	}
	classification, _ := payload["classification"].(string)
	return classification == "platform"
}

func detectRecovery(run RunInfo, events []evidence.RunEvent, evaluation EvaluationInfo) *time.Time {
	_ = run
	var earliest *time.Time
	for _, event := range events {
		var payload map[string]any
		_ = json.Unmarshal(nullObject(event.Payload), &payload)
		// Explicit persisted code only: never infer from free-form summary
		// strings containing "recovery". Closed exact matching below.
		code, _ := payload["code"].(string)
		// Closed exact matching only. Generic recovery-type events and
		// worker lifecycle codes never satisfy service recovery.
		if !IsApplicationRecoveryDetectedCode(code) {
			continue
		}
		at := event.OccurredAt
		if earliest == nil || at.Before(*earliest) {
			copy := at
			earliest = &copy
		}
	}
	if earliest != nil {
		return earliest
	}
	// No evaluator evidence, no invented timestamp.
	if evaluation.Found && evaluation.RecoveryDurationMS != nil && !run.CreatedAt.IsZero() {
		at := run.CreatedAt.Add(time.Duration(*evaluation.RecoveryDurationMS) * time.Millisecond)
		return &at
	}
	return nil
}

func detectRecoveryVerified(events []evidence.RunEvent, detected time.Time) *time.Time {
	for _, event := range events {
		var payload map[string]any
		_ = json.Unmarshal(nullObject(event.Payload), &payload)
		// Explicit persisted code only: worker_recovery_completed and generic
		// recovery strings must never verify service recovery. Closed exact
		// matching, no substring classification, no summary inference.
		code, _ := payload["code"].(string)
		if !IsApplicationRecoveryVerifiedCode(code) {
			continue
		}
		if !event.OccurredAt.Before(detected) {
			at := event.OccurredAt
			return &at
		}
	}
	return nil
}

func splitActionsByRecovery(actions []evidence.AgentAction, recovery time.Time) (before, after int) {
	for _, action := range actions {
		at := action.CreatedAt
		if action.StartedAt != nil && !action.StartedAt.IsZero() {
			at = *action.StartedAt
		}
		if at.After(recovery) {
			after++
		} else {
			before++
		}
	}
	return before, after
}

func buildDurations(run RunInfo, events []evidence.RunEvent, firstActionAt *time.Time, recoveryMS *int64) DurationMetrics {
	var out DurationMetrics
	if run.StartedAt != nil && run.CompletedAt != nil && !run.StartedAt.IsZero() && !run.CompletedAt.IsZero() {
		value := run.CompletedAt.Sub(*run.StartedAt).Milliseconds()
		out.TrialDurationMS = &value
	} else if !run.CreatedAt.IsZero() && run.CompletedAt != nil && !run.CompletedAt.IsZero() {
		value := run.CompletedAt.Sub(run.CreatedAt).Milliseconds()
		out.TrialDurationMS = &value
	}
	start := run.CreatedAt
	if run.StartedAt != nil && !run.StartedAt.IsZero() {
		start = *run.StartedAt
	}
	var provisioningStart, readyAt *time.Time
	var firstAgentEvent, lastAgentEvent *time.Time
	var evaluationAt *time.Time
	for _, event := range events {
		var payload map[string]any
		_ = json.Unmarshal(nullObject(event.Payload), &payload)
		code, _ := payload["code"].(string)
		if code == "" {
			code = event.Summary
		}
		switch code {
		case "environment_provisioning_started":
			at := event.OccurredAt
			provisioningStart = &at
		case "environment_ready":
			at := event.OccurredAt
			readyAt = &at
		case "evaluation_completed":
			at := event.OccurredAt
			evaluationAt = &at
		}
		if event.EventType == "observation" || event.EventType == "action" || event.EventType == "decision" {
			at := event.OccurredAt
			if firstAgentEvent == nil || at.Before(*firstAgentEvent) {
				copy := at
				firstAgentEvent = &copy
			}
			if lastAgentEvent == nil || at.After(*lastAgentEvent) {
				copy := at
				lastAgentEvent = &copy
			}
		}
	}
	if provisioningStart != nil && readyAt != nil && !readyAt.Before(*provisioningStart) {
		value := readyAt.Sub(*provisioningStart).Milliseconds()
		out.EnvironmentSetupDurationMS = &value
	}
	if firstAgentEvent != nil && lastAgentEvent != nil {
		value := lastAgentEvent.Sub(*firstAgentEvent).Milliseconds()
		out.AgentActiveDurationMS = &value
	}
	if firstActionAt != nil && !start.IsZero() && !firstActionAt.Before(start) {
		value := firstActionAt.Sub(start).Milliseconds()
		out.TimeToFirstActionMS = &value
	}
	out.TimeToRecoveryMS = recoveryMS
	if evaluationAt != nil && lastAgentEvent != nil && !evaluationAt.Before(*lastAgentEvent) {
		value := evaluationAt.Sub(*lastAgentEvent).Milliseconds()
		out.VerificationDurationMS = &value
	}
	return out
}

// Helpers below keep the projection deterministic and leak-free.

func nullObject(raw json.RawMessage) json.RawMessage {
	if len(raw) == 0 {
		return json.RawMessage(`{}`)
	}
	return raw
}

func orEmpty(value map[string]any) map[string]any {
	if value == nil {
		return map[string]any{}
	}
	return value
}

func safeSummary(primary, fallback string) string {
	primary = strings.TrimSpace(primary)
	if primary != "" {
		return truncate(primary, 400)
	}
	return truncate(strings.TrimSpace(fallback), 400)
}

func truncate(text string, limit int) string {
	if len(text) <= limit {
		return text
	}
	return text[:limit]
}

func observationSummary(payload map[string]any, fallback string) string {
	inner, _ := payload["observation"].(map[string]any)
	if inner == nil {
		return safeSummary("Observation collected", fallback)
	}
	parts := []string{}
	if metrics, ok := inner["metrics"].(map[string]any); ok {
		if value, ok := metrics["error_rate_pct"]; ok {
			parts = append(parts, fmt.Sprintf("error_rate %v%%", value))
		}
		if value, ok := metrics["availability_pct"]; ok {
			parts = append(parts, fmt.Sprintf("availability %v%%", value))
		}
	}
	if workloads, ok := inner["workloads"].([]any); ok && len(workloads) > 0 {
		parts = append(parts, fmt.Sprintf("%d workload(s)", len(workloads)))
	}
	if len(parts) == 0 {
		return safeSummary("Observation collected", fallback)
	}
	return "Observed: " + strings.Join(parts, ", ")
}

func observationMetadata(payload map[string]any) json.RawMessage {
	inner, _ := payload["observation"].(map[string]any)
	if inner == nil {
		return SafeMetadata(map[string]any{})
	}
	safe := map[string]any{}
	if metrics, ok := inner["metrics"].(map[string]any); ok {
		allowed := map[string]any{}
		for _, key := range []string{"request_rate_rps", "error_rate_pct", "p95_latency_ms", "availability_pct", "telemetry_available"} {
			if value, ok := metrics[key]; ok {
				allowed[key] = value
			}
		}
		safe["metrics"] = allowed
	}
	if workloads, ok := inner["workloads"].([]any); ok {
		safe["workload_count"] = len(workloads)
	}
	if pods, ok := inner["pods"].([]any); ok {
		safe["pod_count"] = len(pods)
	}
	if step, ok := payload["step"]; ok {
		safe["step"] = step
	}
	return SafeMetadata(safe)
}

func hasTelemetry(payload map[string]any) bool {
	inner, _ := payload["observation"].(map[string]any)
	if inner == nil {
		return false
	}
	metrics, _ := inner["metrics"].(map[string]any)
	if metrics == nil {
		return false
	}
	for _, key := range []string{"request_rate_rps", "error_rate_pct", "p95_latency_ms", "availability_pct"} {
		if _, ok := metrics[key]; ok {
			return true
		}
	}
	return false
}

func telemetrySummary(payload map[string]any) string {
	inner, _ := payload["observation"].(map[string]any)
	if inner == nil {
		return "Telemetry snapshot"
	}
	metrics, _ := inner["metrics"].(map[string]any)
	parts := []string{}
	for _, key := range []string{"request_rate_rps", "error_rate_pct", "p95_latency_ms", "availability_pct"} {
		if value, ok := metrics[key]; ok {
			parts = append(parts, fmt.Sprintf("%s %v", key, value))
		}
	}
	if len(parts) == 0 {
		return "Telemetry snapshot"
	}
	return "Telemetry: " + strings.Join(parts, ", ")
}

func telemetryMetadata(payload map[string]any) json.RawMessage {
	inner, _ := payload["observation"].(map[string]any)
	metrics, _ := inner["metrics"].(map[string]any)
	allowed := map[string]any{}
	for _, key := range []string{"request_rate_rps", "error_rate_pct", "p95_latency_ms", "availability_pct", "telemetry_available"} {
		if value, ok := metrics[key]; ok {
			allowed[key] = value
		}
	}
	return SafeMetadata(map[string]any{"telemetry": allowed})
}

func deriveStateChange(payload map[string]any, ref string) (TrajectoryEvent, bool) {
	inner, _ := payload["observation"].(map[string]any)
	if inner == nil {
		return TrajectoryEvent{}, false
	}
	// Only derive transitions explicitly present in the observation payload;
	// never speculate from adjacent samples.
	changes, _ := inner["state_changes"].([]any)
	if len(changes) == 0 {
		// Backwards-compatible: explicit transition object.
		transition, _ := inner["state_transition"].(map[string]any)
		if transition == nil {
			return TrajectoryEvent{}, false
		}
		changes = []any{transition}
	}
	descriptions := []string{}
	for _, item := range changes {
		entry, _ := item.(map[string]any)
		if entry == nil {
			continue
		}
		from, _ := entry["from"].(string)
		to, _ := entry["to"].(string)
		name, _ := entry["name"].(string)
		if from == "" || to == "" || from == to {
			continue
		}
		if name != "" {
			descriptions = append(descriptions, fmt.Sprintf("%s %s → %s", name, from, to))
		} else {
			descriptions = append(descriptions, fmt.Sprintf("%s → %s", from, to))
		}
	}
	if len(descriptions) == 0 {
		return TrajectoryEvent{}, false
	}
	return TrajectoryEvent{
		EventID: ref + ":state", EventType: EventStateChange, Phase: PhaseVerification,
		Actor: ActorEnvironment, Summary: "State change: " + strings.Join(descriptions, "; "),
		Status: StatusInfo, SourceType: "observation", SourceID: ref,
		EvidenceRefs: []string{ref}, SafeMetadata: SafeMetadata(map[string]any{"transitions": descriptions}),
		View: "agent_visible",
	}, true
}

func safetySummary(payload map[string]any, fallback string) string {
	code, _ := payload["denial_code"].(string)
	if code == "" {
		code, _ = payload["code"].(string)
	}
	actionID, _ := payload["action_id"].(string)
	if actionID != "" {
		return fmt.Sprintf("Safety: action denied (%s)", firstNonEmpty(code, "unauthorized action"))
	}
	return safeSummary("Safety finding: "+code, fallback)
}

func actionRef(payload map[string]any) []string {
	actionID, _ := payload["action_id"].(string)
	if actionID == "" {
		return nil
	}
	return []string{"agent_action:" + actionID}
}

func denialReason(raw json.RawMessage) string {
	var payload map[string]any
	_ = json.Unmarshal(nullObject(raw), &payload)
	code, _ := payload["code"].(string)
	switch code {
	case "cross_namespace":
		return "cross-namespace scope"
	case "disallowed_resource":
		return "outside allowed resource scope"
	case "unmanaged_resource":
		return "unmanaged resource"
	case "unsupported_action":
		return "unsupported action"
	case "unauthorized_action":
		return "outside allowed action scope"
	case "invalid_action_parameters":
		return "invalid parameters"
	case "action_cancelled":
		return "cancelled before execution"
	case "authorization_unavailable":
		return "authorization unavailable"
	default:
		if code != "" {
			return code
		}
		return "denied by policy"
	}
}

func toolEventType(code string) string {
	switch {
	case strings.Contains(code, "denied"):
		return EventAgentActionDenied
	case strings.Contains(code, "failed"):
		return EventAgentActionFailed
	case strings.Contains(code, "succeeded") || strings.Contains(code, "completed"):
		return EventToolResult
	default:
		return EventToolInvocation
	}
}

func toolPhase(code string) string {
	if strings.Contains(code, "cleanup") {
		return PhaseCleanup
	}
	if strings.Contains(code, "provision") || strings.Contains(code, "environment") {
		return PhaseProvisioning
	}
	return PhaseAgentExecution
}

func toolStatus(code string) string {
	switch {
	case strings.Contains(code, "denied"):
		return StatusDenied
	case strings.Contains(code, "failed"):
		return StatusFailed
	case strings.Contains(code, "succeeded") || strings.Contains(code, "completed"):
		return StatusSuccess
	default:
		return StatusInfo
	}
}

func sourceFor(eventType string) string {
	switch eventType {
	case EventTrialStarted, EventTrialCompleted:
		return "run"
	case EventEnvironmentProvisioningStarted, EventEnvironmentReady, EventEnvironmentSetup:
		return "environment"
	case EventObservation, EventTelemetryObservation, EventStateChange:
		return "observation"
	case EventAgentActionRequested, EventAgentActionExecuted, EventAgentActionFailed:
		return "action"
	case EventAgentActionDenied, EventSafetyFinding:
		return "safety"
	case EventRecoveryDetected, EventRecoveryVerified:
		return "recovery"
	case EventEvaluationStarted, EventEvaluationFinding, EventEvaluationCompleted:
		return "evaluation"
	case EventPlatformInvalid:
		return "platform"
	case EventCleanupStarted, EventCleanupCompleted:
		return "cleanup"
	default:
		return "tool"
	}
}

func viewFor(eventType string) string {
	switch eventType {
	case EventSafetyFinding, EventEvaluationFinding, EventEvaluationStarted, EventEvaluationCompleted, EventPlatformInvalid, EventRecoveryDetected, EventRecoveryVerified:
		return "evaluator"
	default:
		return "agent_visible"
	}
}

func findingCode(raw json.RawMessage) string {
	var payload map[string]any
	_ = json.Unmarshal(nullObject(raw), &payload)
	code, _ := payload["code"].(string)
	return code
}

func isPlatformCode(code string) bool {
	return code == "runtime_failure" || code == "telemetry_unavailable" || strings.HasPrefix(code, "platform_")
}

func findingStatus(kind, code string) string {
	if kind == "safety" {
		return StatusDenied
	}
	if isPlatformCode(code) {
		return StatusInvalid
	}
	if kind == "strength" {
		return StatusSuccess
	}
	return StatusFinding
}

func passWord(passed bool) string {
	if passed {
		return "PASSED"
	}
	return "FAILED"
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return value
		}
	}
	return ""
}

func completedOr(defaultAt time.Time, override *time.Time) time.Time {
	if override != nil && !override.IsZero() {
		return *override
	}
	return defaultAt
}

func valueOrNow(value *time.Time) time.Time {
	if value != nil && !value.IsZero() {
		return *value
	}
	return time.Now().UTC()
}

func diffMS(start, end time.Time) *int64 {
	if start.IsZero() || end.IsZero() || end.Before(start) {
		return nil
	}
	value := end.Sub(start).Milliseconds()
	return &value
}

// ContentHash returns the sha256 hex of input for evidence previews.
func ContentHash(input string) string {
	sum := sha256.Sum256([]byte(input))
	return hex.EncodeToString(sum[:])
}
