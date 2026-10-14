CODEX: MEDIUM

The investigation is complete.

The root cause is confirmed: the GitHub Actions job-level timeout of 40 minutes cancels the hostile control before it reaches the verifier. The verifier/control semantics are unchanged.

The intended fix is the already identified one-line workflow change:

.github/workflows/verification-e2e.yml
timeout-minutes: 40 → 60

Do not redesign the hostile control.
Do not change verifier semantics.
Do not change the external VERIFIED control.
Do not add retries or arbitrary sleeps.

First show the current diff and validate:
- workflow YAML
- bash syntax for the hostile script
- git diff --check

Then stop and report the exact diff.

Do not commit.
Do not push.
Do not run Kind.
