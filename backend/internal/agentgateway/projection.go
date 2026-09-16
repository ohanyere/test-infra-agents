package agentgateway

import (
	"slices"
	"strings"

	"github.com/evalplane/evalplane/backend/internal/scenariorunner"
	"github.com/evalplane/evalplane/backend/internal/telemetry"
)

// ProjectObservation is the single server-owned agent visibility filter used by
// both direct requests and MCP tools. Event prose and runtime-control objects are
// deliberately excluded because they may reveal evaluator-only execution inputs.
func ProjectObservation(obs Observation, allowed []string) Observation {
	out := Observation{Timestamp: obs.Timestamp, Metrics: map[string]any{}, RecentEvents: []RecentEvent{}}
	if slices.Contains(allowed, "kubernetes_workloads") {
		for _, workload := range obs.Workloads {
			if workload.Name != telemetry.DeploymentName {
				out.Workloads = append(out.Workloads, workload)
			}
		}
	}
	if slices.Contains(allowed, "kubernetes_pods") || slices.Contains(allowed, "pods") {
		for _, pod := range obs.Pods {
			if !strings.HasPrefix(pod.Name, scenariorunner.TrafficJobName+"-") && !strings.HasPrefix(pod.Name, telemetry.DeploymentName+"-") {
				out.Pods = append(out.Pods, pod)
			}
		}
	}
	if slices.Contains(allowed, "kubernetes_events") || slices.Contains(allowed, "events") {
		for _, event := range obs.RecentEvents {
			if strings.HasPrefix(event.InvolvedObjectName, scenariorunner.TrafficJobName) || strings.HasPrefix(event.InvolvedObjectName, telemetry.DeploymentName) {
				continue
			}
			event.Summary = ""
			out.RecentEvents = append(out.RecentEvents, event)
		}
	}
	if available, ok := obs.Metrics["telemetry_available"]; ok {
		out.Metrics["telemetry_available"] = available
	}
	for key, alias := range map[string]string{"request_rate_rps": "request_rate", "error_rate_pct": "error_rate", "p95_latency_ms": "p95_latency", "availability_pct": "availability"} {
		if slices.Contains(allowed, key) || slices.Contains(allowed, alias) {
			if value, ok := obs.Metrics[key]; ok {
				out.Metrics[key] = value
			}
		}
	}
	return out
}
