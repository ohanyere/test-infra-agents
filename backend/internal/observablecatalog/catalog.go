// Package observablecatalog defines the agent-visible scenario observation contract.
// Built-in evaluator read requirements are a separate vocabulary.
package observablecatalog

import (
	_ "embed"
	"encoding/json"
	"errors"
	"regexp"
)

type ID string

const (
	Workloads    ID = "kubernetes-workloads"
	Pods         ID = "kubernetes-pods"
	Events       ID = "kubernetes-events"
	RequestRate  ID = "request-rate"
	ErrorRate    ID = "error-rate"
	P95Latency   ID = "p95-latency"
	Availability ID = "availability"
)

type Definition struct {
	ID                ID       `json:"id"`
	Aliases           []string `json:"aliases"`
	Capability        string   `json:"capability"`
	CapabilityAliases []string `json:"capability_aliases"`
	MetricKey         string   `json:"metric_key"`
	Label             string   `json:"label"`
	Category          string   `json:"category"`
	AuthoringDefault  bool     `json:"authoring_default"`
}

var (
	ErrMalformed   = errors.New("observable identifier must be lowercase kebab-case or a known legacy alias")
	ErrUnsupported = errors.New("observable identifier has no agent-visible runtime provider")
	kebabPattern   = regexp.MustCompile(`^[a-z0-9]+(?:-[a-z0-9]+)*$`)
)

//go:embed catalog.json
var catalogJSON []byte

var definitions []Definition
var byName map[string]ID
var byID map[ID]Definition

func init() {
	if err := json.Unmarshal(catalogJSON, &definitions); err != nil {
		panic(err)
	}
	byName = make(map[string]ID)
	byID = make(map[ID]Definition)
	for _, definition := range definitions {
		if !kebabPattern.MatchString(string(definition.ID)) || definition.Capability == "" || definition.Label == "" || definition.Category == "" {
			panic("invalid observable catalog entry")
		}
		if _, exists := byID[definition.ID]; exists {
			panic("duplicate observable catalog entry")
		}
		byID[definition.ID] = definition
		for _, name := range append([]string{string(definition.ID)}, definition.Aliases...) {
			if _, exists := byName[name]; exists {
				panic("duplicate observable name")
			}
			byName[name] = definition.ID
		}
	}
}

func Parse(name string) (ID, error) {
	if id, ok := byName[name]; ok {
		return id, nil
	}
	if !kebabPattern.MatchString(name) {
		return "", ErrMalformed
	}
	return "", ErrUnsupported
}

func Canonical(name string) (string, error) {
	id, err := Parse(name)
	return string(id), err
}

func Definitions() []Definition {
	return append([]Definition(nil), definitions...)
}

func DefinitionFor(id ID) (Definition, bool) {
	definition, ok := byID[id]
	return definition, ok
}

func CanonicalIDs() []string {
	ids := make([]string, 0, len(definitions))
	for _, definition := range definitions {
		ids = append(ids, string(definition.ID))
	}
	return ids
}

func Resolve(names []string) map[ID]bool {
	resolved := make(map[ID]bool, len(names))
	for _, name := range names {
		if id, err := Parse(name); err == nil {
			resolved[id] = true
		}
	}
	return resolved
}

// CanonicalizeKnown preserves unsupported historical values for transparent API reads.
func CanonicalizeKnown(names []string) []string {
	if names == nil {
		return nil
	}
	out := make([]string, len(names))
	for index, name := range names {
		if canonical, err := Canonical(name); err == nil {
			out[index] = canonical
		} else {
			out[index] = name
		}
	}
	return out
}

// KnownOnly is used for agent-visible views of historical rows. Unsupported
// old values had no runtime provider and must not be advertised as available.
func KnownOnly(names []string) []string {
	if names == nil {
		return nil
	}
	out := make([]string, 0, len(names))
	seen := map[ID]bool{}
	for _, name := range names {
		if id, err := Parse(name); err == nil && !seen[id] {
			out = append(out, string(id))
			seen[id] = true
		}
	}
	return out
}

func HasCapability(id ID, capabilities map[string]bool) bool {
	definition, ok := DefinitionFor(id)
	if !ok {
		return false
	}
	if capabilities[definition.Capability] {
		return true
	}
	for _, alias := range definition.CapabilityAliases {
		if capabilities[alias] {
			return true
		}
	}
	return false
}
