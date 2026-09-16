// Package runtimecontract identifies server-owned execution contracts used by
// reproducibility manifests. These versions change only when semantics change.
package runtimecontract

const (
	CapabilityCatalogVersion   = "capability-catalog:v1"
	TrafficSpikeRuntimeVersion = "kubernetes-traffic-spike:v1"
	// InfrastructureCapabilityCatalogVersion identifies the server-owned
	// semantic competency catalog for agent capability claims (CLAIMED only).
	// Historical AgentVersion claims record this contract so future catalog
	// versions never rewrite them.
	InfrastructureCapabilityCatalogVersion = "infrastructure-capability-catalog:v1"
)
