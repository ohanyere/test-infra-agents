package reliability

// Trial is one persisted trial observation in reliability scope. It is built
// server-side from exact persisted run/evaluation rows; API clients can never
// submit trial outcomes or derived scores (reliability endpoints are GET-only
// and every handler ignores request bodies).
type Trial struct {
	RunID             string
	SuiteExecutionID  string
	ScenarioVersionID string
	AgentVersionID    string
	FamilyID          string
	CaseType          string
	Validity          string
	Result            string
	VariantID         string
	VariantSeed       int64
	// Ordinal is the stable persisted trial order (attempt_number /
	// trial_ordinal). Consecutive-success metrics sort by (Ordinal, RunID).
	Ordinal int
	// EvaluatorVersion pins the deterministic evaluator contract observed.
	EvaluatorVersion string
	// RuntimeContractVersion pins the runtime contract observed.
	RuntimeContractVersion string
	// EnvironmentRef is the opaque run environment reference (never a
	// kubeconfig, secret, or credential). It is provenance metadata only:
	// comparability compares the semantic environment pins below, never
	// the raw ref alone.
	EnvironmentRef string
	// Semantic environment fidelity pins (server-owned catalog values).
	// Populated from the run-owned EnvironmentInstance when available;
	// empty when the persisted evidence carries no semantic metadata
	// (then comparability reports a limitation instead of claiming
	// fidelity from opaque refs).
	EnvironmentProfileID       string
	EnvironmentProfileContract string
	ProviderID                 string
	ProviderContract           string
	IsolationTier              string
	// SafetyViolations counts structured safety findings on this trial.
	SafetyViolations int
}

// IsCountedValid reports whether the trial enters reliability denominators:
// exactly one valid deterministic evaluation exists. Platform-invalid,
// abandoned-before-valid-evaluation, provisioning-failure, and
// telemetry-platform-failure trials are ValidityInvalidPlatform and are
// excluded; incomplete trials are ValidityPending and excluded.
func (t Trial) IsCountedValid() bool { return t.Validity == ValidityValid }

// IsSuccess reports a valid agent pass.
func (t Trial) IsSuccess() bool { return t.IsCountedValid() && t.Result == ResultPass }

// IsAgentFailure reports a valid evaluated non-pass outcome: wrong
// remediation, safety violation, unrestored state, forbidden action, failed
// restraint case, or valid-environment timeout. The existing deterministic
// taxonomy already maps these to fail; inconclusive/stopped are valid trials
// that are neither passes nor agent failures and are tracked separately.
func (t Trial) IsAgentFailure() bool { return t.IsCountedValid() && t.Result == ResultFail }

// OutcomeClass collapses a valid trial to its reliability class for
// saturation stability and distribution reporting. Classes reuse the existing
// deterministic outcome vocabulary; no new taxonomy is introduced.
func (t Trial) OutcomeClass() string {
	if !t.IsCountedValid() {
		return t.Validity
	}
	switch t.Result {
	case ResultPass:
		return ResultPass
	case ResultFail:
		return ResultFail
	default:
		return "other_valid"
	}
}

// ValidTrials returns the counted-valid subset in stable order.
func ValidTrials(trials []Trial) []Trial {
	out := make([]Trial, 0, len(trials))
	for _, trial := range trials {
		if trial.IsCountedValid() {
			out = append(out, trial)
		}
	}
	return out
}
