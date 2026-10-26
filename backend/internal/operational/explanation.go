package operational

import (
	"encoding/json"
	"fmt"
	"strings"
)

// ExecutionBinding is read from server-scoped receipt, audit, and attribution
// records. A receipt is never an observation, and an audit event alone does not
// identify an AI agent.
type ExecutionBinding struct {
	ReceiptID      string `json:"receipt_id"`
	ActionID       string `json:"action_id"`
	PreflightID    string `json:"preflight_id,omitempty"`
	AgentVersionID string `json:"agent_version_id"`
	AuditEventID   string `json:"audit_event_id,omitempty"`
	AuditSourceID  string `json:"audit_source_id,omitempty"`
	DecisionID     string `json:"decision_id,omitempty"`
	DecisionStatus string `json:"decision_status,omitempty"`
	DecisionReason string `json:"decision_reason,omitempty"`
	Operation      string `json:"operation"`
	Image          string `json:"image,omitempty"`
	Namespace      string `json:"namespace"`
	ResourceName   string `json:"resource_name"`
}

// OperationExplanation records only exact identifier joins. State and service
// findings remain session-level unless their evidence names this operation.
type OperationExplanation struct {
	Key                 string        `json:"key"`
	KubernetesPrincipal string        `json:"kubernetes_principal,omitempty"`
	Requested           string        `json:"requested"`
	Resource            string        `json:"resource,omitempty"`
	Attempted           bool          `json:"attempted"`
	GatewayDecision     string        `json:"gateway_decision"`
	GatewayReason       string        `json:"gateway_reason,omitempty"`
	ExecutionStatus     string        `json:"execution_status"`
	ObservationStatus   string        `json:"observation_status"`
	AttributionStatus   string        `json:"attribution_status"`
	AgentVersionID      string        `json:"agent_version_id,omitempty"`
	AttributionReason   string        `json:"attribution_reason,omitempty"`
	OutcomeStatus       string        `json:"outcome_status"`
	ApprovalStatus      string        `json:"approval_status"`
	Evidence            []EvidenceRef `json:"evidence"`
	RuleResults         []Finding     `json:"rule_results"`
	Unverified          []string      `json:"unverified"`
}

func imageSuffix(image string) string {
	if image == "" {
		return ""
	}
	return " to image " + image
}

func explainOperations(report Report, bindings []ExecutionBinding) []OperationExplanation {
	out := []OperationExplanation{}
	for _, requested := range report.RequestedActions {
		out = append(out, OperationExplanation{Key: "canonical_request:" + requested.ID, Requested: fmt.Sprintf("Canonical action request: %s %s", requested.ActionType, requested.TargetRef), Resource: requested.TargetRef, Attempted: true, GatewayDecision: "not linked", ExecutionStatus: "canonical record: " + requested.Status + "; no customer gateway execution linkage established", ObservationStatus: "not linked", AttributionStatus: "not established", OutcomeStatus: "not established", ApprovalStatus: "not established", Unverified: []string{"Whether this request is the same operation as a customer gateway or audit record", "Operation-specific resulting state and service recovery"}})
	}
	for _, raw := range report.AgentClaims {
		var claim struct {
			ClaimID           string `json:"claim_id"`
			AgentVersionID    string `json:"agent_version_id"`
			ActionType        string `json:"action_type"`
			ResourceNamespace string `json:"resource_namespace"`
			ResourceName      string `json:"resource_name"`
			ClaimedOutcome    string `json:"claimed_outcome"`
		}
		if json.Unmarshal(raw, &claim) != nil || claim.ClaimID == "" {
			continue
		}
		out = append(out, OperationExplanation{Key: "claim:" + claim.ClaimID, Requested: fmt.Sprintf("Agent claims %s on %s/%s", claim.ActionType, claim.ResourceNamespace, claim.ResourceName), Resource: claim.ResourceNamespace + "/" + claim.ResourceName, GatewayDecision: "not linked", ExecutionStatus: "agent-reported " + claim.ClaimedOutcome + "; not independent execution evidence", ObservationStatus: "not linked", AttributionStatus: "not established", AgentVersionID: claim.AgentVersionID, OutcomeStatus: "not established", ApprovalStatus: "not established", Evidence: []EvidenceRef{{Reference: "claim:" + claim.ClaimID, Provenance: "unverified_agent_claim"}}, Unverified: []string{"Whether the claimed operation was executed or observed", "Which registered agent caused any observed change", "Operation-specific resulting state and service recovery"}})
	}
	byPreflight := map[string]int{}
	byAudit := map[string]bool{}
	preflightByID := map[string]GatewayPreflight{}
	for _, p := range report.GatewayPreflights {
		preflightByID[p.ID] = p
		x := OperationExplanation{Key: "gateway_preflight:" + p.ID, Requested: fmt.Sprintf("%s Deployment %s/%s%s", p.Operation, p.Namespace, p.ResourceName, imageSuffix(p.Image)), Resource: p.Namespace + "/" + p.ResourceName, Attempted: true, GatewayDecision: p.Decision, GatewayReason: p.ReasonCode, ExecutionStatus: "not established", ObservationStatus: "not established", AttributionStatus: "not established", AgentVersionID: p.AgentVersionID, OutcomeStatus: "not established", ApprovalStatus: "not established", Evidence: []EvidenceRef{{Reference: "gateway_preflight:" + p.ID, Provenance: "server_owned_preflight"}}}
		if p.Decision == "denied" {
			x.ExecutionStatus = "denied by connected gateway"
		} else {
			x.ExecutionStatus = "permitted; execution not established"
		}
		if len(p.ApprovalReferences) > 0 {
			x.ApprovalStatus = "approved for this gateway request"
			if p.Decision == "denied" {
				x.ApprovalStatus = "approval recorded; gateway denied the request"
			}
			for _, ref := range p.ApprovalReferences {
				x.Evidence = append(x.Evidence, EvidenceRef{Reference: ref, Provenance: "customer_approval"})
			}
		} else if p.ReasonCode == "APPROVAL_REQUIRED_OR_DENIED" {
			x.ApprovalStatus = "required; no valid approval established"
			for _, approval := range report.ApprovalRecords {
				if approval.ActionID == p.ActionID && approval.Resource == x.Resource && approval.Decision == "denied" && !approval.RecordedAt.After(p.DecidedAt) && approval.ValidUntil.After(p.DecidedAt) && (approval.RevokedAt == nil || approval.RevokedAt.After(p.DecidedAt)) {
					x.ApprovalStatus = "denied by customer approval record"
					x.Evidence = append(x.Evidence, EvidenceRef{Reference: "approval:" + approval.ID, Provenance: "customer_approval"})
					break
				}
			}
		}
		byPreflight[p.ID] = len(out)
		out = append(out, x)
	}
	for _, b := range bindings {
		index, linked := byPreflight[b.PreflightID]
		if linked && (b.PreflightID == "" || preflightByID[b.PreflightID].ActionID != b.ActionID || preflightByID[b.PreflightID].Decision != "allowed" || preflightByID[b.PreflightID].Operation != b.Operation || preflightByID[b.PreflightID].Namespace != b.Namespace || preflightByID[b.PreflightID].ResourceName != b.ResourceName) {
			linked = false
		}
		if !linked {
			out = append(out, OperationExplanation{Key: "receipt:" + b.ReceiptID, Requested: fmt.Sprintf("%s %s/%s%s (signed execution record)", b.Operation, b.Namespace, b.ResourceName, imageSuffix(b.Image)), Resource: b.Namespace + "/" + b.ResourceName, GatewayDecision: "not linked", ApprovalStatus: "not established", ExecutionStatus: "not established", ObservationStatus: "not established", AttributionStatus: "not established", AgentVersionID: b.AgentVersionID, OutcomeStatus: "not established"})
			index = len(out) - 1
		}
		x := &out[index]
		x.ExecutionStatus = "signed execution receipt recorded; API result requires audit evidence"
		x.Evidence = append(x.Evidence, EvidenceRef{Reference: "receipt:" + b.ReceiptID, Provenance: "registered_execution_issuer"})
		if b.AuditSourceID != "" {
			for _, action := range report.ObservedActions {
				if action.OriginAuthenticated && action.ID == b.AuditSourceID {
					x.KubernetesPrincipal = action.KubernetesPrincipal
					x.ObservationStatus = "authenticated audit event shares the receipt's Audit-ID; operation binding unresolved"
					x.Evidence = append(x.Evidence, EvidenceRef{Reference: "audit:" + b.AuditSourceID, Provenance: action.Source})
					if b.DecisionStatus == "ATTRIBUTED" {
						x.ObservationStatus = "authenticated Kubernetes audit event observed"
						byAudit[action.ID] = true
					}
					break
				}
			}
		}
		if b.DecisionID != "" {
			x.Evidence = append(x.Evidence, EvidenceRef{Reference: "attribution:" + b.DecisionID, Provenance: "server_attribution_decision"})
			x.AttributionReason = b.DecisionReason
			if b.DecisionStatus == "ATTRIBUTED" && x.ObservationStatus == "authenticated Kubernetes audit event observed" {
				x.AttributionStatus = "attributed"
				x.AgentVersionID = b.AgentVersionID
			} else {
				x.AttributionStatus = "unresolved or conflicting"
			}
		}
	}
	for _, a := range report.ObservedActions {
		if byAudit[a.ID] {
			continue
		}
		status := "forwarded candidate; API-server origin unverified"
		if a.OriginAuthenticated {
			status = "authenticated Kubernetes audit event observed"
		}
		execution := "not established"
		principal := ""
		heading := "Forwarded audit candidate reports "
		if a.OriginAuthenticated {
			execution = "successful mutation recorded by authenticated API-server audit"
			principal = a.KubernetesPrincipal
			heading = "Authenticated audit records "
		}
		out = append(out, OperationExplanation{Key: "audit:" + a.ID, Requested: heading + a.Operation + " " + a.Kind + " " + a.Namespace + "/" + a.Name, Resource: a.Namespace + "/" + a.Name, KubernetesPrincipal: principal, GatewayDecision: "not linked", ExecutionStatus: execution, ObservationStatus: status, AttributionStatus: "not established", OutcomeStatus: "not established", ApprovalStatus: "not established", Evidence: []EvidenceRef{{Reference: "audit:" + a.ID, Provenance: a.Source}}})
	}
	for i := range out {
		x := &out[i]
		if strings.HasPrefix(x.Key, "claim:") || strings.HasPrefix(x.Key, "canonical_request:") {
			continue
		}
		x.RuleResults = []Finding{}
		for _, f := range report.Findings {
			if f.RuleID == "" {
				continue
			}
			for _, ev := range f.Evidence {
				for _, own := range x.Evidence {
					matches := ev.Reference == own.Reference || strings.HasPrefix(own.Reference, "audit:") && ev.Reference == strings.TrimPrefix(own.Reference, "audit:")
					independentAudit := !strings.HasPrefix(own.Reference, "audit:") || strings.HasPrefix(x.Key, "audit:") || x.AttributionStatus == "attributed"
					if matches && independentAudit {
						x.RuleResults = append(x.RuleResults, f)
						goto nextFinding
					}
				}
			}
		nextFinding:
		}
		if x.ObservationStatus != "authenticated Kubernetes audit event observed" {
			x.Unverified = append(x.Unverified, "Whether Kubernetes accepted and applied this operation")
		}
		if x.AttributionStatus != "attributed" {
			x.Unverified = append(x.Unverified, "Which registered AI agent caused the observed operation")
		}
		x.Unverified = append(x.Unverified, "Operation-specific resulting state and service recovery; session-level findings do not prove this action caused them")
	}
	return out
}
