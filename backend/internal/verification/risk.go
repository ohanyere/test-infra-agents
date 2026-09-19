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
