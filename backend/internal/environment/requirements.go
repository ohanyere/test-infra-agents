package environment

import (
	"errors"
	"fmt"
	"sort"
	"strings"
)

func errValidation(message string) error {
	return validationError{message: message}
}

type validationError struct{ message string }

func (e validationError) Error() string { return e.message }

// IsValidation reports whether err is an environment validation failure.
func IsValidation(err error) bool {
	var target validationError
	return errors.As(err, &target)
}

// Requirements declares the server-reviewed infrastructure an exact
// ScenarioVersion needs. Part of task execution metadata: it must never
// expose ground truth, reference remediation, hidden variant values, or
// grader internals. The agent may know what infrastructure/tools are
// available; it must not receive evaluator-only reasoning.
type Requirements struct {
	RequiredComponents   []string `json:"required_components"`
	MinimumIsolation     string   `json:"minimum_isolation_tier"`
	RequiredCapabilities []string `json:"required_profile_capabilities,omitempty"`
	RuntimeContract      string   `json:"runtime_contract_version,omitempty"`
	ContractVersion      string   `json:"contract_version"`
}

// RequirementsContractVersion pins requirement semantics.
const RequirementsContractVersion = "environment-requirements:v1"

// ValidateRequirements normalizes and validates requirements.
func ValidateRequirements(in Requirements) (Requirements, error) {
	seen := map[string]bool{}
	components := make([]string, 0, len(in.RequiredComponents))
	for _, raw := range in.RequiredComponents {
		trimmed := strings.TrimSpace(raw)
		if trimmed == "" {
			return Requirements{}, errValidation("required component must not be blank")
		}
		if seen[trimmed] {
			continue
		}
		if _, err := ValidateComponentID(trimmed); err != nil {
			return Requirements{}, err
		}
		seen[trimmed] = true
		components = append(components, trimmed)
	}
	if len(components) == 0 {
		return Requirements{}, errValidation("at least one required component is needed")
	}
	sort.Strings(components)
	minimum := strings.TrimSpace(in.MinimumIsolation)
	if !IsValidIsolation(minimum) {
		return Requirements{}, errValidation("minimum isolation tier must be one of namespace, virtual_cluster, dedicated_cluster")
	}
	runtime := strings.TrimSpace(in.RuntimeContract)
	if runtime == "" {
		runtime = DefaultRuntimeContract
	}
	if !IsAllowedRuntimeContract(runtime) {
		return Requirements{}, errValidation("unsupported runtime contract " + quote(runtime) + ": select a server-owned runtime contract")
	}
	caps := make([]string, 0, len(in.RequiredCapabilities))
	for _, raw := range in.RequiredCapabilities {
		trimmed := strings.TrimSpace(raw)
		if trimmed == "" {
			continue
		}
		if len(trimmed) > 200 {
			return Requirements{}, errValidation("required profile capability must be at most 200 characters")
		}
		caps = append(caps, trimmed)
	}
	sort.Strings(caps)
	return Requirements{
		RequiredComponents:   components,
		MinimumIsolation:     minimum,
		RequiredCapabilities: caps,
		RuntimeContract:      runtime,
		ContractVersion:      RequirementsContractVersion,
	}, nil
}

// LegacyDefaultRequirements is the compatibility default for ScenarioVersions
// with no explicit requirements: current supported traffic-spike/Kubernetes
// behavior. It claims only namespace isolation, never stronger.
func LegacyDefaultRequirements() Requirements {
	return Requirements{
		RequiredComponents: []string{"kubernetes", "prometheus"},
		MinimumIsolation:   IsolationNamespace,
		RuntimeContract:    DefaultRuntimeContract,
		ContractVersion:    RequirementsContractVersion,
	}
}

// RequirementsForTaskFamily returns suggested server-reviewed requirements
// for well-known task shapes. Unknown shapes fall back to the legacy
// default; assigning requirements never makes an unsupported scenario
// runnable.
func RequirementsForTaskFamily(familyID, caseType string) Requirements {
	switch {
	case strings.HasPrefix(familyID, "gitops.") || familyID == "gitops.drift.deployment" || familyID == "gitops.argocd.recovery" || familyID == "gitops.desired_state.remediation":
		return Requirements{
			RequiredComponents: []string{"argocd", "git", "helm", "kubernetes", "prometheus"},
			MinimumIsolation:   IsolationNamespace,
			RuntimeContract:    DefaultRuntimeContract,
			ContractVersion:    RequirementsContractVersion,
		}
	case strings.HasPrefix(familyID, "delivery.progressive") || familyID == "delivery.progressive.error_regression":
		// Progressive-delivery canary analysis needs the rollout
		// controller in addition to workload telemetry. argo_rollouts is
		// a planned component in no active profile, so these requirements
		// stay honest: no eligible environment until a real provider
		// exists.
		return Requirements{
			RequiredComponents: []string{"argo_rollouts", "kubernetes", "prometheus"},
			MinimumIsolation:   IsolationNamespace,
			RuntimeContract:    DefaultRuntimeContract,
			ContractVersion:    RequirementsContractVersion,
		}
	case familyID == "kubernetes.capacity.traffic_spike" || familyID == "kubernetes.workload.baseline" || familyID == "":
		return Requirements{
			RequiredComponents: []string{"kubernetes", "prometheus"},
			MinimumIsolation:   IsolationNamespace,
			RuntimeContract:    DefaultRuntimeContract,
			ContractVersion:    RequirementsContractVersion,
		}
	default:
		// Node/cluster-class situations need dedicated isolation when they
		// become runnable; until then requirements stay honest.
		if strings.Contains(familyID, "node") || strings.Contains(familyID, "cluster") {
			return Requirements{
				RequiredComponents: []string{"kubernetes", "prometheus"},
				MinimumIsolation:   IsolationDedicatedCluster,
				RuntimeContract:    DefaultRuntimeContract,
				ContractVersion:    RequirementsContractVersion,
			}
		}
		return LegacyDefaultRequirements()
	}
}

// SanitizedRequirementsForAgent returns only agent-safe infrastructure
// disclosure: component availability and isolation tier. Evaluator-only
// reasoning is never included (this struct carries none by construction).
func SanitizedRequirementsForAgent(in Requirements) map[string]any {
	return map[string]any{
		"available_components": append([]string(nil), in.RequiredComponents...),
		"minimum_isolation":    in.MinimumIsolation,
		"runtime_contract":     in.RuntimeContract,
	}
}

var _ = fmt.Sprint
