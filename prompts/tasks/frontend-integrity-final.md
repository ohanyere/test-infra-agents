EVALPLANE — STAGE 36 FINAL FRONTEND INTEGRITY AUDIT

Read AGENTS.md first.

Read:

prompts/tasks/complete-customer-frontend.md
prompts/reports/complete-customer-frontend.md

This is NOT a Stage-36 rewrite.

Inspect only the two issues below.

==================================================
1. AUTHENTICATED TASKS NAVIGATION
==================================================

The Stage-36 report states:

- authenticated navigation includes Tasks
- task-catalog entry surfaces retain marketing navigation

Inspect the actual route/layout/navigation behavior.

Requirement:

A signed-in customer who clicks Tasks from the authenticated product navigation
must remain within a coherent authenticated product experience.

The Tasks surface may continue using the existing safe
/v1/qualification-tasks backend data.

Do not unnecessarily duplicate the task catalog.

Acceptable solutions include reusing the existing route with the product shell
or introducing a thin authenticated product route/alias if that matches the
existing architecture better.

Preserve public task discovery if intentionally supported.

Do not break public anonymous access merely to fix authenticated navigation.

The critical invariant is:

authenticated product navigation
→ Tasks
→ still feels and navigates as part of Evalplane customer product

not:

authenticated product
→ Tasks
→ marketing-site shell with no coherent return path.

If the current implementation already handles this correctly, make no
production change and document why.

==================================================
2. TESTED SEMANTICS
==================================================

The completion report says:

"TESTED comes from Stage-26 coverage evidence."

Verify the actual implementation.

Canonical architecture:

Stage 26:
Capability & Claim Model

Stage 27:
Task Families & Capability Coverage

CLAIMED:
exact AgentVersion declares the capability.

TESTED:
required independent task-family/case/trial coverage has been exercised under
the server-owned capability-coverage contract.

QUALIFIED:
qualification policy passes.

The frontend MUST NOT infer TESTED from:

- claim presence
- raw run existence
- one successful Trial
- arbitrary frontend counts

It must consume the authoritative backend coverage semantics.

Preserve existing Stage-27 rules, including where applicable:

- independent Task Family breadth
- required cases/trials
- valid failures may still count as exercised coverage
- platform-invalid trials do not count as valid capability coverage
- hidden-variant repetition is depth, not independent family breadth

Do not reimplement the coverage algorithm in React.

If the implementation already consumes the correct backend coverage contract,
do not change production code. Correct the Stage-36 report wording from
Stage-26 coverage to Stage-27 capability coverage.

If it is actually deriving TESTED incorrectly, make only the minimum frontend
change needed to use authoritative server coverage.

==================================================
3. ROUTE COUNT REPORT CHECK
==================================================

The report says "All 25 listed routes compiled."

Verify the count against the actual route inventory/build output.

Correct the report if this is merely a documentation count mismatch.

Do not add a route just to make the number 25.

==================================================
4. DO NOT CHANGE
==================================================

Do not change:

- qualification planner
- reliability formulas
- qualification-policy semantics
- trajectory semantics
- billing
- environment scheduler
- backend capability-coverage rules
- AgentVersion model
- task safe projection
- Regression logic

No new backend subsystem.

No Docker.
No Kubernetes.

==================================================
5. VERIFICATION
==================================================

Run:

npx tsc --noEmit
npm run build

If no backend code changes, do not run backend test suites.

==================================================
6. REPORT
==================================================

Append a final integrity section to:

prompts/reports/complete-customer-frontend.md

Report:

1. actual authenticated Tasks navigation behavior
2. whether production code changed
3. final TESTED source/semantics
4. whether production code changed
5. route inventory/build count correction if needed
6. TypeScript result
7. production build result
8. confirmation CLAIMED != TESTED != QUALIFIED
9. confirmation no frontend coverage algorithm was introduced

Do not overwrite the original task file.
