package customeractionevidence

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"strings"
	"time"

	"github.com/evalplane/evalplane/backend/internal/identity"
	"github.com/evalplane/evalplane/backend/internal/kubeaudit"
)

const KubernetesAuditProviderID = "kubernetes-apiserver-audit:runner-bound-kind:v1"

var ErrInvalidKubernetesAuditEvidence = errors.New("invalid Kubernetes audit evidence")
var kubernetesNamespace = regexp.MustCompile(`^[a-z0-9]([-a-z0-9]{0,61}[a-z0-9])?$`)
var kubernetesResourceName = regexp.MustCompile(`^[A-Za-z0-9]([A-Za-z0-9._-]{0,251}[A-Za-z0-9])?$`)

// KubernetesEnvironmentBinding must be populated from server-owned registration
// and session records. The adapter validates its values; the repository resolves
// authority before this adapter is called.
type KubernetesEnvironmentBinding struct {
	Scope           Scope
	ClusterIdentity string
	Namespaces      []string
	ResourceKinds   []string
}

// BuildFromKubernetesAudit converts only sealed runner-bound Kind acquisitions.
// Runner binding attests only to the disposable CI acquisition path; it does
// not attest customer-source authentication, customer authorization, agent
// identity, causation, or workload outcome.
func BuildFromKubernetesAudit(ctx context.Context, acquired kubeaudit.AcquiredEvent, binding KubernetesEnvironmentBinding, ingestedAt time.Time) (Evidence, error) {
	event, source, ok := acquired.Snapshot()
	if !ok || !acquired.RunnerBound() {
		return Evidence{}, invalidKubernetesAudit("acquisition is not sealed and runner-bound")
	}
	if ctx == nil {
		ctx = context.Background()
	}
	return buildFromValidatedKubernetesSnapshot(ctx, event, source, binding, ingestedAt)
}

func buildFromValidatedKubernetesSnapshot(ctx context.Context, event kubeaudit.Event, source kubeaudit.SourceIdentity, binding KubernetesEnvironmentBinding, ingestedAt time.Time) (Evidence, error) {
	if err := validateEnvironmentBinding(binding); err != nil {
		return Evidence{}, err
	}
	if source.SourceType != "kubernetes-apiserver-audit" || source.VerificationState != "runner_bound_test_source" || source.ClusterIdentity == "" || source.ClusterIdentity != binding.ClusterIdentity || source.AcquisitionMethod != "runner-docker-copy-from-kind-control-plane" || source.AcquisitionLocation != "evalplane-e2e-control-plane:/var/log/kubernetes/audit/audit.log" || !strings.HasPrefix(source.ProvisioningIdentity, "github-actions:") {
		return Evidence{}, invalidKubernetesAudit("source identity does not match the bound environment")
	}
	if source.ConfigurationIdentity == "" || source.AcquiredAt.IsZero() || len(source.ProvenanceLimitations) == 0 {
		return Evidence{}, invalidKubernetesAudit("acquisition provenance is incomplete")
	}
	policyDigest, ok := policyDigestFromConfiguration(source.ConfigurationIdentity)
	if !ok {
		return Evidence{}, invalidKubernetesAudit("source audit policy identity is invalid")
	}
	if event.APIVersion != kubeaudit.AuditAPIVersion || event.Kind != "Event" || strings.TrimSpace(event.AuditID) == "" || event.AuditID != strings.TrimSpace(event.AuditID) {
		return Evidence{}, invalidKubernetesAudit("audit event identity is incomplete or unsupported")
	}
	if event.Stage != "ResponseComplete" || event.Verb != "patch" && event.Verb != "update" {
		return Evidence{}, invalidKubernetesAudit("audit stage or operation is unsupported")
	}
	if event.ObjectRef.APIGroup != "apps" || event.ObjectRef.APIVersion != "v1" || event.ObjectRef.Resource != "deployments" || !kubernetesNamespace.MatchString(event.ObjectRef.Namespace) || !kubernetesResourceName.MatchString(event.ObjectRef.Name) {
		return Evidence{}, invalidKubernetesAudit("audit target is unsupported or incomplete")
	}
	if !containsString(binding.Namespaces, event.ObjectRef.Namespace) || !containsString(binding.ResourceKinds, "Deployment") {
		return Evidence{}, invalidKubernetesAudit("audit target is outside the registered environment scope")
	}
	if event.RequestAt.IsZero() || event.StageAt.IsZero() || event.RequestAt.After(event.StageAt) || source.AcquiredAt.Before(event.StageAt) {
		return Evidence{}, invalidKubernetesAudit("audit event timestamps are invalid")
	}
	if event.Response.Code < 200 || event.Response.Code >= 300 {
		return Evidence{}, invalidKubernetesAudit("Kubernetes API operation did not succeed")
	}
	if ingestedAt.IsZero() {
		return Evidence{}, invalidKubernetesAudit("ingestion timestamp is required")
	}
	encodedEvent, err := json.Marshal(event)
	if err != nil {
		return Evidence{}, fmt.Errorf("serialize validated Kubernetes audit event: %w", err)
	}
	eventDigest := sha256.Sum256(encodedEvent)
	metadata := &KubernetesAuditMetadata{
		SourceEventDigest:     "sha256:" + hex.EncodeToString(eventDigest[:]),
		Verb:                  event.Verb,
		APIGroup:              event.ObjectRef.APIGroup,
		APIVersion:            event.ObjectRef.APIVersion,
		Resource:              event.ObjectRef.Resource,
		AuditStage:            event.Stage,
		ResponseCode:          event.Response.Code,
		RequestReceivedAt:     event.RequestAt.UTC(),
		StageAt:               event.StageAt.UTC(),
		AcquiredAt:            source.AcquiredAt.UTC(),
		ClusterIdentity:       source.ClusterIdentity,
		AuditPolicyDigest:     policyDigest,
		SourceConfiguration:   source.ConfigurationIdentity,
		AcquisitionLocation:   source.AcquisitionLocation,
		AcquisitionMethod:     source.AcquisitionMethod,
		VerificationState:     source.VerificationState,
		ProvisioningIdentity:  source.ProvisioningIdentity,
		KubernetesPrincipal:   event.User.Username,
		ProvenanceLimitations: append([]string(nil), source.ProvenanceLimitations...),
	}
	uncertainty := []string{
		"runner_bound_test_source is not independently authenticated customer-cluster evidence",
		"a successful Kubernetes API response does not establish workload health or recovery",
		"the Kubernetes API principal does not identify a specific AI agent or establish causation",
	}
	missing := []string(nil)
	if event.User.Username == "" {
		missing = append(missing, "kubernetes_principal")
		uncertainty = append(uncertainty, "the audit record contains no Kubernetes principal")
	}
	candidate := Candidate{
		ContractVersion: ContractVersion,
		Kind:            KindAuthenticatedActionRecord,
		SourceID:        KubernetesAuditProviderID,
		SourceIdentity:  source.ClusterIdentity,
		SourceEventID:   event.AuditID,
		ActionType:      "kubernetes_api_mutation",
		Operation:       event.Verb,
		Target:          Target{Kind: "Deployment", Namespace: event.ObjectRef.Namespace, Name: event.ObjectRef.Name, UID: event.ObjectRef.UID},
		OccurredAt:      event.StageAt,
		ObservedAt:      source.AcquiredAt,
		Uncertainty:     uncertainty,
		MissingFields:   missing,
		KubernetesAudit: metadata,
	}
	provider := runnerBoundKubernetesAuditProvider{scope: binding.Scope, metadataDigest: digestKubernetesAuditMetadata(metadata)}
	return BuildFromProvider(ctx, provider, binding.Scope, candidate, ingestedAt)
}

type runnerBoundKubernetesAuditProvider struct {
	scope          Scope
	metadataDigest [sha256.Size]byte
}

func (runnerBoundKubernetesAuditProvider) verifiedCustomerEvidenceProvider() {}
func (runnerBoundKubernetesAuditProvider) ID() string                        { return KubernetesAuditProviderID }

func (p runnerBoundKubernetesAuditProvider) Verify(ctx context.Context, scope Scope, candidate Candidate) (Assessment, error) {
	if err := ctx.Err(); err != nil {
		return Assessment{}, err
	}
	if scope != p.scope || candidate.KubernetesAudit == nil || digestKubernetesAuditMetadata(candidate.KubernetesAudit) != p.metadataDigest {
		return Assessment{}, invalidKubernetesAudit("provider candidate does not match the validated acquisition")
	}
	return Assessment{
		AuthenticationStatus: "unknown",
		AuthorizationStatus:  "unknown",
		SourceIdentityStatus: "asserted",
		AuthenticationMethod: "runner-bound-test-acquisition",
		CredentialScope:      "runner-binding:disposable-kind-cluster",
		ProvenanceStatus:     "insufficient",
		ActorStatus:          "unknown",
		CausalityStatus:      "unproven",
		Uncertainty:          []string{"runner binding does not authenticate an independently operated customer cluster"},
	}, nil
}

func validateEnvironmentBinding(binding KubernetesEnvironmentBinding) error {
	if strings.TrimSpace(binding.ClusterIdentity) == "" || len(binding.Namespaces) == 0 || len(binding.ResourceKinds) == 0 {
		return invalidKubernetesAudit("environment binding is incomplete")
	}
	for _, namespace := range binding.Namespaces {
		if !kubernetesNamespace.MatchString(namespace) {
			return invalidKubernetesAudit("environment namespace binding is invalid")
		}
	}
	for _, kind := range binding.ResourceKinds {
		if strings.TrimSpace(kind) == "" || kind != strings.TrimSpace(kind) {
			return invalidKubernetesAudit("environment resource-kind binding is invalid")
		}
	}
	if identity.ValidateUUID(binding.Scope.WorkspaceID) != nil || identity.ValidateUUID(binding.Scope.EnvironmentID) != nil || identity.ValidateUUID(binding.Scope.SessionID) != nil || identity.ValidateUUID(binding.Scope.RunID) != nil {
		return invalidKubernetesAudit("environment scope identifiers are invalid")
	}
	return nil
}

func policyDigestFromConfiguration(configuration string) (string, bool) {
	const prefix = "kind-audit-policy:sha256:"
	if !strings.HasPrefix(configuration, prefix) {
		return "", false
	}
	digest := strings.TrimPrefix(configuration, prefix)
	decoded, err := hex.DecodeString(digest)
	return digest, err == nil && len(decoded) == sha256.Size && strings.ToLower(digest) == digest
}

func digestKubernetesAuditMetadata(metadata *KubernetesAuditMetadata) [sha256.Size]byte {
	encoded, _ := json.Marshal(metadata)
	return sha256.Sum256(encoded)
}

func containsString(values []string, target string) bool {
	for _, value := range values {
		if value == target {
			return true
		}
	}
	return false
}

func invalidKubernetesAudit(message string) error {
	return fmt.Errorf("%w: %s", ErrInvalidKubernetesAuditEvidence, message)
}
