CODEX: SOL MEDIUM

Continue the interrupted "Fix hosted E2E deployment" task from the existing
thread and current working tree. Do NOT restart the investigation.

You already established the root cause:

- The Git fixture uses alpine/git:2.49.1.
- The fixture attempted to run `httpd`, but that executable was unavailable.
- The container exited with code 127.
- Because the container was started with --rm, it disappeared.
- The later "No public port 9418/tcp" and "No such container" errors were
  secondary symptoms, not the root cause.
- The existing 9418 publication pattern itself was valid.

You already implemented the intended fix in:
.github/workflows/verification-e2e.yml

The fix adds the required HTTP runtime, starts the available HTTP server,
publishes both 9418 and 8080, preserves failed-container diagnostics, and
adds bounded readiness checks.

NOW:

1. Inspect the current working-tree diff first.
2. Do NOT redesign or rewrite the fix unless validation shows a real defect.
3. Finish the fixture-only validation:
   - build the Git fixture;
   - start it using workflow-equivalent flags;
   - verify the container stays running;
   - verify `docker port <container> 9418/tcp`;
   - verify `docker port <container> 8080/tcp`;
   - verify Git readiness;
   - verify the HTTP control endpoint;
   - clean up deterministically.

4. If Docker/Alpine package download fails because of network access,
   report that as an environment limitation rather than inventing another
   architecture.

5. Finish the focused review of the Stage 1 positive, warning, and
   inconclusive controls. Look only for concrete regressions or previously
   solved operational problems:
   - unbounded waits
   - hidden set -e failures
   - premature readiness
   - asynchronous cleanup races
   - stale version assumptions
   - shared mutable fixture state
   - premature Prometheus evidence

6. Validate:
   - workflow YAML
   - bash syntax for relevant scripts
   - git diff --check

7. Confirm that unrelated `app/` branding/UI changes remain untouched and
   unstaged.

8. Report:
   - root cause
   - current fix
   - fixture validation
   - 9418 result
   - 8080 result
   - control-review findings
   - validation results
   - exact files changed

Do NOT:
- run Kind
- modify verifier/evaluator semantics
- modify evidence-requirements:v1
- modify migration 000024
- modify the Stage 1 verdict definitions
- commit
- push
