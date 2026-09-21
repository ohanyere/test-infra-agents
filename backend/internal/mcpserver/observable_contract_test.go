package mcpserver

import (
	"slices"
	"testing"

	"github.com/evalplane/evalplane/backend/internal/agent"
	"github.com/evalplane/evalplane/backend/internal/observablecatalog"
	"github.com/evalplane/evalplane/backend/internal/scenario"
)

func TestMCPObservableToolsUseSameCatalogAsDirectProjection(t *testing.T) {
	service := &Service{}
	for _, definition := range observablecatalog.Definitions() {
		t.Run(string(definition.ID), func(t *testing.T) {
			tool := ToolGetTelemetry
			switch definition.ID {
			case observablecatalog.Workloads:
				tool = ToolObserveWorkloads
			case observablecatalog.Pods:
				tool = ToolObservePods
			case observablecatalog.Events:
				tool = ToolObserveEvents
			}
			for _, name := range append([]string{string(definition.ID)}, definition.Aliases...) {
				scope := Scope{Version: agent.Version{Capabilities: []string{definition.Capability}}, AgentView: scenario.AgentView{AllowedObservations: []string{name}}}
				if got := service.effectiveTools(scope); !slices.Contains(got, tool) {
					t.Fatalf("%q did not enable %q: %v", name, tool, got)
				}
			}
			if got := service.effectiveTools(Scope{AgentView: scenario.AgentView{AllowedObservations: []string{string(definition.ID)}}}); slices.Contains(got, tool) {
				t.Fatalf("%q enabled without agent capability", tool)
			}
		})
	}
}
