package billing

import "strings"

// MeterUnits returns V1 billable units for one trial. The base unit is one
// standard Kubernetes namespace trial = 1 unit. Higher-fidelity tiers share
// the same unit count in V1 because exact multipliers are not yet published;
// the meter architecture pins isolation/profile/provider so future
// multipliers can reinterpret historical raw usage without losing facts.
func MeterUnits(isolationTier string) int64 {
	// Intentionally flat in V1. Do not finalize dollar multipliers here.
	return 1
}

// SettleAction is the deterministic settlement outcome for one trial.
type SettleAction struct {
	// Action: commit | release | refund | none
	Action string
	// Billable reports whether the trial counts as committed customer
	// evaluation consumption.
	Billable bool
	Reason   string
}

// Settlement actions.
const (
	SettleCommit  = "commit"
	SettleRelease = "release"
	SettleRefund  = "refund"
	SettleNone    = "none"
)

// ClassifySettlement maps terminal trial truth to a settlement action. It is
// pure so concurrency/idempotency rules stay unit-testable without a DB.
//
// Rules (Stage-33/34 validity semantics):
//   - platform-invalid evaluations never become committed agent usage. If a
//     commit already happened before validity was known, refund through a
//     correcting entry; otherwise release the reservation.
//   - valid PASS and valid agent FAIL both commit (customer consumed a valid
//     evaluation; agent failure is not a refund reason).
//   - valid agent timeout commits (billable); platform timeout/failure
//     releases (or refunds if already committed).
//   - user cancellation before execution, provisioning failure, worker
//     abandonment recovery, stale lease recovery, and control stops release
//     without charge.
//   - rows are never deleted; refunds are append-only corrections.
func ClassifySettlement(in SettleInput) SettleAction {
	validity := strings.ToLower(strings.TrimSpace(in.Validity))
	runStatus := strings.ToLower(strings.TrimSpace(in.RunStatus))
	evalStatus := strings.ToLower(strings.TrimSpace(in.EvaluationStatus))
	failClass := strings.ToLower(strings.TrimSpace(in.FailureClass))
	failCode := strings.ToLower(strings.TrimSpace(in.FailureCode))

	// Platform-invalid is never customer consumption.
	if validity == "invalid_platform" || validity == "invalid-platform" {
		return SettleAction{Action: SettleRefund, Billable: false, Reason: "platform-invalid evaluation: refund committed usage, never count platform failure as customer consumption"}
	}
	// Explicit platform failure class without valid evaluation.
	if failClass == "platform" && (evalStatus == "" || evalStatus == "inconclusive") && validity != "valid" {
		return SettleAction{Action: SettleRelease, Billable: false, Reason: "platform failure (" + orDefault(failCode, "unspecified") + "): release reservation without charge"}
	}
	// Control stops / user cancellation before execution.
	if runStatus == "stopped" || failClass == "control" || failCode == "run_stopped" {
		return SettleAction{Action: SettleRelease, Billable: false, Reason: "trial stopped before billable evaluation completed: release reservation"}
	}
	// Provisioning / pre-execution failures that never produced an evaluation.
	if failCode == "environment_provisioning_failed" || failCode == "scenario_prepare_failed" || failCode == "scenario_start_failed" || failCode == "version_mismatch" || failCode == "version_load_failed" || failCode == "run_load_failed" {
		return SettleAction{Action: SettleRelease, Billable: false, Reason: "provisioning failure (" + failCode + "): agent never evaluated, release reservation"}
	}
	// Worker abandonment / stale lease recovery without evaluation.
	if failCode == "worker_lease_expired" || failCode == "run_abandoned" || failCode == "evaluation_persistence_failed" {
		if validity == "valid" && (evalStatus == "pass" || evalStatus == "fail") {
			return SettleAction{Action: SettleCommit, Billable: true, Reason: "valid evaluation exists despite recovery signal: commit billable usage once"}
		}
		return SettleAction{Action: SettleRelease, Billable: false, Reason: "abandoned/stale recovery without valid evaluation: release reservation, no double charge"}
	}
	// Valid terminal evaluations bill.
	if validity == "valid" {
		switch evalStatus {
		case "pass":
			return SettleAction{Action: SettleCommit, Billable: true, Reason: "valid PASS is billable evaluation consumption"}
		case "fail":
			return SettleAction{Action: SettleCommit, Billable: true, Reason: "valid agent FAIL is billable evaluation consumption"}
		case "inconclusive":
			if failClass == "agent" || runStatus == "failed" && failClass != "platform" {
				// Valid agent timeout / agent-caused inconclusive is billable.
				return SettleAction{Action: SettleCommit, Billable: true, Reason: "valid agent timeout/failure is billable evaluation consumption"}
			}
			return SettleAction{Action: SettleRelease, Billable: false, Reason: "inconclusive without agent causality: release reservation"}
		}
		// Valid with failed run status and agent causality (e.g. agent timeout
		// recorded as run failed before evaluation status lands).
		if runStatus == "failed" && (failClass == "agent" || failCode == "run_timeout" || failCode == "agent_timeout") {
			return SettleAction{Action: SettleCommit, Billable: true, Reason: "valid agent timeout is billable"}
		}
		if runStatus == "completed" || runStatus == "failed" {
			// Default for valid terminal trials: the customer consumed an
			// evaluation window.
			return SettleAction{Action: SettleCommit, Billable: true, Reason: "valid terminal evaluation is billable"}
		}
	}
	// Pending/unknown validity without billable evidence: release.
	return SettleAction{Action: SettleRelease, Billable: false, Reason: "no billable valid evaluation: release reservation"}
}

func orDefault(v, fallback string) string {
	if v == "" {
		return fallback
	}
	return v
}
