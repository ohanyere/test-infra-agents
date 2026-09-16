Read prompts/reports/trajectory-evidence-integrity.md and inspect:

- backend/internal/trajectory/project.go
- IsWorkerRecoveryCode
- worker event projection
- existing executionworker recovery event payload/classification
- trialValidity/platform classification logic

This is a very narrow Stage-34 integrity correction.

The current report says all known worker recovery codes PLUS ANY FUTURE
`worker_*` code project as:

event_type=platform_invalid
status=invalid

Do not infer platform-invalid solely from the `worker_*` prefix.

The persisted execution/platform classification remains authoritative.

Requirements:

1. Keep the known Stage-18 recovery codes recognizable as worker lifecycle
   evidence.

2. They may project as platform-invalid only when the existing persisted
   evidence/classification says the event is platform-class/invalid according
   to current validity semantics.

3. A future or unknown `worker_*` event without platform-invalid
   classification must remain visible as safe platform/execution lifecycle
   evidence, but must not manufacture:
   - platform_invalid
   - invalid status
   - agent failure
   - service recovery

4. Do not use prefix matching as a failure-classification rule.

5. Prefix matching may be used only to identify the event as belonging to
   worker/platform lifecycle for display, if needed.

6. Do not change the already-correct service-recovery rules:
   worker events must never set recovery_detected_at, recovery_verified_at,
   time_to_recovery, or actions_after_recovery.

Add focused tests:

- known worker recovery + persisted platform classification projects as
  platform-invalid evidence
- unknown/future worker_* event without platform classification does NOT
  project as platform-invalid
- unknown worker_* still cannot imply service recovery
- trial validity remains driven by existing persisted classification
- requested-vs-executed remains green
- no evaluator-only leakage

Run only:

go test ./internal/trajectory/... -count=1

and directly affected packages if required.

Do not run go test ./...
Do not start Docker/Kubernetes.

Append a short correction section to:

prompts/reports/trajectory-evidence-integrity.md

Include:
- root cause
- classification rule after patch
- known worker recovery behavior
- unknown future worker event behavior
- files changed
- focused test result

Do not change unrelated code.
