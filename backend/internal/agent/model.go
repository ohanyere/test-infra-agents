package agent

import (
	"encoding/json"
	"time"

	"github.com/evalplane/evalplane/backend/internal/identity"
	"github.com/evalplane/evalplane/backend/internal/provenance"
)

type Version struct {
	ID             string   `json:"id"`
	VersionLabel   string   `json:"version_label"`
	InterfaceKind  string   `json:"interface_kind"`
	ToolAccessMode string   `json:"tool_access_mode"`
	EndpointURL    string   `json:"endpoint_url,omitempty"`
	Capabilities   []string `json:"capabilities"`
	// ClaimedCapabilities are immutable competency claims against the
	// server-owned infrastructure catalog (CLAIMED only). They are distinct
	// from low-level Capabilities and never imply qualification.
	ClaimedCapabilities []string `json:"claimed_capabilities"`
	// CapabilityCatalogVersion records which catalog contract the claims came
	// from (infrastructure-capability-catalog:v1). Empty when no claims exist.
	CapabilityCatalogVersion string `json:"capability_catalog_version,omitempty"`
	// ClaimStatus is "claimed" when claims exist, else "". TESTED/QUALIFIED
	// states are never fabricated here; they come from the coverage engine.
	ClaimStatus            string                                 `json:"claim_status,omitempty"`
	ImplementationIdentity provenance.AgentImplementationIdentity `json:"implementation_identity"`
	Metadata               json.RawMessage                        `json:"metadata"`
	CreatedAt              time.Time                              `json:"created_at"`
}

type Agent struct {
	ID               string    `json:"id"`
	OwnerWorkspaceID *string   `json:"owner_workspace_id"`
	Slug             string    `json:"slug"`
	Name             string    `json:"name"`
	Description      string    `json:"description"`
	Visibility       string    `json:"visibility"`
	Status           string    `json:"status"`
	LatestVersion    *Version  `json:"latest_version"`
	CreatedAt        time.Time `json:"created_at"`
	UpdatedAt        time.Time `json:"updated_at"`
}

type VersionInput struct {
	VersionLabel   string
	InterfaceKind  string
	ToolAccessMode string
	EndpointURL    string
	Capabilities   []string
	// ClaimedCapabilityIDs selects competencies from the active server catalog.
	// Persisted immutably at registration; new behavior requires a new version.
	ClaimedCapabilityIDs   []string
	ImplementationIdentity provenance.AgentImplementationIdentity
	Metadata               json.RawMessage
}

type CreateInput struct {
	Name           string
	Slug           string
	Description    string
	Visibility     string
	InitialVersion VersionInput
}

type UpdateInput struct {
	Name        *string
	Description *string
	Visibility  *string
	Status      *string
}

// CapabilityClaim is one immutable competency claim on an exact AgentVersion.
// ClaimStatus is always "claimed" in V1. TestStatus is machine-readable
// (untested by default; partial/tested only from the coverage engine).
// QualificationStatus is always null in V1: per-capability qualification is
// not implemented in this stage and is never inferred from TESTED.
type CapabilityClaim struct {
	CapabilityID        string    `json:"capability_id"`
	CatalogVersion      string    `json:"catalog_version"`
	Domain              string    `json:"domain,omitempty"`
	Family              string    `json:"family,omitempty"`
	DisplayName         string    `json:"display_name,omitempty"`
	Description         string    `json:"description,omitempty"`
	ClaimStatus         string    `json:"claim_status"`
	TestStatus          string    `json:"test_status"`
	QualificationStatus *string   `json:"qualification_status"`
	ClaimedAt           time.Time `json:"claimed_at"`
}

type Filters struct {
	Pagination    identity.Pagination
	Query         string
	Capability    string
	InterfaceKind string
	Status        string
}
