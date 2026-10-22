package operational

import (
	"testing"
	"time"

	"github.com/evalplane/evalplane/backend/internal/agentgateway"
	"github.com/evalplane/evalplane/backend/internal/gitops"
	"github.com/evalplane/evalplane/backend/internal/verification"
)

func finding(r Report, dimension string) Finding {
	for _, f := range r.Findings {
		if f.Dimension == dimension {
			return f
		}
	}
	return Finding{}
}
func TestInfrastructureCanConvergeWhileServiceFails(t *testing.T) {
	now := time.Now().UTC()
	min := float64(3)
	max := float64(1)
	final := verification.Snapshot{Reference: "batch:final", CapturedAt: now.Add(-time.Minute), SourceProvider: "customer-collector", Observation: agentgateway.Observation{Timestamp: now.Add(-time.Minute), Workloads: []agentgateway.WorkloadObservation{{Kind: "Deployment", Namespace: "checkout", Name: "checkout", Generation: 2, ObservedGeneration: 2, UpdatedReplicas: 3, AvailableReplicas: 3, DesiredReplicas: 3}}}}
	c := Contract{ID: "c", Version: 1, Active: true, EffectiveAt: now.Add(-time.Hour), Service: "checkout/checkout", Rules: []Rule{{Type: "min_available_replicas", Namespace: "checkout", ResourceName: "checkout", Minimum: &min}, {Type: "max_http_5xx_pct", Maximum: &max}}}
	errorRate := float64(5)
	sample := MetricSample{ID: "metric-1", Namespace: "checkout", Service: "checkout", SampledAt: now.Add(-30 * time.Second), AcquiredAt: now.Add(-20 * time.Second), ErrorRatePct: &errorRate, ProvenanceStatus: "SOURCE_AUTHENTICATED"}
	r := Evaluate(Input{Canonical: verification.Result{ID: "v", WorkspaceID: "w", RunID: "run", OverallStatus: verification.StatusInconclusive, Final: final}, Contracts: []Contract{c}, MetricSamples: []MetricSample{sample}, EvaluatedAt: now})
	if f := finding(r, "infrastructure_state"); f.Status != Inconclusive || f.Code != "STATE_OBSERVED_PROVISIONAL" {
		t.Fatalf("infrastructure: %+v", f)
	}
	if f := finding(r, "service_outcome"); f.Status != Fail || f.Code != "SERVICE_THRESHOLD_BREACHED" {
		t.Fatalf("service: %+v", f)
	}
	if r.CanonicalOverallStatus != verification.StatusInconclusive {
		t.Fatalf("canonical verdict changed: %s", r.CanonicalOverallStatus)
	}
	sample.ProvenanceStatus = "COLLECTOR_AUTHENTICATED_METRICS_UNVERIFIED"
	r = Evaluate(Input{Canonical: verification.Result{Final: final}, Contracts: []Contract{c}, MetricSamples: []MetricSample{sample}, EvaluatedAt: now})
	if f := finding(r, "service_outcome"); f.Status != Inconclusive || f.Code != "UNVERIFIED_SERVICE_THRESHOLD_BREACH_CANDIDATE" {
		t.Fatalf("forwarded metric was promoted: %+v", f)
	}
}
func TestGitOpsSyncDoesNotProveService(t *testing.T) {
	now := time.Now().UTC()
	final := verification.Snapshot{Reference: "final", CapturedAt: now.Add(-time.Minute), GitOps: &gitops.Evidence{Desired: &gitops.DesiredState{Reference: "git", CapturedAt: now.Add(-2 * time.Minute)}, Argo: &gitops.ArgoState{Reference: "argo", CapturedAt: now.Add(-time.Minute), SyncStatus: "Synced", HealthStatus: "Healthy"}}}
	r := Evaluate(Input{Canonical: verification.Result{Final: final}, EvaluatedAt: now})
	if f := finding(r, "gitops"); f.Status != Inconclusive || f.Code != "SYNC_OBSERVED_PROVISIONAL" {
		t.Fatalf("gitops: %+v", f)
	}
	if f := finding(r, "service_outcome"); f.Status != Inconclusive {
		t.Fatalf("service: %+v", f)
	}
	final.GitOps.Argo.SyncStatus = "OutOfSync"
	r = Evaluate(Input{Canonical: verification.Result{Final: final}, EvaluatedAt: now})
	if f := finding(r, "gitops"); f.Status != Inconclusive || f.Code != "DRIFT_OBSERVED" {
		t.Fatalf("transient drift: %+v", f)
	}
}
func TestExecutedActionOnlyMatchesAnAuditCandidate(t *testing.T) {
	in := Input{Canonical: verification.Result{ActionInspection: verification.ActionInspection{Actions: []verification.InspectedAction{{ID: "claim", Status: "executed", TargetType: "Deployment", TargetRef: "checkout/checkout"}}}}, Actions: []Action{{ID: "audit-other", Kind: "Deployment", Namespace: "other", Name: "checkout", Operation: "patch"}}}
	if f := finding(Evaluate(in), "observed_action"); f.Status != Inconclusive || f.Code != "CLAIM_WITHOUT_OBSERVED_MUTATION" {
		t.Fatalf("wrong scope matched: %+v", f)
	}
	in.Actions = append(in.Actions, Action{ID: "audit-match", Kind: "Deployment", Namespace: "checkout", Name: "checkout", Operation: "patch"})
	if f := finding(Evaluate(in), "observed_action"); f.Status != Inconclusive || f.Code != "MUTATION_CANDIDATE_MATCHED_UNATTRIBUTED" {
		t.Fatalf("candidate was promoted or missed: %+v", f)
	}
	in.Canonical.ActionInspection.Actions = append(in.Canonical.ActionInspection.Actions, verification.InspectedAction{ID: "second-claim", Status: "executed", TargetType: "Deployment", TargetRef: "checkout/other"})
	if f := finding(Evaluate(in), "observed_action"); f.Code != "CLAIM_WITHOUT_OBSERVED_MUTATION" {
		t.Fatalf("second unmatched action was lost: %+v", f)
	}
}
func TestGitOpsRevisionAndLiveImageConflict(t *testing.T) {
	now := time.Now().UTC()
	final := verification.Snapshot{Reference: "final", CapturedAt: now, Observation: agentgateway.Observation{Timestamp: now, Workloads: []agentgateway.WorkloadObservation{{Kind: "Deployment", Namespace: "checkout", Name: "checkout", Image: "checkout:v1"}}}, GitOps: &gitops.Evidence{Desired: &gitops.DesiredState{Reference: "git", Revision: "rev2", Resource: "Deployment/checkout/checkout", Image: "checkout:v2", CapturedAt: now.Add(-time.Minute)}, Argo: &gitops.ArgoState{Reference: "argo", ObservedRevision: "rev1", CapturedAt: now, SyncStatus: "Synced", HealthStatus: "Healthy"}}}
	in := Input{Canonical: verification.Result{Final: final}, EvaluatedAt: now}
	if f := finding(Evaluate(in), "gitops"); f.Code != "REVISION_MISMATCH" || f.Status != Inconclusive {
		t.Fatalf("revision: %+v", f)
	}
	final.GitOps.Argo.ObservedRevision = "rev2"
	in.Canonical.Final = final
	if f := finding(Evaluate(in), "gitops"); f.Code != "DESIRED_LIVE_DRIFT" || f.Status != Fail {
		t.Fatalf("live drift: %+v", f)
	}
}
func TestTemporalGapAndProtectedOperation(t *testing.T) {
	now := time.Now().UTC()
	c := Contract{ID: "protected", Version: 2, Active: true, EffectiveAt: now.Add(-time.Hour), Rules: []Rule{{Type: "forbid_operation", Namespace: "production", ResourceKind: "Deployment", Operation: "delete"}}}
	a := Action{ID: "audit:1", Namespace: "production", Kind: "Deployment", Operation: "delete", Source: "collector-forwarded"}
	samples := []verification.Snapshot{{Reference: "one", CapturedAt: now.Add(-6 * time.Minute)}, {Reference: "two", CapturedAt: now.Add(-time.Minute)}}
	in := Input{Canonical: verification.Result{}, Contracts: []Contract{c}, Actions: []Action{a}, Observations: samples, EvaluatedAt: now, Window: 5 * time.Minute}
	r := Evaluate(in)
	if f := finding(r, "temporal_stability"); f.Code != "OBSERVATION_GAP" {
		t.Fatalf("gap: %+v", f)
	}
	if f := r.Findings[len(r.Findings)-1]; f.Status != Inconclusive || f.Code != "UNVERIFIED_FORBIDDEN_OPERATION_CANDIDATE" {
		t.Fatalf("unverified audit promoted: %+v", f)
	}
	in.Actions[0].OriginAuthenticated = true
	r = Evaluate(in)
	if f := r.Findings[len(r.Findings)-1]; f.Status != Fail || f.ContractVersion != 2 || len(f.Evidence) != 1 {
		t.Fatalf("protected resource violation: %+v", f)
	}
}
func TestUnavailableMetricsNeverPass(t *testing.T) {
	now := time.Now().UTC()
	max := float64(1)
	c := Contract{ID: "service", Version: 1, Active: true, EffectiveAt: now.Add(-time.Hour), Rules: []Rule{{Type: "max_http_5xx_pct", Maximum: &max}}}
	r := Evaluate(Input{Canonical: verification.Result{Final: verification.Snapshot{Reference: "final", CapturedAt: now.Add(-time.Second), Observation: agentgateway.Observation{Timestamp: now.Add(-time.Second)}}}, Contracts: []Contract{c}, EvaluatedAt: now})
	if f := finding(r, "service_outcome"); f.Status != Inconclusive {
		t.Fatalf("missing metrics: %+v", f)
	}
}
func TestTemporalRecoveryAndRegressionRemainSeparateFromService(t *testing.T) {
	now := time.Now().UTC()
	floor := float64(3)
	c := Contract{ID: "replicas", Version: 1, Active: true, EffectiveAt: now.Add(-time.Hour), Rules: []Rule{{Type: "min_available_replicas", Namespace: "checkout", ResourceName: "checkout", Minimum: &floor}}}
	sample := func(at time.Time, n int32) verification.Snapshot {
		return verification.Snapshot{Reference: at.String(), CapturedAt: at, Observation: agentgateway.Observation{Workloads: []agentgateway.WorkloadObservation{{Kind: "Deployment", Namespace: "checkout", Name: "checkout", AvailableReplicas: n}}}}
	}
	samples := []verification.Snapshot{sample(now.Add(-6*time.Minute), 1), sample(now.Add(-4*time.Minute), 2), sample(now.Add(-2*time.Minute), 3), sample(now, 3)}
	in := Input{Canonical: verification.Result{}, Contracts: []Contract{c}, Observations: samples, EvaluatedAt: now, Window: 5 * time.Minute}
	if f := finding(Evaluate(in), "temporal_stability"); f.Code != "RECOVERY_OBSERVED_PROVISIONAL" || f.Status != Inconclusive {
		t.Fatalf("recovery: %+v", f)
	}
	in.Observations = []verification.Snapshot{sample(now.Add(-6*time.Minute), 3), sample(now.Add(-4*time.Minute), 3), sample(now.Add(-2*time.Minute), 2), sample(now, 1)}
	if f := finding(Evaluate(in), "temporal_stability"); f.Code != "REGRESSION_OBSERVED" || f.Status != Fail {
		t.Fatalf("regression: %+v", f)
	}
}
func TestContractRuleValidation(t *testing.T) {
	floor := float64(3)
	ceiling := float64(1)
	if !validRule(Rule{Type: "min_available_replicas", Namespace: "checkout", ResourceName: "checkout", Minimum: &floor}) {
		t.Fatal("valid replica floor rejected")
	}
	if !validRule(Rule{Type: "max_http_5xx_pct", Maximum: &ceiling}) {
		t.Fatal("valid error ceiling rejected")
	}
	if validRule(Rule{Type: "max_http_5xx_pct"}) || validRule(Rule{Type: "unknown"}) || validRule(Rule{Type: "require_approval", Namespace: "production"}) {
		t.Fatal("incomplete contract rule accepted")
	}
}
func TestContractVersionActivationAndOrder(t *testing.T) {
	now := time.Now().UTC()
	history := []Contract{{ID: "b", Version: 1, Active: true, EffectiveAt: now.Add(-time.Hour)}, {ID: "a", Version: 1, Active: true, EffectiveAt: now.Add(-time.Hour)}, {ID: "a", Version: 2, Active: false, EffectiveAt: now.Add(-time.Minute)}, {ID: "b", Version: 2, Active: false, EffectiveAt: now.Add(time.Hour)}}
	selected := activeContracts(history, now)
	if len(selected) != 1 || selected[0].ID != "b" || selected[0].Version != 1 {
		t.Fatalf("effective contract versions: %+v", selected)
	}
}
