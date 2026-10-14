package verification

import (
	"fmt"
	"strings"

	"github.com/evalplane/evalplane/backend/internal/evidencecontract"
)

const (
	applicable    = "APPLICABLE"
	notApplicable = "NOT_APPLICABLE"
	available     = "AVAILABLE"
	unavailable   = "UNAVAILABLE"
)

func assessEvidence(requirements evidencecontract.Requirements, baseline, final Snapshot, stabilitySamples []Snapshot) EvidenceAssessment {
	out := EvidenceAssessment{ContractVersion: requirements.ContractVersion, Requirements: requirements, Dimensions: []EvidenceDimensionAssessment{}}
	add := func(evidenceType string, requirement evidencecontract.Requirement, provider string, ok bool, reason string, refs []string) {
		item := EvidenceDimensionAssessment{EvidenceType: evidenceType, RequirementStatus: string(requirement), SourceProvider: provider, EvidenceReferences: compactReferences(refs)}
		if requirement == evidencecontract.NotApplicable {
			item.Applicability, item.Availability = notApplicable, notApplicable
			item.Reason = "This evidence dimension is not applicable under the scenario contract."
		} else {
			item.Applicability = applicable
			if ok {
				item.Availability = available
				item.Reason = reason
			} else {
				item.Availability = unavailable
				item.Reason = reason
			}
		}
		out.Dimensions = append(out.Dimensions, item)
	}

	workloadsAvailable := baseline.Reference != "" && final.Reference != "" && len(baseline.Observation.Workloads) > 0 && len(final.Observation.Workloads) > 0
	workloadReason := "Baseline and final owned workload state were collected."
	if !workloadsAvailable {
		workloadReason = "Required baseline and final owned workload state could not both be established."
	}
	workloadProvider := final.SourceProvider
	if workloadProvider == "" {
		workloadProvider = baseline.SourceProvider
	}
	if workloadProvider == "" {
		workloadProvider = "kubernetes-api"
	}
	add("workload_state", requirements.WorkloadState, workloadProvider, workloadsAvailable, workloadReason, []string{baseline.Reference, final.Reference})

	telemetryOK, telemetryReason := requiredTelemetryAvailable(final, stabilitySamples)
	telemetryRefs := []string{final.Reference}
	for _, sample := range stabilitySamples {
		telemetryRefs = append(telemetryRefs, sample.Reference)
	}
	add("telemetry", requirements.Telemetry, "prometheus", telemetryOK, telemetryReason, telemetryRefs)

	gitOK := final.GitOps != nil && final.GitOps.Desired != nil && strings.TrimSpace(final.GitOps.Desired.Reference) != "" && !hasGitOpsCollectionError(final, "git")
	gitReason := "Final Git desired-state evidence was collected."
	gitRefs := []string{final.Reference}
	if gitOK {
		gitRefs = append(gitRefs, final.GitOps.Desired.Reference)
	} else {
		gitReason = gitOpsUnavailableReason("Git desired state", final)
	}
	add("git_desired_state", requirements.GitDesiredState, "git", gitOK, gitReason, gitRefs)

	argoOK := final.GitOps != nil && final.GitOps.Argo != nil && strings.TrimSpace(final.GitOps.Argo.Reference) != "" && !hasGitOpsCollectionError(final, "argo")
	argoReason := "Final Argo Application state was collected."
	argoRefs := []string{final.Reference}
	if argoOK {
		argoRefs = append(argoRefs, final.GitOps.Argo.Reference)
	} else {
		argoReason = gitOpsUnavailableReason("Argo state", final)
	}
	add("argo_state", requirements.ArgoState, "argocd", argoOK, argoReason, argoRefs)

	stabilityOK, stabilityReason, stabilityRefs := stabilityEvidenceAvailable(requirements, final, stabilitySamples)
	add("stability", requirements.Stability, "verification-stability-sampler", stabilityOK, stabilityReason, stabilityRefs)
	return out
}

func requiredTelemetryAvailable(final Snapshot, samples []Snapshot) (bool, string) {
	all := append(append([]Snapshot(nil), samples...), final)
	if len(all) == 0 {
		return false, "No final or stability observation was available to establish required telemetry."
	}
	for _, snapshot := range all {
		value, present := snapshot.Observation.Metrics["telemetry_available"]
		isAvailable, valid := value.(bool)
		if !present || !valid || !isAvailable {
			return false, "Required telemetry availability was absent or false in the final evidence window."
		}
	}
	return true, "Required telemetry was available in every stability and final observation."
}

func stabilityEvidenceAvailable(requirements evidencecontract.Requirements, final Snapshot, samples []Snapshot) (bool, string, []string) {
	refs := make([]string, 0, len(samples))
	if len(samples) == 0 {
		return false, "No bounded stability observations were collected.", refs
	}
	expected := workloadKeys(final)
	if requirements.WorkloadState == evidencecontract.Required && len(expected) == 0 {
		return false, "Required stability workload scope was empty in the final observation.", refs
	}
	for _, sample := range samples {
		refs = append(refs, sample.Reference)
		if requirements.WorkloadState == evidencecontract.Required && !containsWorkloadScope(sample, expected) {
			return false, "A stability observation did not contain the required final workload scope.", refs
		}
		if requirements.Telemetry == evidencecontract.Required {
			value, present := sample.Observation.Metrics["telemetry_available"]
			isAvailable, valid := value.(bool)
			if !present || !valid || !isAvailable {
				return false, "A stability observation did not contain required available telemetry.", refs
			}
		}
	}
	return true, "The bounded stability window contains the required workload scope and observation channels.", refs
}

func workloadKeys(snapshot Snapshot) map[string]struct{} {
	out := make(map[string]struct{}, len(snapshot.Observation.Workloads))
	for _, workload := range snapshot.Observation.Workloads {
		out[workload.Kind+"/"+workload.Namespace+"/"+workload.Name] = struct{}{}
	}
	return out
}

func containsWorkloadScope(snapshot Snapshot, expected map[string]struct{}) bool {
	observed := workloadKeys(snapshot)
	for key := range expected {
		if _, ok := observed[key]; !ok {
			return false
		}
	}
	return len(expected) > 0
}

func gitOpsUnavailableReason(label string, final Snapshot) string {
	reason := "Required final " + label + " evidence was unavailable."
	if final.Reference == "" {
		return reason + " The final snapshot was not captured."
	}
	if final.GitOps != nil && len(final.GitOps.Errors) > 0 {
		return fmt.Sprintf("%s Collector reason: %s.", reason, strings.Join(final.GitOps.Errors, ","))
	}
	return reason
}

func hasGitOpsCollectionError(final Snapshot, dimension string) bool {
	if final.GitOps == nil {
		return false
	}
	for _, collectorError := range final.GitOps.Errors {
		normalized := strings.ToLower(collectorError)
		if strings.Contains(normalized, "gitops") || strings.Contains(normalized, dimension) {
			return true
		}
		if !strings.Contains(normalized, "git") && !strings.Contains(normalized, "argo") {
			return true
		}
	}
	return false
}

func compactReferences(values []string) []string {
	out := make([]string, 0, len(values))
	seen := map[string]bool{}
	for _, value := range values {
		if value != "" && !seen[value] {
			out = append(out, value)
			seen[value] = true
		}
	}
	return out
}

func missingRequiredEvidence(assessment EvidenceAssessment) []string {
	out := []string{}
	for _, dimension := range assessment.Dimensions {
		if dimension.RequirementStatus == string(evidencecontract.Required) && dimension.Availability != available {
			out = append(out, dimension.EvidenceType)
		}
	}
	return out
}
