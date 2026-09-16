package coverage

import (
	"fmt"
	"sort"
	"strings"
)

// Case types. Every coverage-relevant ScenarioVersion carries one explicit
// value. Never inferred from scenario names and never classified by an LLM.
const (
	CaseTypePositive    = "positive"
	CaseTypeNegative    = "negative"
	CaseTypeDecoy       = "decoy"
	CaseTypeAdversarial = "adversarial"
	CaseTypeEdge        = "edge"
)

// CaseTypeDefinitions documents V1 semantics for operators and UI help text.
var CaseTypeDefinitions = map[string]string{
	CaseTypePositive:    "The expected capability really is needed.",
	CaseTypeNegative:    "The apparent problem exists, but the claimed remediation should not be used.",
	CaseTypeDecoy:       "Symptoms tempt an unrelated/common incorrect diagnosis.",
	CaseTypeAdversarial: "The environment intentionally tempts unsafe or forbidden action.",
	CaseTypeEdge:        "Boundary behavior or unusual but valid conditions.",
}

// ValidateCaseType rejects blank and unknown case types.
func ValidateCaseType(value string) (string, error) {
	trimmed := strings.TrimSpace(value)
	switch trimmed {
	case CaseTypePositive, CaseTypeNegative, CaseTypeDecoy, CaseTypeAdversarial, CaseTypeEdge:
		return trimmed, nil
	default:
		return "", fmt.Errorf("case_type must be one of positive, negative, decoy, adversarial, edge")
	}
}

// IsContrastCase reports whether the case type exercises restraint: the agent
// must withhold, redirect, or refuse an obvious-but-wrong action.
func IsContrastCase(value string) bool {
	switch value {
	case CaseTypeNegative, CaseTypeDecoy, CaseTypeAdversarial:
		return true
	default:
		return false
	}
}

// NormalizeCaseTypes deduplicates and sorts exercised case types for stable output.
func NormalizeCaseTypes(values []string) []string {
	seen := map[string]bool{}
	out := []string{}
	for _, value := range values {
		trimmed := strings.TrimSpace(value)
		if trimmed == "" || seen[trimmed] {
			continue
		}
		seen[trimmed] = true
		out = append(out, trimmed)
	}
	sort.Strings(out)
	return out
}
