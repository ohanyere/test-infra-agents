OPENCODE: DEFAULT / MEDIUM REASONING

Repository:
~/projects/test-infra-agents

Branch:
infrastructure-agent-verification

TASK:
Diagnose and fix ONLY the hosted external-agent qualification failure where
required Git desired-state evidence is unavailable.

DO NOT redesign the verifier.
DO NOT redesign evidence applicability.
DO NOT change Stage 1 semantics.
DO NOT start Stage 2/3/4/5 hardening.
DO NOT change the external-agent protocol.
DO NOT change verdict semantics.

==================================================
CURRENT FAILURE
==================================================

The hosted external-agent proof progressed through essentially the entire
qualification lifecycle:

- external-agent deployment created
- qualification pod created
- database/auth/registration succeeded
- scenario/batch/run creation succeeded
- worker construction succeeded
- environment setup succeeded
- baseline capture succeeded
- telemetry started
- external agent executed all 7 actions
- final snapshot succeeded
- stability observation succeeded
- persistence succeeded
- cleanup succeeded
- persisted proof fetch succeeded

The verifier then correctly refused to claim a complete proof because required
Git desired-state evidence was unavailable.

Observed evidence assessment:

git_desired_state:
  applicability: APPLICABLE
  requirement_status: REQUIRED
  availability: UNAVAILABLE
  source_provider: git
  reason:
    "Required final Git desired state evidence was unavailable.
     Collector reason: git_desired_state_unavailable."

The resulting attribution was correctly:

action-attribution:v1
status: INCONCLUSIVE

The desired-live-state-consistency invariant was correctly:

INCONCLUSIVE

Overall verification status was correctly:

INCONCLUSIVE

Therefore the verifier behavior is NOT the problem.

==================================================
IMPORTANT PRIOR CI FAILURE
==================================================

Immediately before this, CI failed while building/running the Git fixture with:

Error: No public port '9418/tcp' published for <container-id>

followed by:

Error response from daemon:
No such container: <container-id>

The Git fixture Docker image itself successfully built:

evalplane-git-fixture:e2e

and the Dockerfile installs git-daemon and copies the fixture repository.

Therefore investigate whether the Git desired-state collector failure is caused
by the Git fixture/container/network/port lifecycle.

Port 9418 is especially important because the fixture uses git-daemon.

==================================================
USE PREVIOUS E2E KNOWLEDGE
==================================================

Before changing anything, inspect the repository history/current code for the
previous known-good Git fixture implementation and previous E2E fixes.

Specifically inspect:

- Git fixture Dockerfile
- Git fixture entrypoint
- Git fixture startup code
- run-external-control.sh
- any previous canonical E2E Git fixture startup
- verification-e2e.yml
- Git desired-state collector implementation
- gitops evidence collection code
- previous hosted-E2E reports/prompts
- git history for prior Git fixture/port/lifecycle fixes

We previously had a working hosted GitOps E2E path. Reuse its proven
implementation pattern where applicable.

Do NOT blindly copy old behavior if it conflicts with the current external-agent
architecture. The purpose is to avoid reintroducing failures we already solved.

==================================================
DIAGNOSTIC QUESTIONS
==================================================

Determine exactly:

1. How is the Git fixture container started in the current external-agent
   qualification?

2. How is port 9418 published?

3. How does the workflow obtain the actual host/public port?

4. Does the current external-agent environment expect the Git collector to
   connect to:
   - localhost,
   - a Docker-published host port,
   - a service/container hostname,
   - or another endpoint?

5. Is the Git fixture container still alive when the Git desired-state
   collector runs?

6. Why did the earlier CI step report:
   "No public port '9418/tcp' published"
   and then:
   "No such container"?

7. Why does the final GitOps evidence collector specifically report:
   git_desired_state_unavailable?

8. Is the collector receiving an incorrect/unreachable Git endpoint?

9. Is the Git repository itself healthy and serving the expected branch/revision?

10. Is this a startup/lifecycle/port-discovery problem rather than a verifier
    problem?

==================================================
REQUIRED DEBUGGING
==================================================

Trace the actual runtime path from:

Git fixture startup
    ->
Git daemon listening
    ->
port publication/discovery
    ->
Git endpoint passed into the qualification environment
    ->
Git desired-state collector
    ->
final Git desired-state evidence
    ->
action attribution

Do not stop at static inspection.

If possible, add or improve bounded diagnostics so the next hosted run
identifies:

- Git fixture container ID
- published host port
- container port
- Git endpoint used by the collector
- Git fixture health/readiness
- repository/ref being queried
- exact collector error

Do not print secrets.

==================================================
CONSTRAINTS
==================================================

No local Kind.

Do not touch any remote AWS/EKS environment.

Do not change migration SQL.

Do not change evidence-requirements:v1 semantics.

Do not change action-attribution:v1 semantics.

Do not weaken the requirement that missing required Git evidence produces
INCONCLUSIVE.

Do not replace missing Git evidence with baseline evidence.

Do not make the verifier assume Git evidence is available.

Do not modify unrelated frontend/branding files.

Do not stage or commit anything.

==================================================
VALIDATION
==================================================

Run only bounded, relevant local validation.

Then report:

1. root cause
2. exact runtime path that failed
3. why the previous Git fixture implementation worked
4. what changed in the current implementation
5. minimal fix
6. tests/validation performed
7. exact files changed
8. whether another hosted E2E run is now required

IMPORTANT:
Do not claim the external-agent proof is fixed unless the actual root cause is
identified and the fix is validated.

Do not start another broad architectural investigation.
