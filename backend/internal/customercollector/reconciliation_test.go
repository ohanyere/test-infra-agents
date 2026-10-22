package customercollector

import "testing"

func TestReconciliationNeverPromotesMissingOrForwardedEvidence(t *testing.T) {
	cases := []struct {
		count, attempts int
		state, result   string
	}{
		{0, 1, "unavailable", "none"},
		{0, 2, "unavailable", "none"},
		{0, 3, "reconciliation_completed", "missing"},
		{1, 1, "reconciliation_completed", "insufficient"},
		{2, 1, "reconciliation_completed", "conflicting"},
	}
	for _, c := range cases {
		state, result, _ := reconciliationDecision(c.count, c.attempts)
		if state != c.state || result != c.result {
			t.Fatalf("count=%d attempts=%d: %s/%s", c.count, c.attempts, state, result)
		}
	}
}
