Inspect the Stage-34 trajectory implementation and the appended correction in:

prompts/reports/trajectory-evidence-integrity.md

Specifically inspect:

- backend/internal/trajectory/project.go
- view semantics for agent_visible vs evaluator
- unknown/future worker_* lifecycle projection
- existing trajectory projection tests

This is a final tiny Stage-34 semantic correction.

PROBLEM:

An unknown future `worker_*` event without platform classification currently
projects as:

actor = platform
view = agent_visible

Worker/execution lifecycle evidence is internal Evalplane/platform evidence.
It must not be labeled agent_visible unless there is actual persisted evidence
that the agent received that event.

Requirements:

1. Unknown/non-invalid worker lifecycle events remain visible in the customer
   trajectory as safe platform lifecycle evidence.

2. They must use the non-agent/evaluator-side view already established by the
   trajectory contract.

3. Do not introduce a new view vocabulary unless genuinely necessary.
   Prefer the existing evaluator/internal side if the current contract has only:

   agent_visible
   evaluator

4. Do not change:
   - actor = platform
   - persisted platform-invalid classification logic
   - worker recovery vs service recovery separation
   - requested vs executed behavior
   - trial validity
   - redaction
   - service recovery timestamps

5. Add focused tests proving:

   - unknown worker_* without platform classification stays visible
   - actor remains platform
   - it is NOT tagged agent_visible
   - it does not become platform_invalid
   - it does not imply service recovery
   - known platform-classified worker recovery still behaves as before
   - no evaluator-only/secret leakage

Run only:

go test ./internal/trajectory/... -count=1

and go vet on the trajectory package.

Do not run go test ./...
Do not start Docker/Kubernetes.

Append a final correction section to:

prompts/reports/trajectory-evidence-integrity.md

Report:
- root cause
- final view semantics
- unknown worker lifecycle behavior
- known classified worker behavior
- files changed
- focused test result

Do not change unrelated code.
