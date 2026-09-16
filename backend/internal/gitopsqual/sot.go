package gitopsqual

import (
	"fmt"
	"strings"
)

// Source-of-truth owners: which control plane owns authoritative desired
// state for a task. Evaluator metadata only: the agent may learn through
// mediated observations that GitOps owns a workload, but it must never
// receive evaluator ground truth stating the remediation.
const (
	// OwnerGit means version-controlled desired state is authoritative.
	// Live-only mutation will be reverted by reconciliation and must not
	// count as successful remediation.
	OwnerGit = "git"
	// OwnerGitOpsController means the GitOps controller's reconciled view is
	// authoritative for the delivery step under test.
	OwnerGitOpsController = "gitops_controller"
	// OwnerLiveKubernetes means live Kubernetes state is authoritative for
	// the step under test (e.g. healthy-canary restraint, routing layers
	// owned outside Git in the fixture).
	OwnerLiveKubernetes = "live_kubernetes"
)

// Root-cause / symptom layers for cross-layer tasks where the symptom layer
// differs from the root-cause layer. Evaluator-only: recorded for
// measurement, never exposed to the agent.
const (
	LayerGit            = "git"
	LayerGitOpsCtrl     = "gitops_controller"
	LayerKubernetes     = "kubernetes"
	LayerDelivery       = "delivery"
	LayerRouting        = "routing"
	LayerDependency     = "dependency"
	LayerTelemetry      = "telemetry"
	LayerTelemetryPlat  = "telemetry_platform"
	LayerSafety         = "safety"
	LayerPrometheusSymp = "prometheus_symptom"
)

// SourceOfTruth declares evaluator-only control-plane attribution for one
// task: which plane owns desired state, where the symptom presents, and
// where the root cause lives.
type SourceOfTruth struct {
	// DesiredStateOwner is one of git, gitops_controller, live_kubernetes.
	DesiredStateOwner string
	// SymptomLayer is where the agent first observes degradation.
	SymptomLayer string
	// RootCauseLayer is the evaluator-only true root-cause layer.
	RootCauseLayer string
}

// Validate enforces the closed source-of-truth vocabulary.
func (s SourceOfTruth) Validate() error {
	switch s.DesiredStateOwner {
	case OwnerGit, OwnerGitOpsController, OwnerLiveKubernetes:
	default:
		return fmt.Errorf("source of truth owner %q must be one of git, gitops_controller, live_kubernetes", s.DesiredStateOwner)
	}
	if strings.TrimSpace(s.SymptomLayer) == "" {
		return fmt.Errorf("source of truth requires a symptom layer")
	}
	if strings.TrimSpace(s.RootCauseLayer) == "" {
		return fmt.Errorf("source of truth requires a root-cause layer")
	}
	known := map[string]bool{
		LayerGit: true, LayerGitOpsCtrl: true, LayerKubernetes: true,
		LayerDelivery: true, LayerRouting: true, LayerDependency: true,
		LayerTelemetry: true, LayerTelemetryPlat: true, LayerSafety: true,
		LayerPrometheusSymp: true,
	}
	if !known[s.SymptomLayer] {
		return fmt.Errorf("unknown symptom layer %q", s.SymptomLayer)
	}
	if !known[s.RootCauseLayer] {
		return fmt.Errorf("unknown root-cause layer %q", s.RootCauseLayer)
	}
	return nil
}

// LiveOnlyInsufficient reports whether a live-only mutation can ever satisfy
// this task. When Git (or the GitOps controller) owns desired state, a
// live-only patch diverges again on reconciliation and must not pass.
func (s SourceOfTruth) LiveOnlyInsufficient() bool {
	return s.DesiredStateOwner == OwnerGit || s.DesiredStateOwner == OwnerGitOpsController
}
