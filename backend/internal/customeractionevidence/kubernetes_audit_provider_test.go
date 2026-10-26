package customeractionevidence

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"testing"
	"time"

	"github.com/evalplane/evalplane/backend/internal/kubeaudit"
)

func TestBuildFromValidatedKubernetesSnapshotPreservesSourceWithoutAttribution(t *testing.T) {
	event, source, binding := validKubernetesAuditFixture()
	evidence, err := buildFromValidatedKubernetesSnapshot(context.Background(), event, source, binding, time.Now().UTC())
	if err != nil {
		t.Fatal(err)
	}
	if evidence.Kind != KindAuthenticatedActionRecord || evidence.Trust != TrustInsufficientProvenance {
		t.Fatalf("unexpected kind/trust: %s/%s", evidence.Kind, evidence.Trust)
	}
	if evidence.Source.EventID != event.AuditID || evidence.Source.Identity != source.ClusterIdentity || evidence.Operation != "patch" || evidence.Target.UID != event.ObjectRef.UID {
		t.Fatalf("normalized evidence lost source identity: %+v", evidence)
	}
	metadata := evidence.KubernetesAudit
	if metadata == nil || metadata.KubernetesPrincipal != event.User.Username || metadata.VerificationState != "runner_bound_test_source" || metadata.ResponseCode != 200 || metadata.AuditStage != "ResponseComplete" || metadata.AcquiredAt != source.AcquiredAt.UTC() {
		t.Fatalf("source provenance not preserved: %+v", metadata)
	}
	if metadata.RequestReceivedAt != event.RequestAt || metadata.StageAt != event.StageAt || metadata.ClusterIdentity != source.ClusterIdentity || metadata.AcquisitionMethod != source.AcquisitionMethod || metadata.AuditPolicyDigest == "" || len(metadata.ProvenanceLimitations) != len(source.ProvenanceLimitations) {
		t.Fatalf("audit timestamps or source metadata lost: %+v", metadata)
	}
	if evidence.Source.AuthenticationStatus != "unknown" || evidence.Source.AuthorizationStatus != "unknown" || evidence.Source.IdentityStatus != "asserted" || evidence.Source.VerificationStatus != "insufficient" {
		t.Fatalf("runner-bound test source was misrepresented: %+v", evidence.Source)
	}
	if evidence.Actor.Status != "unknown" || evidence.Actor.Identity != "" || evidence.CausalityStatus != "unproven" {
		t.Fatalf("audit principal was promoted to agent attribution: actor=%+v causality=%s", evidence.Actor, evidence.CausalityStatus)
	}
	if err := Validate(evidence); err != nil {
		t.Fatalf("valid provider evidence failed contract validation: %v", err)
	}
	if _, err := Store(context.Background(), nil, evidence); errors.Is(err, ErrUntrustedInput) {
		t.Fatal("unchanged evidence failed provider authority check")
	}
}

func TestBuildFromValidatedKubernetesSnapshotRejectsInvalidBindingsAndEvidence(t *testing.T) {
	tests := []struct {
		name   string
		change func(*kubeaudit.Event, *kubeaudit.SourceIdentity, *KubernetesEnvironmentBinding)
	}{
		{"source cluster mismatch", func(_ *kubeaudit.Event, source *kubeaudit.SourceIdentity, _ *KubernetesEnvironmentBinding) {
			source.ClusterIdentity = "kind:other"
		}},
		{"environment cluster mismatch", func(_ *kubeaudit.Event, _ *kubeaudit.SourceIdentity, binding *KubernetesEnvironmentBinding) {
			binding.ClusterIdentity = "kind:other"
		}},
		{"wrong namespace", func(event *kubeaudit.Event, _ *kubeaudit.SourceIdentity, _ *KubernetesEnvironmentBinding) {
			event.ObjectRef.Namespace = "outside"
		}},
		{"resource kind outside environment", func(_ *kubeaudit.Event, _ *kubeaudit.SourceIdentity, binding *KubernetesEnvironmentBinding) {
			binding.ResourceKinds = []string{"Pod"}
		}},
		{"unsupported kind", func(event *kubeaudit.Event, _ *kubeaudit.SourceIdentity, _ *KubernetesEnvironmentBinding) {
			event.ObjectRef.Resource = "pods"
		}},
		{"unsupported operation", func(event *kubeaudit.Event, _ *kubeaudit.SourceIdentity, _ *KubernetesEnvironmentBinding) {
			event.Verb = "delete"
		}},
		{"failed response", func(event *kubeaudit.Event, _ *kubeaudit.SourceIdentity, _ *KubernetesEnvironmentBinding) {
			event.Response.Code = 403
		}},
		{"non-final stage", func(event *kubeaudit.Event, _ *kubeaudit.SourceIdentity, _ *KubernetesEnvironmentBinding) {
			event.Stage = "RequestReceived"
		}},
		{"missing event ID", func(event *kubeaudit.Event, _ *kubeaudit.SourceIdentity, _ *KubernetesEnvironmentBinding) {
			event.AuditID = " "
		}},
		{"missing timestamps", func(event *kubeaudit.Event, _ *kubeaudit.SourceIdentity, _ *KubernetesEnvironmentBinding) {
			event.StageAt = time.Time{}
		}},
		{"invalid verification state", func(_ *kubeaudit.Event, source *kubeaudit.SourceIdentity, _ *KubernetesEnvironmentBinding) {
			source.VerificationState = "verified"
		}},
		{"missing source identity", func(_ *kubeaudit.Event, source *kubeaudit.SourceIdentity, _ *KubernetesEnvironmentBinding) {
			source.ClusterIdentity = ""
		}},
		{"missing acquisition provenance", func(_ *kubeaudit.Event, source *kubeaudit.SourceIdentity, _ *KubernetesEnvironmentBinding) {
			source.AcquisitionMethod = ""
		}},
		{"invalid policy digest", func(_ *kubeaudit.Event, source *kubeaudit.SourceIdentity, _ *KubernetesEnvironmentBinding) {
			source.ConfigurationIdentity = "forged"
		}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			event, source, binding := validKubernetesAuditFixture()
			test.change(&event, &source, &binding)
			if _, err := buildFromValidatedKubernetesSnapshot(context.Background(), event, source, binding, time.Now().UTC()); !errors.Is(err, ErrInvalidKubernetesAuditEvidence) {
				t.Fatalf("invalid evidence accepted: %v", err)
			}
		})
	}
}

func TestMissingKubernetesPrincipalRemainsUnknown(t *testing.T) {
	event, source, binding := validKubernetesAuditFixture()
	event.User.Username = ""
	evidence, err := buildFromValidatedKubernetesSnapshot(context.Background(), event, source, binding, time.Now().UTC())
	if err != nil {
		t.Fatal(err)
	}
	if evidence.KubernetesAudit.KubernetesPrincipal != "" || evidence.Actor.Status != "unknown" || evidence.Actor.Identity != "" || !containsString(evidence.MissingFields, "kubernetes_principal") {
		t.Fatalf("missing principal was fabricated: %+v", evidence)
	}
}

func TestAdapterEvidenceMutationCannotRestoreProviderAuthority(t *testing.T) {
	event, source, binding := validKubernetesAuditFixture()
	evidence, err := buildFromValidatedKubernetesSnapshot(context.Background(), event, source, binding, time.Now().UTC())
	if err != nil {
		t.Fatal(err)
	}
	evidence.KubernetesAudit.ProvenanceLimitations[0] = "changed"
	evidence.Trust = TrustAttributionQualified
	evidence.Integrity.Digest = Digest(evidence)
	if _, err := Store(context.Background(), nil, evidence); !errors.Is(err, ErrUntrustedInput) {
		t.Fatalf("mutated evidence retained provider authority: %v", err)
	}
}

func TestPublicAdapterRejectsFabricatedAcquisition(t *testing.T) {
	_, _, binding := validKubernetesAuditFixture()
	if _, err := BuildFromKubernetesAudit(context.Background(), kubeaudit.AcquiredEvent{}, binding, time.Now().UTC()); !errors.Is(err, ErrInvalidKubernetesAuditEvidence) {
		t.Fatalf("fabricated acquisition accepted: %v", err)
	}
}

func validKubernetesAuditFixture() (kubeaudit.Event, kubeaudit.SourceIdentity, KubernetesEnvironmentBinding) {
	requestAt := time.Date(2026, 10, 18, 10, 0, 0, 0, time.UTC)
	policy := sha256.Sum256([]byte("test policy identity"))
	event := kubeaudit.Event{
		APIVersion: kubeaudit.AuditAPIVersion, Kind: "Event", AuditID: "audit-event-1", Stage: "ResponseComplete", Verb: "patch",
		RequestAt: requestAt, StageAt: requestAt.Add(time.Second), User: kubeaudit.User{Username: "system:admin"},
		ObjectRef: kubeaudit.ObjectRef{APIGroup: "apps", APIVersion: "v1", Resource: "deployments", Namespace: "proof", Name: "web", UID: "deployment-uid"},
		Response:  kubeaudit.Response{Code: 200, Reason: "OK"},
	}
	source := kubeaudit.SourceIdentity{
		SourceType: "kubernetes-apiserver-audit", ClusterIdentity: "kind:evalplane-e2e",
		ConfigurationIdentity: "kind-audit-policy:sha256:" + hex.EncodeToString(policy[:]),
		AcquisitionLocation:   "evalplane-e2e-control-plane:/var/log/kubernetes/audit/audit.log",
		AcquisitionMethod:     "runner-docker-copy-from-kind-control-plane", VerificationState: "runner_bound_test_source",
		ProvisioningIdentity: "github-actions:12345:kind-create-cluster:evalplane-e2e", AcquiredAt: requestAt.Add(2 * time.Second),
		ProvenanceLimitations: []string{"runner controls disposable Kind cluster", "not customer source authentication"},
	}
	binding := KubernetesEnvironmentBinding{
		Scope:           Scope{WorkspaceID: "00000000-0000-4000-8000-000000000001", EnvironmentID: "00000000-0000-4000-8000-000000000002", SessionID: "00000000-0000-4000-8000-000000000003", RunID: "00000000-0000-4000-8000-000000000004"},
		ClusterIdentity: "kind:evalplane-e2e", Namespaces: []string{"proof"}, ResourceKinds: []string{"Deployment"},
	}
	return event, source, binding
}
