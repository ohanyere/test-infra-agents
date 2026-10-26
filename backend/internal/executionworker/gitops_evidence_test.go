package executionworker

import (
	"errors"
	"reflect"
	"testing"

	"github.com/evalplane/evalplane/backend/internal/gitops"
)

func TestEvidenceForEventPreservesSpecificProviderProvenance(t *testing.T) {
	provided := gitops.Evidence{ContractVersion: gitops.EvidenceContractVersion, Errors: []string{"argo_state_unavailable_after_remediation"}}
	got := evidenceForEvent(provided, errors.New("provider read failed"))
	if !reflect.DeepEqual(got.Errors, provided.Errors) {
		t.Fatalf("specific evidence provenance was replaced: %v", got.Errors)
	}

	fallback := evidenceForEvent(gitops.Evidence{ContractVersion: gitops.EvidenceContractVersion}, errors.New("provider read failed"))
	if !reflect.DeepEqual(fallback.Errors, []string{"gitops_evidence_unavailable"}) {
		t.Fatalf("generic provider failure lacks fallback provenance: %v", fallback.Errors)
	}
}
