package competency

import (
	"fmt"
	"sort"
	"strings"
)

// MaxClaims bounds the number of competency claims per AgentVersion.
const MaxClaims = 32

// ValidateClaimIDs rejects unknown, non-active, duplicate, or empty claims.
// Only active catalog entries are claimable in V1. Arbitrary customer-created
// capability IDs are never accepted.
func ValidateClaimIDs(ids []string) ([]string, error) {
	if len(ids) > MaxClaims {
		return nil, fmt.Errorf("claimed capabilities must contain at most %d values", MaxClaims)
	}
	out := append([]string{}, ids...)
	seen := make(map[string]struct{}, len(out))
	for _, id := range out {
		if strings.TrimSpace(id) == "" {
			return nil, fmt.Errorf("claimed capability must not be blank")
		}
		if _, dup := seen[id]; dup {
			return nil, fmt.Errorf("claimed capabilities must not contain duplicates")
		}
		seen[id] = struct{}{}
		item, ok := Get(id)
		if !ok {
			return nil, fmt.Errorf("claimed capability %q is unknown: select from the active server catalog", id)
		}
		if item.Status != StatusActive {
			return nil, fmt.Errorf("claimed capability %q is not active and cannot be claimed", id)
		}
	}
	sort.Strings(out)
	return out, nil
}

// Incompatibility describes runtime capabilities missing for a claim.
type Incompatibility struct {
	CapabilityID string   `json:"capability_id"`
	Missing      []string `json:"missing_runtime_capabilities"`
	Required     []string `json:"required_runtime_capabilities"`
}

// MissingRequirements returns structured incompatibilities between declared
// low-level runtime capabilities and claimed competencies. It is tool/runtime
// compatibility only, not scenario coverage.
func MissingRequirements(declaredRuntime, claimed []string) []Incompatibility {
	have := make(map[string]struct{}, len(declaredRuntime))
	for _, id := range declaredRuntime {
		have[id] = struct{}{}
	}
	out := []Incompatibility{}
	for _, id := range claimed {
		item, ok := Get(id)
		if !ok {
			continue
		}
		missing := []string{}
		for _, req := range item.Requires {
			if _, ok := have[req]; !ok {
				missing = append(missing, req)
			}
		}
		if len(missing) > 0 {
			sort.Strings(missing)
			required := append([]string(nil), item.Requires...)
			sort.Strings(required)
			out = append(out, Incompatibility{CapabilityID: id, Missing: missing, Required: required})
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].CapabilityID < out[j].CapabilityID })
	return out
}

// ValidateCompatibility fails registration when a claimed competency requires
// tool/runtime capabilities the version does not declare. Missing permissions
// are never auto-added.
func ValidateCompatibility(declaredRuntime, claimed []string) error {
	missing := MissingRequirements(declaredRuntime, claimed)
	if len(missing) == 0 {
		return nil
	}
	parts := make([]string, 0, len(missing))
	for _, item := range missing {
		parts = append(parts, fmt.Sprintf("%s requires runtime capabilities [%s] (missing [%s])", item.CapabilityID, strings.Join(item.Required, ", "), strings.Join(item.Missing, ", ")))
	}
	return fmt.Errorf("claimed capabilities are incompatible with declared runtime capabilities: %s", strings.Join(parts, "; "))
}
