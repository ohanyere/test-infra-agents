package trajectory

// Contract pins the trajectory & evidence explorer projection.
//
// trajectory-evidence-contract:v1
//
// Every trajectory response pins this contract so historical trajectories
// remain interpretable if projection logic changes. The explorer is a read
// model over existing persisted evidence (run events, observations, actions,
// evidence, evaluation findings, environment events); it introduces no second
// execution engine and no second authoritative store.
const ContractVersion = "trajectory-evidence-contract:v1"

// Closed actor vocabulary (versioned with the contract).
const (
	ActorPlatform    = "platform"
	ActorAgent       = "agent"
	ActorEnvironment = "environment"
	ActorTool        = "tool"
	ActorEvaluator   = "evaluator"
)

// Closed phase vocabulary (versioned with the contract).
const (
	PhaseProvisioning   = "provisioning"
	PhaseSetup          = "setup"
	PhaseAgentExecution = "agent_execution"
	PhaseVerification   = "verification"
	PhaseEvaluation     = "evaluation"
	PhaseCleanup        = "cleanup"
)

// Canonical trajectory event types (superset of persisted run event codes).
const (
	EventTrialStarted                   = "trial_started"
	EventEnvironmentProvisioningStarted = "environment_provisioning_started"
	EventEnvironmentReady               = "environment_ready"
	EventEnvironmentSetup               = "environment_setup"
	EventObservation                    = "observation"
	EventAgentActionRequested           = "agent_action_requested"
	EventAgentActionExecuted            = "agent_action_executed"
	EventAgentActionFailed              = "agent_action_failed"
	EventAgentActionDenied              = "agent_action_denied"
	EventToolInvocation                 = "tool_invocation"
	EventToolResult                     = "tool_result"
	EventTelemetryObservation           = "telemetry_observation"
	EventStateChange                    = "state_change"
	EventRecoveryDetected               = "recovery_detected"
	EventRecoveryVerified               = "recovery_verified"
	EventEvaluationStarted              = "evaluation_started"
	EventEvaluationFinding              = "evaluation_finding"
	EventSafetyFinding                  = "safety_finding"
	EventPlatformInvalid                = "platform_invalid"
	EventEvaluationCompleted            = "evaluation_completed"
	EventCleanupStarted                 = "cleanup_started"
	EventCleanupCompleted               = "cleanup_completed"
	EventTrialCompleted                 = "trial_completed"
)

// Event statuses.
const (
	StatusSuccess = "success"
	StatusFailed  = "failed"
	StatusDenied  = "denied"
	StatusInfo    = "info"
	StatusFinding = "finding"
	StatusInvalid = "invalid"
)

// OrderingRule documents deterministic timeline ordering:
//
//	occurred_at ASC
//	then source priority (run lifecycle < environment < observation < action < tool < recovery < evaluation < safety < platform < cleanup)
//	then persisted sequence / stable ID ASC
//
// Sequence represents display order, not fabricated causality. Two events
// with close timestamps are ordered by the stable tie-breakers above; no
// causal link is implied.
const OrderingRule = "occurred_at ASC, source_priority ASC, persisted_sequence ASC, stable_id ASC"

// SourcePriority assigns a stable merge priority to persisted sources.
func SourcePriority(sourceType string) int {
	switch sourceType {
	case "run":
		return 0
	case "environment":
		return 1
	case "observation":
		return 2
	case "action":
		return 3
	case "tool":
		return 4
	case "recovery":
		return 5
	case "evaluation":
		return 6
	case "safety":
		return 7
	case "platform":
		return 8
	case "cleanup":
		return 9
	default:
		return 50
	}
}

// ValidEventType reports whether an event type belongs to the closed contract vocabulary.
func ValidEventType(value string) bool {
	switch value {
	case EventTrialStarted, EventEnvironmentProvisioningStarted, EventEnvironmentReady,
		EventEnvironmentSetup, EventObservation, EventAgentActionRequested,
		EventAgentActionExecuted, EventAgentActionFailed, EventAgentActionDenied,
		EventToolInvocation, EventToolResult, EventTelemetryObservation,
		EventStateChange, EventRecoveryDetected, EventRecoveryVerified,
		EventEvaluationStarted, EventEvaluationFinding, EventSafetyFinding,
		EventPlatformInvalid, EventEvaluationCompleted, EventCleanupStarted,
		EventCleanupCompleted, EventTrialCompleted:
		return true
	}
	return false
}

// ValidActor reports whether an actor belongs to the closed vocabulary.
func ValidActor(value string) bool {
	switch value {
	case ActorPlatform, ActorAgent, ActorEnvironment, ActorTool, ActorEvaluator:
		return true
	}
	return false
}

// ValidPhase reports whether a phase belongs to the closed vocabulary.
func ValidPhase(value string) bool {
	switch value {
	case PhaseProvisioning, PhaseSetup, PhaseAgentExecution, PhaseVerification, PhaseEvaluation, PhaseCleanup:
		return true
	}
	return false
}
