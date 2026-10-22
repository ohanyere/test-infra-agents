package operational

import (
	"testing"
	"time"
)

func TestGuidanceRankingRespectsScopeAndFreshness(t *testing.T) {
	now := time.Now().UTC()
	op := ProposedOperation{Namespace: "checkout", ResourceKind: "Deployment", ResourceName: "checkout", Operation: "set_image", Service: "checkout/checkout"}
	match := rankEpisode(op, "Deployment", "checkout", "checkout", "checkout/checkout", "set_image", now.Add(-time.Hour), now)
	weaker := rankEpisode(op, "Deployment", "checkout", "other", "checkout/checkout", "patch", now.Add(-24*time.Hour), now)
	if match <= weaker || weaker <= 0 {
		t.Fatalf("ranking failed: match=%d weaker=%d", match, weaker)
	}
	if rankEpisode(op, "Deployment", "other", "checkout", "checkout/checkout", "set_image", now, now) != 0 {
		t.Fatal("wrong namespace retrieved")
	}
	if rankEpisode(op, "Deployment", "checkout", "unrelated", "other/other", "patch", now, now) != 0 {
		t.Fatal("unrelated workload retrieved")
	}
	if rankEpisode(op, "Deployment", "checkout", "checkout", "checkout/checkout", "set_image", now.Add(-91*24*time.Hour), now) != 0 {
		t.Fatal("stale episode retrieved")
	}
	if rankEpisode(op, "Pod", "checkout", "checkout", "checkout/checkout", "set_image", now, now) != 0 {
		t.Fatal("wrong kind retrieved")
	}
}
func TestProposedOperationScopeValidation(t *testing.T) {
	op := ProposedOperation{Namespace: "checkout", ResourceKind: "Deployment", ResourceName: "checkout", Operation: "set_image"}
	if !validOperation(op) {
		t.Fatal("valid operation rejected")
	}
	op.RunID = "not-uuid"
	if validOperation(op) {
		t.Fatal("invalid run reference accepted")
	}
	op.RunID = ""
	op.ResourceKind = "Secret"
	if validOperation(op) {
		t.Fatal("unscoped resource kind accepted")
	}
}
