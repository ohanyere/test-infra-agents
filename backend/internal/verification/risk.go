package verification

import (
	"fmt"
	"time"
)

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

const (
	acceptableReplicaHeadroom = int32(2)
	blockingReplicaHeadroom   = int32(0)
)

// HPACapacityHeadroomAnalyzer evaluates declared deployment capacity against
// the observed HPA ceiling. One spare replica is a meaningful but non-blocking
// risk; no spare capacity is blocking. Absence of matching HPA evidence does
// not create a finding.
type HPACapacityHeadroomAnalyzer struct{}

func (HPACapacityHeadroomAnalyzer) Analyze(_ Snapshot, final Snapshot, now time.Time) []RiskFinding {
	out := []RiskFinding{}
	for _, h := range final.Observation.Autoscalers {
		for _, w := range final.Observation.Workloads {
			if w.Namespace != h.Namespace || w.Name != h.TargetName || h.TargetKind != "Deployment" {
				continue
			}
			headroom := h.MaxReplicas - w.DesiredReplicas
			if headroom >= acceptableReplicaHeadroom {
				continue
			}
			blocking := headroom <= blockingReplicaHeadroom
			severity := "MEDIUM"
			if blocking {
				severity = "HIGH"
			}
			resource := "Deployment/" + w.Namespace + "/" + w.Name
			out = append(out, RiskFinding{
				AnalyzerID:        "kubernetes-capacity-headroom",
				AnalyzerVersion:   "v1",
				Type:              "capacity_headroom",
				Severity:          severity,
				Blocking:          blocking,
				AffectedResources: []string{resource, "HorizontalPodAutoscaler/" + h.Namespace + "/" + h.Name},
				Evidence:          []string{final.Reference},
				Measurements: map[string]float64{
					"observed_headroom_replicas":           float64(headroom),
					"acceptable_minimum_headroom_replicas": float64(acceptableReplicaHeadroom),
					"blocking_headroom_replicas":           float64(blockingReplicaHeadroom),
				},
				Reason:           fmt.Sprintf("Observed replica headroom is %d; policy requires at least %d spare replicas for acceptable capacity headroom, while %d or fewer is blocking.", headroom, acceptableReplicaHeadroom, blockingReplicaHeadroom),
				Trigger:          "Demand requires the deployment to use its remaining HPA capacity.",
				EvidenceStrength: "HIGH",
				CreatedAt:        now,
			})
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
