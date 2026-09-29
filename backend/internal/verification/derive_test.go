package verification

import (
	"encoding/json"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/evalplane/evalplane/backend/internal/actionattribution"
	"github.com/evalplane/evalplane/backend/internal/agentgateway"
	"github.com/evalplane/evalplane/backend/internal/evidence"
	"github.com/evalplane/evalplane/backend/internal/evidencecontract"
	"github.com/evalplane/evalplane/backend/internal/gitops"
)

func event(id int64, summary string, at time.Time, obs agentgateway.Observation) evidence.RunEvent {
	raw, _ := json.Marshal(map[string]any{"observation": obs})
	return evidence.RunEvent{ID: id, RunID: "run", EventType: "observation", Summary: summary, OccurredAt: at, Payload: raw}
}
func gitopsEvent(id int64, summary string, at time.Time, obs agentgateway.Observation, state *gitops.Evidence) evidence.RunEvent {
	raw, _ := json.Marshal(map[string]any{"observation": obs, "gitops_evidence": state})
	return evidence.RunEvent{ID: id, RunID: "run", EventType: "observation", Summary: summary, OccurredAt: at, Payload: raw}
}
func healthy(image string, replicas int32) agentgateway.Observation {
	return agentgateway.Observation{Workloads: []agentgateway.WorkloadObservation{{Kind: "Deployment", Namespace: "run-ns", Name: "checkout", Image: image, DesiredReplicas: replicas, ReadyReplicas: replicas, AvailableReplicas: replicas, ResourceVersion: "10"}}, Metrics: map[string]any{"telemetry_available": true, "error_rate_pct": 0.5}}
}
func validInput() BuildInput {
	now := time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)
	before := healthy("checkout:v18", 3)
	after := healthy("checkout:v17", 6)
	return BuildInput{WorkspaceID: "workspace", RunID: "run", AgentVersionID: "agent-version", ScenarioVersionID: "scenario-version", Objective: "Restore checkout health", Criteria: []Criterion{{Key: "ready", Metric: "deployment_available_replicas", Operator: ">=", Target: json.RawMessage(`3`)}}, EvidenceRequirements: evidencecontract.DefaultRequirements(), Events: []evidence.RunEvent{event(1, "verification_baseline_captured", now, before), event(2, "stability_observation_collected", now.Add(time.Second), after), event(3, "final_observation_collected", now.Add(2*time.Second), after)}, Actions: []evidence.AgentAction{{ID: "action", RunID: "run", ActionType: "scale_workload", Status: "executed", Authorized: true, TargetType: str("Deployment"), TargetRef: str("run-ns/checkout"), RequestPayload: json.RawMessage(`{"replicas":6}`), ResultPayload: json.RawMessage(`{"status":"executed"}`), CreatedAt: now}}, Evaluation: Evaluation{Status: "pass", Summary: "All required criteria passed."}, Now: now.Add(3 * time.Second)}
}
func str(v string) *string { return &v }

func TestStatusSemantics(t *testing.T) {
	tests := []struct {
		name, want string
		mutate     func(*BuildInput)
	}{{"verified", StatusVerified, func(*BuildInput) {}}, {"failed outcome", StatusFailed, func(i *BuildInput) { i.Evaluation.Status = "fail" }}, {"failed stability", StatusFailed, func(i *BuildInput) {
		var payload struct {
			Observation agentgateway.Observation `json:"observation"`
		}
		_ = json.Unmarshal(i.Events[1].Payload, &payload)
		payload.Observation.Workloads[0].ReadyReplicas = 1
		i.Events[1] = event(2, "stability_observation_collected", i.Events[1].OccurredAt, payload.Observation)
	}}, {"warning risk", StatusWarning, func(i *BuildInput) {
		var payload struct {
			Observation agentgateway.Observation `json:"observation"`
		}
		_ = json.Unmarshal(i.Events[2].Payload, &payload)
		payload.Observation.Autoscalers = []agentgateway.AutoscalerObservation{{Name: "checkout", Namespace: "run-ns", TargetName: "checkout", MaxReplicas: 4}}
		i.Events[2] = event(3, "final_observation_collected", i.Events[2].OccurredAt, payload.Observation)
	}}, {"inconclusive evidence", StatusInconclusive, func(i *BuildInput) { i.Events = i.Events[1:] }}}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			in := validInput()
			tc.mutate(&in)
			got := Derive(in, HPAConsistencyAnalyzer{})
			if got.OverallStatus != tc.want {
				t.Fatalf("status=%s reason=%s", got.OverallStatus, got.Reason)
			}
		})
	}
}

func TestAgentStepLimitVerdictPrecedence(t *testing.T) {
	agentFailure, _ := json.Marshal(map[string]any{"classification": "agent", "code": "agent_step_limit_reached"})
	tests := []struct {
		name       string
		evaluation string
		want       string
	}{
		{name: "incomplete execution", evaluation: "inconclusive", want: StatusInconclusive},
		{name: "blocking outcome", evaluation: "fail", want: StatusFailed},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			in := validInput()
			in.Evaluation.Status = test.evaluation
			in.Events = append(in.Events, evidence.RunEvent{ID: 4, RunID: "run", EventType: "system", Summary: "agent_step_limit_reached", OccurredAt: in.Now, Payload: agentFailure})
			got := Derive(in, HPAConsistencyAnalyzer{})
			if got.OverallStatus != test.want || got.OverallStatus == StatusVerified || got.OverallStatus == StatusWarning {
				t.Fatalf("status=%s reason=%s", got.OverallStatus, got.Reason)
			}
		})
	}
}

func TestActionDiffAndAttribution(t *testing.T) {
	got := Derive(validInput())
	if got.ActionInspection.Requested != 1 || got.ActionInspection.Authorized != 1 || got.ActionInspection.Executed != 1 {
		t.Fatalf("inspection=%+v", got.ActionInspection)
	}
	if len(got.InfrastructureDiff) != 1 || got.InfrastructureDiff[0].Attribution != AttributionNotApplicable || got.InfrastructureDiff[0].Classification != "EXPECTED" {
		t.Fatalf("diff=%+v", got.InfrastructureDiff)
	}
	if len(got.InfrastructureDiff[0].Fields) != 4 {
		t.Fatalf("fields=%+v", got.InfrastructureDiff[0].Fields)
	}
	in := validInput()
	in.Actions = nil
	got = Derive(in)
	if got.InfrastructureDiff[0].Classification != "OBSERVED" || got.InfrastructureDiff[0].Attribution != AttributionNotApplicable {
		t.Fatal("non-required change was assigned causal attribution")
	}
}

func TestRequiredKubernetesImageAttribution(t *testing.T) {
	tests := []struct {
		name        string
		mutate      func(*BuildInput, *actionattribution.Evidence)
		wantStatus  string
		wantVerdict string
	}{
		{name: "normal attribution", wantStatus: AttributionAttributed, wantVerdict: StatusVerified},
		{name: "wrong UID recreation", mutate: func(in *BuildInput, _ *actionattribution.Evidence) {
			rewriteFinalWorkload(t, in, func(w *agentgateway.WorkloadObservation) { w.UID = "uid-b" })
		}, wantStatus: AttributionNotAttributed, wantVerdict: StatusFailed},
		{name: "wrong value", mutate: func(in *BuildInput, _ *actionattribution.Evidence) {
			rewriteFinalWorkload(t, in, func(w *agentgateway.WorkloadObservation) { w.Image = "checkout:v19" })
		}, wantStatus: AttributionNotAttributed, wantVerdict: StatusFailed},
		{name: "missing execution evidence", mutate: func(in *BuildInput, _ *actionattribution.Evidence) {
			in.Actions[0].ResultPayload = json.RawMessage(`{"status":"executed"}`)
		}, wantStatus: AttributionInconclusive, wantVerdict: StatusInconclusive},
		{name: "failed action followed by external mutation", mutate: func(in *BuildInput, _ *actionattribution.Evidence) { in.Actions[0].Status = "failed" }, wantStatus: AttributionNotAttributed, wantVerdict: StatusFailed},
		{name: "resource version contradiction", mutate: func(_ *BuildInput, evidence *actionattribution.Evidence) {
			evidence.PostMutation.ResourceVersion = evidence.PreMutation.ResourceVersion
		}, wantStatus: AttributionNotAttributed, wantVerdict: StatusFailed},
		{name: "generation contradiction", mutate: func(_ *BuildInput, evidence *actionattribution.Evidence) {
			evidence.PostMutation.Generation = evidence.PreMutation.Generation
		}, wantStatus: AttributionNotAttributed, wantVerdict: StatusFailed},
		{name: "out of band mutation returns plausible value", mutate: func(in *BuildInput, _ *actionattribution.Evidence) {
			rewriteFinalWorkload(t, in, func(w *agentgateway.WorkloadObservation) { w.Generation = 3; w.ResourceVersion = "13" })
		}, wantStatus: AttributionNotAttributed, wantVerdict: StatusFailed},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			in, executorEvidence := attributedImageInput(t)
			if test.mutate != nil {
				test.mutate(&in, executorEvidence)
				if in.Actions[0].Status == "executed" && strings.Contains(string(in.Actions[0].ResultPayload), "attribution") {
					payload, _ := json.Marshal(map[string]any{"status": "executed", "authorized": true, "executed": true, "attribution": executorEvidence})
					in.Actions[0].ResultPayload = payload
				}
			}
			got := Derive(in)
			if got.ActionInspection.Attribution.Status != test.wantStatus || got.OverallStatus != test.wantVerdict {
				t.Fatalf("attribution=%+v verdict=%s reason=%s", got.ActionInspection.Attribution, got.OverallStatus, got.Reason)
			}
		})
	}
}

func attributedImageInput(t *testing.T) (BuildInput, *actionattribution.Evidence) {
	t.Helper()
	in := validInput()
	in.ScenarioSourceMetadata = json.RawMessage(`{"action_attribution":"required"}`)
	started := in.Events[0].OccurredAt.Add(500 * time.Millisecond)
	preAt := started.Add(100 * time.Millisecond)
	completed := started.Add(500 * time.Millisecond)
	evidencePayload := &actionattribution.Evidence{ContractVersion: actionattribution.ContractVersion, RunID: in.RunID, ActionID: "action", Executor: "kubernetes-client-go", ActionType: "set_workload_image", Target: agentgateway.ActionTarget{Kind: "Deployment", Namespace: "run-ns", Name: "checkout"}, Field: "spec.template.spec.containers[0].image", RequestedValue: "checkout:v17", AttemptedValue: "checkout:v17", ExecutionStartedAt: started, ExecutionCompletedAt: completed, PreMutation: &actionattribution.ResourceIdentity{UID: "uid-a", ResourceVersion: "10", Generation: 1, Value: "checkout:v18", ObservedAt: preAt}, PostMutation: &actionattribution.ResourceIdentity{UID: "uid-a", ResourceVersion: "11", Generation: 2, Value: "checkout:v17", ObservedAt: completed}}
	request, _ := json.Marshal(map[string]any{"type": "set_workload_image", "image": "checkout:v17", "target": map[string]any{"kind": "Deployment", "namespace": "run-ns", "name": "checkout"}})
	result, _ := json.Marshal(map[string]any{"status": "executed", "authorized": true, "executed": true, "attribution": evidencePayload})
	actionCompleted := completed
	in.Actions = []evidence.AgentAction{{ID: "action", RunID: in.RunID, ActionType: "set_workload_image", Status: "executed", Authorized: true, TargetType: str("Deployment"), TargetRef: str("run-ns/checkout"), RequestPayload: request, ResultPayload: result, CreatedAt: started, StartedAt: &started, CompletedAt: &actionCompleted}}
	for i := range in.Events {
		rewriteEventWorkload(t, &in.Events[i], func(w *agentgateway.WorkloadObservation) {
			w.UID = "uid-a"
			w.DesiredReplicas, w.ReadyReplicas, w.AvailableReplicas = 3, 3, 3
			if i == 0 {
				w.ResourceVersion, w.Generation = "10", 1
			} else {
				w.ResourceVersion, w.Generation = "12", 2
			}
		})
	}
	return in, evidencePayload
}

func rewriteFinalWorkload(t *testing.T, in *BuildInput, mutate func(*agentgateway.WorkloadObservation)) {
	t.Helper()
	for i := range in.Events {
		if in.Events[i].Summary == "final_observation_collected" || in.Events[i].Summary == "stability_observation_collected" {
			rewriteEventWorkload(t, &in.Events[i], mutate)
		}
	}
}

func rewriteEventWorkload(t *testing.T, event *evidence.RunEvent, mutate func(*agentgateway.WorkloadObservation)) {
	t.Helper()
	var payload struct {
		Observation agentgateway.Observation `json:"observation"`
	}
	if err := json.Unmarshal(event.Payload, &payload); err != nil {
		t.Fatal(err)
	}
	mutate(&payload.Observation.Workloads[0])
	event.Payload, _ = json.Marshal(payload)
}

func TestDeniedAndFailedActionsRemainDistinct(t *testing.T) {
	in := validInput()
	in.Actions = []evidence.AgentAction{{ID: "denied", RunID: "run", ActionType: "restart", Status: "denied", CreatedAt: in.Now}, {ID: "failed", RunID: "run", ActionType: "scale", Status: "failed", Authorized: true, CreatedAt: in.Now}}
	got := Derive(in)
	if got.ActionInspection.Denied != 1 || got.ActionInspection.Failed != 1 || got.ActionInspection.Authorized != 1 || got.ActionInspection.Executed != 0 {
		t.Fatalf("inspection=%+v", got.ActionInspection)
	}
}

func TestSnapshotProjectionExcludesSecretLikeUnstructuredData(t *testing.T) {
	in := validInput()
	var payload struct {
		Observation agentgateway.Observation `json:"observation"`
	}
	_ = json.Unmarshal(in.Events[0].Payload, &payload)
	payload.Observation.Metrics["bearer_token"] = "secret"
	payload.Observation.RecentEvents = []agentgateway.RecentEvent{{Summary: "password=secret"}}
	in.Events[0] = event(1, "verification_baseline_captured", in.Events[0].OccurredAt, payload.Observation)
	got := Derive(in)
	raw, _ := json.Marshal(got.Baseline)
	if strings.Contains(string(raw), "secret") || strings.Contains(string(raw), "bearer") || len(got.Baseline.Observation.RecentEvents) != 0 {
		t.Fatalf("unsafe snapshot: %s", raw)
	}
}

func TestRiskRequiresEvidence(t *testing.T) {
	in := validInput()
	got := Derive(in, HPAConsistencyAnalyzer{})
	if len(got.ForwardRisks) != 0 {
		t.Fatal("fabricated risk")
	}
	var payload struct {
		Observation agentgateway.Observation `json:"observation"`
	}
	_ = json.Unmarshal(in.Events[2].Payload, &payload)
	payload.Observation.Autoscalers = []agentgateway.AutoscalerObservation{{Name: "checkout-hpa", Namespace: "run-ns", TargetName: "checkout", MaxReplicas: 5}}
	in.Events[2] = event(3, "final_observation_collected", in.Events[2].OccurredAt, payload.Observation)
	got = Derive(in, HPAConsistencyAnalyzer{})
	if len(got.ForwardRisks) != 1 || got.ForwardRisks[0].EvidenceStrength != "HIGH" {
		t.Fatalf("risks=%+v", got.ForwardRisks)
	}
}

func TestInvalidTaskAndTelemetryUnavailableAreInconclusive(t *testing.T) {
	in := validInput()
	in.Criteria = nil
	got := Derive(in)
	if got.Validity != ValidityInvalidTask || got.OverallStatus != StatusInconclusive {
		t.Fatalf("result=%+v", got)
	}
	in = validInput()
	var payload struct {
		Observation agentgateway.Observation `json:"observation"`
	}
	_ = json.Unmarshal(in.Events[1].Payload, &payload)
	payload.Observation.Metrics["telemetry_available"] = false
	in.Events[1] = event(2, "stability_observation_collected", in.Events[1].OccurredAt, payload.Observation)
	got = Derive(in)
	if got.Validity != ValidityInvalidPlatform || got.Stability.Status != "INCONCLUSIVE" {
		t.Fatalf("result=%+v", got)
	}
}

func TestAgentFailureRemainsDistinctFromPlatformInvalidity(t *testing.T) {
	in := validInput()
	in.Evaluation.Status = "inconclusive"
	raw, _ := json.Marshal(map[string]any{"classification": "agent", "code": "agent_timeout"})
	in.Events = append(in.Events, evidence.RunEvent{ID: 4, RunID: "run", EventType: "system", Summary: "agent_timeout", Payload: raw, OccurredAt: in.Now})
	got := Derive(in)
	if got.Validity != ValidityValid || got.OverallStatus != StatusInconclusive {
		t.Fatalf("validity=%s status=%s", got.Validity, got.OverallStatus)
	}
}

func TestGitOpsInvariantAndResultPolicy(t *testing.T) {
	in := validInput()
	in.EvidenceRequirements = evidencecontract.GitOpsRequirements()
	desired := &gitops.DesiredState{Repository: "fixture", Revision: strings.Repeat("a", 40), Path: "checkout.yaml", Resource: "Deployment/run-ns/checkout", Container: "checkout", Image: "checkout:v18", Reference: "git:pin:checkout.yaml"}
	argo := &gitops.ArgoState{Application: "checkout", Namespace: "argocd", SyncStatus: "OutOfSync", Automated: true, Reference: "argo-application:argocd/checkout:rv:7"}
	state := &gitops.Evidence{ContractVersion: gitops.EvidenceContractVersion, Desired: desired, Argo: argo}
	before := healthy("checkout:v18", 3)
	after := healthy("checkout:v17", 3)
	in.Events = []evidence.RunEvent{gitopsEvent(1, "verification_baseline_captured", in.Now, before, &gitops.Evidence{ContractVersion: gitops.EvidenceContractVersion, Desired: desired, Argo: &gitops.ArgoState{Application: "checkout", Namespace: "argocd", SyncStatus: "Synced", Reference: "argo:baseline"}}), gitopsEvent(2, "stability_observation_collected", in.Now.Add(time.Second), after, state), gitopsEvent(3, "final_observation_collected", in.Now.Add(2*time.Second), after, state)}
	got := Derive(in, GitOpsReconciliationAnalyzer{})
	if got.Outcome.Status != "PASSED" || len(got.Invariants) != 1 || got.Invariants[0].Status != "FAILED" || got.Stability.Status != "UNSTABLE" || got.OverallStatus != StatusFailed || len(got.ForwardRisks) != 1 {
		t.Fatalf("result=%+v", got)
	}
	if !strings.Contains(got.Reason, "immediate recovery succeeded") {
		t.Fatalf("reason=%s", got.Reason)
	}
}

func TestGitOpsPositiveAndMissingEvidenceControls(t *testing.T) {
	in := validInput()
	in.EvidenceRequirements = evidencecontract.GitOpsRequirements()
	desired := &gitops.DesiredState{Resource: "Deployment/run-ns/checkout", Image: "checkout:v17", Reference: "git:pin"}
	synced := &gitops.Evidence{ContractVersion: gitops.EvidenceContractVersion, Desired: desired, Argo: &gitops.ArgoState{SyncStatus: "Synced", Reference: "argo:synced"}}
	after := healthy("checkout:v17", 6)
	in.Events = []evidence.RunEvent{gitopsEvent(1, "verification_baseline_captured", in.Now, after, synced), gitopsEvent(2, "stability_observation_collected", in.Now.Add(time.Second), after, synced), gitopsEvent(3, "final_observation_collected", in.Now.Add(2*time.Second), after, synced)}
	got := Derive(in, GitOpsReconciliationAnalyzer{})
	if got.Invariants[0].Status != "PASSED" || got.Stability.Status != "STABLE" || got.OverallStatus != StatusVerified || len(got.ForwardRisks) != 0 {
		t.Fatalf("positive=%+v", got)
	}
	missing := &gitops.Evidence{ContractVersion: gitops.EvidenceContractVersion, Desired: desired, Errors: []string{"argo_state_unavailable_after_remediation"}}
	in.Events[2] = gitopsEvent(3, "final_observation_collected", in.Now.Add(2*time.Second), after, missing)
	got = Derive(in, GitOpsReconciliationAnalyzer{})
	if got.ActionInspection.Executed != 1 || got.Invariants[0].Status != "INCONCLUSIVE" || got.OverallStatus != StatusInconclusive || got.Validity != ValidityInvalidPlatform || got.Reason != "Required verification evidence was unavailable: argo_state." || got.Final.GitOps == nil || got.Final.GitOps.Argo != nil || !slices.Equal(got.Final.GitOps.Errors, []string{"argo_state_unavailable_after_remediation"}) || len(got.ForwardRisks) != 0 {
		t.Fatalf("missing=%+v", got)
	}
}

func TestEvidenceApplicabilityAndCompleteness(t *testing.T) {
	desired := &gitops.DesiredState{Resource: "Deployment/run-ns/checkout", Image: "checkout:v17", Reference: "git:final"}
	argo := &gitops.ArgoState{SyncStatus: "Synced", Reference: "argo:final"}
	complete := &gitops.Evidence{ContractVersion: gitops.EvidenceContractVersion, Desired: desired, Argo: argo}

	t.Run("required GitOps present evaluates normally", func(t *testing.T) {
		in := validInput()
		in.EvidenceRequirements = evidencecontract.GitOpsRequirements()
		in.Events[0] = gitopsEvent(1, "verification_baseline_captured", in.Events[0].OccurredAt, healthy("checkout:v18", 3), complete)
		in.Events[1] = gitopsEvent(2, "stability_observation_collected", in.Events[1].OccurredAt, healthy("checkout:v17", 6), complete)
		in.Events[2] = gitopsEvent(3, "final_observation_collected", in.Events[2].OccurredAt, healthy("checkout:v17", 6), complete)
		got := Derive(in)
		if len(got.Invariants) != 1 || got.Invariants[0].Status != "PASSED" || got.OverallStatus != StatusVerified {
			t.Fatalf("result=%+v", got)
		}
	})

	t.Run("GitOps not applicable does not require an invariant", func(t *testing.T) {
		got := Derive(validInput())
		if len(got.Invariants) != 0 || got.OverallStatus != StatusVerified {
			t.Fatalf("result=%+v", got)
		}
		for _, dimension := range got.EvidenceAssessment.Dimensions {
			if (dimension.EvidenceType == "git_desired_state" || dimension.EvidenceType == "argo_state") && (dimension.Applicability != notApplicable || dimension.Availability != notApplicable) {
				t.Fatalf("dimension=%+v", dimension)
			}
		}
	})

	t.Run("non-applicable telemetry absence does not cause inconclusive", func(t *testing.T) {
		in := validInput()
		in.EvidenceRequirements.Telemetry = evidencecontract.NotApplicable
		for index, source := range in.Events {
			var payload struct {
				Observation agentgateway.Observation `json:"observation"`
			}
			if err := json.Unmarshal(source.Payload, &payload); err != nil {
				t.Fatal(err)
			}
			payload.Observation.Metrics = map[string]any{}
			in.Events[index] = event(source.ID, source.Summary, source.OccurredAt, payload.Observation)
		}
		got := Derive(in)
		if got.OverallStatus != StatusVerified || got.Validity != ValidityValid {
			t.Fatalf("result=%+v", got)
		}
		for _, dimension := range got.EvidenceAssessment.Dimensions {
			if dimension.EvidenceType == "telemetry" && (dimension.Applicability != notApplicable || dimension.Availability != notApplicable) {
				t.Fatalf("dimension=%+v", dimension)
			}
		}
	})

	t.Run("baseline GitOps never substitutes for missing final GitOps", func(t *testing.T) {
		in := validInput()
		in.EvidenceRequirements = evidencecontract.GitOpsRequirements()
		in.Events[0] = gitopsEvent(1, "verification_baseline_captured", in.Events[0].OccurredAt, healthy("checkout:v18", 3), complete)
		got := Derive(in)
		if got.OverallStatus != StatusInconclusive || got.Validity != ValidityInvalidPlatform || len(got.Invariants) != 1 || got.Invariants[0].Status != "INCONCLUSIVE" {
			t.Fatalf("result=%+v", got)
		}
		if strings.Contains(strings.Join(got.Invariants[0].ObservedEvidence, " "), "git:final") {
			t.Fatalf("baseline evidence leaked into final invariant: %+v", got.Invariants[0])
		}
		if got.Reason != "Required verification evidence was unavailable: git_desired_state, argo_state." {
			t.Fatalf("reason=%q", got.Reason)
		}
	})
}

func TestRequiredStabilityCompleteness(t *testing.T) {
	t.Run("empty required workload scope is inconclusive", func(t *testing.T) {
		in := validInput()
		empty := agentgateway.Observation{Metrics: map[string]any{"telemetry_available": true}}
		in.Events = []evidence.RunEvent{
			event(1, "verification_baseline_captured", in.Now, empty),
			event(2, "stability_observation_collected", in.Now.Add(time.Second), empty),
			event(3, "final_observation_collected", in.Now.Add(2*time.Second), empty),
		}
		got := Derive(in)
		if got.Stability.Status != "INCONCLUSIVE" || got.OverallStatus != StatusInconclusive || got.Validity != ValidityInvalidPlatform {
			t.Fatalf("result=%+v", got)
		}
	})

	t.Run("required healthy scoped evidence is stable", func(t *testing.T) {
		got := Derive(validInput())
		if got.Stability.Status != "STABLE" || got.OverallStatus != StatusVerified {
			t.Fatalf("result=%+v", got)
		}
	})
}
