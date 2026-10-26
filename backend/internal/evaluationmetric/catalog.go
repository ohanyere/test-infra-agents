// Package evaluationmetric defines the metrics supported by the deterministic
// evaluator. Scenario validation and evaluation share this exact catalog.
package evaluationmetric

type Definition struct {
	Canonical               string
	Unit                    string
	Reducer                 string
	Telemetry               bool
	EvidenceScope           string
	RequiresPositiveTraffic bool
}

const (
	EvidenceFullRun            = "full_run"
	EvidencePostActionRecovery = "post_action_recovery"
)

var definitions = map[string]Definition{
	"request_rate":                  {"request_rate", "rps", "latest", true, EvidenceFullRun, false},
	"request_rate_rps":              {"request_rate", "rps", "latest", true, EvidenceFullRun, false},
	"application.request_rate_rps":  {"request_rate", "rps", "latest", true, EvidenceFullRun, false},
	"error_rate":                    {"error_rate", "pct", "maximum", true, EvidencePostActionRecovery, true},
	"error_rate_pct":                {"error_rate", "pct", "maximum", true, EvidencePostActionRecovery, true},
	"application.error_rate_pct":    {"error_rate", "pct", "maximum", true, EvidencePostActionRecovery, true},
	"current_error_rate":            {"current_error_rate", "pct", "latest", true, EvidencePostActionRecovery, true},
	"p95_latency":                   {"p95_latency", "ms", "maximum", true, EvidencePostActionRecovery, true},
	"p95_latency_ms":                {"p95_latency", "ms", "maximum", true, EvidencePostActionRecovery, true},
	"application.p95_latency_ms":    {"p95_latency", "ms", "maximum", true, EvidencePostActionRecovery, true},
	"availability":                  {"availability", "pct", "minimum", true, EvidencePostActionRecovery, true},
	"availability_pct":              {"availability", "pct", "minimum", true, EvidencePostActionRecovery, true},
	"application.availability_pct":  {"availability", "pct", "minimum", true, EvidencePostActionRecovery, true},
	"deployment_desired_replicas":   {"deployment_desired_replicas", "count", "latest", false, EvidenceFullRun, false},
	"deployment.desired_replicas":   {"deployment_desired_replicas", "count", "latest", false, EvidenceFullRun, false},
	"deployment_available_replicas": {"deployment_available_replicas", "count", "latest", false, EvidenceFullRun, false},
	"deployment.available_replicas": {"deployment_available_replicas", "count", "latest", false, EvidenceFullRun, false},
	"pod_ready_count":               {"pod_ready_count", "count", "latest", false, EvidenceFullRun, false},
	"pods.ready_count":              {"pod_ready_count", "count", "latest", false, EvidenceFullRun, false},
	"pod_not_ready_count":           {"pod_not_ready_count", "count", "latest", false, EvidenceFullRun, false},
	"pods.not_ready_count":          {"pod_not_ready_count", "count", "latest", false, EvidenceFullRun, false},
	"pod_restart_count":             {"pod_restart_count", "count", "latest", false, EvidenceFullRun, false},
	"pods.restart_count":            {"pod_restart_count", "count", "latest", false, EvidenceFullRun, false},
	"agent_action_count":            {"agent_action_count", "count", "count", false, EvidenceFullRun, false},
}

func Lookup(key string) (Definition, bool) {
	definition, ok := definitions[key]
	return definition, ok
}

func Definitions() map[string]Definition {
	out := make(map[string]Definition, len(definitions))
	for key, definition := range definitions {
		out[key] = definition
	}
	return out
}
