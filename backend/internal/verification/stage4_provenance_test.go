package verification

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/evalplane/evalplane/backend/internal/evaluator"
	"github.com/evalplane/evalplane/backend/internal/evidence"
	"github.com/evalplane/evalplane/backend/internal/evidencecontract"
	"github.com/evalplane/evalplane/backend/internal/gitops"
	"github.com/evalplane/evalplane/backend/internal/provenance"
)

// stage4CanonicalInput mirrors the hosted canonical run shape: GitOps
// requirements, an executed set_workload_image remediation, final Git desired
// v18 / live v17 divergence with Argo OutOfSync, and a real environment
// provider identity as persisted through the managed run lifecycle.
func stage4CanonicalInput() BuildInput {
	in := validInput()
	in.EvidenceRequirements = evidencecontract.GitOpsRequirements()
	desired := &gitops.DesiredState{Repository: "fixture", Revision: "0123456789abcdef0123456789abcdef01234567", Path: "checkout.yaml", Resource: "Deployment/run-ns/checkout", Container: "checkout", Image: "checkout:v18", Reference: "git:pin:checkout.yaml"}
	argo := &gitops.ArgoState{Application: "checkout", Namespace: "argocd", SyncStatus: "OutOfSync", Automated: true, Reference: "argo-application:argocd/checkout:rv:7"}
	state := &gitops.Evidence{ContractVersion: gitops.EvidenceContractVersion, Desired: desired, Argo: argo}
	before := healthy("checkout:v18", 3)
	after := healthy("checkout:v17", 3)
	in.Events = []evidence.RunEvent{gitopsEvent(1, "verification_baseline_captured", in.Now, before, &gitops.Evidence{ContractVersion: gitops.EvidenceContractVersion, Desired: desired, Argo: &gitops.ArgoState{Application: "checkout", Namespace: "argocd", SyncStatus: "Synced", Reference: "argo:baseline"}}), gitopsEvent(2, "stability_observation_collected", in.Now.Add(time.Second), after, state), gitopsEvent(3, "final_observation_collected", in.Now.Add(2*time.Second), after, state)}
	in.Actions = []evidence.AgentAction{{ID: "action", RunID: "run", ActionType: "set_workload_image", Status: "executed", Authorized: true, TargetType: str("Deployment"), TargetRef: str("run-ns/checkout"), RequestPayload: json.RawMessage(`{"image":"checkout:v17"}`), ResultPayload: json.RawMessage(`{"status":"executed"}`), CreatedAt: in.Now}}
	in.Evaluation = Evaluation{Status: "pass", Summary: "All required criteria passed.", ContractVersion: evaluator.ContractVersion}
	return in
}

func TestStage4CanonicalInputSatisfiesProviderContract(t *testing.T) {
	got := Derive(stage4CanonicalInput(), GitOpsReconciliationAnalyzer{})
	if got.OverallStatus != StatusFailed {
		t.Fatalf("canonical verdict changed: status=%s reason=%s", got.OverallStatus, got.Reason)
	}
	if shortfalls := Stage4ProviderShortfalls(got.Provenance); len(shortfalls) != 0 {
		t.Fatalf("shortfalls=%v providers=%+v", shortfalls, got.Provenance.Providers)
	}
	seen := map[string]provenance.ProviderIdentity{}
	for _, provider := range got.Provenance.Providers {
		if previous, dup := seen[provider.Role]; dup && previous != provider {
			t.Fatalf("role %s overwritten: %+v vs %+v", provider.Role, previous, provider)
		}
		seen[provider.Role] = provider
	}
	for _, want := range []stage4ProviderExpectation{{role: "environment_runtime", providerID: "kind-dev:v1"}, {role: "workload_state", providerID: "kubernetes-api"}, {role: "telemetry", providerID: "prometheus"}, {role: "git_desired_state", providerID: "git"}, {role: "argo_state", providerID: "argocd"}, {role: "stability", providerID: "verification-stability-sampler"}, {role: "action_policy", providerID: "executionworker-action-policy"}, {role: "action_executor", providerID: "kubernetes-client-go"}} {
		provider, ok := seen[want.role]
		if !ok || provider.ProviderID != want.providerID {
			t.Fatalf("role %s identity lost: providers=%+v", want.role, got.Provenance.Providers)
		}
	}
}

func TestStage4MissingEnvironmentProvenanceIsFlaggedWithoutChangingVerdict(t *testing.T) {
	withProvider := Derive(stage4CanonicalInput(), GitOpsReconciliationAnalyzer{})
	legacy := stage4CanonicalInput()
	legacy.EnvironmentProviderID = ""
	legacy.EnvironmentProviderContractVersion = ""
	withoutProvider := Derive(legacy, GitOpsReconciliationAnalyzer{})
	if withoutProvider.OverallStatus != withProvider.OverallStatus || withoutProvider.Validity != withProvider.Validity {
		t.Fatalf("verdict depends on provider provenance: with=%s/%s without=%s/%s", withProvider.OverallStatus, withProvider.Validity, withoutProvider.OverallStatus, withoutProvider.Validity)
	}
	shortfalls := Stage4ProviderShortfalls(withoutProvider.Provenance)
	if len(shortfalls) != 1 || shortfalls[0] != "provider role environment_runtime is missing or mismatched" {
		t.Fatalf("shortfalls=%v providers=%+v", shortfalls, withoutProvider.Provenance.Providers)
	}
	for _, provider := range withoutProvider.Provenance.Providers {
		if provider.Role == "environment_runtime" {
			t.Fatalf("empty provider identity was invented: %+v", provider)
		}
	}
}

func TestStage4StaleProviderContractIsFlagged(t *testing.T) {
	in := stage4CanonicalInput()
	in.EnvironmentProviderContractVersion = "environment-provider-contract:v0"
	got := Derive(in, GitOpsReconciliationAnalyzer{})
	shortfalls := Stage4ProviderShortfalls(got.Provenance)
	if len(shortfalls) != 1 || shortfalls[0] != "provider role environment_runtime is missing or mismatched" {
		t.Fatalf("shortfalls=%v providers=%+v", shortfalls, got.Provenance.Providers)
	}
	if got.OverallStatus != StatusFailed {
		t.Fatalf("verdict changed by contract pin: status=%s", got.OverallStatus)
	}
}

func TestStage4ProviderShortfallsTable(t *testing.T) {
	complete := &provenance.Verification{ContractVersion: provenance.ContractVersion, Providers: []provenance.ProviderIdentity{
		{Role: "environment_runtime", ProviderID: "kind-dev:v1", ContractVersion: "environment-provider-contract:v1"},
		{Role: "workload_state", ProviderID: "kubernetes-api"},
		{Role: "telemetry", ProviderID: "prometheus"},
		{Role: "git_desired_state", ProviderID: "git", ContractVersion: "gitops-evidence:v1"},
		{Role: "argo_state", ProviderID: "argocd", ContractVersion: "gitops-evidence:v1"},
		{Role: "stability", ProviderID: "verification-stability-sampler"},
		{Role: "action_policy", ProviderID: "executionworker-action-policy"},
		{Role: "action_executor", ProviderID: "kubernetes-client-go"},
	}}
	if shortfalls := Stage4ProviderShortfalls(complete); len(shortfalls) != 0 {
		t.Fatalf("complete provenance flagged: %v", shortfalls)
	}
	if shortfalls := Stage4ProviderShortfalls(nil); len(shortfalls) != 8 {
		t.Fatalf("absent provenance must flag every role: %v", shortfalls)
	}
	if shortfalls := Stage4ProviderShortfalls(&provenance.Verification{}); len(shortfalls) != 8 {
		t.Fatalf("empty providers must flag every role: %v", shortfalls)
	}
	for _, tc := range []struct {
		name   string
		mutate func(*provenance.Verification)
		role   string
	}{
		{"environment runtime removed", func(p *provenance.Verification) { p.Providers = dropProviderRole(p.Providers, "environment_runtime") }, "environment_runtime"},
		{"environment contract stale", func(p *provenance.Verification) {
			setProviderContract(p, "environment_runtime", "environment-provider-contract:v0")
		}, "environment_runtime"},
		{"workload provider replaced", func(p *provenance.Verification) { setProviderID(p, "workload_state", "other") }, "workload_state"},
		{"telemetry removed", func(p *provenance.Verification) { p.Providers = dropProviderRole(p.Providers, "telemetry") }, "telemetry"},
		{"git contract missing", func(p *provenance.Verification) { setProviderContract(p, "git_desired_state", "") }, "git_desired_state"},
		{"argo provider replaced", func(p *provenance.Verification) { setProviderID(p, "argo_state", "other") }, "argo_state"},
		{"stability removed", func(p *provenance.Verification) { p.Providers = dropProviderRole(p.Providers, "stability") }, "stability"},
		{"action policy removed", func(p *provenance.Verification) { p.Providers = dropProviderRole(p.Providers, "action_policy") }, "action_policy"},
		{"action executor removed", func(p *provenance.Verification) { p.Providers = dropProviderRole(p.Providers, "action_executor") }, "action_executor"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			mutated := *complete
			mutated.Providers = append([]provenance.ProviderIdentity(nil), complete.Providers...)
			tc.mutate(&mutated)
			shortfalls := Stage4ProviderShortfalls(&mutated)
			if len(shortfalls) != 1 || shortfalls[0] != "provider role "+tc.role+" is missing or mismatched" {
				t.Fatalf("shortfalls=%v", shortfalls)
			}
		})
	}
}

func dropProviderRole(providers []provenance.ProviderIdentity, role string) []provenance.ProviderIdentity {
	out := providers[:0]
	for _, provider := range providers {
		if provider.Role != role {
			out = append(out, provider)
		}
	}
	return out
}

func setProviderID(p *provenance.Verification, role, id string) {
	for i := range p.Providers {
		if p.Providers[i].Role == role {
			p.Providers[i].ProviderID = id
		}
	}
}

func setProviderContract(p *provenance.Verification, role, contract string) {
	for i := range p.Providers {
		if p.Providers[i].Role == role {
			p.Providers[i].ContractVersion = contract
		}
	}
}
