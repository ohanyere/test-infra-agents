package agentgateway

import (
	"reflect"
	"testing"

	"github.com/evalplane/evalplane/backend/internal/observablecatalog"
)

func contractObservation() Observation {
	return Observation{
		Workloads:    []WorkloadObservation{{Kind: "Deployment", Name: "checkout", Image: "evalplane-reference:v18"}},
		Pods:         []PodObservation{{Name: "checkout-pod", Ready: true}},
		RecentEvents: []RecentEvent{{Reason: "Started", InvolvedObjectName: "checkout-pod"}},
		Metrics: map[string]any{
			"telemetry_available": true,
			"request_rate_rps":    12.0,
			"error_rate_pct":      0.5,
			"p95_latency_ms":      100.0,
			"availability_pct":    99.5,
		},
	}
}

func TestProjectObservationUsesCanonicalCatalogAndLegacyAliases(t *testing.T) {
	observation := contractObservation()
	for _, definition := range observablecatalog.Definitions() {
		t.Run(string(definition.ID), func(t *testing.T) {
			canonical := ProjectObservation(observation, []string{string(definition.ID)})
			for _, alias := range definition.Aliases {
				legacy := ProjectObservation(observation, []string{alias})
				if !reflect.DeepEqual(canonical, legacy) {
					t.Fatalf("legacy alias %q changed projection", alias)
				}
			}
			switch definition.ID {
			case observablecatalog.Workloads:
				if len(canonical.Workloads) != 1 || canonical.Workloads[0].Image != "evalplane-reference:v18" {
					t.Fatalf("workload image not exposed: %+v", canonical.Workloads)
				}
			case observablecatalog.Pods:
				if len(canonical.Pods) != 1 {
					t.Fatalf("pods not exposed: %+v", canonical.Pods)
				}
			case observablecatalog.Events:
				if len(canonical.RecentEvents) != 1 {
					t.Fatalf("events not exposed: %+v", canonical.RecentEvents)
				}
			default:
				if _, ok := canonical.Metrics[definition.MetricKey]; !ok {
					t.Fatalf("metric %q not exposed: %+v", definition.MetricKey, canonical.Metrics)
				}
			}
		})
	}
}

func TestUnsupportedObservableDoesNotExposeEvidence(t *testing.T) {
	projected := ProjectObservation(contractObservation(), []string{"cpu", "desired-manifest"})
	if len(projected.Workloads) != 0 || len(projected.Pods) != 0 || len(projected.RecentEvents) != 0 || len(projected.Metrics) != 1 {
		t.Fatalf("unsupported names exposed evidence: %+v", projected)
	}
}
