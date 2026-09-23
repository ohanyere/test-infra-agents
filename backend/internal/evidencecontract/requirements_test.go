package evidencecontract

import "testing"

func TestRequirementContracts(t *testing.T) {
	for name, requirement := range map[string]Requirements{
		"default": DefaultRequirements(),
		"gitops":  GitOpsRequirements(),
	} {
		t.Run(name, func(t *testing.T) {
			if err := requirement.Validate(); err != nil {
				t.Fatal(err)
			}
		})
	}
	invalid := GitOpsRequirements()
	invalid.ArgoState = NotApplicable
	if err := invalid.Validate(); err == nil {
		t.Fatal("split Git/Argo requirements were accepted")
	}
}
