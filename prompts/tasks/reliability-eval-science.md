EVALPLANE — RELIABILITY & EVAL SCIENCE

Read AGENTS.md first.

Inspect existing implementation for:

- EvaluationSuite / SuiteVersion / SuiteExecution
- Trial / Run execution lifecycle
- hidden variants
- platform-invalid handling
- deterministic evaluator
- capability coverage
- QualificationPlan
- QualificationPolicy
- AgentVersion regression
- Task Family / case type
- Stage-30 Kubernetes/SRE catalog
- Stage-31 GitOps/progressive-delivery catalog
- scenario certification
- qualification/comparison APIs and frontend
- evidence persistence

Do not broadly refactor the repository.

==================================================
PURPOSE
==================================================

Evalplane must distinguish:

"the agent passed once"

from:

"the agent reliably succeeds across repeated independent trials."

Introduce deterministic reliability/evaluation-science primitives for
AgentVersion qualification evidence.

The core reliability concepts are:

pass@k
pass^k
valid sample size
family-level reliability
capability-level reliability
variant diversity
platform-invalid exclusion
task saturation

Do not implement LLM judging.

Do not alter deterministic task grading.

==================================================
1. TERMINOLOGY
==================================================

Use the following semantics exactly.

pass@k:

Probability/evidence that at least one of k valid trials succeeds.

Operational interpretation:

"Can the agent succeed at least once when given k attempts?"

pass^k:

Probability/evidence that all k valid trials succeed.

Operational interpretation:

"Can the agent succeed consistently across k attempts?"

For infrastructure qualification, pass^k / consistency is usually the
stronger reliability signal.

Do not confuse pass@k with pass^k.

Do not call pass@k "consistency."

==================================================
2. EMPIRICAL METRICS
==================================================

For observed repeated trials, compute deterministic empirical statistics.

At minimum:

valid_trials
passed_trials
failed_trials
platform_invalid_trials
pass_rate

For grouped repeated runs where the product has enough information:

pass_at_k
pass_to_the_k

Use statistically defensible formulas for the data model.

Do not invent precision unsupported by available samples.

If exact benchmark-style pass@k estimation assumptions do not hold,
name the metric appropriately and document the estimator.

==================================================
3. VALID TRIALS ONLY
==================================================

Reliability denominators must exclude:

platform-invalid
abandoned-before-valid-evaluation
infrastructure-provisioning failure
telemetry-platform failure
other existing platform-class invalid outcomes

These should remain visible separately.

Never convert platform failure into agent failure.

Never silently remove invalid trials from the UI without showing their count.

==================================================
4. AGENT FAILURES COUNT
==================================================

Valid evaluated outcomes where the agent:

makes the wrong remediation
violates a safety condition
fails to restore required state
takes forbidden action
fails a restraint case
times out under an otherwise valid environment

count as valid agent failures when existing taxonomy says they do.

Reuse existing failure taxonomy.

Do not introduce conflicting failure classification.

==================================================
5. RELIABILITY CONTRACT
==================================================

Introduce a versioned contract:

reliability-metrics-contract:v1

Pin it to persisted reliability snapshots/reports.

Historical reports must remain reproducible if formulas change later.

==================================================
6. RELIABILITY SCOPE
==================================================

Support aggregation at least at:

ScenarioVersion
Task Family
Capability
SuiteExecution
AgentVersion

Do not collapse everything into one global score.

The most important views are:

Task reliability
Family reliability
Capability reliability

A globally high score must not hide a weak critical capability.

==================================================
7. TASK-LEVEL RELIABILITY
==================================================

For one exact ScenarioVersion, report:

valid trials
passes
failures
platform-invalid count
pass rate
pass@k where meaningful
pass^k / consecutive consistency measure where meaningful
hidden-variant count exercised
distinct hidden variants exercised

Pin:

ScenarioVersion
AgentVersion
runtime contract
environment profile/provider
evaluator contract
reliability contract

Do not compare trials across materially incompatible execution contracts.

==================================================
8. FAMILY-LEVEL RELIABILITY
==================================================

Aggregate only compatible valid evidence from ScenarioVersions belonging to
the same Task Family.

Report:

distinct ScenarioVersions
distinct variants
valid trials
pass rate
consistency
case-type distribution

Do not let 20 retries of one scenario masquerade as broad family reliability.

Breadth and repetition must remain separate.

==================================================
9. CAPABILITY-LEVEL RELIABILITY
==================================================

Use Stage-27/32 capability-to-family mappings.

Capability reliability must show:

required families
covered families
family reliability
valid trial count
contrast-case reliability where applicable
overall capability reliability state

Do not create QUALIFIED state here.

This stage measures reliability.

Stage-22 QualificationPolicy still owns the final qualification verdict.

==================================================
10. SAMPLE SUFFICIENCY
==================================================

Introduce explicit sample sufficiency states.

Suggested:

insufficient
provisional
sufficient

Do not imply statistical confidence from 1–2 trials.

Use deterministic thresholds in a versioned policy/config.

Preferred initial behavior:

insufficient:
below minimum valid sample count

provisional:
minimum evidence exists but stronger consistency sample not reached

sufficient:
configured reliability sample target reached

Keep the thresholds modest and transparent.

Do not claim formal statistical significance unless actually computed.

==================================================
11. MINIMUM VALID SAMPLE
==================================================

Create a versioned default reliability policy.

For example:

reliability-policy:v1

It may define:

minimum valid trials per ScenarioVersion
minimum valid trials per Task Family
minimum distinct variants
minimum distinct ScenarioVersions
minimum negative/contrast evidence where required

Use conservative defaults.

Do not retroactively modify coverage-contract semantics.

Reliability requirements are separate from breadth requirements.

==================================================
12. PASS@K
==================================================

Implement pass@k carefully.

If the repository stores multiple independent attempts for the same exact task,
derive the metric only from valid independent attempts.

Document the estimator.

Avoid naive:

1 - (1 - pass_rate)^k

unless the independence/stationarity assumption is explicitly part of the
contract.

If using the standard finite-sample unbiased estimator, implement and test it
precisely.

For n valid attempts with c successes:

pass@k = 1 - C(n-c, k) / C(n, k)

when n >= k.

If n < k:

metric unavailable.

Never fabricate k-sized reliability from too few samples.

==================================================
13. PASS^K
==================================================

Implement a clearly named consistency metric.

For empirical all-success groups, use a deterministic definition pinned in the
contract.

Possible V1 direction:

For k selected valid attempts:

pass^k = fraction/probability estimate that all k succeed.

If using finite-sample combinatorics:

pass^k = C(c, k) / C(n, k)

for n >= k.

This gives the probability that k sampled valid trials are all successes.

Document this clearly.

Do not simply compute pass_rate ^ k and silently assume independence.

If you decide another estimator fits the repository better, document and pin it.

==================================================
14. K VALUES
==================================================

Support a bounded server-owned set.

Suggested:

k = 1
k = 2
k = 3
k = 5

Only emit a metric when:

n >= k

Do not emit fake zeros for unavailable k.

Represent unavailable explicitly.

==================================================
15. CONSISTENCY EMPHASIS
==================================================

For infrastructure-agent qualification, display consistency more prominently
than "at least one success."

Example:

pass@5 = 100%
pass^5 = 40%

Interpretation:

The agent can usually succeed if retried, but is not consistently reliable.

Do not let high pass@k visually hide poor pass^k.

==================================================
16. CONSECUTIVE SUCCESS
==================================================

If trial ordering is meaningful, also support:

max_consecutive_passes

and optionally:

latest_consecutive_passes

Do not use consecutive success as the only reliability metric.

Ordering must be stable and based on persisted execution/trial order.

==================================================
17. HIDDEN VARIANT DIVERSITY
==================================================

Track:

total eligible variants known
distinct variants exercised
variant repetitions
variant coverage ratio where denominator is legitimate

Do not expose hidden variant values to customers/agents if they are evaluator-only.

Safe output may say:

4 distinct hidden variants exercised

without revealing them.

==================================================
18. VARIANT DOMINANCE
==================================================

Detect when reliability evidence is dominated by one variant.

Suggested signal:

variant_concentration

or:

dominant_variant_share

If 90% of successful trials come from one hidden variant, reliability should
not appear broad.

Do not fail qualification directly here.

Expose the evidence to QualificationPolicy later.

==================================================
19. TASK SATURATION
==================================================

Introduce scenario/task saturation measurement.

Purpose:

Detect when repeated evaluation on the same task no longer adds much evidence.

At minimum track:

number of executions
number of distinct AgentVersions evaluated
number of distinct variants exercised
outcome distribution
recent outcome stability
family alternatives available

Create states such as:

fresh
developing
saturated

Do not automatically retire a task.

This is evaluator/catalog-maintenance evidence.

==================================================
20. SATURATION DOES NOT MEAN EASY
==================================================

A task can be saturated even if hard.

Saturation means:

additional repetitions of the same task/variant configuration provide
diminishing new evidence.

Do not define saturation merely as high pass rate.

==================================================
21. AGENT-VERSION COMPARISON
==================================================

Extend existing regression comparison with reliability deltas.

For the same exact compatible suite/task evidence, allow reporting:

pass-rate delta
pass@k delta
pass^k delta
valid sample delta
platform-invalid delta
family reliability delta

Do not claim improvement when samples are incompatible.

==================================================
22. COMPARABILITY
==================================================

Reliability comparisons require compatible pins.

At minimum compare:

same ScenarioVersion or compatible family scope
same evaluator contract
same runtime contract
compatible environment fidelity
same reliability contract

If not comparable:

return structured not_comparable reasons.

Reuse existing comparison architecture where possible.

==================================================
23. PLATFORM HEALTH SIGNAL
==================================================

Expose platform-invalid rate separately.

Example:

Agent valid reliability: 92%
Platform-invalid trials: 18%

This is important because noisy evaluation infrastructure can undermine trust.

Do not mix platform-invalid rate into agent pass rate denominator.

==================================================
24. QUALIFICATION POLICY INTEGRATION
==================================================

Do not replace Stage-22 QualificationPolicy.

Add reliability evidence so a policy can later require things like:

pass^3 >= threshold
minimum valid samples
minimum family reliability
maximum platform-invalid ratio

If current QualificationPolicy architecture cleanly supports versioned rules,
add reliability rule types carefully.

Otherwise persist/report the reliability data and defer policy-rule adoption.

Do not hardcode final qualification thresholds in this analytics package.

==================================================
25. PLANNER RELATIONSHIP
==================================================

Stage-32 planner decides minimum suite construction.

Stage-33 reliability does not rewrite an already generated QualificationPlan.

However, reliability policy may later inform recommended repeat counts.

For V1:

keep planner trial counts unchanged unless a versioned integration can be done
without breaking Stage-32 semantics.

Do not silently increase old SuiteVersion trial counts.

==================================================
26. PERSISTENCE
==================================================

Prefer deterministic recomputation from trial evidence when cheap.

Persist snapshots only where required for:

historical reproducibility
comparison
qualification assessment
API performance

If persisting:

pin all relevant contract versions.

Do not create mutable "current score" rows that erase history.

==================================================
27. API
==================================================

Add authenticated workspace-scoped APIs consistent with current architecture.

Potential shapes:

GET
/v1/workspaces/{workspaceID}/agents/{agentID}/versions/{versionID}/reliability

GET
/v1/workspaces/{workspaceID}/suite-executions/{executionID}/reliability

GET
/v1/workspaces/{workspaceID}/scenarios/{scenarioID}/versions/{versionID}/reliability

Use existing IDs/routes where possible.

Read-only reliability endpoints are sufficient.

Do not create arbitrary mutation endpoints for calculated scores.

==================================================
28. FRONTEND — AGENT RELIABILITY
==================================================

Extend the AgentVersion qualification section.

Show restrained reliability information such as:

Capability
Valid trials
Pass rate
Consistency (pass^k)
Family breadth
Sample sufficiency
Platform-invalid trials

Do not create radar charts.

Do not create one giant "AI reliability score."

==================================================
29. FRONTEND — TRIAL/SUITE RESULTS
==================================================

On suite/trial results, show:

valid vs invalid trials
pass rate
pass@k
pass^k
sample sufficiency
family breakdown

Where k is unavailable:

show "insufficient valid trials"

not 0%.

==================================================
30. FRONTEND — TASK SATURATION
==================================================

Task detail/catalog maintainers may show:

Fresh
Developing
Saturated

with short explanation.

Do not expose hidden variant contents.

If there is no maintainer/admin-specific surface yet, implement only safe
backend/API fields and a restrained task-detail display.

==================================================
31. NO BENCHMARK GAMING
==================================================

Reliability calculations must use exact persisted trial outcomes.

Users must not be allowed to submit:

passed_trials
pass_rate
pass_at_k
pass_to_the_k
saturation state
sample sufficiency

These are server-derived.

==================================================
32. FAILURE TAXONOMY
==================================================

Reuse Stage-22 taxonomy.

Reliability reports should be able to break failures down by existing classes.

Examples:

agent diagnosis failure
agent remediation failure
safety violation
timeout
platform invalid

Do not invent a competing taxonomy.

==================================================
33. SECURITY / LEAKAGE
==================================================

Reliability APIs may expose:

safe aggregate statistics
safe Task Family IDs
safe capability IDs
counts
contract pins

Never expose:

hidden variant values
reference remediation
ground truth
root-cause evaluator metadata
provider secrets
kubeconfigs
agent credentials

==================================================
34. TESTS
==================================================

Focused tests must cover at minimum:

reliability contract version pinned

0 valid trials:
metrics unavailable / insufficient

1 valid pass:
pass rate correct
no k > 1

mixed valid pass/fail:
denominator correct

platform-invalid excluded from agent denominator

agent failure included in denominator

standard finite-sample pass@k formula correct

pass@k unavailable when n < k

pass^k finite-sample formula correct

pass^k unavailable when n < k

all-pass sample:
pass@k = 1
pass^k = 1 for supported k <= n

all-fail sample:
pass@k = 0
pass^k = 0

mixed sample:
pass@k > pass^k where mathematically expected

stable deterministic calculations

sample sufficiency transitions

Task Family aggregation does not treat retries as independent families

distinct ScenarioVersion breadth preserved

hidden variant diversity counted without leaking values

variant dominance signal correct

platform-invalid rate separate

capability aggregation uses required families

negative/contrast evidence preserved

task saturation not based on pass rate alone

saturation deterministic

regression comparison reliability delta

incompatible contracts → not_comparable

workspace isolation

user cannot submit calculated reliability values

no evaluator-only leakage

no LLM calculation/judging

existing Stage-32 planner regression green

existing qualification policy behavior not weakened

==================================================
35. MATHEMATICAL TEST VECTORS
==================================================

Add explicit deterministic vectors.

Example:

n=5
c=3
k=2

pass@2:

1 - C(2,2)/C(5,2)
= 1 - 1/10
= 0.9

pass^2:

C(3,2)/C(5,2)
= 3/10
= 0.3

Test exact rational/intermediate behavior before floating formatting.

Another:

n=5
c=5
k=3

pass@3 = 1
pass^3 = 1

Another:

n=5
c=0
k=3

pass@3 = 0
pass^3 = 0

Avoid unstable floating-point comparison in tests.

==================================================
36. PRECISION
==================================================

Compute using integer combinatorics / rational-safe intermediate calculations.

Convert to decimal only at the API/display edge.

Define rounding centrally.

Suggested display:

percentage to one decimal place

but preserve higher precision internally if current API convention permits.

==================================================
37. VERIFICATION
==================================================

gofmt touched Go files.

Run focused packages only:

new reliability package
evaluationsuite
execution/evidence outcome package
coverage
qualification
agentregression
httpapi
qualplan

plus directly affected packages.

Do not run:

go test ./...
Docker
Kind
Kubernetes
Argo CD

Frontend:

npx tsc --noEmit

One production build if practical.

==================================================
38. DO NOT IMPLEMENT
==================================================

No:

Bayesian reliability model
complex confidence intervals unless trivial and well-tested
ML-based scoring
LLM grading
LLM reliability interpretation
billing
Stripe
production Kubernetes environment
real GitOps provider
trajectory explorer
full governance
CI release gates
new execution engine
automatic task retirement

==================================================
DEFINITION OF DONE
==================================================

Evalplane can take persisted valid trial evidence for an AgentVersion and
report reproducible reliability at task, family and capability scopes.

It distinguishes:

success at least once
from
consistent success.

pass@k and pass^k have explicit, tested mathematical definitions.

Platform-invalid trials never count as agent failures.

Repeated attempts on one task never masquerade as task-family breadth.

Hidden-variant diversity is measured without revealing evaluator-only values.

Sample sufficiency is explicit.

Task saturation is measured separately from task difficulty.

Reliability remains evidence for QualificationPolicy rather than a replacement
for the qualification verdict.

==================================================
COMPLETION REPORT
==================================================

Report:

1. files changed
2. migration/schema
3. reliability contract/version
4. reliability policy/version
5. valid-trial definition
6. pass-rate calculation
7. pass@k definition/formula
8. pass^k definition/formula
9. supported k values
10. sample sufficiency
11. task-level reliability
12. family-level reliability
13. capability-level reliability
14. hidden-variant diversity
15. variant dominance
16. platform-invalid handling
17. task saturation
18. comparison/regression integration
19. comparability rules
20. QualificationPolicy integration/defer decision
21. planner relationship
22. persistence/reproducibility
23. API
24. frontend AgentVersion UX
25. frontend suite/trial UX
26. task saturation UX
27. security/leakage
28. mathematical test vectors
29. focused backend tests
30. TypeScript/build
31. confirmation pass@k != pass^k
32. confirmation invalid-platform excluded from agent denominator
33. confirmation retries do not fake family breadth
34. confirmation hidden variants do not leak
35. confirmation no LLM calculation/judging
36. real-runtime limitations
37. deferred work

Write to:

prompts/reports/reliability-eval-science.md

DO NOT overwrite this task file.
