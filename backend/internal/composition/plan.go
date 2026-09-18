// Package composition deterministically places WorldSpec components onto
// capability-declaring adapters. It performs no live provisioning.
package composition

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"

	"github.com/evalplane/evalplane/backend/internal/environment"
	"github.com/evalplane/evalplane/backend/internal/worldspec"
)

const ContractV1 = "composition-plan:v1"

type Adapter struct {
	ID              string   `json:"id"`
	ContractVersion string   `json:"contract_version"`
	ProviderID      string   `json:"provider_id"`
	ComponentTypes  []string `json:"component_types"`
	Capabilities    []string `json:"capabilities"`
	MaximumFidelity string   `json:"maximum_fidelity"`
	ProfileID       string   `json:"profile_id"`
	IsolationTier   string   `json:"isolation_tier"`
	Priority        int      `json:"priority"`
}
type Placement struct {
	ComponentID            string `json:"component_id"`
	ComponentType          string `json:"component_type"`
	ComponentVersion       string `json:"component_version,omitempty"`
	AdapterID              string `json:"adapter_id"`
	AdapterContractVersion string `json:"adapter_contract_version"`
	ProviderID             string `json:"provider_id"`
	ProfileID              string `json:"profile_id"`
}
type Plan struct {
	ContractVersion      string                `json:"contract_version"`
	ScenarioVersionID    string                `json:"scenario_version_id"`
	WorldSpecHash        string                `json:"world_spec_hash"`
	Placements           []Placement           `json:"placements"`
	DependencyOrder      []string              `json:"dependency_order"`
	Edges                []worldspec.Edge      `json:"edges"`
	ProfileID            string                `json:"profile_id"`
	IsolationTier        string                `json:"isolation_tier"`
	RequiredCapabilities []string              `json:"required_capabilities"`
	LifecycleOrder       []string              `json:"lifecycle_order"`
	Initializers         []worldspec.Binding   `json:"initializers,omitempty"`
	Workloads            []worldspec.Binding   `json:"workloads,omitempty"`
	Conditions           []worldspec.Binding   `json:"conditions,omitempty"`
	Observations         []worldspec.Binding   `json:"observations,omitempty"`
	Tools                []worldspec.Binding   `json:"tools,omitempty"`
	Verifiers            []worldspec.Assertion `json:"verifiers,omitempty"`
	CleanupStrategy      string                `json:"cleanup_strategy"`
	MinimumFidelity      string                `json:"minimum_fidelity"`
	SelectedFidelity     string                `json:"selected_fidelity"`
	PlanHash             string                `json:"plan_hash"`
}
type AgentProjection struct {
	ContractVersion string              `json:"contract_version"`
	WorldSpecHash   string              `json:"world_spec_hash"`
	Components      []Placement         `json:"components"`
	Edges           []worldspec.Edge    `json:"edges"`
	Observations    []worldspec.Binding `json:"observations"`
	Tools           []worldspec.Binding `json:"tools"`
}

// LegacyAdapterCatalog projects current truthful provider capability into the
// component-aware planner. It does not make any new provider live.
func LegacyAdapterCatalog() []Adapter {
	return []Adapter{
		{ID: "kind-kubernetes:v1", ContractVersion: "component-adapter:v1", ProviderID: environment.ProviderKindDev, ComponentTypes: []string{"kubernetes"}, Capabilities: []string{}, MaximumFidelity: worldspec.FidelityLocal, ProfileID: environment.ProfileKubernetesNamespace, IsolationTier: environment.IsolationNamespace, Priority: 10},
		{ID: "kind-prometheus:v1", ContractVersion: "component-adapter:v1", ProviderID: environment.ProviderKindDev, ComponentTypes: []string{"prometheus"}, Capabilities: []string{}, MaximumFidelity: worldspec.FidelityLocal, ProfileID: environment.ProfileKubernetesNamespace, IsolationTier: environment.IsolationNamespace, Priority: 10},
	}
}

func (p Plan) AgentView() AgentProjection {
	return AgentProjection{ContractVersion: p.ContractVersion, WorldSpecHash: p.WorldSpecHash, Components: append([]Placement(nil), p.Placements...), Edges: append([]worldspec.Edge(nil), p.Edges...), Observations: append([]worldspec.Binding(nil), p.Observations...), Tools: append([]worldspec.Binding(nil), p.Tools...)}
}

func Compile(scenarioVersionID string, spec worldspec.Spec, adapters []Adapter) (Plan, error) {
	if strings.TrimSpace(scenarioVersionID) == "" {
		return Plan{}, errors.New("scenario version id is required")
	}
	normalized, err := worldspec.Normalize(spec)
	if err != nil {
		return Plan{}, err
	}
	worldHash, _ := worldspec.Hash(normalized)
	order, err := worldspec.TopologicalOrder(normalized.Components, normalized.Edges)
	if err != nil {
		return Plan{}, err
	}
	for _, a := range adapters {
		if strings.TrimSpace(a.ID) == "" || strings.TrimSpace(a.ContractVersion) == "" || strings.TrimSpace(a.ProviderID) == "" || strings.TrimSpace(a.ProfileID) == "" || isolationRank(a.IsolationTier) == 0 || !worldspec.ValidFidelity(a.MaximumFidelity) {
			return Plan{}, errors.New("adapter catalog contains invalid contract or fidelity truth")
		}
	}
	adapters = append([]Adapter(nil), adapters...)
	sort.Slice(adapters, func(i, j int) bool {
		if adapters[i].Priority != adapters[j].Priority {
			return adapters[i].Priority < adapters[j].Priority
		}
		return adapters[i].ID < adapters[j].ID
	})
	placements := make([]Placement, 0, len(normalized.Components))
	selectedFidelity := ""
	profile := ""
	isolation := ""
	caps := []string{}
	availableCaps := []string{}
	for _, c := range normalized.Components {
		var picked *Adapter
		for i := range adapters {
			a := &adapters[i]
			if !contains(a.ComponentTypes, c.Type) || !containsAll(a.Capabilities, c.RequiredCapabilities) || !worldspec.FidelitySatisfies(a.MaximumFidelity, normalized.MinimumFidelity) {
				continue
			}
			picked = a
			break
		}
		if picked == nil {
			return Plan{}, fmt.Errorf("no adapter satisfies component %q capabilities and fidelity", c.ID)
		}
		if !worldspec.ValidFidelity(picked.MaximumFidelity) {
			return Plan{}, fmt.Errorf("adapter %q has unknown maximum fidelity", picked.ID)
		}
		placements = append(placements, Placement{ComponentID: c.ID, ComponentType: c.Type, ComponentVersion: c.Version, AdapterID: picked.ID, AdapterContractVersion: picked.ContractVersion, ProviderID: picked.ProviderID, ProfileID: picked.ProfileID})
		availableCaps = append(availableCaps, picked.Capabilities...)
		if selectedFidelity == "" || fidelityLess(picked.MaximumFidelity, selectedFidelity) {
			selectedFidelity = picked.MaximumFidelity
		}
		if profile == "" {
			profile = picked.ProfileID
		} else if profile != picked.ProfileID {
			profile = "composed"
		}
		if isolation == "" || isolationRank(picked.IsolationTier) > isolationRank(isolation) {
			isolation = picked.IsolationTier
		}
		caps = append(caps, c.RequiredCapabilities...)
	}
	if !containsAll(availableCaps, normalized.RequiredCapabilities) {
		return Plan{}, errors.New("selected adapters do not satisfy required world capabilities")
	}
	caps = append(caps, normalized.RequiredCapabilities...)
	sort.Slice(placements, func(i, j int) bool { return placements[i].ComponentID < placements[j].ComponentID })
	caps = unique(caps)
	verifiers := append([]worldspec.Assertion{}, normalized.BaselineAssertions...)
	verifiers = append(verifiers, normalized.ScenarioAssertions...)
	verifiers = append(verifiers, normalized.FinalAssertions...)
	verifiers = append(verifiers, normalized.ResetAssertions...)
	p := Plan{ContractVersion: ContractV1, ScenarioVersionID: scenarioVersionID, WorldSpecHash: worldHash, Placements: placements, DependencyOrder: order, Edges: normalized.Edges, ProfileID: profile, IsolationTier: isolation, RequiredCapabilities: caps, LifecycleOrder: []string{"provision", "initialize", "baseline_verify", "workload", "condition", "scenario_verify", "agent", "final_verify", "cleanup"}, Initializers: normalized.Initializers, Workloads: normalized.Workloads, Conditions: normalized.Conditions, Observations: normalized.Observations, Tools: normalized.Tools, Verifiers: verifiers, CleanupStrategy: normalized.CleanupStrategy, MinimumFidelity: normalized.MinimumFidelity, SelectedFidelity: selectedFidelity}
	h, err := Hash(p)
	if err != nil {
		return Plan{}, err
	}
	p.PlanHash = h
	return p, nil
}
func Hash(p Plan) (string, error) {
	p.PlanHash = ""
	b, err := json.Marshal(p)
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(b)
	return "sha256:" + hex.EncodeToString(sum[:]), nil
}
func contains(v []string, x string) bool {
	for _, item := range v {
		if item == x {
			return true
		}
	}
	return false
}
func containsAll(have, need []string) bool {
	for _, n := range need {
		if !contains(have, n) {
			return false
		}
	}
	return true
}
func unique(v []string) []string {
	m := map[string]bool{}
	for _, x := range v {
		m[x] = true
	}
	v = v[:0]
	for x := range m {
		v = append(v, x)
	}
	sort.Strings(v)
	return v
}
func fidelityLess(a, b string) bool {
	order := map[string]int{worldspec.FidelityLegacy: 0, worldspec.FidelitySynthetic: 1, worldspec.FidelityLocal: 2, worldspec.FidelityReplica: 3, worldspec.FidelityReal: 4}
	return order[a] < order[b]
}

func isolationRank(v string) int {
	switch v {
	case environment.IsolationNamespace:
		return 1
	case environment.IsolationVirtualCluster:
		return 2
	case environment.IsolationDedicatedCluster:
		return 3
	}
	return 0
}
