package customeractionevidence

import (
	"context"
	"testing"
	"time"
)

type verifiedTestProvider struct{ assessment Assessment }

func (verifiedTestProvider) verifiedCustomerEvidenceProvider() {}
func (p verifiedTestProvider) ID() string                      { return "audit" }
func (p verifiedTestProvider) Verify(context.Context, Scope, Candidate) (Assessment, error) {
	return p.assessment, nil
}

func TestTrustClassificationIsDerivedFromProvenance(t *testing.T) {
	scope := Scope{WorkspaceID: "00000000-0000-4000-8000-000000000001", EnvironmentID: "00000000-0000-4000-8000-000000000002", SessionID: "00000000-0000-4000-8000-000000000003", RunID: "00000000-0000-4000-8000-000000000004"}
	candidate := Candidate{ContractVersion: ContractVersion, Kind: KindAuthenticatedActionRecord, SourceID: "audit", SourceIdentity: "cluster-a", SourceEventID: "event-1", ActionType: "deployment", Operation: "update", Target: Target{Kind: "Deployment", Namespace: "app", Name: "web"}, ActorAssertion: "forged-user", OccurredAt: time.Now().UTC(), ObservedAt: time.Now().UTC()}
	assessment := Assessment{AuthenticationStatus: "verified", AuthorizationStatus: "authorized", SourceIdentityStatus: "verified", ProvenanceStatus: "verified", AuthenticationMethod: "mTLS", CredentialScope: "environment", ActorStatus: "unknown", CausalityStatus: "unproven"}
	evidence, err := Build(scope, candidate, assessment, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	if evidence.Trust != TrustAuthenticatedActionRecord || evidence.Actor.Identity != "forged-user" || evidence.Actor.Status != "asserted" {
		t.Fatalf("unexpected evidence assessment: %+v", evidence)
	}
	if err := Validate(evidence); err != nil {
		t.Fatal(err)
	}
	if got := Digest(evidence); got != evidence.Integrity.Digest {
		t.Fatalf("digest changed: %s", got)
	}

	assessment.ActorIdentity, assessment.ActorStatus, assessment.CausalityStatus = "verified-user", "verified", "qualified"
	qualified, err := Build(scope, candidate, assessment, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	if qualified.Trust != TrustAttributionQualified || qualified.Actor.Identity != "verified-user" {
		t.Fatalf("qualified evidence=%+v", qualified)
	}
}

func TestClaimsAndStateObservationsCannotProveAttribution(t *testing.T) {
	scope := Scope{WorkspaceID: "00000000-0000-4000-8000-000000000001", EnvironmentID: "00000000-0000-4000-8000-000000000002", SessionID: "00000000-0000-4000-8000-000000000003", RunID: "00000000-0000-4000-8000-000000000004"}
	base := Candidate{ContractVersion: ContractVersion, SourceID: "source", SourceIdentity: "cluster", SourceEventID: "event", ActionType: "deployment", Operation: "update", Target: Target{Kind: "Deployment", Namespace: "app", Name: "web"}, OccurredAt: time.Now(), ObservedAt: time.Now()}
	assessment := Assessment{AuthenticationStatus: "verified", AuthorizationStatus: "authorized", SourceIdentityStatus: "verified", ProvenanceStatus: "verified", ActorStatus: "verified", ActorIdentity: "actor", CausalityStatus: "qualified"}
	base.Kind = KindAgentDeclaredAction
	claim, err := Build(scope, base, assessment, time.Now())
	if err != nil || claim.Trust != TrustUnverifiedAgentClaim {
		t.Fatalf("claim=%+v err=%v", claim, err)
	}
	base.Kind = KindObservedState
	state, err := Build(scope, base, assessment, time.Now())
	if err != nil || state.Trust != TrustIndependentlyObservedState {
		t.Fatalf("state=%+v err=%v", state, err)
	}
	assessment.SourceIdentityStatus = "unknown"
	insufficient, err := Build(scope, base, assessment, time.Now())
	if err != nil || insufficient.Trust != TrustInsufficientProvenance {
		t.Fatalf("insufficient=%+v err=%v", insufficient, err)
	}
}

func TestUnsupportedProvenanceAndTamperingAreRejected(t *testing.T) {
	scope := Scope{WorkspaceID: "00000000-0000-4000-8000-000000000001", EnvironmentID: "00000000-0000-4000-8000-000000000002", SessionID: "00000000-0000-4000-8000-000000000003", RunID: "00000000-0000-4000-8000-000000000004"}
	candidate := Candidate{ContractVersion: ContractVersion, Kind: KindObservedState, SourceID: "source", SourceIdentity: "cluster", SourceEventID: "event"}
	if _, err := Build(scope, candidate, Assessment{AuthenticationStatus: "client_says_verified"}, time.Now()); err == nil {
		t.Fatal("unsupported provenance status accepted")
	}
	evidence, err := Build(scope, candidate, Assessment{}, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	evidence.Trust = TrustAttributionQualified
	if err := Validate(evidence); err == nil {
		t.Fatal("tampered trust classification accepted")
	}
}

func TestCallerBuiltAssessmentCannotReachPersistence(t *testing.T) {
	scope := Scope{WorkspaceID: "00000000-0000-4000-8000-000000000001", EnvironmentID: "00000000-0000-4000-8000-000000000002", SessionID: "00000000-0000-4000-8000-000000000003", RunID: "00000000-0000-4000-8000-000000000004"}
	candidate := Candidate{ContractVersion: ContractVersion, Kind: KindAuthenticatedActionRecord, SourceID: "audit", SourceIdentity: "cluster", SourceEventID: "event"}
	forgedAssessment := Assessment{AuthenticationStatus: "verified", AuthorizationStatus: "authorized", SourceIdentityStatus: "verified", ProvenanceStatus: "verified", AuthenticationMethod: "forged", ActorStatus: "verified", ActorIdentity: "forged actor", CausalityStatus: "qualified"}
	forged, err := Build(scope, candidate, forgedAssessment, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := Store(context.Background(), nil, forged); err != ErrUntrustedInput {
		t.Fatalf("caller-built assessment persistence error=%v, want untrusted input", err)
	}
	verified, err := BuildFromProvider(context.Background(), verifiedTestProvider{assessment: forgedAssessment}, scope, candidate, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	if !verified.providerVerified || verified.Trust != TrustAttributionQualified {
		t.Fatalf("provider-verified assessment was not sealed: %+v", verified)
	}
	if _, err := Store(context.Background(), nil, verified); err == ErrUntrustedInput {
		t.Fatal("unchanged provider evidence lost its authority")
	}
	if _, err := BuildFromProvider(context.Background(), verifiedTestProvider{assessment: forgedAssessment}, scope, Candidate{ContractVersion: ContractVersion, Kind: candidate.Kind, SourceID: "another-provider"}, time.Now()); err == nil {
		t.Fatal("evidence from a different source was accepted by provider")
	}
}

func TestProviderSealRejectsMutationsEvenAfterPublicDigestRecalculation(t *testing.T) {
	scope := Scope{WorkspaceID: "00000000-0000-4000-8000-000000000001", EnvironmentID: "00000000-0000-4000-8000-000000000002", SessionID: "00000000-0000-4000-8000-000000000003", RunID: "00000000-0000-4000-8000-000000000004"}
	candidate := Candidate{ContractVersion: ContractVersion, Kind: KindAuthenticatedActionRecord, SourceID: "audit", SourceIdentity: "cluster", SourceEventID: "event-1", ActionType: "deployment", Operation: "patch", Target: Target{Kind: "Deployment", Namespace: "app", Name: "web"}, CorrelationIDs: []string{"corr-1"}, EvidenceRefs: []string{"ref-1"}, Uncertainty: []string{"uncertain"}, MissingFields: []string{"actor"}}
	assessment := Assessment{AuthenticationStatus: "verified", AuthorizationStatus: "authorized", SourceIdentityStatus: "verified", ProvenanceStatus: "verified", AuthenticationMethod: "runner-bound-test", CredentialScope: "test", ActorStatus: "unknown", CausalityStatus: "unproven", Uncertainty: []string{"limited"}}
	build := func() Evidence {
		evidence, err := BuildFromProvider(context.Background(), verifiedTestProvider{assessment: assessment}, scope, candidate, time.Now().UTC())
		if err != nil {
			t.Fatal(err)
		}
		return evidence
	}

	mutations := map[string]func(*Evidence){
		"scope":                      func(e *Evidence) { e.Scope.WorkspaceID = "00000000-0000-4000-8000-000000000099" },
		"trust":                      func(e *Evidence) { e.Trust = TrustAttributionQualified },
		"target":                     func(e *Evidence) { e.Target.Name = "attacker" },
		"source":                     func(e *Evidence) { e.Source.Identity = "other-cluster" },
		"actor":                      func(e *Evidence) { e.Actor.Identity = "claimed-agent" },
		"nested evidence reference":  func(e *Evidence) { e.EvidenceRefs[0] = "forged-ref" },
		"nested correlation":         func(e *Evidence) { e.CorrelationIDs[0] = "forged-correlation" },
		"nested uncertainty":         func(e *Evidence) { e.Uncertainty[0] = "forged-uncertainty" },
		"nested provenance conflict": func(e *Evidence) { e.ProvenanceConflicts = append(e.ProvenanceConflicts, "forged-conflict") },
		"public digest":              func(e *Evidence) { e.Integrity.Digest = "sha256:forged" },
	}
	for name, mutate := range mutations {
		t.Run(name, func(t *testing.T) {
			evidence := build()
			mutate(&evidence)
			if name != "public digest" {
				evidence.Integrity.Digest = Digest(evidence)
			}
			if _, err := Store(context.Background(), nil, evidence); err != ErrUntrustedInput {
				t.Fatalf("mutated provider evidence reached persistence gate: %v", err)
			}
		})
	}
}
