Read prompts/reports/reliability-contract-consistency.md and inspect the actual
implementation/tests for:

- Empirical.DecisiveTrials
- Empirical.DecisivePassRate
- Empirical.ValidSuccessRate
- TestConsistencyPassRateDenominator
- TestConsistencyInconclusiveBehavior

There is a possible arithmetic inconsistency in completion-report §8.

The report says:

1 pass + 1 inconclusive + 1 stopped
→ decisive pass rate 50%
→ valid success rate 33.3%
→ k=1 = 1/3

Under the documented contract, that sample should have:

valid_trials = 3
decisive_trials = 1
decisive_pass_rate = 100%
valid_success_rate = 33.3%
k=1 = 1/3

A 50% decisive pass rate would require one pass + one fail.

Determine whether this is:

1. only a completion-report wording mistake, or
2. an implementation/test bug.

Do not change production code unless it is actually wrong.

If implementation/tests are correct, fix only the completion report so the
example exactly matches the test vector.

If implementation is wrong, correct the denominator logic and focused tests
according to the Stage-33 contract.

Run only:

go test ./internal/reliability/... -count=1

Do not run go test ./...
Do not start Docker/Kubernetes.

Append a short correction section to:

prompts/reports/reliability-contract-consistency.md

stating:
- root cause
- exact input vector
- exact decisive rate
- exact valid-success rate
- exact k=1 result
- whether production code changed
- focused test result
