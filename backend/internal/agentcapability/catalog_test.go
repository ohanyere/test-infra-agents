package agentcapability

import "testing"

func TestMissingIsDeterministic(t *testing.T) {
	got := Missing([]string{"inspect"}, []string{"request_rate", "inspect", "availability", "request_rate"})
	if len(got) != 2 || got[0] != "availability" || got[1] != "request_rate" {
		t.Fatalf("missing=%v", got)
	}
}

func TestCatalogContainsCurrentRuntimeCapabilities(t *testing.T) {
	for _, id := range []string{"inspect", "scale_workload", "set_workload_image", "set_live_workload_image", "set_desired_workload_image", "restart_pod", "no_op", "request_rate", "error_rate", "p95_latency", "availability"} {
		if !Known(id) {
			t.Fatalf("missing catalog capability %q", id)
		}
	}
}
