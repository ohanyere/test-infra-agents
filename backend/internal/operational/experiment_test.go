package operational

import (
	"testing"
)

func TestCompareGuidanceArmsRequiresMatchedEvidence(t *testing.T) {
	ref := []EvidenceRef{{Reference: "report:1", Provenance: "source_authenticated"}}
	a := ExperimentArm{DatasetID: "data-1", AgentVersionID: "agent-1", ScenarioVersionID: "scenario-1", ExecutionKind: "offline_replay", Mode: "without_guidance", Cases: []ExperimentCase{{ID: "case-a", TaskSuccess: true, Evidence: ref}, {ID: "case-b", ForbiddenAttempts: 1, Evidence: ref}}}
	b := a
	b.Mode = "with_guidance"
	b.Cases = []ExperimentCase{{ID: "case-a", TaskSuccess: false, UnsupportedGuidance: true, Evidence: ref}, {ID: "case-b", TaskSuccess: true, Evidence: ref}}
	report, err := CompareGuidanceArms(a, b)
	if err != nil {
		t.Fatal(err)
	}
	if report.WithoutGuidance.Cases != 2 || report.WithGuidance.Cases != 2 || report.WithoutGuidance.TaskSuccess != 1 || report.WithGuidance.TaskSuccess != 1 || len(report.WorseCaseIDs) != 1 || report.WorseCaseIDs[0] != "case-a" {
		t.Fatalf("comparison missed denominator or worse case: %+v", report)
	}
	b.Cases[0].Evidence = nil
	if _, err = CompareGuidanceArms(a, b); err == nil {
		t.Fatal("evidence-free result accepted")
	}
	b.Cases[0].Evidence = ref
	b.DatasetID = "other"
	if _, err = CompareGuidanceArms(a, b); err == nil {
		t.Fatal("non-equivalent dataset accepted")
	}
}
