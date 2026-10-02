package agentcapability

import (
	"sort"

	"github.com/evalplane/evalplane/backend/internal/observablecatalog"
)

type Capability struct {
	ID       string `json:"id"`
	Kind     string `json:"kind"`
	Label    string `json:"label"`
	Category string `json:"category"`
}

var actionCatalog = []Capability{
	{ID: "inspect", Kind: "action", Label: "Inspect", Category: "actions"},
	{ID: "scale_workload", Kind: "action", Label: "Scale workload", Category: "actions"},
	{ID: "set_workload_image", Kind: "action", Label: "Set workload image (legacy live mutation)", Category: "actions"},
	{ID: "set_live_workload_image", Kind: "action", Label: "Set live workload image", Category: "actions"},
	{ID: "set_desired_workload_image", Kind: "action", Label: "Set desired workload image", Category: "actions"},
	{ID: "restart_pod", Kind: "action", Label: "Restart pod", Category: "actions"},
	{ID: "no_op", Kind: "action", Label: "No operation", Category: "actions"},
}

// Retained as a known coarse agent declaration, not an observable provider.
var legacyCatalog = Capability{ID: "kubernetes", Kind: "observation", Label: "Kubernetes (legacy)", Category: "infrastructure"}

var catalog = buildCatalog()

func buildCatalog() []Capability {
	out := make([]Capability, 0, len(observablecatalog.Definitions())+len(actionCatalog)+3)
	for _, definition := range observablecatalog.Definitions() {
		out = append(out, Capability{ID: definition.Capability, Kind: "observation", Label: definition.Label, Category: definition.Category})
		for _, alias := range definition.CapabilityAliases {
			out = append(out, Capability{ID: alias, Kind: "observation", Label: definition.Label, Category: definition.Category})
		}
	}
	out = append(out, actionCatalog...)
	out = append(out, legacyCatalog)
	return out
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
