package agent

import (
	"encoding/json"
	"fmt"
	"regexp"
	"sort"
	"strings"

	"github.com/evalplane/evalplane/backend/internal/agentcapability"
	"github.com/evalplane/evalplane/backend/internal/agentgateway"
	"github.com/evalplane/evalplane/backend/internal/competency"
	"github.com/evalplane/evalplane/backend/internal/identity"
)

const maxCapabilities = 32
const maxCapabilityLength = 100

var capabilityPattern = regexp.MustCompile(`^[a-z0-9]+(?:[_-][a-z0-9]+)*$`)

func ValidateCreate(in CreateInput) (CreateInput, error) {
	in.Name = strings.TrimSpace(in.Name)
	in.Slug = strings.TrimSpace(in.Slug)
	in.Description = strings.TrimSpace(in.Description)
	if in.Visibility == "" {
		in.Visibility = "workspace"
	}
	if in.Name == "" || len(in.Name) > 120 {
		return in, validation("agent name is required and must be at most 120 characters")
	}
	if err := identity.ValidateSlug(in.Slug); err != nil {
		return in, err
	}
	if len(in.Description) > 4000 {
		return in, validation("description must be at most 4000 characters")
	}
	if in.Visibility != "workspace" && in.Visibility != "private" {
		return in, validation("visibility must be workspace or private")
	}
	version, err := ValidateVersion(in.InitialVersion)
	in.InitialVersion = version
	return in, err
}

func ValidateVersion(in VersionInput) (VersionInput, error) {
	in.VersionLabel = strings.TrimSpace(in.VersionLabel)
	if in.VersionLabel == "" || len(in.VersionLabel) > 100 {
		return in, validation("version_label is required and must be at most 100 characters")
	}
	if in.InterfaceKind == "" {
		in.InterfaceKind = "unknown"
	}
	if !validInterfaceKind(in.InterfaceKind) {
		return in, validation("interface_kind must be unknown, http, mcp, webhook, or container")
	}
	if in.ToolAccessMode == "" {
		in.ToolAccessMode = "direct"
	}
	if in.ToolAccessMode != "direct" && in.ToolAccessMode != "mcp" {
		return in, validation("tool_access_mode must be direct or mcp")
	}
	if in.ToolAccessMode == "mcp" && in.InterfaceKind != "http" {
		return in, validation("MCP tool access requires the HTTP agent interface")
	}
	in.EndpointURL = strings.TrimSpace(in.EndpointURL)
	if in.InterfaceKind == "http" {
		if _, err := agentgateway.ValidateEndpoint(in.EndpointURL); err != nil {
			return in, validation("endpoint_url must be a valid HTTP(S) agent endpoint without embedded credentials or fragments")
		}
	} else if in.EndpointURL != "" {
		return in, validation("endpoint_url is supported only for the http interface")
	}
	if len(in.Capabilities) > maxCapabilities {
		return in, validation("capabilities must contain at most 32 values")
	}
	if in.Capabilities == nil {
		in.Capabilities = []string{}
	}
	seen := make(map[string]struct{}, len(in.Capabilities))
	for _, capability := range in.Capabilities {
		if capability == "" || len(capability) > maxCapabilityLength || !capabilityPattern.MatchString(capability) {
			return in, validation("capabilities must be canonical lowercase identifiers of at most 100 characters")
		}
		if _, exists := seen[capability]; exists {
			return in, validation("capabilities must not contain duplicates")
		}
		seen[capability] = struct{}{}
		if in.InterfaceKind == "http" && !agentcapability.Known(capability) {
			return in, validation("capabilities contains an unknown server capability")
		}
	}
	sort.Strings(in.Capabilities)
	claimed, err := competency.ValidateClaimIDs(in.ClaimedCapabilityIDs)
	if err != nil {
		return in, validation(err.Error())
	}
	in.ClaimedCapabilityIDs = claimed
	if err := competency.ValidateCompatibility(in.Capabilities, in.ClaimedCapabilityIDs); err != nil {
		return in, validation(err.Error())
	}
	if len(in.Metadata) == 0 {
		in.Metadata = json.RawMessage(`{}`)
	}
	var metadata map[string]any
	if err := json.Unmarshal(in.Metadata, &metadata); err != nil || metadata == nil {
		return in, validation("metadata must be a JSON object")
	}
	if containsSensitiveMetadata(metadata) {
		return in, validation("metadata must not contain credentials, secrets, tokens, or connection URLs")
	}
	return in, nil
}

func containsSensitiveMetadata(value map[string]any) bool {
	for key, item := range value {
		normalized := strings.ToLower(strings.ReplaceAll(key, "-", "_"))
		for _, forbidden := range []string{"password", "secret", "token", "api_key", "apikey", "credential", "bearer", "connection_url", "webhook_url"} {
			if strings.Contains(normalized, forbidden) {
				return true
			}
		}
		switch nested := item.(type) {
		case map[string]any:
			if containsSensitiveMetadata(nested) {
				return true
			}
		case []any:
			for _, member := range nested {
				if object, ok := member.(map[string]any); ok && containsSensitiveMetadata(object) {
					return true
				}
			}
		}
	}
	return false
}

func ValidateUpdate(in UpdateInput) (UpdateInput, error) {
	if in.Name == nil && in.Description == nil && in.Visibility == nil && in.Status == nil {
		return in, validation("update must contain at least one mutable field")
	}
	if in.Name != nil {
		value := strings.TrimSpace(*in.Name)
		in.Name = &value
		if value == "" || len(value) > 120 {
			return in, validation("agent name is required and must be at most 120 characters")
		}
	}
	if in.Description != nil {
		value := strings.TrimSpace(*in.Description)
		in.Description = &value
		if len(value) > 4000 {
			return in, validation("description must be at most 4000 characters")
		}
	}
	if in.Visibility != nil && *in.Visibility != "workspace" && *in.Visibility != "private" {
		return in, validation("visibility must be workspace or private")
	}
	if in.Status != nil && *in.Status != "active" && *in.Status != "archived" {
		return in, validation("status must be active or archived")
	}
	return in, nil
}

func ValidateFilters(filters Filters, public bool) (Filters, error) {
	filters.Query = strings.TrimSpace(filters.Query)
	filters.Capability = strings.TrimSpace(filters.Capability)
	if filters.Capability != "" && (len(filters.Capability) > maxCapabilityLength || !capabilityPattern.MatchString(filters.Capability)) {
		return filters, validation("capability must be a canonical lowercase identifier")
	}
	if filters.InterfaceKind != "" && !validInterfaceKind(filters.InterfaceKind) {
		return filters, validation("invalid interface_kind")
	}
	if !public && filters.Status == "" {
		filters.Status = "active"
	}
	if !public && filters.Status != "active" && filters.Status != "archived" && filters.Status != "all" {
		return filters, validation("status must be active, archived, or all")
	}
	return filters, nil
}

func validInterfaceKind(value string) bool {
	switch value {
	case "unknown", "http", "mcp", "webhook", "container":
		return true
	default:
		return false
	}
}

func validation(message string) error { return fmt.Errorf("%w: %s", identity.ErrValidation, message) }
