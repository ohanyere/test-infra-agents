package verification

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/evalplane/evalplane/backend/internal/agentgateway"
	"github.com/evalplane/evalplane/backend/internal/evidence"
	"github.com/evalplane/evalplane/backend/internal/gitops"
)

// Stage 5: attribution must retain the correct mutation authority. A
// desired-state action is attributed only against the Git desired-state
// transition; a final live value alone is never sufficient.

func desiredAttributionAction(actionType, status string, authorized bool) evidence.AgentAction {
	request, _ := json.Marshal(map[string]any{"type": actionType, "image": "checkout:v17", "target": map[string]any{"kind": "Deployment", "namespace": "run-ns", "name": "checkout"}})
	at := time.Date(2026, 2, 3, 4, 5, 6, 0, time.UTC)
	return evidence.AgentAction{ID: "action", RunID: "run", ActionType: actionType, Status: status, Authorized: authorized, TargetType: str("Deployment"), TargetRef: str("run-ns/checkout"), RequestPayload: request, ResultPayload: json.RawMessage(`{"status":"executed"}`), CreatedAt: at}
}

func desiredFinalSnapshot(desired *gitops.DesiredState, argo *gitops.ArgoState, liveImage string) Snapshot {
	return Snapshot{ContractVersion: SnapshotVersion, Reference: "run_event:3", CapturedAt: time.Date(2026, 2, 3, 4, 6, 0, 0, time.UTC),
		Observation: agentgateway.Observation{Workloads: []agentgateway.WorkloadObservation{{Kind: "Deployment", Namespace: "run-ns", Name: "checkout", Image: liveImage, UID: "uid-a", ResourceVersion: "14", Generation: 5}}},
		GitOps:      &gitops.Evidence{ContractVersion: gitops.EvidenceContractVersion, Desired: desired, Argo: argo}}
}

func syncedGitTransition() (*gitops.DesiredState, *gitops.ArgoState) {
	return &gitops.DesiredState{Repository: "fixture", Revision: "0123456789abcdef0123456789abcdef01234567", Path: "desired/checkout.yaml", Resource: "Deployment/run-ns/checkout", Container: "checkout", Image: "checkout:v17", Reference: "git:0123456789abcdef0123456789abcdef01234567:desired/checkout.yaml"},
		&gitops.ArgoState{Application: "checkout", Namespace: "argocd", SyncStatus: "Synced", Reference: "argo:synced"}
}

func TestDesiredStateActionAttributesAgainstGitTransition(t *testing.T) {
	desired, argo := syncedGitTransition()
	assessment := assessActionAttribution([]evidence.AgentAction{desiredAttributionAction("set_desired_workload_image", "executed", true)}, desiredFinalSnapshot(desired, argo, "checkout:v17"), true)
	if assessment.Status != AttributionAttributed || len(assessment.Actions) != 1 || assessment.Actions[0].Status != AttributionAttributed {
		t.Fatalf("assessment=%+v", assessment)
	}
}

func TestDesiredStateActionIgnoresLiveValueAlone(t *testing.T) {
	// No GitOps evidence at all, but the live workload already shows v17.
	// Attributing here would mistake a live fact for a desired-state cause.
	final := desiredFinalSnapshot(nil, nil, "checkout:v17")
	final.GitOps = nil
	assessment := assessActionAttribution([]evidence.AgentAction{desiredAttributionAction("set_desired_workload_image", "executed", true)}, final, true)
	if assessment.Status != AttributionInconclusive || len(assessment.Actions) != 1 || assessment.Actions[0].Status != AttributionInconclusive {
		t.Fatalf("live value alone must never attribute a desired-state action: %+v", assessment)
	}
}

func TestDesiredStateActionRequiresProvenReconciliation(t *testing.T) {
	desired, _ := syncedGitTransition()
	outOfSync := &gitops.ArgoState{Application: "checkout", Namespace: "argocd", SyncStatus: "OutOfSync", Reference: "argo:drift"}
	assessment := assessActionAttribution([]evidence.AgentAction{desiredAttributionAction("set_desired_workload_image", "executed", true)}, desiredFinalSnapshot(desired, outOfSync, "checkout:v17"), true)
	if assessment.Status != AttributionInconclusive || assessment.Actions[0].Status != AttributionInconclusive {
		t.Fatalf("unreconciled desired state must stay inconclusive: %+v", assessment)
	}
}

func TestDesiredStateActionContradictsDivergentDesiredState(t *testing.T) {
	stale := &gitops.DesiredState{Repository: "fixture", Revision: "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", Path: "desired/checkout.yaml", Resource: "Deployment/run-ns/checkout", Container: "checkout", Image: "checkout:v18", Reference: "git:stale"}
	argo := &gitops.ArgoState{Application: "checkout", Namespace: "argocd", SyncStatus: "Synced", Reference: "argo:stale"}
	assessment := assessActionAttribution([]evidence.AgentAction{desiredAttributionAction("set_desired_workload_image", "executed", true)}, desiredFinalSnapshot(stale, argo, "checkout:v18"), true)
	if assessment.Status != AttributionNotAttributed || assessment.Actions[0].Status != AttributionNotAttributed {
		t.Fatalf("divergent desired state must contradict: %+v", assessment)
	}
}

func TestDesiredStateActionRequiresExecution(t *testing.T) {
	desired, argo := syncedGitTransition()
	final := desiredFinalSnapshot(desired, argo, "checkout:v17")
	for _, status := range []string{"failed", "denied"} {
		assessment := assessActionAttribution([]evidence.AgentAction{desiredAttributionAction("set_desired_workload_image", status, status == "failed")}, final, true)
		if assessment.Actions[0].Status != AttributionNotAttributed {
			t.Fatalf("status %s must not attribute: %+v", status, assessment)
		}
	}
}

func TestLiveTypedActionAttributesAgainstLiveEvidence(t *testing.T) {
	in, _ := attributedImageInput(t)
	for i := range in.Actions {
		in.Actions[i].ActionType = "set_live_workload_image"
	}
	var requested map[string]any
	_ = json.Unmarshal(in.Actions[0].RequestPayload, &requested)
	requested["type"] = "set_live_workload_image"
	in.Actions[0].RequestPayload, _ = json.Marshal(requested)
	var result map[string]any
	_ = json.Unmarshal(in.Actions[0].ResultPayload, &result)
	result["attribution"].(map[string]any)["action_type"] = "set_live_workload_image"
	in.Actions[0].ResultPayload, _ = json.Marshal(result)
	got := Derive(in)
	if got.ActionInspection.Attribution.Status != AttributionAttributed {
		t.Fatalf("live typed action was not attributed: %+v", got.ActionInspection.Attribution)
	}
}
