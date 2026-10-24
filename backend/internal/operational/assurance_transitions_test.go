package operational

import (
	"testing"
	"time"

	"github.com/evalplane/evalplane/backend/internal/agentgateway"
	"github.com/evalplane/evalplane/backend/internal/verification"
)

func TestSourceOutageIsMonitoringUncertainty(t *testing.T) {
	now := time.Now().UTC()
	events := []MonitoringEvent{{ID: "old", SourceID: "source-a", Status: "acquired", RecordedAt: now.Add(-time.Minute)}, {ID: "new", SourceID: "source-a", Status: "unavailable", Reason: "source offline", RecordedAt: now}}
	finding := assessSourceHealth(events)
	if len(finding) != 1 || finding[0].Dimension != "source_health" || finding[0].Status != Inconclusive || finding[0].Code != "SOURCE_ACQUISITION_UNAVAILABLE" {
		t.Fatalf("outage represented as application health: %+v", finding)
	}
	if notificationKind(finding[0], findingDisposition(finding[0])) != "evidence_unavailable" {
		t.Fatal("source outage notification missing")
	}
	recovered := Report{MonitoringEvents: append(events, MonitoringEvent{ID: "later", SourceID: "source-a", Status: "acquired", RecordedAt: now.Add(time.Minute)})}
	transitions := assuranceTransitions(Report{MonitoringEvents: events}, recovered)
	if len(transitions) != 1 || transitions[0].Code != "SOURCE_CONTACT_RESTORED" || transitions[0].Dimension != "source_health" {
		t.Fatalf("source recovery confused with service recovery: %+v", transitions)
	}
}

func TestMonitoringOnlyGapDoesNotReplaceFactualEpisode(t *testing.T) {
	prior := Report{CanonicalResultID: "canonical-1", DirectObservations: []DirectObservation{{ID: "observation-1"}}}
	gap := prior
	gap.MonitoringEvents = []MonitoringEvent{{ID: "event-1", SourceID: "source-1", Status: "unavailable"}}
	if !monitoringOnlyGap(prior, gap) {
		t.Fatal("monitoring outage alone replaced factual episode")
	}
	gap.DirectObservations = []DirectObservation{{ID: "observation-2"}}
	if monitoringOnlyGap(prior, gap) {
		t.Fatal("new source observation hidden as monitoring-only gap")
	}
}

func TestAssuranceRegressionAndRecoveryRequireEvidence(t *testing.T) {
	ev := []EvidenceRef{{Reference: "direct_outcome:one", Provenance: "direct-prometheus:pinned-tls"}}
	pass := Finding{Dimension: "service_outcome", Status: Pass, ProvenanceStatus: "SOURCE_AUTHENTICATED", Evidence: ev}
	fail := Finding{Dimension: "service_outcome", Status: Fail, ProvenanceStatus: "SOURCE_AUTHENTICATED", Evidence: ev}
	regression := assuranceTransitions(Report{Findings: []Finding{pass}}, Report{Findings: []Finding{fail}})
	if len(regression) != 1 || regression[0].Code != "ASSURANCE_service_outcome_REGRESSION" || notificationKind(regression[0], findingDisposition(regression[0])) != "assurance_regression" {
		t.Fatalf("regression: %+v", regression)
	}
	if recovery := assuranceTransitions(Report{Findings: []Finding{fail}}, Report{Findings: []Finding{pass}}); len(recovery) != 0 {
		t.Fatalf("single healthy sample proved recovery: %+v", recovery)
	}
	stable := Finding{Dimension: "service_stability", Status: Pass, ProvenanceStatus: "SOURCE_AUTHENTICATED", Evidence: ev}
	recovery := assuranceTransitions(Report{Findings: []Finding{fail}}, Report{Findings: []Finding{pass, stable}})
	if len(recovery) != 1 || recovery[0].Code != "ASSURANCE_service_outcome_RECOVERED" {
		t.Fatalf("sustained recovery: %+v", recovery)
	}
	provisional := fail
	provisional.ProvenanceStatus = "COLLECTOR_AUTHENTICATED_METRICS_UNVERIFIED"
	if x := assuranceTransitions(Report{Findings: []Finding{pass}}, Report{Findings: []Finding{provisional}}); len(x) != 0 {
		t.Fatalf("provisional evidence promoted: %+v", x)
	}
}

func TestDirectStabilityRequiresCoveredWindow(t *testing.T) {
	now := time.Now().UTC()
	floor := float64(3)
	in := Input{EvaluatedAt: now, Window: 2 * time.Minute, Freshness: 2 * time.Minute, Contracts: []Contract{{Active: true, Rules: []Rule{{Type: "min_available_replicas", Namespace: "checkout", ResourceName: "checkout", Minimum: &floor}}}}}
	snapshot := func(id string, at time.Time, available int32) verification.Snapshot {
		return verification.Snapshot{Reference: "direct_outcome:" + id, SourceProvider: "direct-kubernetes-api:pinned-tls", CapturedAt: at, Observation: agentgateway.Observation{Workloads: []agentgateway.WorkloadObservation{{Kind: "Deployment", Namespace: "checkout", Name: "checkout", AvailableReplicas: available}}}}
	}
	series := []verification.Snapshot{snapshot("a", now.Add(-2*time.Minute), 3), snapshot("b", now.Add(-time.Minute), 3), snapshot("c", now, 3)}
	if f := assessDirectStability(in, series); f.Status != Pass {
		t.Fatalf("covered direct window: %+v", f)
	}
	series[1] = snapshot("b", now.Add(-time.Minute), 2)
	if f := assessDirectStability(in, series); f.Status != Fail {
		t.Fatalf("replica regression: %+v", f)
	}
	series = series[:1]
	if f := assessDirectStability(in, series); f.Status != Inconclusive {
		t.Fatalf("single snapshot proved stability: %+v", f)
	}
}

func TestInfrastructureRecoveryAndGitOpsReversionStaySeparate(t *testing.T) {
	evidence := []EvidenceRef{{Reference: "direct_outcome:state", Provenance: "direct-kubernetes-api:pinned-tls"}}
	failed := Finding{Dimension: "infrastructure_state", Status: Fail, ProvenanceStatus: "SOURCE_AUTHENTICATED", Evidence: evidence}
	healthy := Finding{Dimension: "infrastructure_state", Status: Pass, ProvenanceStatus: "SOURCE_AUTHENTICATED", Evidence: evidence}
	if findings := assuranceTransitions(Report{Findings: []Finding{failed}}, Report{Findings: []Finding{healthy}}); len(findings) != 0 {
		t.Fatalf("single direct snapshot established sustained recovery: %+v", findings)
	}
	stable := Finding{Dimension: "direct_stability", Status: Pass, ProvenanceStatus: "SOURCE_AUTHENTICATED", Evidence: evidence}
	findings := assuranceTransitions(Report{Findings: []Finding{failed}}, Report{Findings: []Finding{healthy, stable}})
	if len(findings) != 1 || findings[0].Code != "ASSURANCE_infrastructure_state_RECOVERED" {
		t.Fatalf("covered infrastructure recovery: %+v", findings)
	}
	gitPass := Finding{Dimension: "gitops", Status: Pass, ProvenanceStatus: "SOURCE_AUTHENTICATED", Evidence: evidence}
	gitFail := Finding{Dimension: "gitops", Status: Fail, ProvenanceStatus: "SOURCE_AUTHENTICATED", Evidence: evidence}
	findings = assuranceTransitions(Report{Findings: []Finding{gitPass}}, Report{Findings: []Finding{gitFail}})
	if len(findings) != 1 || findings[0].Code != "ASSURANCE_gitops_REGRESSION" {
		t.Fatalf("GitOps reversion: %+v", findings)
	}
}
