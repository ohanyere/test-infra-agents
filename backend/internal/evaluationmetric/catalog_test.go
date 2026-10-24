package evaluationmetric

import "testing"

func TestTemporalEvidenceClassification(t *testing.T) {
	for _, key := range []string{"error_rate", "current_error_rate", "p95_latency", "availability"} {
		definition, ok := Lookup(key)
		if !ok || definition.EvidenceScope != EvidencePostActionRecovery || !definition.RequiresPositiveTraffic {
			t.Fatalf("recovery metric %q classification=%+v found=%t", key, definition, ok)
		}
	}
	for _, key := range []string{"request_rate", "deployment_available_replicas", "pod_ready_count", "agent_action_count"} {
		definition, ok := Lookup(key)
		if !ok || definition.EvidenceScope != EvidenceFullRun {
			t.Fatalf("non-recovery metric %q classification=%+v found=%t", key, definition, ok)
		}
	}
}
