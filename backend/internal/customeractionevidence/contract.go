package customeractionevidence

import (
	"context"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/evalplane/evalplane/backend/internal/identity"
)

var ErrInvalidEvidence = errors.New("invalid customer action evidence")

type Provider interface {
	verifiedCustomerEvidenceProvider()
	ID() string
	Verify(context.Context, Scope, Candidate) (Assessment, error)
}

func Build(scope Scope, candidate Candidate, assessment Assessment, ingestedAt time.Time) (Evidence, error) {
	if candidate.ContractVersion != ContractVersion {
		return Evidence{}, invalid("unsupported contract version")
	}
	if candidate.Kind != KindAgentDeclaredAction && candidate.Kind != KindObservedState && candidate.Kind != KindAuthenticatedActionRecord {
		return Evidence{}, invalid("unsupported evidence kind")
	}
	for _, id := range []string{scope.WorkspaceID, scope.EnvironmentID, scope.SessionID, scope.RunID} {
		if identity.ValidateUUID(id) != nil {
			return Evidence{}, invalid("server-derived workspace, environment, session, and run identifiers must be UUIDs")
		}
	}
	candidate.SourceID = strings.TrimSpace(candidate.SourceID)
	candidate.SourceIdentity = strings.TrimSpace(candidate.SourceIdentity)
	candidate.SourceEventID = strings.TrimSpace(candidate.SourceEventID)
	candidate.ActionType = strings.TrimSpace(candidate.ActionType)
	candidate.Operation = strings.TrimSpace(candidate.Operation)
	candidate.ActorAssertion = strings.TrimSpace(candidate.ActorAssertion)
	if len(candidate.SourceID) > 128 || len(candidate.SourceIdentity) > 256 || len(candidate.SourceEventID) > 256 || len(candidate.ActionType) > 100 || len(candidate.Operation) > 100 {
		return Evidence{}, invalid("source and action identifiers exceed contract limits")
	}
	if len(candidate.CorrelationIDs) > 32 || len(candidate.EvidenceRefs) > 64 || len(candidate.Uncertainty) > 32 || len(candidate.MissingFields) > 32 || len(assessment.Conflicts) > 32 || len(assessment.Uncertainty) > 32 {
		return Evidence{}, invalid("evidence metadata exceeds contract limits")
	}
	if len(candidate.Target.Kind) > 100 || len(candidate.Target.Namespace) > 63 || len(candidate.Target.Name) > 253 || len(candidate.Target.UID) > 128 {
		return Evidence{}, invalid("target identity exceeds contract limits")
	}
	if len(assessment.ActorIdentity) > 512 || len(assessment.ActorAuthority) > 256 || len(assessment.AuthenticationMethod) > 128 || len(assessment.CredentialScope) > 256 {
		return Evidence{}, invalid("provenance identity exceeds contract limits")
	}
	if ingestedAt.IsZero() {
		return Evidence{}, invalid("ingestion time must be assigned by the server")
	}

	assessment.AuthenticationStatus = normalizedStatus(assessment.AuthenticationStatus, "unknown")
	assessment.AuthorizationStatus = normalizedStatus(assessment.AuthorizationStatus, "unknown")
	assessment.SourceIdentityStatus = normalizedStatus(assessment.SourceIdentityStatus, "unknown")
	assessment.ProvenanceStatus = normalizedStatus(assessment.ProvenanceStatus, "insufficient")
	assessment.ActorStatus = normalizedStatus(assessment.ActorStatus, "unknown")
	assessment.CausalityStatus = normalizedStatus(assessment.CausalityStatus, "unproven")
	if !oneOf(assessment.AuthenticationStatus, "verified", "unverified", "failed", "unknown") ||
		!oneOf(assessment.AuthorizationStatus, "authorized", "denied", "unknown") ||
		!oneOf(assessment.SourceIdentityStatus, "verified", "asserted", "unknown", "conflicting") ||
		!oneOf(assessment.ProvenanceStatus, "verified", "insufficient", "conflicting", "failed", "unknown") ||
		!oneOf(assessment.ActorStatus, "verified", "asserted", "unknown", "conflicting") ||
		!oneOf(assessment.CausalityStatus, "qualified", "unproven", "conflicting", "not_applicable") {
		return Evidence{}, invalid("unsupported provenance assessment status")
	}
	assessment.AuthenticationMethod = strings.TrimSpace(assessment.AuthenticationMethod)
	assessment.CredentialScope = strings.TrimSpace(assessment.CredentialScope)
	assessment.ActorIdentity = strings.TrimSpace(assessment.ActorIdentity)
	assessment.ActorAuthority = strings.TrimSpace(assessment.ActorAuthority)
	assessment.Conflicts = normalizedList(assessment.Conflicts)
	assessment.Uncertainty = normalizedList(assessment.Uncertainty)
	candidate.CorrelationIDs = normalizedList(candidate.CorrelationIDs)
	candidate.EvidenceRefs = normalizedList(candidate.EvidenceRefs)
	candidate.Uncertainty = normalizedList(candidate.Uncertainty)
	candidate.MissingFields = normalizedList(candidate.MissingFields)

	actor := ActorIdentity{Identity: candidate.ActorAssertion, Status: "asserted"}
	if candidate.KubernetesAudit != nil && candidate.ActorAssertion == "" {
		actor.Status = assessment.ActorStatus
	}
	if assessment.ActorStatus == "verified" && assessment.ActorIdentity != "" {
		actor = ActorIdentity{Identity: assessment.ActorIdentity, Authority: assessment.ActorAuthority, Status: "verified"}
	}
	missing := append([]string(nil), candidate.MissingFields...)
	for field, present := range map[string]bool{
		"source_identity":  candidate.SourceIdentity != "",
		"source_event_id":  candidate.SourceEventID != "",
		"action_type":      candidate.Kind == KindObservedState || candidate.ActionType != "",
		"operation":        candidate.Kind == KindObservedState || candidate.Operation != "",
		"target.kind":      candidate.Target.Kind != "",
		"target.namespace": candidate.Target.Namespace != "",
		"target.name":      candidate.Target.Name != "",
		"occurred_at":      !candidate.OccurredAt.IsZero(),
		"observed_at":      !candidate.ObservedAt.IsZero(),
	} {
		if !present {
			missing = append(missing, field)
		}
	}
	missing = normalizedList(missing)
	uncertainty := normalizedList(append(candidate.Uncertainty, assessment.Uncertainty...))
	trust := classify(candidate, assessment, actor)
	provenance := SourceProvenance{
		ID: candidate.SourceID, Identity: candidate.SourceIdentity, EventID: candidate.SourceEventID,
		AuthenticationMethod: assessment.AuthenticationMethod, CredentialScope: assessment.CredentialScope,
		AuthenticationStatus: assessment.AuthenticationStatus, AuthorizationStatus: assessment.AuthorizationStatus,
		IdentityStatus: assessment.SourceIdentityStatus, VerificationStatus: assessment.ProvenanceStatus,
	}
	out := Evidence{
		ContractVersion: ContractVersion, Kind: candidate.Kind, Scope: scope, Source: provenance,
		ActionType: candidate.ActionType, Operation: candidate.Operation, Target: candidate.Target, Actor: actor,
		OccurredAt: candidate.OccurredAt.UTC(), ObservedAt: candidate.ObservedAt.UTC(), IngestedAt: ingestedAt.UTC(),
		CorrelationIDs: candidate.CorrelationIDs, EvidenceRefs: candidate.EvidenceRefs,
		ProvenanceStatus: assessment.ProvenanceStatus, CausalityStatus: assessment.CausalityStatus,
		ProvenanceConflicts: assessment.Conflicts, Trust: trust, Uncertainty: uncertainty, MissingFields: missing,
		KubernetesAudit: cloneKubernetesAuditMetadata(candidate.KubernetesAudit),
	}
	out.Integrity = Integrity{Algorithm: "sha256", Digest: Digest(out)}
	return out, nil
}

func cloneKubernetesAuditMetadata(metadata *KubernetesAuditMetadata) *KubernetesAuditMetadata {
	if metadata == nil {
		return nil
	}
	copy := *metadata
	copy.ProvenanceLimitations = append([]string(nil), metadata.ProvenanceLimitations...)
	return &copy
}

// BuildFromProvider seals evidence from trusted server code. Customer Store
// separately requires verified source authentication and authorization; runner
// test evidence uses its own restricted repository path.
func BuildFromProvider(ctx context.Context, provider Provider, scope Scope, candidate Candidate, ingestedAt time.Time) (Evidence, error) {
	if provider == nil || strings.TrimSpace(provider.ID()) == "" || strings.TrimSpace(candidate.SourceID) != provider.ID() {
		return Evidence{}, invalid("candidate source does not match its trusted provider")
	}
	assessment, err := provider.Verify(ctx, scope, candidate)
	if err != nil {
		return Evidence{}, fmt.Errorf("verify customer action evidence source: %w", err)
	}
	evidence, err := Build(scope, candidate, assessment, ingestedAt)
	if err != nil {
		return Evidence{}, err
	}
	evidence.providerVerified = true
	evidence.providerSeal = providerEvidenceDigest(evidence)
	return evidence, nil
}

func providerEvidenceDigest(e Evidence) [sha256.Size]byte {
	encoded, _ := json.Marshal(e)
	return sha256.Sum256(encoded)
}

func hasValidProviderSeal(e Evidence) bool {
	if !e.providerVerified {
		return false
	}
	digest := providerEvidenceDigest(e)
	return subtle.ConstantTimeCompare(e.providerSeal[:], digest[:]) == 1
}

func Validate(e Evidence) error {
	if e.ContractVersion != ContractVersion || e.Integrity.Algorithm != "sha256" || e.Integrity.Digest == "" || e.Integrity.Digest != Digest(e) {
		return invalid("contract or payload digest is invalid")
	}
	if e.Trust != classifyEvidence(e) {
		return invalid("trust classification does not match server-verified provenance")
	}
	return nil
}

func Digest(e Evidence) string {
	e.IngestedAt = time.Time{}
	e.Integrity = Integrity{}
	encoded, _ := json.Marshal(e)
	digest := sha256.Sum256(encoded)
	return "sha256:" + hex.EncodeToString(digest[:])
}

func classify(candidate Candidate, assessment Assessment, actor ActorIdentity) TrustClassification {
	if candidate.Kind == KindAgentDeclaredAction {
		return TrustUnverifiedAgentClaim
	}
	if assessment.ProvenanceStatus == "conflicting" || len(assessment.Conflicts) > 0 || assessment.ActorStatus == "conflicting" || assessment.CausalityStatus == "conflicting" {
		return TrustConflictingProvenance
	}
	if candidate.SourceID == "" || candidate.SourceIdentity == "" || assessment.SourceIdentityStatus != "verified" || assessment.AuthenticationStatus != "verified" || assessment.AuthorizationStatus != "authorized" || assessment.ProvenanceStatus != "verified" {
		return TrustInsufficientProvenance
	}
	if candidate.Kind == KindObservedState {
		return TrustIndependentlyObservedState
	}
	if candidate.Kind == KindAuthenticatedActionRecord {
		if actor.Status == "verified" && assessment.CausalityStatus == "qualified" {
			return TrustAttributionQualified
		}
		return TrustAuthenticatedActionRecord
	}
	return TrustInsufficientProvenance
}

func classifyEvidence(e Evidence) TrustClassification {
	candidate := Candidate{Kind: e.Kind, SourceID: e.Source.ID, SourceIdentity: e.Source.Identity}
	assessment := Assessment{AuthenticationStatus: e.Source.AuthenticationStatus, AuthorizationStatus: e.Source.AuthorizationStatus, SourceIdentityStatus: e.Source.IdentityStatus, ProvenanceStatus: e.ProvenanceStatus, ActorStatus: e.Actor.Status, CausalityStatus: e.CausalityStatus, Conflicts: e.ProvenanceConflicts}
	if e.Trust == TrustUnverifiedAgentClaim {
		candidate.Kind = KindAgentDeclaredAction
	}
	return classify(candidate, assessment, e.Actor)
}

func normalizedStatus(value, fallback string) string {
	value = strings.ToLower(strings.TrimSpace(value))
	if value == "" {
		return fallback
	}
	return value
}

func oneOf(value string, allowed ...string) bool {
	for _, candidate := range allowed {
		if value == candidate {
			return true
		}
	}
	return false
}

func normalizedList(values []string) []string {
	seen := make(map[string]struct{}, len(values))
	out := make([]string, 0, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" {
			continue
		}
		if _, ok := seen[value]; ok {
			continue
		}
		seen[value] = struct{}{}
		out = append(out, value)
	}
	sort.Strings(out)
	return out
}

func invalid(message string) error {
	return fmt.Errorf("%w: %s", ErrInvalidEvidence, message)
}
