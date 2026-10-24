package operational

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/evalplane/evalplane/backend/internal/verification"
)

func TestCorrectedLessonPreservesObservedCondition(t *testing.T) {
	original := []byte(`{"resource_kind":"Deployment","namespace":"checkout","contract_version":3}`)
	corrected, err := correctedLessonCondition(original, "  only during checkout rollout  ")
	if err != nil {
		t.Fatal(err)
	}
	var facts map[string]any
	if err := json.Unmarshal(corrected, &facts); err != nil {
		t.Fatal(err)
	}
	if facts["resource_kind"] != "Deployment" || facts["namespace"] != "checkout" || facts["contract_version"] != float64(3) || facts["customer_applicability_note"] != "only during checkout rollout" {
		t.Fatalf("correction changed source condition: %s", corrected)
	}
	if string(original) != `{"resource_kind":"Deployment","namespace":"checkout","contract_version":3}` {
		t.Fatal("source lesson was mutated")
	}
}

// syntheticAuthenticatedShape exercises the policy boundary. It is not a
// genuine customer-source or hosted infrastructure proof.
func syntheticAuthenticatedShape() Report {
	now := time.Now().UTC()
	return Report{RunID: "11111111-1111-4111-8111-111111111111", Revision: 1, CanonicalOverallStatus: verification.StatusVerified,
		Initial: verification.Snapshot{Reference: "initial", SourceProvider: "kubernetes-api"}, Final: verification.Snapshot{Reference: "final", SourceProvider: "kubernetes-api"},
		ObservedActions:      []Action{{ID: "audit-1", Source: "kubernetes-api", OriginAuthenticated: true, Kind: "Deployment", Namespace: "checkout", Name: "checkout", Operation: "patch", OccurredAt: now}},
		ServiceMetricSamples: []MetricSample{{ID: "metric-1", ProvenanceStatus: "SOURCE_AUTHENTICATED"}},
		Findings:             []Finding{{Dimension: "infrastructure_state", Status: Pass, ProvenanceStatus: "SOURCE_AUTHENTICATED", Evidence: []EvidenceRef{{Reference: "final", Provenance: "kubernetes-api"}}}, {Dimension: "service_outcome", Status: Pass, ProvenanceStatus: "SOURCE_AUTHENTICATED", Evidence: []EvidenceRef{{Reference: "metric-1", Provenance: "SOURCE_AUTHENTICATED"}}}, {Dimension: "temporal_stability", Status: Pass, ProvenanceStatus: "SOURCE_AUTHENTICATED", Evidence: []EvidenceRef{{Reference: "window", Provenance: "kubernetes-api"}}}}, EvaluatedAt: now}
}
func TestEpisodeEligibilityFailsClosed(t *testing.T) {
	r := syntheticAuthenticatedShape()
	if got := episodeEligibility(r); got != "verified" {
		t.Fatalf("synthetic authenticated shape: %s", got)
	}
	r.Attribution.Status = "NOT_ATTRIBUTED"
	if got := episodeEligibility(r); got != "verified" {
		t.Fatalf("factual environment outcome should not require agent attribution: %s", got)
	}
	r.ObservedActions[0].Source = "collector_forwarded_audit_unverified"
	r.ObservedActions[0].OriginAuthenticated = false
	if got := episodeEligibility(r); got != "provisional" {
		t.Fatalf("forwarded audit promoted: %s", got)
	}
	r = syntheticAuthenticatedShape()
	r.ServiceMetricSamples[0].ProvenanceStatus = "COLLECTOR_AUTHENTICATED_METRICS_UNVERIFIED"
	if got := episodeEligibility(r); got != "provisional" {
		t.Fatalf("forwarded metric promoted: %s", got)
	}
	r = syntheticAuthenticatedShape()
	r.ObservedActions = nil
	if got := episodeEligibility(r); got != "inconclusive" {
		t.Fatalf("missing observed action: %s", got)
	}
	r = syntheticAuthenticatedShape()
	r.Findings[0].Status = Conflicting
	if got := episodeEligibility(r); got != "conflicting" {
		t.Fatalf("conflicting finding: %s", got)
	}
	r = syntheticAuthenticatedShape()
	r.Reconciliations = []ReconciliationState{{State: "rejected"}}
	if got := episodeEligibility(r); got != "conflicting" {
		t.Fatalf("rejected reconciliation: %s", got)
	}
}
func TestEpisodeRetainsSourceRevisionAndDigest(t *testing.T) {
	r := syntheticAuthenticatedShape()
	x, err := episodeFromReport("w", "e", "s", r)
	if err != nil {
		t.Fatal(err)
	}
	if x.ReportRevision != 1 || x.Report.RunID != r.RunID || len(x.ContentDigest) != 71 || x.ResourceKind != "Deployment" {
		t.Fatalf("episode projection incomplete: %+v", x)
	}
	r.Findings[0].Code = "changed"
	other, err := episodeFromReport("w", "e", "s", r)
	if err != nil {
		t.Fatal(err)
	}
	if other.ContentDigest == x.ContentDigest {
		t.Fatal("contradictory report content kept same digest")
	}
}
