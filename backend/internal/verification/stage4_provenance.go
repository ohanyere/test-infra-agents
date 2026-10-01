package verification

import (
	"github.com/evalplane/evalplane/backend/internal/environment"
	"github.com/evalplane/evalplane/backend/internal/gitops"
	"github.com/evalplane/evalplane/backend/internal/provenance"
)

// Stage 4 provider provenance expectations, mirrored from the hosted
// verification-e2e workflow assertion ("Stage 4 provider provenance"). The
// workflow jq expression remains the hosted authority; this helper gives Go
// tests and diagnostics the same contract without duplicating literals.
type stage4ProviderExpectation struct {
	role            string
	providerID      string
	requireAnyID    bool
	contractVersion string
}

func stage4ProviderExpectations() []stage4ProviderExpectation {
	return []stage4ProviderExpectation{
		{role: "environment_runtime", requireAnyID: true, contractVersion: environment.ProviderContractVersion},
		{role: "workload_state", providerID: "kubernetes-api"},
		{role: "telemetry", providerID: "prometheus"},
		{role: "git_desired_state", providerID: "git", contractVersion: gitops.EvidenceContractVersion},
		{role: "argo_state", providerID: "argocd", contractVersion: gitops.EvidenceContractVersion},
		{role: "stability", providerID: "verification-stability-sampler"},
		{role: "action_policy", providerID: "executionworker-action-policy"},
		{role: "action_executor", providerID: "kubernetes-client-go"},
	}
}

// Stage4ProviderShortfalls flags persisted provider provenance that would
// fail the hosted Stage 4 assertion. It reports one entry per unmet role and
// returns nil when the record satisfies the contract. A nil provenance (for
// example a historical pre-Stage-4 row) reports every role as missing; callers
// deciding compatibility of historical rows must treat that case separately
// instead of rejecting the row.
func Stage4ProviderShortfalls(p *provenance.Verification) []string {
	expectations := stage4ProviderExpectations()
	if p == nil {
		out := make([]string, 0, len(expectations))
		for _, want := range expectations {
			out = append(out, "provider role "+want.role+" is missing: provenance is absent")
		}
		return out
	}
	var out []string
	for _, want := range expectations {
		matched := false
		for _, got := range p.Providers {
			if got.Role != want.role {
				continue
			}
			if want.requireAnyID && got.ProviderID == "" {
				continue
			}
			if !want.requireAnyID && got.ProviderID != want.providerID {
				continue
			}
			if want.contractVersion != "" && got.ContractVersion != want.contractVersion {
				continue
			}
			matched = true
			break
		}
		if !matched {
			out = append(out, "provider role "+want.role+" is missing or mismatched")
		}
	}
	return out
}
