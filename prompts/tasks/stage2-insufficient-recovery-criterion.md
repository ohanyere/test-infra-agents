CODEX: MEDIUM

Continue the existing Stage 2 investigation. Do NOT restart Stage 2 and do NOT redesign anything.

The latest hosted E2E result has one narrow failure:

Stage 2 step-limit: PASSED
Stage 2 temporal: PASSED

Stage 2 insufficient:
  "incomplete recovery evidence INCONCLUSIVE"        PASSED
  "recovery criterion inconclusive"                  FAILED

The qualification pod itself completed the full lifecycle:
worker execution
→ final snapshot
→ persistence
→ cleanup
→ persisted proof fetch
→ qualification proof

So this is not an environment/bootstrap/cleanup failure.

We need to determine whether this is:

A. a real Stage 2 verifier/evaluator bug where the overall result becomes INCONCLUSIVE but the affected recovery criterion is not itself INCONCLUSIVE despite incomplete required evidence,

or

B. an E2E assertion bug where the criterion is actually INCONCLUSIVE but run-stage2-control.sh reads/asserts the wrong field.

Do not weaken the assertion merely to make CI green.

Investigate only this insufficient-evidence control.

1. Inspect:
   - backend/e2e/run-stage2-control.sh
   - the insufficient Stage 2 qualification scenario/control definition
   - the persisted verification-result/proof JSON from the failed run if available in artifacts
   - the Stage 2 recovery criterion/evidence model
   - the evaluator/temporal recovery derivation that maps missing evidence to criterion status.

2. Establish the ACTUAL persisted value of:
   - overall verification status
   - each recovery criterion status
   - the reason/evidence state for the incomplete criterion.

3. Compare that actual structure with what run-stage2-control.sh asserts.

4. If the criterion is not INCONCLUSIVE:
   identify the exact verifier code path responsible and determine the smallest semantic fix required so that missing required recovery evidence makes that criterion INCONCLUSIVE.

5. If the criterion is already INCONCLUSIVE:
   fix only the E2E assertion/path used to read it.

6. Preserve the passing Stage 2 temporal control and Stage 1 contracts. Do not change them.

7. Add or update a focused regression test for this exact distinction:
   required recovery evidence complete → criterion can become VERIFIED/PASSED;
   required recovery evidence incomplete → criterion is INCONCLUSIVE;
   overall result remains INCONCLUSIVE.

8. Do not run Kind locally.
9. Do not commit or push.
10. Do not perform broad refactoring.

Report the exact actual criterion value before making any change.
