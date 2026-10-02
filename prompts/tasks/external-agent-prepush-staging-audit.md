The external-agent independence milestone is implemented.

Do NOT add more features and do NOT redesign anything.

Your report says the only remaining blocker is hosted proof:
- external independent agent -> VERIFIED
- hostile independent agent -> FAILED
- identity pinning observed from the running pod image
- credential isolation
- policy denials
- zero forbidden live mutations
- desired-state attribution
- existing positive-control regression remains green

We now need to prepare this exact implementation for the hosted truth event.

1. Inspect the current working tree:
   git status --short
   git diff --stat

2. Verify that no unrelated app/, frontend, branding, or previous user work will be staged.

3. Run the final cheap pre-push checks only:
   - gofmt on changed Go files
   - relevant focused Go tests if not already current
   - workflow YAML parse
   - bash -n for changed/new shell scripts and workflow run blocks
   - git diff --check

Do NOT run Kind locally.

4. Show me the exact files belonging to this external-agent milestone.

5. Stage ONLY those milestone files.
   Never use:
   git add .
   git add -A
   git add -u

6. After staging, show:
   git status --short
   git diff --cached --stat
   git diff --cached --check

7. Do NOT commit or push yet.

Report:
- exact staged file list
- any unrelated files left unstaged
- test/check results
- whether anything remains that should prevent the hosted E2E run

Do not modify code unless one of these final checks exposes a concrete defect.
