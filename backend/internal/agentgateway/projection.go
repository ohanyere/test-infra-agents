package agentgateway

import (
	"strings"

	"github.com/evalplane/evalplane/backend/internal/observablecatalog"
	"github.com/evalplane/evalplane/backend/internal/scenariorunner"
	"github.com/evalplane/evalplane/backend/internal/telemetry"
)

// ProjectObservation is the single server-owned agent visibility filter used by
// both direct requests and MCP tools. Event prose and runtime-control objects are
// deliberately excluded because they may reveal evaluator-only execution inputs.
func ProjectObservation(obs Observation, allowed []string) Observation {
	out := Observation{Timestamp: obs.Timestamp, Metrics: map[string]any{}, RecentEvents: []RecentEvent{}}
	resolved := observablecatalog.Resolve(allowed)
	if resolved[observablecatalog.Workloads] {
		for _, workload := range obs.Workloads {
			if workload.Name != telemetry.DeploymentName {
				out.Workloads = append(out.Workloads, workload)
			}
		}
	}
	if resolved[observablecatalog.Pods] {
		for _, pod := range obs.Pods {
			if !strings.HasPrefix(pod.Name, scenariorunner.TrafficJobName+"-") && !strings.HasPrefix(pod.Name, telemetry.DeploymentName+"-") {
				out.Pods = append(out.Pods, pod)
			}
		}
	}
	if resolved[observablecatalog.Events] {
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
	for _, definition := range observablecatalog.Definitions() {
		if definition.MetricKey != "" && resolved[definition.ID] {
			if value, ok := obs.Metrics[definition.MetricKey]; ok {
				out.Metrics[definition.MetricKey] = value
			}
		}
	}
	return out
}
