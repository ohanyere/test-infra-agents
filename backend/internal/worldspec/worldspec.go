// Package worldspec defines the immutable, provider-neutral world requested by
// an exact ScenarioVersion. It deliberately contains no evaluator-only truth.
package worldspec

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"regexp"
	"sort"
	"strings"

	"github.com/evalplane/evalplane/backend/internal/environment"
	"github.com/evalplane/evalplane/backend/internal/scenario"
)

const (
	ContractV1        = "world-spec:v1"
	LegacyCompilerV1  = "legacy-kubernetes-traffic-spike:v1"
	CleanupDestroy    = "destroy"
	CleanupReset      = "reset"
	FidelityLegacy    = "legacy_unspecified"
	FidelitySynthetic = "synthetic_fixture"
	FidelityLocal     = "local_emulated"
	FidelityReplica   = "isolated_replica"
	FidelityReal      = "isolated_real_provider"
)

var idPattern = regexp.MustCompile(`^[a-z][a-z0-9_.-]{0,99}$`)

type ArtifactRef struct {
	Kind      string `json:"kind"`
	Reference string `json:"reference"`
	Digest    string `json:"digest,omitempty"`
}
type Component struct {
	ID                   string       `json:"id"`
	Type                 string       `json:"type"`
	Version              string       `json:"version,omitempty"`
	Artifact             *ArtifactRef `json:"artifact,omitempty"`
	RequiredCapabilities []string     `json:"required_capabilities,omitempty"`
}
type Edge struct {
	From         string `json:"from"`
	To           string `json:"to"`
	Relationship string `json:"relationship"`
}
type Binding struct {
	ID          string `json:"id"`
	ComponentID string `json:"component_id"`
	Contract    string `json:"contract"`
	Reference   string `json:"reference,omitempty"`
}
type Assertion struct {
	ID           string `json:"id"`
	ComponentID  string `json:"component_id"`
	Contract     string `json:"contract"`
	AgentVisible bool   `json:"agent_visible"`
}

type Spec struct {
	ContractVersion      string      `json:"contract_version"`
	CompilerID           string      `json:"compiler_id"`
	Components           []Component `json:"components"`
	Edges                []Edge      `json:"edges"`
	RequiredCapabilities []string    `json:"required_capabilities,omitempty"`
	Initializers         []Binding   `json:"initializers,omitempty"`
	Workloads            []Binding   `json:"workloads,omitempty"`
	Conditions           []Binding   `json:"conditions,omitempty"`
	Observations         []Binding   `json:"observations,omitempty"`
	Tools                []Binding   `json:"tools,omitempty"`
	BaselineAssertions   []Assertion `json:"baseline_assertions,omitempty"`
	ScenarioAssertions   []Assertion `json:"scenario_assertions,omitempty"`
	FinalAssertions      []Assertion `json:"final_assertions,omitempty"`
	ResetAssertions      []Assertion `json:"reset_assertions,omitempty"`
	MinimumFidelity      string      `json:"minimum_fidelity"`
	MinimumIsolation     string      `json:"minimum_isolation_tier"`
	CleanupStrategy      string      `json:"cleanup_strategy"`
}

type AgentProjection struct {
	ContractVersion string      `json:"contract_version"`
	Components      []Component `json:"components"`
	Edges           []Edge      `json:"edges"`
	Observations    []Binding   `json:"observations"`
	Tools           []Binding   `json:"tools"`
}

func (s Spec) AgentView() AgentProjection {
	return AgentProjection{ContractVersion: s.ContractVersion, Components: append([]Component(nil), s.Components...), Edges: append([]Edge(nil), s.Edges...), Observations: append([]Binding(nil), s.Observations...), Tools: append([]Binding(nil), s.Tools...)}
}

func ValidFidelity(v string) bool { return fidelityRank(v) >= 0 }
func FidelitySatisfies(actual, minimum string) bool {
	return fidelityRank(actual) >= fidelityRank(minimum) && fidelityRank(minimum) >= 0
}
func fidelityRank(v string) int {
	switch v {
	case FidelityLegacy:
		return 0
	case FidelitySynthetic:
		return 1
	case FidelityLocal:
		return 2
	case FidelityReplica:
		return 3
	case FidelityReal:
		return 4
	}
	return -1
}

func Normalize(in Spec) (Spec, error) {
	encoded, err := json.Marshal(in)
	if err != nil {
		return Spec{}, err
	}
	var copied Spec
	if err := json.Unmarshal(encoded, &copied); err != nil {
		return Spec{}, err
	}
	in = copied
	if in.ContractVersion != ContractV1 {
		return Spec{}, fmt.Errorf("unsupported world spec contract %q", in.ContractVersion)
	}
	if strings.TrimSpace(in.CompilerID) == "" {
		return Spec{}, errors.New("compiler_id is required")
	}
	if !ValidFidelity(in.MinimumFidelity) {
		return Spec{}, errors.New("invalid minimum fidelity")
	}
	if in.CleanupStrategy != CleanupDestroy && in.CleanupStrategy != CleanupReset {
		return Spec{}, errors.New("invalid cleanup strategy")
	}
	if len(in.Components) == 0 {
		return Spec{}, errors.New("at least one component is required")
	}
	seen := map[string]bool{}
	for i := range in.Components {
		c := &in.Components[i]
		c.ID = strings.TrimSpace(c.ID)
		c.Type = strings.TrimSpace(c.Type)
		if !idPattern.MatchString(c.ID) || !idPattern.MatchString(c.Type) {
			return Spec{}, errors.New("component id and type must be non-empty stable identifiers")
		}
		if seen[c.ID] {
			return Spec{}, fmt.Errorf("duplicate component id %q", c.ID)
		}
		seen[c.ID] = true
		if c.Artifact != nil && (strings.TrimSpace(c.Artifact.Kind) == "" || strings.TrimSpace(c.Artifact.Reference) == "") {
			return Spec{}, fmt.Errorf("component %q has invalid artifact", c.ID)
		}
		if c.Artifact != nil {
			if err := ValidateSafeRef(c.Artifact.Reference); err != nil {
				return Spec{}, fmt.Errorf("component %q artifact: %w", c.ID, err)
			}
		}
		c.RequiredCapabilities = uniqueSorted(c.RequiredCapabilities)
	}
	in.RequiredCapabilities = uniqueSorted(in.RequiredCapabilities)
	for _, e := range in.Edges {
		if !seen[e.From] || !seen[e.To] {
			return Spec{}, fmt.Errorf("edge references unknown component %q -> %q", e.From, e.To)
		}
		if e.From == e.To || strings.TrimSpace(e.Relationship) == "" {
			return Spec{}, errors.New("invalid topology edge")
		}
	}
	if _, err := TopologicalOrder(in.Components, in.Edges); err != nil {
		return Spec{}, err
	}
	allBindings := [][]Binding{in.Initializers, in.Workloads, in.Conditions, in.Observations, in.Tools}
	for _, group := range allBindings {
		for _, b := range group {
			if !idPattern.MatchString(b.ID) || !seen[b.ComponentID] || strings.TrimSpace(b.Contract) == "" {
				return Spec{}, fmt.Errorf("invalid lifecycle binding %q", b.ID)
			}
			if err := ValidateSafeRef(b.Reference); err != nil {
				return Spec{}, fmt.Errorf("binding %q: %w", b.ID, err)
			}
		}
	}
	for _, group := range [][]Assertion{in.BaselineAssertions, in.ScenarioAssertions, in.FinalAssertions, in.ResetAssertions} {
		for _, a := range group {
			if !idPattern.MatchString(a.ID) || !seen[a.ComponentID] || strings.TrimSpace(a.Contract) == "" {
				return Spec{}, fmt.Errorf("invalid assertion %q", a.ID)
			}
		}
	}
	sort.Slice(in.Components, func(i, j int) bool { return in.Components[i].ID < in.Components[j].ID })
	sort.Slice(in.Edges, func(i, j int) bool {
		if in.Edges[i].From != in.Edges[j].From {
			return in.Edges[i].From < in.Edges[j].From
		}
		if in.Edges[i].To != in.Edges[j].To {
			return in.Edges[i].To < in.Edges[j].To
		}
		return in.Edges[i].Relationship < in.Edges[j].Relationship
	})
	sortBindings := func(v []Binding) { sort.Slice(v, func(i, j int) bool { return v[i].ID < v[j].ID }) }
	sortBindings(in.Initializers)
	sortBindings(in.Workloads)
	sortBindings(in.Conditions)
	sortBindings(in.Observations)
	sortBindings(in.Tools)
	sortAssertions := func(v []Assertion) { sort.Slice(v, func(i, j int) bool { return v[i].ID < v[j].ID }) }
	sortAssertions(in.BaselineAssertions)
	sortAssertions(in.ScenarioAssertions)
	sortAssertions(in.FinalAssertions)
	sortAssertions(in.ResetAssertions)
	return in, nil
}

func TopologicalOrder(components []Component, edges []Edge) ([]string, error) {
	indegree := map[string]int{}
	next := map[string][]string{}
	for _, c := range components {
		if _, ok := indegree[c.ID]; ok {
			return nil, fmt.Errorf("duplicate component id %q", c.ID)
		}
		indegree[c.ID] = 0
	}
	for _, e := range edges {
		if _, ok := indegree[e.From]; !ok {
			return nil, fmt.Errorf("unknown edge source %q", e.From)
		}
		if _, ok := indegree[e.To]; !ok {
			return nil, fmt.Errorf("unknown edge target %q", e.To)
		}
		indegree[e.To]++
		next[e.From] = append(next[e.From], e.To)
	}
	ready := []string{}
	for id, n := range indegree {
		if n == 0 {
			ready = append(ready, id)
		}
	}
	sort.Strings(ready)
	out := []string{}
	for len(ready) > 0 {
		id := ready[0]
		ready = ready[1:]
		out = append(out, id)
		sort.Strings(next[id])
		for _, to := range next[id] {
			indegree[to]--
			if indegree[to] == 0 {
				ready = append(ready, to)
				sort.Strings(ready)
			}
		}
	}
	if len(out) != len(components) {
		return nil, errors.New("component topology contains a cycle")
	}
	return out, nil
}

func Hash(in Spec) (string, error) {
	n, err := Normalize(in)
	if err != nil {
		return "", err
	}
	b, err := json.Marshal(n)
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(b)
	return "sha256:" + hex.EncodeToString(sum[:]), nil
}

func CompileLegacy(v scenario.Version, req environment.Requirements) (Spec, error) {
	validated, err := environment.ValidateRequirements(req)
	if err != nil {
		return Spec{}, fmt.Errorf("invalid legacy environment requirements: %w", err)
	}
	req = validated
	var execution struct {
		Type string `json:"type"`
	}
	if json.Unmarshal(v.ExecutionSpec, &execution) != nil || execution.Type != "traffic_spike" {
		return Spec{}, errors.New("unsupported legacy runtime mapping")
	}
	if req.RuntimeContract != "" && req.RuntimeContract != environment.DefaultRuntimeContract {
		return Spec{}, errors.New("unsupported legacy runtime mapping")
	}
	components := make([]Component, 0, len(req.RequiredComponents))
	for _, id := range req.RequiredComponents {
		components = append(components, Component{ID: id, Type: id, Version: "legacy-current"})
	}
	component := func(preferred string) string {
		for _, c := range components {
			if c.ID == preferred {
				return preferred
			}
		}
		return components[0].ID
	}
	obs := make([]Binding, 0, len(v.Observables))
	for _, name := range uniqueSorted(v.Observables) {
		obs = append(obs, Binding{ID: "observe." + sanitizeID(name), ComponentID: component("prometheus"), Contract: "legacy-observation:v1", Reference: name})
	}
	tools := make([]Binding, 0, len(v.AllowedActions))
	for _, name := range uniqueSorted(v.AllowedActions) {
		tools = append(tools, Binding{ID: "tool." + sanitizeID(name), ComponentID: component("kubernetes"), Contract: "legacy-action:v1", Reference: name})
	}
	edges := []Edge{}
	if component("kubernetes") == "kubernetes" && component("prometheus") == "prometheus" {
		edges = append(edges, Edge{From: "kubernetes", To: "prometheus", Relationship: "observed_by"})
	}
	s := Spec{ContractVersion: ContractV1, CompilerID: LegacyCompilerV1, Components: components, Edges: edges,
		Initializers:         []Binding{{ID: "prepare-runtime", ComponentID: component("kubernetes"), Contract: "legacy-runtime-prepare:v1"}},
		Workloads:            []Binding{{ID: "traffic-spike", ComponentID: component("kubernetes"), Contract: environment.DefaultRuntimeContract, Reference: "scenario-version:execution_spec"}},
		Conditions:           []Binding{{ID: "traffic-spike-condition", ComponentID: component("kubernetes"), Contract: environment.DefaultRuntimeContract, Reference: "scenario-version:execution_spec"}},
		RequiredCapabilities: req.RequiredCapabilities, Observations: obs, Tools: tools,
		BaselineAssertions: []Assertion{{ID: "runtime-ready", ComponentID: component("kubernetes"), Contract: "legacy-runtime-ready:v1"}},
		MinimumFidelity:    FidelityLegacy, MinimumIsolation: req.MinimumIsolation, CleanupStrategy: CleanupDestroy}
	return Normalize(s)
}

func uniqueSorted(in []string) []string {
	m := map[string]bool{}
	for _, v := range in {
		v = strings.TrimSpace(v)
		if v != "" {
			m[v] = true
		}
	}
	out := make([]string, 0, len(m))
	for v := range m {
		out = append(out, v)
	}
	sort.Strings(out)
	return out
}
func sanitizeID(v string) string {
	v = strings.ToLower(strings.TrimSpace(v))
	var b strings.Builder
	for _, r := range v {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') || r == '.' || r == '_' || r == '-' {
			b.WriteRune(r)
		} else {
			b.WriteByte('-')
		}
	}
	out := strings.Trim(b.String(), "-.")
	if out == "" || out[0] < 'a' || out[0] > 'z' {
		out = "item-" + out
	}
	return out
}

// ValidateSafeRef rejects credential-bearing URLs and common secret material.
func ValidateSafeRef(ref string) error {
	l := strings.ToLower(ref)
	for _, bad := range []string{"token=", "password=", "secret=", "authorization:", "bearer ", "kubeconfig", "private_key"} {
		if strings.Contains(l, bad) {
			return errors.New("safe reference contains secret-like provider data")
		}
	}
	if u, err := url.Parse(ref); err == nil && u.User != nil {
		return errors.New("safe reference must not contain URL credentials")
	}
	return nil
}
