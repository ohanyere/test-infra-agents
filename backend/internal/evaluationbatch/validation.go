package evaluationbatch

import (
	"encoding/json"
	"fmt"
	"github.com/evalplane/evalplane/backend/internal/identity"
	"regexp"
	"strings"
)

func ValidateCreate(in CreateInput) (CreateInput, error) {
	in.IdempotencyKey = strings.TrimSpace(in.IdempotencyKey)
	if len(in.IdempotencyKey) > 200 {
		return in, invalid("idempotency_key must be at most 200 characters")
	}
	for _, value := range []string{in.SourceMetadata.Provider, in.SourceMetadata.Repository, in.SourceMetadata.CommitSHA, in.SourceMetadata.PullRequest, in.SourceMetadata.Workflow} {
		if len(value) > 500 {
			return in, invalid("source_metadata values must be at most 500 characters")
		}
	}
	if in.SourceMetadata.CommitSHA != "" && !regexp.MustCompile(`^[0-9A-Fa-f]{7,64}$`).MatchString(in.SourceMetadata.CommitSHA) {
		return in, invalid("source_metadata.commit_sha must be a hexadecimal commit identifier")
	}
	if identity.ValidateUUID(in.ScenarioVersionID) != nil {
		return in, invalid("scenario_version_id must be a valid UUID")
	}
	if len(in.AgentVersionIDs) < 1 {
		return in, invalid("at least one agent_version_id is required")
	}
	if len(in.AgentVersionIDs) > 20 {
		return in, invalid("agent_version_ids must contain at most 20 values")
	}
	seen := map[string]struct{}{}
	for _, id := range in.AgentVersionIDs {
		if identity.ValidateUUID(id) != nil {
			return in, invalid("agent_version_ids must contain valid UUIDs")
		}
		if _, ok := seen[id]; ok {
			return in, invalid("agent_version_ids must not contain duplicates")
		}
		seen[id] = struct{}{}
	}
	if in.RunsPerAgent == 0 {
		in.RunsPerAgent = 1
	}
	if in.RunsPerAgent < 1 || in.RunsPerAgent > 20 {
		return in, invalid("runs_per_agent must be between 1 and 20")
	}
	if in.EnvironmentMode == "" {
		in.EnvironmentMode = "isolated"
	}
	if in.EnvironmentMode != "isolated" {
		return in, invalid("environment_mode must be isolated")
	}
	if len(in.Config) == 0 {
		in.Config = json.RawMessage(`{}`)
	}
	var object map[string]any
	if json.Unmarshal(in.Config, &object) != nil || object == nil {
		return in, invalid("config must be a JSON object")
	}
	return in, nil
}
func ValidateFilters(in Filters) (Filters, error) {
	if in.Status == "" {
		in.Status = "all"
	}
	switch in.Status {
	case "draft", "queued", "running", "completed", "failed", "stopped", "all":
		return in, nil
	}
	return in, invalid("status filter is invalid")
}
func ValidateRunFilters(in Filters) (Filters, error) {
	if in.Status == "" {
		in.Status = "all"
	}
	switch in.Status {
	case "queued", "provisioning", "running", "completed", "failed", "stopped", "all":
		return in, nil
	}
	return in, invalid("status filter is invalid")
}
func invalid(message string) error { return fmt.Errorf("%w: %s", identity.ErrValidation, message) }
