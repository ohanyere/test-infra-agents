package operational

import (
	"encoding/json"
	"fmt"
	"github.com/evalplane/evalplane/backend/internal/customerattribution"
	"sort"
	"strings"
	"time"

	"github.com/evalplane/evalplane/backend/internal/agentgateway"
	"github.com/evalplane/evalplane/backend/internal/gitops"
	"github.com/evalplane/evalplane/backend/internal/verification"
)

const ContractVersion = "operational-assessment:v1"
const (
	Pass          = "PASS"
	Fail          = "FAIL"
	Inconclusive  = "INCONCLUSIVE"
	Conflicting   = "CONFLICTING"
	NotApplicable = "NOT_APPLICABLE"
)

type Rule struct {
	Type             string   `json:"type"`
	Namespace        string   `json:"namespace,omitempty"`
	ResourceKind     string   `json:"resource_kind,omitempty"`
	ResourceName     string   `json:"resource_name,omitempty"`
	ResourceUID      string   `json:"resource_uid,omitempty"`
	Operation        string   `json:"operation,omitempty"`
	Minimum          *float64 `json:"minimum,omitempty"`
	Maximum          *float64 `json:"maximum,omitempty"`
	ApprovalRequired bool     `json:"approval_required,omitempty"`
	Value            string   `json:"value,omitempty"`
	WindowSeconds    int      `json:"window_seconds,omitempty"`
	GraceSeconds     int      `json:"grace_seconds,omitempty"`
	FreshnessSeconds int      `json:"freshness_seconds,omitempty"`
}
type Contract struct {
	ID            string    `json:"id"`
	WorkspaceID   string    `json:"workspace_id"`
	EnvironmentID string    `json:"environment_id"`
	Version       int       `json:"version"`
	Active        bool      `json:"active"`
	State         string    `json:"state"`
	EffectiveAt   time.Time `json:"effective_at"`
	OwnerUserID   string    `json:"owner_user_id"`
	Service       string    `json:"service,omitempty"`
	Rules         []Rule    `json:"rules"`
}
type ApprovalRecord struct {
	ID         string     `json:"id"`
	ActionID   string     `json:"action_id"`
	Resource   string     `json:"resource"`
	Decision   string     `json:"decision"`
	ApprovedBy string     `json:"approved_by"`
	RecordedAt time.Time  `json:"recorded_at"`
	ValidUntil time.Time  `json:"valid_until"`
	RevokedAt  *time.Time `json:"revoked_at,omitempty"`
}
type ReconciliationState struct {
	ID          string     `json:"id"`
	ClaimID     string     `json:"claim_id"`
	State       string     `json:"state"`
	Result      string     `json:"result"`
	Revision    int        `json:"revision"`
	Reason      string     `json:"reason"`
	EvaluatedAt *time.Time `json:"evaluated_at,omitempty"`
}
type EvidenceRef struct {
	Reference  string `json:"reference"`
	Provenance string `json:"provenance"`
}
type Finding struct {
	Dimension        string        `json:"dimension"`
	Status           string        `json:"status"`
	ProvenanceStatus string        `json:"provenance_status"`
	Code             string        `json:"code"`
	Reason           string        `json:"reason"`
	Evidence         []EvidenceRef `json:"evidence"`
	ContractID       string        `json:"contract_id,omitempty"`
	ContractVersion  int           `json:"contract_version,omitempty"`
	RuleType         string        `json:"rule_type,omitempty"`
	RuleID           string        `json:"rule_id,omitempty"`
	AffectedResource string        `json:"affected_resource,omitempty"`
}
type Action struct {
	ID                  string    `json:"id"`
	KubernetesPrincipal string    `json:"kubernetes_principal,omitempty"`
	Operation           string    `json:"operation"`
	Namespace           string    `json:"namespace"`
	Kind                string    `json:"kind"`
	Name                string    `json:"name"`
	UID                 string    `json:"uid"`
	OccurredAt          time.Time `json:"occurred_at"`
	Source              string    `json:"source"`
	OriginAuthenticated bool      `json:"origin_authenticated"`
}
type MetricSample struct {
	ID                    string    `json:"id"`
	Namespace             string    `json:"namespace"`
	Service               string    `json:"service"`
	SampledAt             time.Time `json:"sampled_at"`
	AcquiredAt            time.Time `json:"acquired_at"`
	ErrorRatePct          *float64  `json:"error_rate_pct,omitempty"`
	RequestSuccessRatePct *float64  `json:"request_success_rate_pct,omitempty"`
	P95LatencyMS          *float64  `json:"p95_latency_ms,omitempty"`
	SourceDigest          string    `json:"source_digest"`
	RequestCount          int64     `json:"request_count,omitempty"`
	WindowStartedAt       time.Time `json:"window_started_at,omitempty"`
	ProvenanceStatus      string    `json:"provenance_status"`
}
type Input struct {
	Canonical          verification.Result
	Claims             []string
	ClaimPayloads      []json.RawMessage
	Actions            []Action
	MetricSamples      []MetricSample
	DirectObservations []DirectObservation
	Approvals          []ApprovalRecord
	GatewayPreflights  []GatewayPreflight
	Reconciliations    []ReconciliationState
	Contracts          []Contract
	Observations       []verification.Snapshot
	Window             time.Duration
	Freshness          time.Duration
	EvaluatedAt        time.Time
}
type Report struct {
	ContractVersion        string                              `json:"contract_version"`
	WorkspaceID            string                              `json:"workspace_id"`
	RunID                  string                              `json:"run_id"`
	EnvironmentID          string                              `json:"environment_id"`
	Revision               int                                 `json:"revision"`
	CanonicalResultID      string                              `json:"canonical_result_id"`
	SourceFingerprint      string                              `json:"source_fingerprint,omitempty"`
	CanonicalOverallStatus string                              `json:"canonical_overall_status"`
	Intent                 verification.IntentResult           `json:"intent"`
	RequestedActions       []verification.InspectedAction      `json:"requested_actions"`
	AgentClaims            []json.RawMessage                   `json:"agent_claims"`
	ClaimReferences        []string                            `json:"claim_references"`
	ObservedActions        []Action                            `json:"observed_actions"`
	ServiceMetricSamples   []MetricSample                      `json:"service_metric_samples"`
	DirectObservations     []DirectObservation                 `json:"direct_observations,omitempty"`
	IndependentFinal       *verification.Snapshot              `json:"independent_final_state,omitempty"`
	DirectGitOps           *gitops.Evidence                    `json:"direct_gitops,omitempty"`
	ApprovalRecords        []ApprovalRecord                    `json:"approval_records"`
	GatewayPreflights      []GatewayPreflight                  `json:"gateway_preflights,omitempty"`
	MonitoringEvents       []MonitoringEvent                   `json:"monitoring_events,omitempty"`
	Reconciliations        []ReconciliationState               `json:"reconciliations"`
	Attribution            verification.AttributionAssessment  `json:"attribution"`
	CustomerAttribution    []customerattribution.Assessment    `json:"customer_attribution"`
	CanonicalOutcome       verification.OutcomeResult          `json:"canonical_outcome"`
	InfrastructureDiff     []verification.InfrastructureChange `json:"infrastructure_diff"`
	Initial                verification.Snapshot               `json:"initial_state"`
	Final                  verification.Snapshot               `json:"final_state"`
	Observations           []verification.Snapshot             `json:"temporal_observations"`
	Contracts              []Contract                          `json:"operational_contracts"`
	Findings               []Finding                           `json:"findings"`
	OperationExplanations  []OperationExplanation              `json:"operation_explanations,omitempty"`
	Limitations            []string                            `json:"limitations"`
	EvaluatedAt            time.Time                           `json:"evaluated_at"`
}

// Evaluate leaves the canonical result untouched. Findings about a customer
// collector remain provisional even when its transport was authenticated.
func Evaluate(in Input) Report {
	canonical := in.Canonical
	independentFinal, directGitOps := applyDirectEvidence(&in)
	conflictEvidence := conflictingDirectState(in.DirectObservations)
	if len(conflictEvidence) > 0 {
		in.Canonical.Final = canonical.Final
		independentFinal = nil
	}
	r := Report{ContractVersion: ContractVersion, WorkspaceID: canonical.WorkspaceID, RunID: canonical.RunID, CanonicalResultID: canonical.ID, CanonicalOverallStatus: canonical.OverallStatus, Intent: canonical.Intent, RequestedActions: append([]verification.InspectedAction{}, canonical.ActionInspection.Actions...), AgentClaims: append([]json.RawMessage{}, in.ClaimPayloads...), ClaimReferences: append([]string{}, in.Claims...), ObservedActions: append([]Action{}, in.Actions...), ServiceMetricSamples: append([]MetricSample{}, in.MetricSamples...), DirectObservations: append([]DirectObservation{}, in.DirectObservations...), IndependentFinal: independentFinal, DirectGitOps: directGitOps, ApprovalRecords: append([]ApprovalRecord{}, in.Approvals...), GatewayPreflights: append([]GatewayPreflight{}, in.GatewayPreflights...), Reconciliations: append([]ReconciliationState{}, in.Reconciliations...), Attribution: canonical.ActionInspection.Attribution, CanonicalOutcome: canonical.Outcome, InfrastructureDiff: append([]verification.InfrastructureChange{}, canonical.InfrastructureDiff...), Initial: canonical.Baseline, Final: canonical.Final, Contracts: append([]Contract{}, in.Contracts...), EvaluatedAt: in.EvaluatedAt.UTC(), Limitations: []string{"Forwarded collector audit and metric records remain provisional. Direct source trust depends on customer endpoint and credential isolation; agent causality and service recovery are separate assessments."}}
	for _, direct := range in.DirectObservations {
		if direct.Kind == "external_reachability" {
			r.Limitations = append(r.Limitations, "Reachability describes one operator-declared external vantage and a bound Ingress address at one time; failed attempts do not prove privacy or universal inaccessibility.")
			break
		}
	}
	r.Observations = append([]verification.Snapshot{}, in.Observations...)
	if !in.Canonical.Baseline.CapturedAt.IsZero() {
		r.Observations = append(r.Observations, in.Canonical.Baseline)
	}
	if !in.Canonical.Final.CapturedAt.IsZero() {
		r.Observations = append(r.Observations, in.Canonical.Final)
	}
	sort.Slice(r.Observations, func(i, j int) bool { return r.Observations[i].CapturedAt.Before(r.Observations[j].CapturedAt) })
	if in.Freshness <= 0 {
		in.Freshness = 2 * time.Minute
	}
	if in.Window <= 0 {
		in.Window = 5 * time.Minute
	}
	r.Findings = append(r.Findings, assessInfrastructure(in))
	if len(conflictEvidence) > 0 {
		r.Findings = append(r.Findings, Finding{Dimension: "source_consistency", Status: Conflicting, ProvenanceStatus: "CONFLICTING_DIRECT_SOURCES", Code: "CONTRADICTORY_DIRECT_STATE", Reason: "Two separately registered direct sources reported incompatible state for the same Deployment at nearly the same time; no independent final state is selected.", Evidence: conflictEvidence})
	}
	r.Findings = append(r.Findings, assessActionComparison(in))
	r.Findings = append(r.Findings, assessGitOps(in))
	r.Findings = append(r.Findings, assessDriftWindow(in, r.Observations))
	r.Findings = append(r.Findings, assessService(in))
	r.Findings = append(r.Findings, assessTemporal(in, r.Observations))
	r.Findings = append(r.Findings, assessDirectStability(in, r.Observations))
	r.Findings = append(r.Findings, assessMetricWindow(in))
	r.Findings = append(r.Findings, assessRecovery(in))
	r.Findings = append(r.Findings, assessGatewayControl(in)...)
	r.Findings = append(r.Findings, assessActionEffect(in))
	r.Findings = append(r.Findings, assessDirectDeletion(in)...)
	for _, state := range in.Reconciliations {
		if state.State == "unavailable" || state.State == "rejected" || state.Result == "missing" || state.Result == "conflicting" {
			r.Findings = append(r.Findings, Finding{Dimension: "evidence_reconciliation", Status: Inconclusive, ProvenanceStatus: "SERVER_OWNED_RECONCILIATION", Code: "RECONCILIATION_FAILURE", Reason: "Required audit evidence is unavailable, rejected, missing, or conflicting: " + state.Reason, Evidence: []EvidenceRef{{Reference: "reconciliation:" + state.ID, Provenance: "server_reconciliation"}}})
		}
	}
	r.Findings = append(r.Findings, assessContractConflicts(in.Contracts)...)
	for _, c := range in.Contracts {
		if c.Active && !c.EffectiveAt.After(in.EvaluatedAt) {
			for index, rule := range c.Rules {
				f := assessRule(in, c.Service, rule)
				f.ContractID = c.ID
				f.ContractVersion = c.Version
				f.RuleType = rule.Type
				f.RuleID = fmt.Sprintf("%s:v%d:rule:%d", c.ID, c.Version, index+1)
				f.AffectedResource = strings.Trim(strings.Join([]string{rule.Namespace, rule.ResourceName}, "/"), "/")
				r.Findings = append(r.Findings, f)
			}
		}
	}
	return r
}
func assessContractConflicts(contracts []Contract) []Finding {
	type selected struct {
		contract Contract
		rule     Rule
	}
	seen := map[string]selected{}
	out := []Finding{}
	for _, c := range contracts {
		if !c.Active {
			continue
		}
		for _, rule := range c.Rules {
			key := strings.Join([]string{rule.Type, c.Service, rule.Namespace, rule.ResourceKind, rule.ResourceName, rule.ResourceUID, rule.Operation}, "|")
			if prior, ok := seen[key]; ok && prior.contract.ID != c.ID {
				a, _ := json.Marshal(prior.rule)
				b, _ := json.Marshal(rule)
				if string(a) != string(b) {
					out = append(out, Finding{Dimension: "contract_governance", Status: Conflicting, ProvenanceStatus: "SERVER_OWNED_CONTRACTS", Code: "OVERLAPPING_CONTRACT_RULES", Reason: "Active contracts specify different requirements for the same scope; neither is silently preferred.", Evidence: []EvidenceRef{{Reference: fmt.Sprintf("contract:%s:v%d", prior.contract.ID, prior.contract.Version), Provenance: "customer_contract"}, {Reference: fmt.Sprintf("contract:%s:v%d", c.ID, c.Version), Provenance: "customer_contract"}}, RuleType: rule.Type, AffectedResource: strings.Trim(rule.Namespace+"/"+rule.ResourceName, "/")})
				}
			} else if !ok {
				seen[key] = selected{c, rule}
			}
		}
	}
	return out
}

func ref(s verification.Snapshot) []EvidenceRef {
	if s.Reference == "" {
		return nil
	}
	return []EvidenceRef{{Reference: s.Reference, Provenance: s.SourceProvider}}
}
func freshSnapshot(s verification.Snapshot, maxAge time.Duration) bool {
	if s.Reference == "" || s.CapturedAt.IsZero() || s.Observation.Timestamp.IsZero() {
		return false
	}
	lag := s.CapturedAt.Sub(s.Observation.Timestamp)
	return lag >= -30*time.Second && lag <= maxAge
}
func workload(s verification.Snapshot, ns, name string) *agentgateway.WorkloadObservation {
	for i := range s.Observation.Workloads {
		w := &s.Observation.Workloads[i]
		if w.Kind == "Deployment" && w.Namespace == ns && w.Name == name {
			return w
		}
	}
	return nil
}
func assessActionComparison(in Input) Finding {
	f := Finding{Dimension: "observed_action", Status: Inconclusive, ProvenanceStatus: "AUDIT_ORIGIN_UNVERIFIED", Code: "NO_ACTION_TO_COMPARE", Reason: "No executed Deployment action was recorded in the canonical run."}
	requestedCount, matchedCount := 0, 0
	for _, requested := range in.Canonical.ActionInspection.Actions {
		if requested.Status != "executed" || requested.TargetType != "Deployment" {
			continue
		}
		requestedCount++
		parts := strings.Split(requested.TargetRef, "/")
		if len(parts) != 2 {
			f.Code = "ACTION_TARGET_UNRESOLVED"
			f.Reason = "The requested Deployment target is not a namespace/name pair."
			return f
		}
		matched := false
		for _, observed := range in.Actions {
			if observed.Kind == "Deployment" && observed.Namespace == parts[0] && observed.Name == parts[1] && (observed.Operation == "patch" || observed.Operation == "update") {
				f.Evidence = append(f.Evidence, EvidenceRef{Reference: observed.ID, Provenance: observed.Source})
				matched = true
				break
			}
		}
		if matched {
			matchedCount++
		}
	}
	if requestedCount > 0 && matchedCount < requestedCount {
		f.Code = "CLAIM_WITHOUT_OBSERVED_MUTATION"
		f.Reason = "At least one executed Deployment action lacks a matching independently acquired audit candidate."
	} else if requestedCount > 0 {
		f.Code = "MUTATION_CANDIDATE_MATCHED_UNATTRIBUTED"
		f.Reason = "Audit candidates name the requested Deployments; operation and AI-agent causality remain unproven."
	}
	return f
}
func assessInfrastructure(in Input) Finding {
	f := Finding{Dimension: "infrastructure_state", Status: Inconclusive, ProvenanceStatus: "COLLECTOR_AUTHENTICATED_SOURCE_UNVERIFIED", Code: "MISSING_AUTHORIZED_DESIRED_STATE", Reason: "An agent claim alone does not authorize a desired state.", Evidence: ref(in.Canonical.Final)}
	direct := in.Canonical.Final.SourceProvider == "direct-kubernetes-api:pinned-tls"
	if !freshSnapshot(in.Canonical.Final, in.Freshness) || (direct && (in.EvaluatedAt.Sub(in.Canonical.Final.CapturedAt) > in.Freshness || in.EvaluatedAt.Before(in.Canonical.Final.CapturedAt))) {
		f.Code = "MISSING_OR_STALE_STATE"
		f.Reason = "A fresh final Kubernetes observation is required."
		return f
	}
	authorized := false
	for _, c := range in.Contracts {
		if !c.Active || c.EffectiveAt.After(in.EvaluatedAt) {
			continue
		}
		for _, rule := range c.Rules {
			if rule.Type == "min_available_replicas" && rule.Minimum != nil {
				authorized = true
				w := workload(in.Canonical.Final, rule.Namespace, rule.ResourceName)
				if w == nil {
					f.Code = "RESOURCE_NOT_OBSERVED"
					f.Reason = "The Deployment is absent from the collected snapshot; collection completeness is unproven."
					return f
				}
				if w.Generation <= 0 || w.ObservedGeneration <= 0 {
					f.Code = "MISSING_GENERATION"
					f.Reason = "Deployment generation or observed generation is unavailable."
					return f
				}
				if w.ObservedGeneration < w.Generation || w.UpdatedReplicas < w.DesiredReplicas {
					f.Code = "ROLLOUT_IN_PROGRESS"
					f.Reason = "The controller has not observed the current generation or updated all desired replicas."
					return f
				}
				if g := in.Canonical.Final.GitOps; g != nil && g.Desired != nil && g.Desired.Image != "" && w.Image != g.Desired.Image {
					f.Status = Fail
					if direct {
						f.ProvenanceStatus = "SOURCE_AUTHENTICATED"
					}
					f.Code = "DESIRED_IMAGE_CONFLICT"
					f.Reason = "The live Deployment image conflicts with pinned Git desired state."
					return f
				}
				if float64(w.AvailableReplicas) < *rule.Minimum {
					f.Status = Fail
					if direct {
						f.ProvenanceStatus = "SOURCE_AUTHENTICATED"
					}
					f.Code = "DESIRED_STATE_UNMET"
					f.Reason = "Available replicas are below the authorized minimum."
					return f
				}
			}
		}
	}
	if authorized {
		f.Code = "STATE_OBSERVED_PROVISIONAL"
		f.Reason = "The final collector observation satisfies the configured replica floor; source integrity and sustained outcome require separate evidence."
		if direct {
			f.Status = Pass
			f.ProvenanceStatus = "SOURCE_AUTHENTICATED"
			f.Code = "AUTHORIZED_STATE_OBSERVED"
			f.Reason = "Direct, pinned Kubernetes API observation satisfies the active customer replica floor; service health remains separate."
		}
	}
	return f
}
func assessGitOps(in Input) Finding {
	f := Finding{Dimension: "gitops", Status: Inconclusive, ProvenanceStatus: "GITOPS_SOURCE_CONTEXT_DEPENDENT", Code: "GITOPS_NOT_CONFIGURED", Reason: "No pinned Git desired state and Argo observation were supplied."}
	g := in.Canonical.Final.GitOps
	direct := in.Canonical.Final.SourceProvider == "direct-kubernetes-api:pinned-tls" && g != nil && g.Argo != nil && strings.HasPrefix(g.Argo.Reference, "direct_outcome:") && g.Desired != nil && strings.HasPrefix(g.Desired.Reference, "contract:")
	if g == nil || g.Desired == nil || g.Argo == nil || len(g.Errors) > 0 {
		return f
	}
	if direct && (in.EvaluatedAt.Sub(g.Argo.CapturedAt) > in.Freshness || in.EvaluatedAt.Sub(in.Canonical.Final.CapturedAt) > in.Freshness) {
		f.Code = "STALE_GITOPS_EVIDENCE"
		f.Reason = "Direct GitOps or live state evidence is stale."
		return f
	}
	f.Evidence = []EvidenceRef{{Reference: g.Desired.Reference, Provenance: "git"}, {Reference: g.Argo.Reference, Provenance: "argocd"}}
	if g.Desired.Reference == "" || g.Argo.Reference == "" || g.Desired.CapturedAt.IsZero() || g.Argo.CapturedAt.IsZero() || g.Argo.CapturedAt.Before(g.Desired.CapturedAt) || in.Canonical.Final.CapturedAt.Sub(g.Argo.CapturedAt) > in.Freshness {
		f.Code = "STALE_GITOPS_EVIDENCE"
		f.Reason = "Git and Argo evidence must be ordered, referenced, and fresh."
		return f
	}
	for _, a := range in.Actions {
		if !a.OccurredAt.IsZero() && g.Argo.CapturedAt.Before(a.OccurredAt) {
			f.Code = "PRE_ACTION_ARGO_OBSERVATION"
			f.Reason = "The Argo observation predates an audit candidate for the operation."
			return f
		}
	}
	if g.Desired.Revision != "" && g.Argo.ObservedRevision != "" && g.Desired.Revision != g.Argo.ObservedRevision {
		f.Code = "REVISION_MISMATCH"
		f.Reason = "Argo observed a revision different from pinned Git desired state."
		return f
	}
	parts := strings.Split(g.Desired.Resource, "/")
	if len(parts) == 3 && parts[0] == "Deployment" && g.Desired.Image != "" {
		w := workload(in.Canonical.Final, parts[1], parts[2])
		if w == nil {
			f.Code = "LIVE_RESOURCE_NOT_OBSERVED"
			f.Reason = "The desired Deployment is absent from the live snapshot; collection completeness is unproven."
			return f
		}
		if w.Image != g.Desired.Image {
			f.Status = Fail
			if direct {
				f.ProvenanceStatus = "SOURCE_AUTHENTICATED"
			}
			f.Code = "DESIRED_LIVE_DRIFT"
			f.Reason = "Live Deployment image differs from pinned Git desired image."
			return f
		}
	}
	if g.Argo.SyncStatus == "OutOfSync" {
		f.Code = "DRIFT_OBSERVED"
		f.Reason = "Argo reports drift; a bounded reconciliation window is required before concluding failure."
		return f
	}
	if g.Argo.SyncStatus != "Synced" || g.Argo.HealthStatus != "Healthy" {
		f.Code = "RECONCILIATION_INCOMPLETE"
		f.Reason = "Argo has not reported both synced and healthy."
		return f
	}
	f.Code = "SYNC_OBSERVED_PROVISIONAL"
	f.Reason = "Argo reports synced and healthy; service outcome remains separate."
	if direct {
		f.Status = Pass
		f.ProvenanceStatus = "SOURCE_AUTHENTICATED"
		f.Code = "GITOPS_CONVERGED"
		f.Reason = "Direct Argo and Kubernetes observations match the customer contract's desired image and registered Git revision; service health remains separate."
	}
	return f
}
func assessDriftWindow(in Input, samples []verification.Snapshot) Finding {
	f := Finding{Dimension: "drift_regression", Status: Inconclusive, ProvenanceStatus: "COLLECTOR_AUTHENTICATED_SOURCE_UNVERIFIED", Code: "DRIFT_WINDOW_UNOBSERVED", Reason: "Pinned Git desired state and multiple live observations are required."}
	g := in.Canonical.Final.GitOps
	if g == nil || g.Desired == nil || g.Desired.Image == "" || len(samples) < 2 {
		return f
	}
	parts := strings.Split(g.Desired.Resource, "/")
	if len(parts) != 3 || parts[0] != "Deployment" {
		return f
	}
	var first, last *agentgateway.WorkloadObservation
	var firstAt, lastAt time.Time
	for _, sample := range samples {
		w := workload(sample, parts[1], parts[2])
		if w == nil {
			continue
		}
		if first == nil {
			first = w
			firstAt = sample.CapturedAt
		}
		last = w
		lastAt = sample.CapturedAt
		f.Evidence = append(f.Evidence, ref(sample)...)
	}
	if first == nil || last == nil || lastAt.Sub(firstAt) < in.Window {
		return f
	}
	if last.Image != g.Desired.Image {
		if first.Image == g.Desired.Image {
			f.Code = "POST_CHANGE_DRIFT_CANDIDATE"
			f.Reason = "Live Deployment image diverged from pinned desired state after an earlier matching observation."
		} else {
			f.Code = "PERSISTENT_DRIFT_CANDIDATE"
			f.Reason = "Live Deployment image differs from pinned desired state across the bounded observation window."
		}
		return f
	}
	if first.Image != g.Desired.Image {
		f.Code = "GITOPS_RECONCILIATION_OBSERVED_PROVISIONAL"
		f.Reason = "Live image converged to pinned desired state during the window; service outcome remains separate."
		return f
	}
	f.Code = "NO_IMAGE_DRIFT_OBSERVED_PROVISIONAL"
	f.Reason = "The observed Deployment image matched pinned desired state across the window; replica autoscaling alone is not image drift."
	return f
}
func serviceSample(in Input, service string) *MetricSample {
	if in.Canonical.Final.Observation.Timestamp.IsZero() {
		return nil
	}
	parts := strings.Split(service, "/")
	if len(parts) != 2 || parts[0] == "" || parts[1] == "" {
		return nil
	}
	var latest *MetricSample
	for i := range in.MetricSamples {
		s := &in.MetricSamples[i]
		if s.Namespace != parts[0] || s.Service != parts[1] || s.SampledAt.Before(in.Canonical.Final.Observation.Timestamp) || s.AcquiredAt.Sub(s.SampledAt) > in.Freshness || s.AcquiredAt.Before(s.SampledAt.Add(-30*time.Second)) || (s.ProvenanceStatus == "SOURCE_AUTHENTICATED" && in.EvaluatedAt.Sub(s.SampledAt) > in.Freshness) {
			continue
		}
		if latest == nil || s.SampledAt.After(latest.SampledAt) {
			latest = s
		}
	}
	return latest
}
func sampleValue(s *MetricSample, typ string) (float64, bool) {
	if s == nil {
		return 0, false
	}
	var p *float64
	switch typ {
	case "max_http_5xx_pct":
		p = s.ErrorRatePct
	case "max_p95_latency_ms":
		p = s.P95LatencyMS
	case "min_request_success_pct":
		p = s.RequestSuccessRatePct
	}
	if p == nil {
		return 0, false
	}
	return *p, true
}
func assessService(in Input) Finding {
	f := Finding{Dimension: "service_outcome", Status: Inconclusive, ProvenanceStatus: "COLLECTOR_AUTHENTICATED_METRICS_UNVERIFIED", Code: "MISSING_SERVICE_METRICS", Reason: "Fresh service metrics and customer thresholds are required.", Evidence: ref(in.Canonical.Final)}
	if in.Canonical.Final.Observation.Timestamp.IsZero() {
		return f
	}
	seen := false
	for _, c := range in.Contracts {
		if !c.Active || c.EffectiveAt.After(in.EvaluatedAt) {
			continue
		}
		for _, rule := range c.Rules {
			if rule.Type != "max_http_5xx_pct" && rule.Type != "max_p95_latency_ms" && rule.Type != "min_request_success_pct" {
				continue
			}
			key := map[string]string{"max_http_5xx_pct": "error_rate_pct", "max_p95_latency_ms": "p95_latency_ms", "min_request_success_pct": "request_success_rate_pct"}[rule.Type]
			sample := serviceSample(in, c.Service)
			v, ok := sampleValue(sample, rule.Type)
			if !ok {
				return f
			}
			f.Evidence = []EvidenceRef{{Reference: "metric_sample:" + sample.ID, Provenance: sample.ProvenanceStatus}}
			seen = true
			if (rule.Maximum != nil && v > *rule.Maximum) || (rule.Minimum != nil && v < *rule.Minimum) {
				f.Code = "UNVERIFIED_SERVICE_THRESHOLD_BREACH_CANDIDATE"
				f.Reason = fmt.Sprintf("Forwarded %s %.3f breaches the threshold; metric origin is unverified.", key, v)
				if sample.ProvenanceStatus == "SOURCE_AUTHENTICATED" {
					f.Status = Fail
					f.ProvenanceStatus = "SOURCE_AUTHENTICATED"
					f.Code = "SERVICE_THRESHOLD_BREACHED"
					f.Reason = fmt.Sprintf("Source-authenticated %s %.3f breaches contract threshold.", key, v)
				}
				return f
			}
		}
	}
	if seen {
		f.Code = "SERVICE_METRICS_PROVISIONAL"
		f.Reason = "Collector metrics satisfy configured thresholds; sustained service health requires temporal evidence."
		allDirect := true
		for _, c := range in.Contracts {
			for _, rule := range c.Rules {
				if rule.Type == "max_http_5xx_pct" || rule.Type == "max_p95_latency_ms" || rule.Type == "min_request_success_pct" {
					sample := serviceSample(in, c.Service)
					if sample == nil || sample.ProvenanceStatus != "SOURCE_AUTHENTICATED" || sample.RequestCount <= 0 {
						allDirect = false
					}
				}
			}
		}
		if allDirect {
			f.Status = Pass
			f.ProvenanceStatus = "SOURCE_AUTHENTICATED"
			f.Code = "SERVICE_THRESHOLDS_MET"
			f.Reason = "Fresh, direct Prometheus samples meet active customer thresholds with configured request-volume sufficiency; sustained recovery is assessed separately."
		}
	}
	return f
}
func assessTemporal(in Input, samples []verification.Snapshot) Finding {
	f := Finding{Dimension: "temporal_stability", Status: Inconclusive, ProvenanceStatus: "COLLECTOR_AUTHENTICATED_SOURCE_UNVERIFIED", Code: "INSUFFICIENT_OBSERVATIONS", Reason: "A bounded interval with multiple fresh observations is required."}
	for _, c := range in.Contracts {
		if !c.Active {
			continue
		}
		for _, rule := range c.Rules {
			if rule.WindowSeconds > 0 && time.Duration(rule.WindowSeconds)*time.Second > in.Window {
				in.Window = time.Duration(rule.WindowSeconds) * time.Second
			}
		}
	}
	if len(samples) < 2 {
		return f
	}
	first, last := samples[0], samples[len(samples)-1]
	f.Evidence = append(ref(first), ref(last)...)
	if first.CapturedAt.IsZero() || last.CapturedAt.Sub(first.CapturedAt) < in.Window || last.CapturedAt.After(in.EvaluatedAt) {
		return f
	}
	maxGap := in.Window / 2
	for i := 1; i < len(samples); i++ {
		if samples[i].CapturedAt.Sub(samples[i-1].CapturedAt) > maxGap {
			f.Code = "OBSERVATION_GAP"
			f.Reason = "The stability window contains an observation gap."
			return f
		}
	}
	for _, c := range in.Contracts {
		if !c.Active || c.EffectiveAt.After(in.EvaluatedAt) {
			continue
		}
		for _, rule := range c.Rules {
			if rule.Type != "min_available_replicas" || rule.Minimum == nil {
				continue
			}
			initial := workload(first, rule.Namespace, rule.ResourceName)
			final := workload(last, rule.Namespace, rule.ResourceName)
			if initial == nil || final == nil {
				f.Code = "INCOMPLETE_WORKLOAD_SERIES"
				f.Reason = "The protected Deployment is missing from a temporal observation."
				return f
			}
			for _, sample := range samples[1 : len(samples)-1] {
				if workload(sample, rule.Namespace, rule.ResourceName) == nil {
					f.Code = "INCOMPLETE_WORKLOAD_SERIES"
					f.Reason = "The protected Deployment is missing from an intermediate observation."
					return f
				}
			}
			wasHealthy := float64(initial.AvailableReplicas) >= *rule.Minimum
			endedHealthy := float64(final.AvailableReplicas) >= *rule.Minimum
			firstBelow := time.Time{}
			for _, sample := range samples {
				w := workload(sample, rule.Namespace, rule.ResourceName)
				if w != nil && float64(w.AvailableReplicas) < *rule.Minimum && firstBelow.IsZero() {
					firstBelow = sample.CapturedAt
				}
			}
			if wasHealthy && !endedHealthy {
				f.Status = Fail
				f.Code = "REGRESSION_OBSERVED"
				f.Reason = "The protected workload fell below its replica floor during the observation window."
				return f
			}
			if !wasHealthy && endedHealthy {
				f.Code = "RECOVERY_OBSERVED_PROVISIONAL"
				f.Reason = "The protected workload recovered in collector observations; service recovery remains separate."
				if rule.GraceSeconds > 0 && !firstBelow.IsZero() && last.CapturedAt.Sub(firstBelow) > time.Duration(rule.GraceSeconds)*time.Second {
					f.Code = "RECOVERY_AFTER_GRACE_CANDIDATE"
					f.Reason = "Collector observations show recovery after the allowed grace period; source integrity is unverified."
				}
				return f
			}
			if wasHealthy && endedHealthy && !firstBelow.IsZero() {
				f.Code = "INTERMEDIATE_REGRESSION_CANDIDATE"
				f.Reason = "An intermediate collector observation fell below the replica floor before recovery."
				return f
			}
		}
	}
	f.Code = "WINDOW_OBSERVED_PROVISIONAL"
	f.Reason = "The collector supplied a continuous bounded observation window; source integrity and service thresholds are assessed separately."
	return f
}
func assessMetricWindow(in Input) Finding {
	f := Finding{Dimension: "service_stability", Status: Inconclusive, ProvenanceStatus: "COLLECTOR_AUTHENTICATED_METRICS_UNVERIFIED", Code: "SERVICE_WINDOW_UNOBSERVED", Reason: "Multiple timely service metric samples are required for a stability window."}
	for _, c := range in.Contracts {
		if !c.Active || c.Service == "" {
			continue
		}
		for _, rule := range c.Rules {
			if rule.Type != "max_http_5xx_pct" && rule.Type != "max_p95_latency_ms" && rule.Type != "min_request_success_pct" {
				continue
			}
			parts := strings.Split(c.Service, "/")
			if len(parts) != 2 {
				continue
			}
			window := in.Window
			if rule.WindowSeconds > 0 {
				window = time.Duration(rule.WindowSeconds) * time.Second
			}
			if window <= 0 {
				window = 5 * time.Minute
			}
			end := time.Time{}
			for _, s := range in.MetricSamples {
				if s.Namespace == parts[0] && s.Service == parts[1] && s.SampledAt.After(end) {
					end = s.SampledAt
				}
			}
			var samples []MetricSample
			for _, s := range in.MetricSamples {
				if s.Namespace == parts[0] && s.Service == parts[1] && !s.SampledAt.Before(end.Add(-window)) {
					samples = append(samples, s)
				}
			}
			sort.Slice(samples, func(i, j int) bool { return samples[i].SampledAt.Before(samples[j].SampledAt) })
			if len(samples) < 2 {
				return f
			}
			coverageStart := samples[0].SampledAt
			if !samples[0].WindowStartedAt.IsZero() && samples[0].WindowStartedAt.Before(coverageStart) {
				coverageStart = samples[0].WindowStartedAt
			}
			if coverageStart.After(end.Add(-window)) || in.EvaluatedAt.Sub(end) > in.Freshness || end.After(in.EvaluatedAt) {
				return f
			}
			for i, s := range samples {
				if s.AcquiredAt.Sub(s.SampledAt) > in.Freshness || (i > 0 && s.SampledAt.Sub(samples[i-1].SampledAt) > window/2 && (s.WindowStartedAt.IsZero() || s.WindowStartedAt.After(samples[i-1].SampledAt))) {
					f.Code = "SERVICE_WINDOW_GAP"
					f.Reason = "Service metric samples have a freshness or coverage gap."
					return f
				}
			}
			f.Evidence = []EvidenceRef{}
			allDirect := true
			for _, s := range samples {
				v, ok := sampleValue(&s, rule.Type)
				if !ok {
					f.Code = "SERVICE_METRIC_MISSING"
					return f
				}
				f.Evidence = append(f.Evidence, EvidenceRef{Reference: "metric_sample:" + s.ID, Provenance: s.ProvenanceStatus})
				if s.ProvenanceStatus != "SOURCE_AUTHENTICATED" || s.RequestCount <= 0 || s.WindowStartedAt.IsZero() {
					allDirect = false
				}
				if (rule.Maximum != nil && v > *rule.Maximum) || (rule.Minimum != nil && v < *rule.Minimum) {
					f.Code = "UNVERIFIED_SERVICE_WINDOW_BREACH_CANDIDATE"
					f.Reason = "A metric sample in the stability window breaches the contract; source origin is unverified."
					if s.ProvenanceStatus == "SOURCE_AUTHENTICATED" {
						f.Status = Fail
						f.ProvenanceStatus = "SOURCE_AUTHENTICATED"
						f.Code = "SERVICE_WINDOW_BREACH"
						f.Reason = "Source-authenticated metric breaches the stability window."
					}
					return f
				}
			}
			f.Code = "SERVICE_WINDOW_OBSERVED_PROVISIONAL"
			f.Reason = "Forwarded metric samples cover the window; source integrity remains unverified."
			if allDirect {
				f.Status = Pass
				f.ProvenanceStatus = "SOURCE_AUTHENTICATED"
				f.Code = "SERVICE_WINDOW_STABLE"
				f.Reason = "Direct source-authenticated samples cover the customer stability window without a threshold breach."
			}
			return f
		}
	}
	return f
}
func assessRule(in Input, service string, rule Rule) Finding {
	f := Finding{Dimension: "operational_contract", Status: Inconclusive, ProvenanceStatus: "SOURCE_UNVERIFIED", Code: "OBSERVATION_SOURCE_MISSING", Reason: "The rule cannot be verified from available evidence.", Evidence: ref(in.Canonical.Final)}
	freshness := in.Freshness
	if rule.FreshnessSeconds > 0 {
		freshness = time.Duration(rule.FreshnessSeconds) * time.Second
	}
	if rule.Type == "require_approval" {
		return assessApproval(in, rule)
	}
	if rule.Type == "min_available_replicas" && !freshSnapshot(in.Canonical.Final, freshness) {
		f.Code = "MISSING_OR_STALE_OBSERVATION"
		f.Reason = "A fresh observation is required for this rule."
		return f
	}
	switch rule.Type {
	case "min_available_replicas":
		if rule.Minimum == nil || in.Canonical.Final.Reference == "" {
			return f
		}
		w := workload(in.Canonical.Final, rule.Namespace, rule.ResourceName)
		if w == nil {
			f.Code = "RESOURCE_NOT_OBSERVED"
			f.Reason = "The Deployment is absent from the snapshot; completeness is unproven."
		} else if float64(w.AvailableReplicas) < *rule.Minimum {
			f.Status = Fail
			if in.Canonical.Final.SourceProvider == "direct-kubernetes-api:pinned-tls" {
				f.ProvenanceStatus = "SOURCE_AUTHENTICATED"
			}
			f.Code = "REPLICA_FLOOR_BREACHED"
			f.Reason = "Available replicas are below the required floor."
		} else {
			f.Code = "REPLICA_FLOOR_OBSERVED_PROVISIONAL"
			f.Reason = "Collector observation meets the replica floor."
			if in.Canonical.Final.SourceProvider == "direct-kubernetes-api:pinned-tls" {
				f.Status = Pass
				f.ProvenanceStatus = "SOURCE_AUTHENTICATED"
				f.Code = "REPLICA_FLOOR_MET"
				f.Reason = "Direct Kubernetes API observation meets the customer replica floor."
			}
		}
	case "max_http_5xx_pct", "max_p95_latency_ms", "min_request_success_pct":
		sample := serviceSample(in, service)
		v, ok := sampleValue(sample, rule.Type)
		if !ok {
			return f
		}
		f.Evidence = []EvidenceRef{{Reference: "metric_sample:" + sample.ID, Provenance: sample.ProvenanceStatus}}
		if (rule.Maximum != nil && v > *rule.Maximum) || (rule.Minimum != nil && v < *rule.Minimum) {
			f.Code = "UNVERIFIED_METRIC_THRESHOLD_BREACH_CANDIDATE"
			f.Reason = "A forwarded metric breaches the contract threshold; metric origin is unverified."
			if sample.ProvenanceStatus == "SOURCE_AUTHENTICATED" {
				f.Status = Fail
				f.ProvenanceStatus = "SOURCE_AUTHENTICATED"
				f.Code = "METRIC_THRESHOLD_BREACHED"
				f.Reason = "Source-authenticated service metric breaches the contract."
			}
		} else {
			f.Code = "METRIC_OBSERVED_PROVISIONAL"
			f.Reason = "Collector metric meets the contract threshold."
		}
	case "forbid_operation", "allowed_mutation_scope":
		for _, a := range in.Actions {
			match := rule.Namespace == "" || rule.Namespace == a.Namespace
			if rule.ResourceKind != "" {
				match = match && rule.ResourceKind == a.Kind
			}
			if rule.ResourceName != "" {
				match = match && rule.ResourceName == a.Name
			}
			if rule.ResourceUID != "" {
				match = match && rule.ResourceUID == a.UID
			}
			if rule.Type == "forbid_operation" && match && rule.Operation == a.Operation {
				f.Evidence = []EvidenceRef{{Reference: "audit:" + strings.TrimPrefix(a.ID, "audit:"), Provenance: a.Source}}
				if !a.OriginAuthenticated {
					f.Code = "UNVERIFIED_FORBIDDEN_OPERATION_CANDIDATE"
					f.Reason = "A forwarded audit candidate describes a forbidden operation, but API-server origin is unverified."
					return f
				}
				f.Status = Fail
				f.ProvenanceStatus = "SOURCE_AUTHENTICATED"
				f.Code = "FORBIDDEN_OPERATION_OBSERVED"
				f.Reason = "Source-authenticated audit evidence records a forbidden operation."
				return f
			}
			if rule.Type == "allowed_mutation_scope" && !match {
				f.Evidence = []EvidenceRef{{Reference: "audit:" + strings.TrimPrefix(a.ID, "audit:"), Provenance: a.Source}}
				if !a.OriginAuthenticated {
					f.Code = "UNVERIFIED_OUT_OF_SCOPE_CANDIDATE"
					f.Reason = "A forwarded audit candidate describes an out-of-scope mutation, but API-server origin is unverified."
					return f
				}
				f.Status = Fail
				f.ProvenanceStatus = "SOURCE_AUTHENTICATED"
				f.Code = "MUTATION_OUTSIDE_SCOPE"
				f.Reason = "Source-authenticated audit evidence records a mutation outside allowed scope."
				return f
			}
		}
	case "approved_image":
		for _, requested := range requestedOperations(in) {
			if requested.Resource != rule.Namespace+"/"+rule.ResourceName {
				continue
			}
			var payload struct {
				Image string `json:"image"`
			}
			if json.Unmarshal(requested.Requested, &payload) != nil || payload.Image == "" {
				f.Code = "REQUESTED_IMAGE_MISSING"
				f.Reason = "The requested image is unavailable."
				return f
			}
			if payload.Image != rule.Value {
				f.Code = "UNAPPROVED_IMAGE_REQUEST_CANDIDATE"
				f.Reason = "The recorded request names an image outside the approved contract; execution remains unproven."
				return f
			}
			f.Code = "APPROVED_IMAGE_REQUESTED_PROVISIONAL"
			f.Reason = "The recorded request names an approved image; the observed operation is assessed separately."
			return f
		}
		f.Status = NotApplicable
		f.Code = "NO_MATCHING_ACTION"
		f.Reason = "No executed action targets this image rule."
	case "ready_before_mutation":
		for _, requested := range requestedOperations(in) {
			if requested.Resource != rule.Namespace+"/"+rule.ResourceName {
				continue
			}
			baseline := in.Canonical.Baseline
			if !freshSnapshot(baseline, freshness) || baseline.Observation.Timestamp.After(requested.At) || requested.At.Sub(baseline.Observation.Timestamp) > freshness {
				f.Code = "STALE_OR_MISSING_PRECONDITION"
				f.Reason = "A fresh observation before the action is required."
				return f
			}
			f.Evidence = ref(baseline)
			w := workload(baseline, rule.Namespace, rule.ResourceName)
			if w == nil {
				f.Code = "PRECONDITION_RESOURCE_MISSING"
				return f
			}
			if rule.Minimum != nil && float64(w.AvailableReplicas) < *rule.Minimum {
				f.Code = "UNVERIFIED_READINESS_PRECONDITION_BREACH_CANDIDATE"
				f.Reason = "The collector baseline reports readiness below the required floor; source integrity is unverified."
				return f
			}
			f.Code = "READINESS_PRECONDITION_OBSERVED_PROVISIONAL"
			f.Reason = "The collector baseline meets the readiness floor."
			return f
		}
		f.Status = NotApplicable
		f.Code = "NO_MATCHING_ACTION"
	case "service_ready_before_mutation":
		if rule.Maximum == nil {
			f.Code = "SERVICE_PRECONDITION_THRESHOLD_MISSING"
			return f
		}
		parts := strings.Split(service, "/")
		if len(parts) != 2 {
			f.Code = "SERVICE_SCOPE_MISSING"
			return f
		}
		for _, requested := range requestedOperations(in) {
			if requested.Resource != rule.Namespace+"/"+rule.ResourceName {
				continue
			}
			var sample *MetricSample
			for i := range in.MetricSamples {
				candidate := &in.MetricSamples[i]
				if candidate.Namespace == parts[0] && candidate.Service == parts[1] && !candidate.SampledAt.After(requested.At) && requested.At.Sub(candidate.SampledAt) <= freshness && candidate.AcquiredAt.Sub(candidate.SampledAt) <= freshness {
					if sample == nil || candidate.SampledAt.After(sample.SampledAt) {
						sample = candidate
					}
				}
			}
			if sample == nil || sample.ErrorRatePct == nil {
				f.Code = "SERVICE_PRECONDITION_METRIC_MISSING"
				f.Reason = "A fresh service metric before the action is required."
				return f
			}
			f.Evidence = []EvidenceRef{{Reference: "metric_sample:" + sample.ID, Provenance: sample.ProvenanceStatus}}
			if *sample.ErrorRatePct > *rule.Maximum {
				f.Code = "SERVICE_READINESS_BREACH_CANDIDATE"
				f.Reason = "A forwarded pre-action 5xx sample exceeds the allowed threshold; source and action time are not independently established."
				return f
			}
			f.Code = "SERVICE_READINESS_OBSERVED_PROVISIONAL"
			f.Reason = "A forwarded pre-action 5xx sample meets the threshold; metric origin and action time remain unverified."
			return f
		}
		f.Status = NotApplicable
		f.Code = "NO_MATCHING_ACTION"
	case "post_image":
		if !freshSnapshot(in.Canonical.Final, freshness) {
			f.Code = "STALE_OR_MISSING_POSTCONDITION"
			return f
		}
		w := workload(in.Canonical.Final, rule.Namespace, rule.ResourceName)
		if w == nil || w.Generation == 0 || w.ObservedGeneration < w.Generation {
			f.Code = "POSTCONDITION_ROLLOUT_INCOMPLETE"
			return f
		}
		if w.Image != rule.Value {
			f.Code = "UNVERIFIED_IMAGE_POSTCONDITION_BREACH_CANDIDATE"
			f.Reason = "The collector final state differs from the required image; source integrity is unverified."
			if in.Canonical.Final.SourceProvider == "direct-kubernetes-api:pinned-tls" {
				f.Status = Fail
				f.ProvenanceStatus = "SOURCE_AUTHENTICATED"
				f.Code = "IMAGE_POSTCONDITION_BREACHED"
				f.Reason = "Direct Kubernetes API observation differs from the customer-required image."
			}
			return f
		}
		f.Code = "IMAGE_POSTCONDITION_OBSERVED_PROVISIONAL"
		f.Reason = "The collector final state names the required image."
		if in.Canonical.Final.SourceProvider == "direct-kubernetes-api:pinned-tls" {
			f.Status = Pass
			f.ProvenanceStatus = "SOURCE_AUTHENTICATED"
			f.Code = "IMAGE_POSTCONDITION_MET"
			f.Reason = "Direct Kubernetes API observation matches the customer-required image."
		}
	case "no_public_exposure":
		f = assessExposureConfiguration(in, rule)
	}
	return f
}

type requestedOperation struct {
	ID, Resource string
	At           time.Time
	Requested    json.RawMessage
}

func requestedOperations(in Input) []requestedOperation {
	targets := []requestedOperation{}
	for _, requested := range in.Canonical.ActionInspection.Actions {
		if requested.Status == "executed" {
			targets = append(targets, requestedOperation{requested.ID, requested.TargetRef, requested.OccurredAt, requested.Requested})
		}
	}
	for _, raw := range in.ClaimPayloads {
		var claim struct {
			ClaimID   string    `json:"claim_id"`
			Namespace string    `json:"resource_namespace"`
			Name      string    `json:"resource_name"`
			StartedAt time.Time `json:"claimed_started_at"`
			Outcome   string    `json:"claimed_outcome"`
		}
		if json.Unmarshal(raw, &claim) == nil && claim.Outcome == "executed" {
			targets = append(targets, requestedOperation{ID: claim.ClaimID, Resource: claim.Namespace + "/" + claim.Name, At: claim.StartedAt})
		}
	}
	return targets
}
func assessApproval(in Input, rule Rule) Finding {
	f := Finding{Dimension: "operational_contract", Status: Inconclusive, ProvenanceStatus: "CUSTOMER_APPROVAL_RECORD", Code: "APPROVAL_EVIDENCE_MISSING", Reason: "No action-scoped customer approval record is available."}
	target := rule.Namespace + "/" + rule.ResourceName
	for _, requested := range requestedOperations(in) {
		if requested.Resource != target {
			continue
		}
		approved, denied := false, false
		for _, record := range in.Approvals {
			if record.ActionID != requested.ID || record.Resource != target || record.RecordedAt.After(requested.At) || record.ValidUntil.Before(requested.At) || (record.RevokedAt != nil && !record.RevokedAt.After(requested.At)) {
				continue
			}
			f.Evidence = append(f.Evidence, EvidenceRef{Reference: "approval:" + record.ID, Provenance: "authenticated_customer_user"})
			if record.Decision == "approved" {
				approved = true
			} else if record.Decision == "denied" {
				denied = true
			}
		}
		if approved && denied {
			f.Status = Conflicting
			f.Code = "CONFLICTING_APPROVAL_RECORDS"
			f.Reason = "Authenticated customer records both approve and deny this action."
			return f
		}
		if denied {
			f.Code = "ACTION_CLAIM_CONFLICTS_WITH_DENIAL"
			f.Reason = "A customer denial exists, but the claimed operation is not independently attributed."
			return f
		}
		if approved {
			f.Code = "APPROVAL_RECORDED"
			f.Reason = "An authenticated customer approved this action before its recorded execution time; operation attribution remains separate."
			return f
		}
		return f
	}
	f.Status = NotApplicable
	f.Code = "NO_MATCHING_ACTION"
	f.Reason = "No executed action targets this approval rule."
	return f
}
