package customerattribution

import (
	"testing"
	"time"
)

func TestCustomerAttributionRemainsConservative(t *testing.T) {
	id := IdentityBinding{WorkspaceID: "tenant", EnvironmentID: "environment", ClusterIdentity: "cluster-a", AgentVersionID: "agent-a", AgentExecutionSessionID: "session-a", ActionID: "action-a", CredentialID: "credential-a", KubernetesPrincipal: "system:serviceaccount:agents:shared"}
	claim := Claim{Identity: id, ResourceUID: "uid-a", ActionType: "set_live_workload_image"}
	audit := AuditObservation{ClusterIdentity: "cluster-a", ResourceUID: "uid-a", ResponseCode: 200, KubernetesPrincipal: id.KubernetesPrincipal, OriginStatus: "unverified", CollectorAuthenticationStatus: "verified", EventID: "event-a"}
	now := time.Now()
	if got := Assess(claim, nil, nil, now); got.Status != StatusInconclusive || got.ReasonCode != "MISSING_AUDIT_EVIDENCE" {
		t.Fatalf("missing audit: %+v", got)
	}
	if got := Assess(claim, &audit, nil, now); got.Status != StatusInconclusive || got.ReasonCode != "UNVERIFIED_AUDIT_ORIGIN" {
		t.Fatalf("collector transport promoted origin: %+v", got)
	}
	bad := audit
	bad.ClusterIdentity = "cluster-b"
	if got := Assess(claim, &bad, nil, now); got.Status != StatusInconclusive || got.ReasonCode != "UNVERIFIED_AUDIT_ORIGIN" {
		t.Fatalf("wrong cluster: %+v", got)
	}
	bad = audit
	bad.ResourceUID = "uid-b"
	if got := Assess(claim, &bad, nil, now); got.Status != StatusInconclusive || got.ReasonCode != "UNVERIFIED_AUDIT_ORIGIN" {
		t.Fatalf("wrong UID: %+v", got)
	}
	bad = audit
	bad.ResponseCode = 403
	if got := Assess(claim, &bad, nil, now); got.ReasonCode != "UNVERIFIED_AUDIT_ORIGIN" {
		t.Fatalf("failed mutation: %+v", got)
	}
	audit.OriginStatus = "source-authenticated"
	bad.OriginStatus = "source-authenticated"
	if got := Assess(claim, &bad, nil, now); got.ReasonCode != "MUTATION_FAILED" {
		t.Fatalf("source-authenticated failed mutation: %+v", got)
	}
	if got := Assess(claim, &audit, nil, now); got.ReasonCode != "MISSING_EXECUTION_BINDING" {
		t.Fatalf("audit principal promoted to agent: %+v", got)
	}
	record := ExecutionRecord{Identity: id, EventID: audit.EventID, Signature: []byte("forged")}
	if got := Assess(claim, &audit, &record, now); got.Status != StatusInconclusive || got.ReasonCode != "EXECUTION_RECORD_UNVERIFIED" {
		t.Fatalf("forged record accepted: %+v", got)
	}
	record.Identity.AgentVersionID = "agent-b"
	if got := Assess(claim, &audit, &record, now); got.Status != StatusInconclusive || got.ReasonCode != "EXECUTION_RECORD_UNVERIFIED" {
		t.Fatalf("other agent accepted: %+v", got)
	}
	record.Identity = id
	record.CredentialRevoked = true
	if got := Assess(claim, &audit, &record, now); got.ReasonCode != "EXECUTION_RECORD_UNVERIFIED" {
		t.Fatalf("revoked credential: %+v", got)
	}
	record.CredentialRevoked = false
	// A shared service account and a later state observation cannot turn an
	// unverified receipt into customer-agent attribution.
	if got := Assess(claim, &audit, &record, now); got.Status != StatusInconclusive {
		t.Fatalf("shared principal promoted: %+v", got)
	}
	record.Identity = id
	record.EventID = "replayed"
	if got := Assess(claim, &audit, &record, now); got.Status != StatusInconclusive || got.ReasonCode != "EXECUTION_RECORD_UNVERIFIED" {
		t.Fatalf("replayed receipt accepted: %+v", got)
	}
}
