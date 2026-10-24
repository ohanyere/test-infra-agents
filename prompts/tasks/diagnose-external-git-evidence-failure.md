We need to diagnose the latest failed hosted external-agent E2E run. DO NOT implement a fix yet.

Read the current repository and the existing verifier-hardening/audit material first.

Failure from the latest external proof:

EXTERNAL PROOF ASSERTION FAILED: required evidence complete

evidence_requirements:v1:
- workload_state REQUIRED / AVAILABLE
- telemetry REQUIRED / AVAILABLE
- git_desired_state REQUIRED / UNAVAILABLE
  source_provider: git
  reason: "Required final Git desired state evidence was unavailable. Collector reason: git_desired_state_unavailable."
- argo_state REQUIRED / AVAILABLE
- stability REQUIRED / AVAILABLE

The persisted result was:
overall_status: INCONCLUSIVE
validity: invalid_platform

Action execution:
- 7 requested
- 7 authorized
- 7 executed
- 0 failed
- mutating action:
  set_desired_workload_image
  target Deployment/eval-aa91db14f9e14e5cb4b0df2152f6cbe2/eval-reference-app
  requested image: evalplane-reference:v17
  status: executed

Final live workload:
eval-reference-app image = evalplane-reference:v17

GitOps:
- Argo application: checkout-gitops-proof-external
- sync_status: Synced
- health_status: Healthy
- target_revision: external-main
- observed_revision: e59a8ae4189d7f38ab8704076c338040b8b9b0d9
- automated_reconciliation: true
- self_heal: false

Action attribution:
action-attribution:v1
status: INCONCLUSIVE
reason:
"At least one required workload action lacks complete causal attribution evidence."

Specifically:
"The action lacks complete Git desired-state transition evidence."

Infrastructure diff:
Deployment image changed v18 -> v17
classification: UNATTRIBUTED
attribution: INCONCLUSIVE

The run otherwise completed all major phases:
external-agent deployment
external agent pod readiness
qualification setup
agent registration
scenario creation
run start/claim
worker construction
environment setup
baseline capture
agent execution
traffic completion
stability observation
final snapshot
persistence
cleanup
proof fetch
process exit

Important architectural context from the recent audit:
- Required evidence applicability was intentionally introduced in Stage 1.
- Missing required evidence MUST produce INCONCLUSIVE.
- Baseline Git evidence MUST NOT be substituted for missing final Git evidence.
- Stage 3 is intended to strengthen causal action attribution.
- Stage 5 is intended to resolve the action-semantic split between live mutation and desired-state/Git remediation.
- The audit specifically identified that qualification controls have historically changed the semantics of set_workload_image between live Deployment patching and Git update + Argo reconciliation.
- The previous successful canonical GitOps E2E already established how the Git fixture, Git desired-state observation, Argo reconciliation, and desired/live evidence worked. Reuse that knowledge; do not redesign it blindly.

Your task is investigation only.

1. Trace the code path that collects final git_desired_state evidence.
2. Identify the exact condition that produces:
   git_desired_state_unavailable
3. Trace the external qualification path for set_desired_workload_image from agent request -> authorization -> executor -> Git mutation -> Git fixture -> collector -> final evidence.
4. Compare that path with the previously working canonical GitOps E2E path.
5. Inspect the Git fixture/server configuration, repository/ref/branch configuration, collector configuration, and any environment variables involved.
6. Determine whether this is:
   a. Git collector/configuration failure,
   b. Git fixture availability/connectivity failure,
   c. missing Git transition/evidence emission by the external action executor,
   d. action semantic mismatch,
   e. branch/ref/repository mismatch,
   f. another concrete issue.
7. Check whether the earlier Git fixture port/publication problem could be related, but only conclude that if repository evidence supports it.
8. Do not weaken the evidence requirement.
9. Do not make Git evidence optional.
10. Do not fall back to baseline Git state.
11. Do not change verdict semantics.
12. Do not redesign Stage 3 or Stage 5 yet.
13. Do not touch unrelated frontend/branding files.
14. Do not run Kind.
15. Do not mutate any remote EKS environment.
16. Do not commit or push.

Use read-only inspection and repository analysis.

At the end, report:

A. Exact root cause
B. Exact files/functions involved
C. Why the previous canonical GitOps E2E worked
D. Why the external-agent path currently does not
E. Smallest correct fix
F. Whether the fix belongs to Stage 1, Stage 3, Stage 5, or external-agent qualification plumbing
G. What hosted assertion should prove the fix

Do not implement the fix until I explicitly approve the diagnosis.
