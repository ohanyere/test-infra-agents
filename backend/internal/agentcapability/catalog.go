package agentcapability

import "sort"

type Capability struct {
	ID       string `json:"id"`
	Kind     string `json:"kind"`
	Label    string `json:"label"`
	Category string `json:"category"`
}

var catalog = []Capability{
	{ID: "kubernetes_workloads", Kind: "observation", Label: "Kubernetes workloads", Category: "infrastructure"},
	{ID: "kubernetes_pods", Kind: "observation", Label: "Kubernetes pods", Category: "infrastructure"},
	{ID: "kubernetes_events", Kind: "observation", Label: "Kubernetes events", Category: "infrastructure"},
	{ID: "pods", Kind: "observation", Label: "Pods", Category: "infrastructure"},
	{ID: "events", Kind: "observation", Label: "Events", Category: "infrastructure"},
	{ID: "request_rate", Kind: "observation", Label: "Request rate", Category: "telemetry"},
	{ID: "error_rate", Kind: "observation", Label: "Error rate", Category: "telemetry"},
	{ID: "p95_latency", Kind: "observation", Label: "P95 latency", Category: "telemetry"},
	{ID: "availability", Kind: "observation", Label: "Availability", Category: "telemetry"},
	{ID: "inspect", Kind: "action", Label: "Inspect", Category: "actions"},
	{ID: "scale_workload", Kind: "action", Label: "Scale workload", Category: "actions"},
	{ID: "restart_pod", Kind: "action", Label: "Restart pod", Category: "actions"},
	{ID: "no_op", Kind: "action", Label: "No operation", Category: "actions"},
	// Retained as a known coarse legacy declaration for pre-catalog fixtures.
	{ID: "kubernetes", Kind: "observation", Label: "Kubernetes (legacy)", Category: "infrastructure"},
}

func List() []Capability {
	out := append([]Capability(nil), catalog...)
	return out
}

func Known(id string) bool {
	for _, item := range catalog {
		if item.ID == id {
			return true
		}
	}
	return false
}

func Missing(declared, required []string) []string {
	have := make(map[string]struct{}, len(declared))
	for _, id := range declared {
		have[id] = struct{}{}
	}
	seen := map[string]struct{}{}
	missing := []string{}
	for _, id := range required {
		if _, ok := have[id]; ok {
			continue
		}
		if _, ok := seen[id]; !ok {
			seen[id] = struct{}{}
			missing = append(missing, id)
		}
	}
	sort.Strings(missing)
	return missing
}
