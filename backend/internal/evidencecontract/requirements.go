// Package evidencecontract defines the fixed evidence applicability contract
// shared by immutable scenario versions and the resulting-state verifier.
package evidencecontract

import "fmt"

const ContractV1 = "evidence-requirements:v1"

type Requirement string

const (
	Required      Requirement = "REQUIRED"
	NotApplicable Requirement = "NOT_APPLICABLE"
)

type Requirements struct {
	ContractVersion string      `json:"contract_version"`
	WorkloadState   Requirement `json:"workload_state"`
	Telemetry       Requirement `json:"telemetry"`
	GitDesiredState Requirement `json:"git_desired_state"`
	ArgoState       Requirement `json:"argo_state"`
	Stability       Requirement `json:"stability"`
}

// DefaultRequirements preserves the current non-GitOps runtime contract:
// owned workload state, runtime telemetry, and bounded stability are required.
func DefaultRequirements() Requirements {
	return Requirements{
		ContractVersion: ContractV1,
		WorkloadState:   Required,
		Telemetry:       Required,
		GitDesiredState: NotApplicable,
		ArgoState:       NotApplicable,
		Stability:       Required,
	}
}

func GitOpsRequirements() Requirements {
	out := DefaultRequirements()
	out.GitDesiredState = Required
	out.ArgoState = Required
	return out
}

func (r Requirements) IsZero() bool {
	return r.ContractVersion == "" && r.WorkloadState == "" && r.Telemetry == "" && r.GitDesiredState == "" && r.ArgoState == "" && r.Stability == ""
}

func (r Requirements) Validate() error {
	if r.ContractVersion != ContractV1 {
		return fmt.Errorf("unsupported evidence requirements contract %q", r.ContractVersion)
	}
	for name, requirement := range map[string]Requirement{
		"workload_state": r.WorkloadState, "telemetry": r.Telemetry,
		"git_desired_state": r.GitDesiredState, "argo_state": r.ArgoState,
		"stability": r.Stability,
	} {
		if requirement != Required && requirement != NotApplicable {
			return fmt.Errorf("evidence requirement %s must be REQUIRED or NOT_APPLICABLE", name)
		}
	}
	if r.GitDesiredState != r.ArgoState {
		return fmt.Errorf("git_desired_state and argo_state must have the same requirement for the current GitOps invariant")
	}
	if r.Stability == Required && r.WorkloadState != Required {
		return fmt.Errorf("required stability evidence requires workload_state")
	}
	return nil
}
