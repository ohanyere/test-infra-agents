package operational

import (
	"encoding/json"
	"github.com/evalplane/evalplane/backend/internal/agentgateway"
	"github.com/evalplane/evalplane/backend/internal/gitops"
	"github.com/evalplane/evalplane/backend/internal/verification"
	"time"
)

func conflictingDirectState(records []DirectObservation) []EvidenceRef {
	type item struct {
		record   DirectObservation
		workload agentgateway.WorkloadObservation
	}
	seen := []item{}
	for _, record := range records {
		if record.Kind != "kubernetes_state" {
			continue
		}
		var value struct {
			Observation agentgateway.Observation `json:"observation"`
		}
		if json.Unmarshal(record.Payload, &value) != nil || len(value.Observation.Workloads) != 1 {
			continue
		}
		current := item{record: record, workload: value.Observation.Workloads[0]}
		for _, previous := range seen {
			if previous.record.SourceID == record.SourceID || previous.workload.Namespace != current.workload.Namespace || previous.workload.Name != current.workload.Name || previous.record.AcquiredAt.Sub(record.AcquiredAt) > 5*time.Second || record.AcquiredAt.Sub(previous.record.AcquiredAt) > 5*time.Second {
				continue
			}
			if previous.workload.UID != current.workload.UID || previous.workload.Generation == current.workload.Generation && previous.workload.Image != current.workload.Image {
				return []EvidenceRef{{Reference: "direct_outcome:" + previous.record.ID, Provenance: "direct-kubernetes-api:pinned-tls"}, {Reference: "direct_outcome:" + record.ID, Provenance: "direct-kubernetes-api:pinned-tls"}}
			}
		}
		seen = append(seen, current)
	}
	return nil
}

// applyDirectEvidence provides independently acquired source observations to
// the existing evaluator while the canonical result remains immutable.
func applyDirectEvidence(in *Input) (*verification.Snapshot, *gitops.Evidence) {
	var state *verification.Snapshot
	var desired *gitops.Evidence
	for _, record := range in.DirectObservations {
		switch record.Kind {
		case "kubernetes_state":
			var value struct {
				Observation agentgateway.Observation `json:"observation"`
			}
			if json.Unmarshal(record.Payload, &value) != nil || len(value.Observation.Workloads) == 0 {
				continue
			}
			snapshot := verification.Snapshot{Reference: "direct_outcome:" + record.ID, CapturedAt: record.AcquiredAt, SourceProvider: "direct-kubernetes-api:pinned-tls", Observation: value.Observation}
			in.Observations = append(in.Observations, snapshot)
			if state == nil || snapshot.CapturedAt.After(state.CapturedAt) {
				state = &snapshot
			}
		case "argocd":
			var value struct {
				GitOps gitops.Evidence `json:"gitops"`
			}
			if json.Unmarshal(record.Payload, &value) != nil || value.GitOps.Argo == nil {
				continue
			}
			if desired == nil || value.GitOps.Argo.CapturedAt.After(desired.Argo.CapturedAt) {
				copy := value.GitOps
				desired = &copy
			}
		case "prometheus":
			var value struct {
				MetricSample MetricSample `json:"metric_sample"`
			}
			if json.Unmarshal(record.Payload, &value) != nil || value.MetricSample.ID == "" {
				continue
			}
			in.MetricSamples = append(in.MetricSamples, value.MetricSample)
		}
	}
	if state != nil {
		in.Canonical.Final = *state
	}
	if desired != nil {
		in.Canonical.Final.GitOps = desired
	}
	return state, desired
}
func directFresh(snapshot *verification.Snapshot, at time.Time, maxAge time.Duration) bool {
	return snapshot != nil && snapshot.SourceProvider == "direct-kubernetes-api:pinned-tls" && at.Sub(snapshot.CapturedAt) >= 0 && at.Sub(snapshot.CapturedAt) <= maxAge
}
