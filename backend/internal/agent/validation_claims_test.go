package agent

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/evalplane/evalplane/backend/internal/competency"
)

func validVersionInput() VersionInput {
	return VersionInput{
		VersionLabel:           "v1",
		InterfaceKind:          "http",
		EndpointURL:            "https://agent.example/v1/decide",
		Capabilities:           []string{"inspect", "kubernetes_workloads", "kubernetes_pods"},
		ImplementationIdentity: declaredIdentity(),
		Metadata:               json.RawMessage(`{}`),
	}
}

func TestVersionClaimsAccepted(t *testing.T) {
	in := validVersionInput()
	in.ClaimedCapabilityIDs = []string{"kubernetes.workload.diagnosis"}
	got, err := ValidateVersion(in)
	if err != nil {
		t.Fatalf("validate=%v", err)
	}
	if len(got.ClaimedCapabilityIDs) != 1 || got.ClaimedCapabilityIDs[0] != "kubernetes.workload.diagnosis" {
		t.Fatalf("claims=%v", got.ClaimedCapabilityIDs)
	}
}

func TestVersionUnknownClaimRejected(t *testing.T) {
	in := validVersionInput()
	in.ClaimedCapabilityIDs = []string{"unknown.capability"}
	if _, err := ValidateVersion(in); err == nil {
		t.Fatal("expected unknown claim rejection")
	}
}

func TestVersionDuplicateClaimRejected(t *testing.T) {
	in := validVersionInput()
	in.ClaimedCapabilityIDs = []string{"gitops.drift.remediation", "gitops.drift.remediation"}
	if _, err := ValidateVersion(in); err == nil {
		t.Fatal("expected duplicate claim rejection")
	}
}

func TestVersionIncompatibleClaimRejected(t *testing.T) {
	in := validVersionInput()
	// workload.remediation needs restart_pod, which is not declared above.
	in.ClaimedCapabilityIDs = []string{"kubernetes.workload.remediation"}
	_, err := ValidateVersion(in)
	if err == nil {
		t.Fatal("expected incompatibility rejection")
	}
	if !strings.Contains(err.Error(), "restart_pod") {
		t.Fatalf("error must name missing runtime capability, got %v", err)
	}
}

func TestVersionCapacityRemediationCompatibility(t *testing.T) {
	in := validVersionInput()
	in.Capabilities = []string{"inspect", "kubernetes_workloads", "request_rate", "scale_workload"}
	in.ClaimedCapabilityIDs = []string{"kubernetes.capacity.remediation"}
	if _, err := ValidateVersion(in); err != nil {
		t.Fatalf("compatible claims must pass, got %v", err)
	}
	bad := validVersionInput()
	bad.ClaimedCapabilityIDs = []string{"kubernetes.capacity.remediation"}
	if _, err := ValidateVersion(bad); err == nil {
		t.Fatal("expected capacity remediation without scale to fail")
	}
}

func TestVersionWithoutClaimsStillLoads(t *testing.T) {
	got, err := ValidateVersion(validVersionInput())
	if err != nil {
		t.Fatal(err)
	}
	if len(got.ClaimedCapabilityIDs) != 0 {
		t.Fatalf("old versions have zero claims, got %v", got.ClaimedCapabilityIDs)
	}
}

func TestVersionClaimCatalogVersionAndStatus(t *testing.T) {
	if competency.ContractVersion != "infrastructure-capability-catalog:v1" {
		t.Fatalf("contract=%q", competency.ContractVersion)
	}
	if competency.ClaimStatusClaimed != "claimed" {
		t.Fatalf("claim status=%q", competency.ClaimStatusClaimed)
	}
	// Version JSON must expose claims distinctly from tool permissions and
	// must never serialize a qualified=true flag.
	in := validVersionInput()
	in.ClaimedCapabilityIDs = []string{"kubernetes.workload.diagnosis"}
	got, err := ValidateVersion(in)
	if err != nil {
		t.Fatal(err)
	}
	_ = got
	var v Version
	v.ClaimedCapabilities = []string{"kubernetes.workload.diagnosis"}
	v.CapabilityCatalogVersion = competency.ContractVersion
	v.ClaimStatus = competency.ClaimStatusClaimed
	raw, _ := json.Marshal(v)
	var decoded map[string]any
	if err := json.Unmarshal(raw, &decoded); err != nil {
		t.Fatal(err)
	}
	if _, ok := decoded["qualified"]; ok {
		t.Fatal("version must not serialize qualified")
	}
	if decoded["claim_status"] != "claimed" {
		t.Fatalf("claim_status=%v", decoded["claim_status"])
	}
}
