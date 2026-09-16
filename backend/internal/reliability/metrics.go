package reliability

import "sort"

// Empirical is the deterministic summary of observed repeated trials.
// Denominators contain valid trials only; platform-invalid trials are
// counted separately and never converted into agent failures.
//
// Denominator contract (reliability-metrics-contract:v1, Design B):
//
//	PassRate / DecisivePassRate: passed / (passed + failed) as a 0-100
//	  percentage, nil when no decisive valid outcomes exist.
//	  Inconclusive/stopped trials are valid evidence but neither success
//	  nor failure, so they are excluded from this decisive denominator
//	  (matching suite aggregation semantics). PassRate is the historical
//	  V1 field and always equals DecisivePassRate.
//	ValidSuccessRate: passed / valid_trials as a 0-100 percentage, nil
//	  when no valid trials exist. Every valid non-pass (fail,
//	  inconclusive, stopped, unknown-result) is a non-success here.
//	DecisiveTrials: passed + failed (the decisive denominator).
//	K-metrics (pass@k / pass^k): n = all valid trials, c = passes; every
//	  valid non-pass is a non-success for k-estimation.
type Empirical struct {
	TotalTrials           int `json:"total_trials"`
	ValidTrials           int `json:"valid_trials"`
	PassedTrials          int `json:"passed_trials"`
	FailedTrials          int `json:"failed_trials"`
	InconclusiveTrials    int `json:"inconclusive_trials"`
	StoppedTrials         int `json:"stopped_trials"`
	PendingTrials         int `json:"pending_trials"`
	PlatformInvalidTrials int `json:"platform_invalid_trials"`
	// DecisiveTrials is passed + failed: the denominator of PassRate and
	// DecisivePassRate.
	DecisiveTrials int `json:"decisive_trials"`
	// PassRate is passed / (passed + failed) as a 0-100 percentage, nil when
	// no decisive valid outcomes exist. Historical V1 semantics, frozen:
	// always equals DecisivePassRate.
	PassRate *float64 `json:"pass_rate"`
	// DecisivePassRate is the explicit decisive-only rate: passed /
	// (passed + failed), nil when decisive == 0. Identical to PassRate;
	// exposed so the denominator is unambiguous.
	DecisivePassRate *float64 `json:"decisive_pass_rate"`
	// ValidSuccessRate is passed / valid_trials as a 0-100 percentage, nil
	// when no valid trials exist. Valid inconclusive/stopped trials are
	// non-success here and are never hidden: their counts stay visible.
	ValidSuccessRate *float64 `json:"valid_success_rate"`
	// PlatformInvalidRate is platform_invalid / (valid + platform_invalid)
	// terminal trials as a 0-100 percentage, nil when no terminal trials
	// exist. It is always reported separately from PassRate.
	PlatformInvalidRate *float64 `json:"platform_invalid_rate"`
	// SafetyViolationTrials counts valid trials with structured safety findings.
	SafetyViolationTrials int `json:"safety_violation_trials"`
}

// Summarize builds the empirical summary. Pure function of trial evidence.
func Summarize(trials []Trial) Empirical {
	out := Empirical{TotalTrials: len(trials)}
	for _, trial := range trials {
		switch trial.Validity {
		case ValidityInvalidPlatform:
			out.PlatformInvalidTrials++
		case ValidityPending:
			out.PendingTrials++
		case ValidityValid:
			out.ValidTrials++
			switch trial.Result {
			case ResultPass:
				out.PassedTrials++
			case ResultFail:
				out.FailedTrials++
			case ResultInconclusive:
				out.InconclusiveTrials++
			case ResultStopped:
				out.StoppedTrials++
			default:
				out.InconclusiveTrials++
			}
			if trial.SafetyViolations > 0 {
				out.SafetyViolationTrials++
			}
		default:
			out.PendingTrials++
		}
	}
	out.DecisiveTrials = out.PassedTrials + out.FailedTrials
	if out.DecisiveTrials > 0 {
		value := 100 * float64(out.PassedTrials) / float64(out.DecisiveTrials)
		out.PassRate = &value
		decisive := value
		out.DecisivePassRate = &decisive
	}
	if out.ValidTrials > 0 {
		value := 100 * float64(out.PassedTrials) / float64(out.ValidTrials)
		out.ValidSuccessRate = &value
	}
	if terminal := out.ValidTrials + out.PlatformInvalidTrials; terminal > 0 {
		value := 100 * float64(out.PlatformInvalidTrials) / float64(terminal)
		out.PlatformInvalidRate = &value
	}
	return out
}

// KMetric carries the finite-sample pass@k / pass^k estimates for one k.
// When n < k the metric is unavailable: Available is false and both
// estimates are nil (never a fabricated zero).
type KMetric struct {
	K          int      `json:"k"`
	Available  bool     `json:"available"`
	PassAtK    *float64 `json:"pass_at_k"`
	PassToTheK *float64 `json:"pass_to_the_k"`
}

// KMetrics computes pass@k and pass^k for every supported k.
//
// Estimator (pinned by reliability-metrics-contract:v1): for n valid trials
// with c passes, treating each valid trial as success (pass) or non-success
// (any other valid outcome) and sampling k distinct valid trials uniformly
// without replacement:
//
//	pass@k = 1 - C(n-c, k) / C(n, k)
//	pass^k = C(c, k) / C(n, k)
//
// pass@k answers "can the agent succeed at least once in k attempts?";
// pass^k answers "do all k attempts succeed?" (consistency). The naive
// pass_rate^k independence assumption is never used. Products are evaluated
// multiplicatively (prod (n-c-i)/(n-i)) so no integer combinatorics can
// overflow; results are exact up to float64 rounding.
func KMetrics(trials []Trial) []KMetric {
	valid := ValidTrials(trials)
	n := len(valid)
	c := 0
	for _, trial := range valid {
		if trial.Result == ResultPass {
			c++
		}
	}
	out := make([]KMetric, 0, len(SupportedK))
	for _, k := range SupportedK {
		metric := KMetric{K: k}
		if n < k {
			out = append(out, metric)
			continue
		}
		metric.Available = true
		at := passAtK(n, c, k)
		to := passToTheK(n, c, k)
		metric.PassAtK = &at
		metric.PassToTheK = &to
		out = append(out, metric)
	}
	return out
}

func passAtK(n, c, k int) float64 {
	// Fewer than k non-successes: every k-subset contains a success.
	if n-c < k {
		return 1
	}
	product := 1.0
	for i := 0; i < k; i++ {
		product *= float64(n-c-i) / float64(n-i)
	}
	return 1 - product
}

func passToTheK(n, c, k int) float64 {
	// Fewer than k successes: no k-subset is all-success.
	if c < k {
		return 0
	}
	product := 1.0
	for i := 0; i < k; i++ {
		product *= float64(c-i) / float64(n-i)
	}
	return product
}

// KLookup returns the metric for one k, or nil when k is unsupported.
func KLookup(metrics []KMetric, k int) *KMetric {
	for i := range metrics {
		if metrics[i].K == k {
			return &metrics[i]
		}
	}
	return nil
}

// ConsecutiveSuccess reports stable-order consecutive-pass signals over the
// valid trials. Ordering is (Ordinal, RunID) from persisted execution order.
// Max is the longest pass run; Latest is the trailing pass run at the end
// (0 when the most recent valid trial is not a pass). Consecutive success
// is a secondary signal, never the only reliability metric.
func ConsecutiveSuccess(trials []Trial) (max, latest int) {
	valid := ValidTrials(trials)
	sort.Slice(valid, func(i, j int) bool {
		if valid[i].Ordinal != valid[j].Ordinal {
			return valid[i].Ordinal < valid[j].Ordinal
		}
		return valid[i].RunID < valid[j].RunID
	})
	run := 0
	for _, trial := range valid {
		if trial.Result == ResultPass {
			run++
			if run > max {
				max = run
			}
		} else {
			run = 0
		}
	}
	for i := len(valid) - 1; i >= 0; i-- {
		if valid[i].Result != ResultPass {
			break
		}
		latest++
	}
	return max, latest
}

// Diversity tracks hidden-variant exercise without leaking evaluator-only
// values. VariantID values are opaque identifiers already surfaced per-trial
// by suite results; hidden configs, reference remediations, and ground truth
// are never selected or exposed here.
type Diversity struct {
	DistinctVariants int `json:"distinct_variants"`
	// VariantTrials counts valid trials carrying a variant assignment.
	VariantTrials int `json:"variant_trials"`
	// EligibleVariants is the total eligible variant population when the
	// denominator is legitimate (execution scope with configured variants).
	EligibleVariants *int `json:"eligible_variants,omitempty"`
	// VariantCoverageRatio is distinct/eligible, present only when the
	// denominator is legitimate.
	VariantCoverageRatio *float64 `json:"variant_coverage_ratio,omitempty"`
	// DominantVariantShare is the largest single-variant share of valid
	// trials (0-1). When ~0.9+, success evidence is dominated by one hidden
	// variant and reliability must not appear broad. Exposed as evidence
	// for QualificationPolicy; it never fails qualification here.
	DominantVariantShare float64 `json:"dominant_variant_share"`
}

// DescribeDiversity counts variant exercise. eligible may be nil when no
// legitimate denominator exists (cross-execution scopes); the ratio is then
// omitted rather than fabricated.
func DescribeDiversity(trials []Trial, eligible *int) Diversity {
	valid := ValidTrials(trials)
	counts := map[string]int{}
	for _, trial := range valid {
		if trial.VariantID == "" {
			continue
		}
		counts[trial.VariantID]++
	}
	out := Diversity{DistinctVariants: len(counts)}
	peak := 0
	for _, count := range counts {
		out.VariantTrials += count
		if count > peak {
			peak = count
		}
	}
	if len(valid) > 0 && peak > 0 {
		out.DominantVariantShare = float64(peak) / float64(len(valid))
	}
	if eligible != nil && *eligible > 0 {
		total := *eligible
		out.EligibleVariants = &total
		ratio := float64(out.DistinctVariants) / float64(total)
		if ratio > 1 {
			ratio = 1
		}
		out.VariantCoverageRatio = &ratio
	}
	return out
}
