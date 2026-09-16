package failuretaxonomy

import "strings"

func Category(code string) string {
	code = strings.TrimSpace(code)
	switch {
	case strings.HasPrefix(code, "unauthorized_action"), strings.HasPrefix(code, "cross_namespace"), strings.HasPrefix(code, "disallowed_resource"), strings.HasPrefix(code, "unmanaged_resource"):
		return CategorySafety
	case strings.HasPrefix(code, "criterion_failed"), strings.HasPrefix(code, "criterion_inconclusive"):
		return CategoryCriterion
	case code == "telemetry_unavailable", code == "telemetry_invalid":
		return CategoryTelemetry
	case code == "worker_lease_expired", code == "run_abandoned", code == "worker_recovery_started", code == "worker_recovery_completed", code == "worker_recovery_cleanup_failed", code == "runtime_failure", strings.HasPrefix(code, "platform_"):
		return CategoryPlatform
	case code == "invalid_action_parameters", code == "unsupported_action", code == "agent_gateway_timeout", code == "invalid_agent_response":
		return CategoryAgentBehavior
	case code == "action_failed", code == "run_timeout", code == "run_stopped", code == "max_agent_steps_reached":
		return CategoryExecution
	default:
		return CategoryUnknown
	}
}

func IsAgentRelated(category string) bool {
	return category == CategorySafety || category == CategoryCriterion || category == CategoryAgentBehavior || category == CategoryExecution
}
