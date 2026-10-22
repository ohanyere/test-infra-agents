package customeractionevidence

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"time"

	"github.com/evalplane/evalplane/backend/internal/kubeaudit"
)

const CustomerAuditWebhookProviderID = "kubernetes-apiserver-audit:direct-mtls-webhook:v1"

// BuildFromCustomerAuditWebhook is called only after the dedicated TLS listener
// has checked the peer certificate against a server-owned source registration.
// The event carries the Kubernetes principal, never an AI-agent identity.
func BuildFromCustomerAuditWebhook(ctx context.Context, event kubeaudit.Event, raw []byte, binding KubernetesEnvironmentBinding, sourceID, certificateFingerprint string, acquiredAt time.Time) (Evidence, error) {
	if err := validateEnvironmentBinding(binding); err != nil {
		return Evidence{}, err
	}
	kind, namespace, supported := kubeaudit.CustomerActionTarget(event)
	if sourceID == "" || certificateFingerprint == "" || !supported || event.RequestAt.IsZero() || event.StageAt.IsZero() || event.RequestAt.After(event.StageAt) || acquiredAt.Before(event.StageAt) || len(raw) == 0 {
		return Evidence{}, invalidKubernetesAudit("unsupported or incomplete customer audit event")
	}
	if !containsString(binding.Namespaces, namespace) || !containsString(binding.ResourceKinds, kind) || !kubernetesNamespace.MatchString(namespace) || !kubernetesResourceName.MatchString(event.ObjectRef.Name) {
		return Evidence{}, invalidKubernetesAudit("audit event outside registered scope")
	}
	var decoded kubeaudit.Event
	if json.Unmarshal(raw, &decoded) != nil || decoded.AuditID != event.AuditID || decoded.StageAt != event.StageAt || decoded.ObjectRef != event.ObjectRef || decoded.Verb != event.Verb {
		return Evidence{}, invalidKubernetesAudit("raw audit event differs from validated event")
	}
	digest := sha256.Sum256(raw)
	metadata := &KubernetesAuditMetadata{SourceEventDigest: "sha256:" + hex.EncodeToString(digest[:]), Verb: event.Verb, APIGroup: event.ObjectRef.APIGroup, APIVersion: event.ObjectRef.APIVersion, Resource: event.ObjectRef.Resource, AuditStage: event.Stage, ResponseCode: event.Response.Code, RequestReceivedAt: event.RequestAt.UTC(), StageAt: event.StageAt.UTC(), AcquiredAt: acquiredAt.UTC(), ClusterIdentity: binding.ClusterIdentity, SourceConfiguration: "registered-client-certificate:sha256:" + certificateFingerprint, AcquisitionLocation: "direct-audit-webhook", AcquisitionMethod: "mutual-tls-direct-webhook", VerificationState: "customer_direct_mtls_webhook", ProvisioningIdentity: sourceID, KubernetesPrincipal: event.User.Username, ProvenanceLimitations: []string{"Client-certificate exclusivity to the customer API server requires operator deployment proof.", "An audit principal does not identify an AI agent."}}
	candidate := Candidate{ContractVersion: ContractVersion, Kind: KindAuthenticatedActionRecord, SourceID: CustomerAuditWebhookProviderID, SourceIdentity: binding.ClusterIdentity, SourceEventID: event.AuditID, ActionType: "kubernetes_api_mutation", Operation: event.Verb, Target: Target{Kind: kind, Namespace: namespace, Name: event.ObjectRef.Name, UID: event.ObjectRef.UID}, OccurredAt: event.RequestAt, ObservedAt: event.StageAt, KubernetesAudit: metadata, Uncertainty: []string{"A successful API response does not establish resulting workload, exposure, or service health.", "Kubernetes principal identity alone does not establish AI-agent causality."}}
	provider := customerAuditWebhookProvider{scope: binding.Scope, sourceID: sourceID, metadataDigest: digestKubernetesAuditMetadata(metadata)}
	evidence, err := BuildFromProvider(ctx, provider, binding.Scope, candidate, acquiredAt)
	if err != nil {
		return Evidence{}, fmt.Errorf("build customer audit evidence: %w", err)
	}
	return evidence, nil
}

type customerAuditWebhookProvider struct {
	scope          Scope
	sourceID       string
	metadataDigest [sha256.Size]byte
}

func (customerAuditWebhookProvider) verifiedCustomerEvidenceProvider() {}
func (customerAuditWebhookProvider) ID() string                        { return CustomerAuditWebhookProviderID }
func (p customerAuditWebhookProvider) Verify(ctx context.Context, scope Scope, candidate Candidate) (Assessment, error) {
	if err := ctx.Err(); err != nil {
		return Assessment{}, err
	}
	if scope != p.scope || candidate.KubernetesAudit == nil || candidate.KubernetesAudit.ProvisioningIdentity != p.sourceID || digestKubernetesAuditMetadata(candidate.KubernetesAudit) != p.metadataDigest {
		return Assessment{}, invalidKubernetesAudit("customer audit source scope changed")
	}
	return Assessment{AuthenticationStatus: "verified", AuthorizationStatus: "authorized", SourceIdentityStatus: "verified", AuthenticationMethod: "direct-mutual-tls-pinned-client-certificate", CredentialScope: "registered-customer-audit-source", ProvenanceStatus: "verified", ActorStatus: "unknown", CausalityStatus: "unproven", Uncertainty: []string{"The registered audit client certificate must be confined to the customer API server.", "Source authorization permits evidence intake; it does not authorize the observed infrastructure operation."}}, nil
}
