CODEX: MEDIUM

Continue the existing hosted external-agent E2E investigation from the
current repository state. DO NOT restart the earlier stages and DO NOT
redesign the verifier.

Latest hosted run:

EXTERNAL VERIFIED CONTROL:
PASSED completely.

Confirmed:
- required evidence complete
- evidence validity valid
- overall VERIFIED
- external remediation authorized/executed
- desired-state action semantics
- expected v18 -> v17 transition
- outcome PASSED
- side effects NONE
- Git desired v17
- live Kubernetes v17
- Argo Synced
- desired/live invariant PASSED
- desired-state attribution succeeds
- stability STABLE
- no forward risks
- gateway boundary
- action proposed persisted
- action transition persisted
- external agent identity observed
- implementation identity pinned to observed digest

HOSTILE CONTROL:
- external-agent-hostile deployment created
- service created
- deployment became ready
- qualification-hostile pod created
- qualification-hostile pod became ready
- then the operation was canceled

The hostile control did NOT produce a verifier FAILED result.

TASK:

Investigate ONLY why the hostile control was canceled.

1. Inspect:
   - backend/e2e/run-hostile-control.sh
   - the workflow step invoking it
   - any timeout/wait/polling logic
   - qualification-hostile pod command/configuration
   - cleanup/trap behavior
   - relevant workflow logs/events if available

2. Determine the exact cancellation source.

Distinguish between:
   A. GitHub/workflow cancellation or timeout
   B. shell/process cancellation
   C. bounded wait/polling cancellation
   D. qualification pod failure
   E. verifier/application failure
   F. cleanup race
   G. something else

Do NOT guess from the message "The operation was canceled".
Trace the command that was actually running when cancellation occurred.

3. Preserve the already-proven external VERIFIED control.
Do not modify it unless the investigation proves it is directly responsible
for the hostile cancellation.

4. Preserve the hostile-control semantic objective:
   the hostile external agent must attempt a forbidden/out-of-band mutation
   and Realverax must independently detect/attribute the resulting state
   correctly and produce the expected FAILED result, while enforcing the
   intended credential/policy isolation.

5. Apply the smallest fix necessary for the cancellation.

6. Before any push, run the cheapest relevant validation available.
Do NOT run local Kind.

7. Do not redesign the verifier, external-agent protocol, provenance model,
or previously proven stages.

8. Do not commit or push.

REPORT:
- exact cancellation source
- evidence proving that source
- smallest fix
- whether hostile semantics were changed
- whether the external VERIFIED control remains untouched
- local/component validation performed
- remaining hosted proof required

IMPORTANT:
Do not report the hostile control as FAILED unless the qualification
actually reaches the verifier and produces the expected FAILED assertion.
