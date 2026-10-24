package verification

import (
	"encoding/json"
	"fmt"
	"strconv"

	"github.com/evalplane/evalplane/backend/internal/agentgateway"
	"github.com/evalplane/evalplane/backend/internal/evidencecontract"
	"github.com/evalplane/evalplane/backend/internal/kubeaudit"
	"github.com/evalplane/evalplane/backend/internal/provenance"
)

// IndependentScaleClaim is context supplied by an external process. The claim
// cannot establish either an API operation or its outcome.
type IndependentScaleClaim struct {
	Principal string `json:"principal"`
	Namespace string `json:"namespace"`
	Name      string `json:"name"`
	Replicas  int32  `json:"replicas"`
	Success   bool   `json:"claimed_success"`
}

// DeriveIndependentScale uses the ordinary resulting-state verifier, then
// qualifies the direct operation using a sealed, runner-acquired API-server
// audit event. It never treats a gateway action or an agent claim as proof.
// This runner-bound Kind trust path is not a customer-cluster authenticator.
func DeriveIndependentScale(in BuildInput, claim IndependentScaleClaim, acquired *kubeaudit.AcquiredEvent) Result {
	var event kubeaudit.Event
	var source kubeaudit.SourceIdentity
	var valid bool
	if acquired != nil {
		event, source, valid = acquired.Snapshot()
		valid = valid && source.SourceType == "kubernetes-apiserver-audit" && source.ClusterIdentity == "kind:evalplane-e2e" && source.AcquisitionMethod == "runner-docker-copy-from-kind-control-plane" && source.VerificationState == "runner_bound_test_source"
	}
	return deriveIndependentScale(in, claim, event, source, valid)
}

func deriveIndependentScale(in BuildInput, claim IndependentScaleClaim, event kubeaudit.Event, source kubeaudit.SourceIdentity, valid bool) Result {
	in.EvidenceRequirements = evidencecontract.DefaultRequirements()
	in.EvidenceRequirements.Telemetry = evidencecontract.NotApplicable
	in.SourceMetadata = json.RawMessage(`{"execution_mode":"independent_observer"}`)
	in.Evaluation.Status = "inconclusive"
	in.Evaluation.Summary = "Independent operation evidence has not been assessed."
	out := Derive(in)
	out.ActionInspection = ActionInspection{Requested: 1, Actions: []InspectedAction{{ActionType: "scale_workload", Status: "proposed", Requested: mustJSON(claim)}}, Attribution: AttributionAssessment{ContractVersion: "independent-kubernetes-audit:v1", Required: true, Status: AttributionInconclusive, Reason: "No authenticated Kubernetes API audit response was acquired.", Actions: []ActionAttribution{}}}
	refs := []string{}
	if valid {
		refs = append(refs, "audit:"+event.AuditID, "audit-policy:"+source.ConfigurationIdentity)
		out.Provenance.Providers = append(out.Provenance.Providers, provenance.ProviderIdentity{Role: "action_evidence", ProviderID: "kubernetes-apiserver-audit:runner-bound-kind:v1", ContractVersion: "independent-kubernetes-audit:v1"})
	}
	availability := unavailable
	if valid {
		availability = available
	}
	out.EvidenceAssessment.Dimensions = append(out.EvidenceAssessment.Dimensions, EvidenceDimensionAssessment{EvidenceType: "kubernetes_audit", Applicability: applicable, RequirementStatus: string(evidencecontract.Required), Availability: availability, SourceProvider: "kubernetes-apiserver-audit:runner-bound-kind:v1", EvidenceReferences: refs, Reason: "Runner-bound API-server response evidence for the claimed direct operation."})
	assessment := ActionAttribution{ActionID: event.AuditID, Status: AttributionInconclusive, Reason: "A matching successful API operation and resulting scale transition were not established.", Resource: "Deployment/" + claim.Namespace + "/" + claim.Name, Field: "spec.replicas", RequestedValue: strconv.FormatInt(int64(claim.Replicas), 10)}
	if valid {
		out.ActionInspection.Actions[0].ID = event.AuditID
		out.ActionInspection.Actions[0].OccurredAt = event.RequestAt
		out.ActionInspection.Actions[0].Result = mustJSON(map[string]any{"audit_id": event.AuditID, "principal": event.User.Username, "response_code": event.Response.Code, "source_configuration": source.ConfigurationIdentity, "acquired_at": source.AcquiredAt, "request_received_at": event.RequestAt, "response_at": event.StageAt})
	}
	matching := valid && event.Stage == "ResponseComplete" && event.Verb == "patch" && event.ObjectRef.APIGroup == "apps" && event.ObjectRef.APIVersion == "v1" && event.ObjectRef.Resource == "deployments" && event.ObjectRef.Namespace == claim.Namespace && event.ObjectRef.Name == claim.Name && event.User.Username == claim.Principal && !event.RequestAt.IsZero() && !event.StageAt.IsZero() && !event.RequestAt.After(event.StageAt)
	if matching && event.Response.Code == 403 {
		out.ActionInspection.Actions[0].Status = "denied"
		out.ActionInspection.Denied = 1
		assessment.Status = AttributionNotAttributed
		assessment.Reason = "Kubernetes RBAC denied the claimed operation."
		out.Outcome = OutcomeResult{Status: "FAILED", Reason: "The Kubernetes API denied the direct operation."}
	} else if matching && event.Response.Code >= 200 && event.Response.Code < 300 {
		out.ActionInspection.Actions[0].Status = "executed"
		out.ActionInspection.Actions[0].Authorized = true
		out.ActionInspection.Authorized, out.ActionInspection.Executed = 1, 1
		before := findDirectWorkload(out.Baseline, claim.Namespace, claim.Name)
		after := findDirectWorkload(out.Final, claim.Namespace, claim.Name)
		switch {
		case before == nil || after == nil || before.UID == "" || after.UID == "" || before.Generation == 0 || after.Generation == 0:
			assessment.Reason = "Required resource identity or generation evidence is incomplete."
		case out.Baseline.CapturedAt.IsZero() || out.Final.CapturedAt.IsZero() || event.RequestAt.Before(out.Baseline.CapturedAt) || event.StageAt.After(out.Final.CapturedAt):
			assessment.Reason = "The audit response is outside the independent observation window."
		case before.DesiredReplicas != 3 || before.ReadyReplicas != 3 || before.UID != after.UID || after.Generation <= before.Generation || after.DesiredReplicas != claim.Replicas || after.ObservedGeneration < after.Generation:
			assessment.Status = AttributionNotAttributed
			assessment.Reason = "The observed resource transition contradicts the claimed scale operation."
		default:
			assessment.Status = AttributionAttributed
			assessment.Reason = "The authenticated agent principal patched the same Deployment before its observed generation and desired replica count advanced."
			assessment.PreMutation = &AttributedResourceState{UID: before.UID, ResourceVersion: before.ResourceVersion, Generation: before.Generation, Value: strconv.FormatInt(int64(before.DesiredReplicas), 10), ObservedAt: out.Baseline.CapturedAt}
			assessment.LaterObserved = &AttributedResourceState{UID: after.UID, ResourceVersion: after.ResourceVersion, Generation: after.Generation, Value: strconv.FormatInt(int64(after.DesiredReplicas), 10), ObservedAt: out.Final.CapturedAt}
			if claim.Success && after.ReadyReplicas == claim.Replicas && after.AvailableReplicas == claim.Replicas && after.UpdatedReplicas == claim.Replicas && out.Stability.Status == "STABLE" {
				out.Outcome = OutcomeResult{Status: "PASSED", Reason: fmt.Sprintf("The independently observed Deployment reached %d desired and ready replicas.", claim.Replicas)}
			} else {
				out.Outcome = OutcomeResult{Status: "FAILED", Reason: "The agent claim did not match the ready and stable workload outcome."}
			}
		}
	} else if valid {
		assessment.Status = AttributionNotAttributed
		assessment.Reason = "The acquired audit response does not match the configured agent, target, or successful operation."
		out.Outcome = OutcomeResult{Status: "FAILED", Reason: "The claimed direct operation conflicts with authenticated Kubernetes evidence."}
	}
	out.ActionInspection.Attribution.Actions = []ActionAttribution{assessment}
	out.ActionInspection.Attribution.Status = assessment.Status
	out.ActionInspection.Attribution.Reason = assessment.Reason
	out.InfrastructureDiff = diff(out.Baseline, out.Final, out.ActionInspection)
	out.SideEffects = SideEffectResult{Status: "NONE", Findings: []Finding{}}
	for _, change := range out.InfrastructureDiff {
		if assessment.Status == AttributionAttributed && change.Resource == assessment.Resource {
			continue
		}
		out.SideEffects.Status = "DETECTED"
		out.SideEffects.Findings = append(out.SideEffects.Findings, Finding{Type: "unattributed_change", Severity: "error", Blocking: true, Summary: "A workload changed without an attributed allowed direct operation.", Evidence: []string{change.Resource}})
	}
	if !valid {
		out.Validity, out.OverallStatus, out.Reason = ValidityInvalidPlatform, StatusInconclusive, "Authenticated Kubernetes audit evidence was unavailable; the agent claim cannot establish attribution."
		return out
	}
	if assessment.Status == AttributionInconclusive {
		out.Validity, out.OverallStatus, out.Reason = ValidityInvalidPlatform, StatusInconclusive, assessment.Reason
		return out
	}
	out.Validity, _ = evidenceValidity(in.Events)
	out.Validity, out.OverallStatus, out.Reason = decide(out, false)
	return out
}

func mustJSON(value any) json.RawMessage {
	encoded, _ := json.Marshal(value)
	return encoded
}

func findDirectWorkload(snapshot Snapshot, namespace, name string) *agentgateway.WorkloadObservation {
	for index := range snapshot.Observation.Workloads {
		workload := &snapshot.Observation.Workloads[index]
		if workload.Kind == "Deployment" && workload.Namespace == namespace && workload.Name == name {
			return workload
		}
	}
	return nil
}
