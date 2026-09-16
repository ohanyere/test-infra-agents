package scenariocertification

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"sort"
	"strings"

	"github.com/evalplane/evalplane/backend/internal/identity"
	"github.com/evalplane/evalplane/backend/internal/scenario"
)

var allowedReferenceActionTypes = map[string]bool{
	"inspect":        true,
	"scale_workload": true,
	"restart_pod":    true,
	"no_op":          true,
	// Provider-neutral state transformations for non-Kubernetes futures.
	"set_state":     true,
	"verify_state":  true,
	"cleanup_state": true,
}

// ValidateReferenceInput enforces the smallest useful reference remediation
// contract: preconditions, ordered reference actions or state transformation,
// expected resulting state, verification notes, cleanup expectations.
func ValidateReferenceInput(in ReferenceRemediationInput) (ReferenceRemediationInput, error) {
	if len(in.Preconditions) == 0 {
		in.Preconditions = json.RawMessage(`{}`)
	}
	if len(in.ExpectedState) == 0 {
		in.ExpectedState = json.RawMessage(`{}`)
	}
	if err := requireJSONObject(in.Preconditions, "preconditions"); err != nil {
		return in, err
	}
	if err := requireJSONObject(in.ExpectedState, "expected_resulting_state"); err != nil {
		return in, err
	}
	if len(in.ReferenceActions) == 0 {
		return in, validation("reference remediation must include at least one ordered reference action or state transformation")
	}
	if len(in.ReferenceActions) > 64 {
		return in, validation("reference remediation must contain at most 64 reference actions")
	}
	seen := map[string]bool{}
	for i := range in.ReferenceActions {
		action := in.ReferenceActions[i]
		action.Type = strings.TrimSpace(action.Type)
		action.Target = strings.TrimSpace(action.Target)
		if !allowedReferenceActionTypes[action.Type] {
			return in, validation(fmt.Sprintf("reference action %d has unsupported type %q", i, in.ReferenceActions[i].Type))
		}
		if action.Type == "scale_workload" {
			if action.Target == "" {
				return in, validation("scale_workload reference actions must name a target")
			}
			if action.Replicas == nil || *action.Replicas < 1 || *action.Replicas > 100 {
				return in, validation("scale_workload reference actions must set replicas between 1 and 100")
			}
		}
		if len(action.Params) > 0 && !json.Valid(action.Params) {
			return in, validation(fmt.Sprintf("reference action %d has invalid params JSON", i))
		}
		key := action.Type + "\x00" + action.Target
		if seen[key] && action.Type != "inspect" {
			// Duplicate non-inspect steps usually indicate a copy error.
			return in, validation("reference remediation must not repeat identical non-inspect actions")
		}
		seen[key] = true
		in.ReferenceActions[i] = action
	}
	in.VerificationNotes = strings.TrimSpace(in.VerificationNotes)
	in.CleanupExpected = strings.TrimSpace(in.CleanupExpected)
	if len(in.VerificationNotes) > 4000 || len(in.CleanupExpected) > 4000 {
		return in, validation("verification notes and cleanup expectations must each be at most 4000 characters")
	}
	return in, nil
}

// HashReferenceContent pins the exact reference bytes for stale detection.
func HashReferenceContent(in ReferenceRemediationInput) string {
	actions, _ := json.Marshal(in.ReferenceActions)
	sum := sha256.Sum256([]byte(string(in.Preconditions) + "\x00" + string(actions) + "\x00" + string(in.ExpectedState)))
	return hex.EncodeToString(sum[:])
}

// CriteriaVersion pins the exact criteria definition for stale detection.
func CriteriaVersion(criteria []scenario.Criterion) string {
	parts := make([]string, 0, len(criteria))
	for _, c := range criteria {
		parts = append(parts, c.Key+"\x00"+c.MetricKey+"\x00"+c.Operator+"\x00"+string(c.TargetValue)+"\x00"+nullableUnit(c.Unit))
	}
	sort.Strings(parts)
	sum := sha256.Sum256([]byte(strings.Join(parts, "\n")))
	return hex.EncodeToString(sum[:])[:32]
}

func nullableUnit(unit *string) string {
	if unit == nil {
		return ""
	}
	return strings.ToLower(strings.TrimSpace(*unit))
}

func requireJSONObject(raw json.RawMessage, label string) error {
	var v map[string]any
	if len(raw) == 0 {
		return validation(label + " must be a JSON object")
	}
	if json.Unmarshal(raw, &v) != nil || v == nil {
		return validation(label + " must be a JSON object")
	}
	return nil
}

func validation(message string) error {
	return fmt.Errorf("%w: %s", identity.ErrValidation, message)
}
