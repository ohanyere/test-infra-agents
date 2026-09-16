package evaluationsuite

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/evalplane/evalplane/backend/internal/identity"
)

func ValidateCreateSuite(in CreateSuiteInput) (CreateSuiteInput, error) {
	in.Name = strings.TrimSpace(in.Name)
	if in.Name == "" || len(in.Name) > 200 {
		return in, invalid("name is required and must be at most 200 characters")
	}
	return in, nil
}

func ValidateVersion(in CreateVersionInput) (CreateVersionInput, error) {
	in.Description = strings.TrimSpace(in.Description)
	if len(in.Description) > 4000 {
		return in, invalid("description must be at most 4000 characters")
	}
	if len(in.Metadata) == 0 {
		in.Metadata = json.RawMessage(`{}`)
	}
	var metadata map[string]any
	if json.Unmarshal(in.Metadata, &metadata) != nil || metadata == nil {
		return in, invalid("metadata must be a JSON object")
	}
	if len(in.Scenarios) == 0 || len(in.Scenarios) > 100 {
		return in, invalid("scenarios must contain between 1 and 100 entries")
	}
	seen := map[string]bool{}
	for i := range in.Scenarios {
		s := &in.Scenarios[i]
		if identity.ValidateUUID(s.ScenarioVersionID) != nil {
			return in, invalid("scenario_version_id must be a valid UUID")
		}
		if seen[s.ScenarioVersionID] {
			return in, invalid("scenario versions must not contain duplicates")
		}
		seen[s.ScenarioVersionID] = true
		if s.TrialCount < 1 || s.TrialCount > 20 {
			return in, invalid("trial_count must be between 1 and 20")
		}
		if len(s.HiddenVariants) > 20 {
			return in, invalid("hidden_variants must contain at most 20 entries")
		}
		variantSeen := map[string]bool{}
		for _, variant := range s.HiddenVariants {
			if err := validateHiddenVariant(variant); err != nil {
				return in, err
			}
			raw, _ := json.Marshal(variant)
			if variantSeen[string(raw)] {
				return in, invalid("hidden_variants must not contain duplicates")
			}
			variantSeen[string(raw)] = true
		}
		// Request array order is the canonical, stable membership order.
		s.SortOrder = i
	}
	return in, nil
}

func validateHiddenVariant(variant HiddenVariantConfig) error {
	if variant.TrafficVUs == nil && variant.InitialReplicas == nil && variant.CapacityPerPod == nil && variant.TrafficDelaySeconds == nil {
		return invalid("hidden variant must configure at least one supported dimension")
	}
	if variant.TrafficVUs != nil && (*variant.TrafficVUs < 1 || *variant.TrafficVUs > 1000) {
		return invalid("traffic_vus must be between 1 and 1000")
	}
	if variant.InitialReplicas != nil && (*variant.InitialReplicas < 1 || *variant.InitialReplicas > 20) {
		return invalid("initial_replicas must be between 1 and 20")
	}
	if variant.CapacityPerPod != nil && (*variant.CapacityPerPod < 1 || *variant.CapacityPerPod > 10000) {
		return invalid("capacity_per_pod must be between 1 and 10000")
	}
	if variant.TrafficDelaySeconds != nil && (*variant.TrafficDelaySeconds < 0 || *variant.TrafficDelaySeconds > 60) {
		return invalid("traffic_delay_seconds must be between 0 and 60")
	}
	return nil
}

func ValidateStart(in StartExecutionInput) (StartExecutionInput, error) {
	if identity.ValidateUUID(in.SuiteVersionID) != nil {
		return in, invalid("suite_version_id must be a valid UUID")
	}
	if len(in.AgentVersionIDs) < 1 || len(in.AgentVersionIDs) > 20 {
		return in, invalid("agent_version_ids must contain between 1 and 20 values")
	}
	seen := map[string]bool{}
	for _, id := range in.AgentVersionIDs {
		if identity.ValidateUUID(id) != nil {
			return in, invalid("agent_version_ids must contain valid UUIDs")
		}
		if seen[id] {
			return in, invalid("agent_version_ids must not contain duplicates")
		}
		seen[id] = true
	}
	return in, nil
}

func invalid(message string) error { return fmt.Errorf("%w: %s", identity.ErrValidation, message) }
