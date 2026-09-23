// Package evaluationmetric defines the metrics supported by the deterministic
// evaluator. Scenario validation and evaluation share this exact catalog.
package evaluationmetric

type Definition struct {
	Canonical string
	Unit      string
	Reducer   string
	Telemetry bool
}

var definitions = map[string]Definition{
	"request_rate":                  {"request_rate", "rps", "latest", true},
	"request_rate_rps":              {"request_rate", "rps", "latest", true},
	"application.request_rate_rps":  {"request_rate", "rps", "latest", true},
	"error_rate":                    {"error_rate", "pct", "maximum", true},
	"error_rate_pct":                {"error_rate", "pct", "maximum", true},
	"application.error_rate_pct":    {"error_rate", "pct", "maximum", true},
	"current_error_rate":            {"current_error_rate", "pct", "latest", true},
	"p95_latency":                   {"p95_latency", "ms", "maximum", true},
	"p95_latency_ms":                {"p95_latency", "ms", "maximum", true},
	"application.p95_latency_ms":    {"p95_latency", "ms", "maximum", true},
	"availability":                  {"availability", "pct", "minimum", true},
	"availability_pct":              {"availability", "pct", "minimum", true},
	"application.availability_pct":  {"availability", "pct", "minimum", true},
	"deployment_desired_replicas":   {"deployment_desired_replicas", "count", "latest", false},
	"deployment.desired_replicas":   {"deployment_desired_replicas", "count", "latest", false},
	"deployment_available_replicas": {"deployment_available_replicas", "count", "latest", false},
	"deployment.available_replicas": {"deployment_available_replicas", "count", "latest", false},
	"pod_ready_count":               {"pod_ready_count", "count", "latest", false},
	"pods.ready_count":              {"pod_ready_count", "count", "latest", false},
	"pod_not_ready_count":           {"pod_not_ready_count", "count", "latest", false},
	"pods.not_ready_count":          {"pod_not_ready_count", "count", "latest", false},
	"pod_restart_count":             {"pod_restart_count", "count", "latest", false},
	"pods.restart_count":            {"pod_restart_count", "count", "latest", false},
	"agent_action_count":            {"agent_action_count", "count", "count", false},
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
