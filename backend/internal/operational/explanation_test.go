package operational

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/evalplane/evalplane/backend/internal/verification"
)

func TestOperationExplanationsKeepTrustStepsSeparate(t *testing.T) {
	preflight := GatewayPreflight{ID: "preflight-1", ActionID: "action-1", AgentVersionID: "version-1", Namespace: "checkout", ResourceName: "api", Operation: "patch", Decision: "allowed"}
	audit := Action{ID: "audit-1", Namespace: "checkout", Name: "api", Kind: "Deployment", Operation: "patch", OriginAuthenticated: true, Source: "kubernetes-api"}
	rule := Finding{RuleID: "contract-1:v2:rule:1", ContractID: "contract-1", ContractVersion: 2, Status: Fail, Code: "FORBIDDEN_OPERATION_OBSERVED", Evidence: []EvidenceRef{{Reference: "audit-1", Provenance: "kubernetes-api"}}}
	report := Report{GatewayPreflights: []GatewayPreflight{preflight}, ObservedActions: []Action{audit}, Findings: []Finding{rule}}
	binding := ExecutionBinding{ReceiptID: "receipt-1", ActionID: "action-1", PreflightID: "preflight-1", AgentVersionID: "version-1", AuditSourceID: "audit-1", DecisionID: "decision-1", DecisionStatus: "ATTRIBUTED", Operation: "patch", Namespace: "checkout", ResourceName: "api"}
	out := explainOperations(report, []ExecutionBinding{binding})
	if len(out) != 1 || !out[0].Attempted || out[0].GatewayDecision != "allowed" || out[0].AttributionStatus != "attributed" || out[0].ObservationStatus != "authenticated Kubernetes audit event observed" || len(out[0].RuleResults) != 1 || out[0].OutcomeStatus != "not established" {
		t.Fatalf("incorrect positive explanation: %+v", out)
	}
	binding.DecisionStatus = "INCONCLUSIVE"
	out = explainOperations(report, []ExecutionBinding{binding})
	if out[0].AgentVersionID != "version-1" || out[0].AttributionStatus == "attributed" {
		t.Fatalf("inconclusive attribution promoted: %+v", out[0])
	}
	binding.DecisionStatus = "NOT_ATTRIBUTED"
	out = explainOperations(report, []ExecutionBinding{binding})
	if len(out) != 2 || out[0].AttributionStatus == "attributed" || len(out[0].RuleResults) != 0 || out[1].Key != "audit:audit-1" || len(out[1].RuleResults) != 1 {
		t.Fatalf("conflicting attribution promoted: %+v", out[0])
	}
}

func TestOperationExplanationsDenyAndUnlinkedEvidence(t *testing.T) {
	now := time.Now().UTC()
	report := Report{GatewayPreflights: []GatewayPreflight{{ID: "denied-1", ActionID: "action-1", Namespace: "checkout", ResourceName: "api", Operation: "patch", Decision: "denied", ReasonCode: "APPROVAL_REQUIRED_OR_DENIED", DecidedAt: now}}, ApprovalRecords: []ApprovalRecord{{ID: "approval-1", ActionID: "action-1", Resource: "checkout/api", Decision: "denied", RecordedAt: now.Add(-time.Minute), ValidUntil: now.Add(time.Minute)}}, ObservedActions: []Action{{ID: "candidate-1", Namespace: "checkout", Name: "api", OriginAuthenticated: false}}}
	binding := ExecutionBinding{ReceiptID: "receipt-1", ActionID: "action-1", PreflightID: "denied-1", AuditSourceID: "candidate-1", DecisionStatus: "ATTRIBUTED", AgentVersionID: "version-1", Operation: "patch", Namespace: "checkout", ResourceName: "api"}
	out := explainOperations(report, []ExecutionBinding{binding})
	if len(out) != 3 || out[0].ExecutionStatus != "denied by connected gateway" || out[0].ApprovalStatus != "denied by customer approval record" || out[0].ObservationStatus != "not established" || out[1].AttributionStatus == "attributed" || out[2].AttributionStatus == "attributed" {
		t.Fatalf("denied or unverified record was promoted: %+v", out)
	}
}

func TestOperationExplanationsDoNotJoinByResourceOrClaim(t *testing.T) {
	claim, _ := json.Marshal(map[string]string{"claim_id": "claim-1", "action_type": "set_workload_image", "resource_namespace": "checkout", "resource_name": "api", "claimed_outcome": "executed", "agent_version_id": "version-1"})
	report := Report{AgentClaims: []json.RawMessage{claim}, RequestedActions: []verification.InspectedAction{{ID: "canonical-1", ActionType: "set_workload_image", TargetRef: "checkout/api", Status: "executed"}}, ObservedActions: []Action{{ID: "audit-1", Namespace: "checkout", Name: "api", OriginAuthenticated: true}}}
	out := explainOperations(report, nil)
	if len(out) != 3 || out[0].ObservationStatus != "not linked" || out[1].AttributionStatus != "not established" || out[2].AgentVersionID != "" || !strings.Contains(out[1].ExecutionStatus, "agent-reported") {
		t.Fatalf("claim joined by target: %+v", out)
	}
}

func TestSignedReceiptWithoutAuditIsNotAnObservedOperation(t *testing.T) {
	out := explainOperations(Report{}, []ExecutionBinding{{ReceiptID: "receipt-1", ActionID: "action-1", AgentVersionID: "version-1", Operation: "patch", Namespace: "checkout", ResourceName: "api"}})
	if len(out) != 1 || out[0].ObservationStatus != "not established" || out[0].AttributionStatus != "not established" || out[0].OutcomeStatus != "not established" || len(out[0].Unverified) == 0 {
		t.Fatalf("receipt promoted to observation: %+v", out)
	}
}

func TestContractRuleIdentitySurvivesEvaluation(t *testing.T) {
	contract := Contract{ID: "contract-1", Version: 4, Active: true, Rules: []Rule{{Type: "forbid_operation", Operation: "delete", Namespace: "production"}, {Type: "min_available_replicas", Namespace: "production", ResourceName: "api"}}}
	report := Evaluate(Input{Canonical: verification.Result{}, Contracts: []Contract{contract}, EvaluatedAt: time.Now().UTC()})
	var found []Finding
	for _, finding := range report.Findings {
		if finding.RuleID != "" {
			found = append(found, finding)
		}
	}
	if len(found) != 2 || found[0].RuleID != "contract-1:v4:rule:1" || found[1].RuleID != "contract-1:v4:rule:2" || found[0].ContractVersion != 4 {
		t.Fatalf("unstable rule identity: %+v", found)
	}
}
