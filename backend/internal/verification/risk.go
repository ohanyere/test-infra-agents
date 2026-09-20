package verification

import "time"

type Analyzer interface {
	Analyze(Snapshot, Snapshot, time.Time) []RiskFinding
}

// HPAConsistencyAnalyzer reports only controller behavior supported by an
// observed autoscaler contract. Absence of HPA evidence produces no finding.
type HPAConsistencyAnalyzer struct{}

func (HPAConsistencyAnalyzer) Analyze(_ Snapshot, final Snapshot, now time.Time) []RiskFinding {
	out := []RiskFinding{}
	for _, h := range final.Observation.Autoscalers {
		for _, w := range final.Observation.Workloads {
			if w.Namespace != h.Namespace || w.Name != h.TargetName {
				continue
			}
			if w.DesiredReplicas > h.MaxReplicas {
				resource := "Deployment/" + w.Namespace + "/" + w.Name
				out = append(out, RiskFinding{AnalyzerID: "kubernetes-hpa-consistency", AnalyzerVersion: "v1", Type: "controller_reconciliation", Severity: "HIGH", AffectedResources: []string{resource, "HorizontalPodAutoscaler/" + h.Namespace + "/" + h.Name}, Evidence: []string{final.Reference}, Reason: "The deployment replica target exceeds the observed HPA maximum.", Trigger: "The HPA reconciles the scale subresource.", EvidenceStrength: "HIGH", CreatedAt: now})
			}
		}
	}
	return out
}

// GitOpsReconciliationAnalyzer reports only when the pinned Git desired state,
// live Kubernetes state, and Argo drift state are all present and disagree.
type GitOpsReconciliationAnalyzer struct{}

func (GitOpsReconciliationAnalyzer) Analyze(_ Snapshot, final Snapshot, now time.Time) []RiskFinding {
	if final.GitOps == nil || final.GitOps.Desired == nil || final.GitOps.Argo == nil || len(final.GitOps.Errors) > 0 || final.GitOps.Argo.SyncStatus != "OutOfSync" {
		return []RiskFinding{}
	}
	desired, argo := final.GitOps.Desired, final.GitOps.Argo
	for _, workload := range final.Observation.Workloads {
		resource := workload.Kind + "/" + workload.Namespace + "/" + workload.Name
		if resource != desired.Resource || workload.Image == desired.Image {
			continue
		}
		trigger := "A manual or future Argo CD synchronization can restore the pinned Git desired image."
		if argo.Automated && argo.SelfHeal {
			trigger = "Argo CD automated self-healing can restore the pinned Git desired image."
		}
		return []RiskFinding{{AnalyzerID: "gitops-reconciliation-reversion", AnalyzerVersion: "v1", Type: "controller_reconciliation", Severity: "HIGH", AffectedResources: []string{resource, "Application/" + argo.Namespace + "/" + argo.Application}, Evidence: []string{desired.Reference, argo.Reference, final.Reference}, Reason: "Live " + resource + " is running " + workload.Image + " while Git desired state is " + desired.Image + " and Argo CD reports OutOfSync.", Trigger: trigger, EvidenceStrength: "HIGH", CreatedAt: now}}
	}
	return []RiskFinding{}
}
