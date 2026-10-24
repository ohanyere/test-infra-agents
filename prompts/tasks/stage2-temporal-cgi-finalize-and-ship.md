OPENCODE: DEFAULT / MEDIUM-LEVEL REASONING

Repository:
~/projects/test-infra-agents

Branch:
infrastructure-agent-verification

TASK:
Finalize and ship ONLY the already-proven Stage 2 temporal CGI fix.

Do not redesign or re-investigate the temporal semantics.

==================================================
PROVEN ROOT CAUSE
==================================================

The hosted Stage 2 temporal failure was caused by a malformed generated CGI
script, NOT by temporal verification semantics.

The generated:

/www/cgi-bin/stage2-temporal-state

lost its executable shebang because the workflow heredoc opener accidentally
contained trailing content, causing:

#!/bin/sh

to be swallowed into the heredoc-opening line/comment.

The generated script therefore began with:

set -eu

instead of:

#!/bin/sh

BusyBox httpd attempted direct CGI execution and produced ENOEXEC, surfaced as
HTTP 500.

The first three agent actions succeeded because they were inspect/read-only
actions and did not invoke this CGI endpoint.

The fourth action:

set_desired_workload_image

successfully reached the remediation endpoint and changed the desired branch,
but its chained state read hit the broken CGI endpoint and returned HTTP 500.

The worker consequently reported:

platform: action_execution_failed: controlled action failed

The fix already made:

1. restores the CGI shebang correctly in
   .github/workflows/verification-e2e.yml;

2. improves the controlled-action failure summary in
   backend/internal/executionworker/loop.go without changing failure class or
   semantics;

3. adds a regression test in
   backend/cmd/e2equalification/positive_control_test.go proving every generated
   CGI script:
   - has a clean heredoc opener;
   - begins with #!/bin/sh.

Already validated:

- regression test fails against broken rendering and passes after fix;
- cmd/e2equalification tests pass;
- internal/executionworker tests pass;
- internal/verification tests pass;
- internal/agent tests pass;
- internal/evaluation tests pass;
- internal/actionexec tests pass;
- gofmt pass;
- go vet pass;
- git diff --check pass;
- workflow YAML parses;
- all 22 embedded workflow shell blocks pass bash -n;
- run-stage2-control.sh passes bash -n.

Stage 1 semantics changed: NO.
Stage 2 step-limit semantics changed: NO.
Stage 2 temporal proof semantics changed: NO.
No Kind was run.

==================================================
1. INSPECT CURRENT WORKING TREE
==================================================

Run:

git status --short

Then inspect the current diff for ONLY:

.github/workflows/verification-e2e.yml
backend/internal/executionworker/loop.go
backend/cmd/e2equalification/positive_control_test.go

Confirm the changes match the proven diagnosis above.

Do not modify them unless inspection reveals an actual defect.

==================================================
2. PROTECT UNRELATED WORK
==================================================

There may still be unrelated frontend/branding changes under app/.

Do NOT stage:

app/**
or any unrelated files.

Do NOT use:

git add .
git add -A
git add -u

Stage ONLY the three intended Stage 2 fix files.

==================================================
3. FINAL LIGHTWEIGHT VALIDATION
==================================================

Do not repeat expensive broad investigation.

Run only:

- the focused CGI regression test if needed;
- git diff --check;
- confirm workflow YAML parses;
- confirm changed/generated shell blocks remain syntactically valid.

No Kind.

==================================================
4. COMMIT
==================================================

If everything remains clean, commit ONLY those three files.

Suggested commit:

fix(e2e): preserve stage2 temporal CGI execution

==================================================
5. PUSH
==================================================

Push:

origin infrastructure-agent-verification

Do not make any additional source changes after pushing.

==================================================
6. REPORT
==================================================

Return:

1. staged files;
2. commit SHA;
3. push result;
4. confirmation unrelated app/frontend files were not committed;
5. confirmation Stage 1 semantics unchanged;
6. confirmation Stage 2 step-limit semantics unchanged;
7. confirmation Stage 2 temporal semantics unchanged;
8. hosted workflow run ID/URL if immediately available.

Do not start Stage 3.
The next action after this push is to observe the hosted Stage 2 qualification.
