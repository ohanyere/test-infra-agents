# Product Doctrine + Frontend Alignment

## 1. Repository state

- Branch: `infrastructure-agent-verification`
- Starting HEAD: `8eec3e38ef537b04fbbab39100499c9652fa5b42`
- Initial status:
  - modified: `backend/cmd/e2equalification/main.go`
  - untracked: `.github/workflows/verification-e2e.yml`
  - untracked: `prompts/reports/github-hosted-e2e-verification.md`
  - untracked: `prompts/reports/live-e2e-verification-proof.md`
- Those E2E changes were treated as legitimate pre-existing work and preserved.
- No suspicious unrelated modifications were present at the start.

## 2. Canonical doctrine

`docs/PRODUCT_DOCTRINE.md` is the authoritative, durable product doctrine. The engineering reference in `backend/README.md` points to it without duplicating it. One navigation-definition comment also points product-surface decisions to the doctrine.

North star:

> VERIFY AUTONOMOUS INFRASTRUCTURE CHANGES.

Supporting statement:

> Independently prove that an autonomous infrastructure change worked, left the system stable, preserved critical invariants, and did not introduce unacceptable risk.

The doctrine fixes Kubernetes/SRE remediation verification as the initial wedge, treats scenarios as development test infrastructure, and makes the verifier—not simulation machinery—the product.

## 3. Customer-facing route audit

Classification: **A CORE**, **B SUPPORTING**, **C LEGACY / HIDDEN**, **D MISALIGNED**.

| Route | Class | Treatment |
|---|---|---|
| `/` | A CORE | North-star positioning, canonical failed GitOps demo, verification model, and honest development/planned-production modes. |
| `/signup`, `/login`, `/onboarding` | B SUPPORTING | Access and setup; onboarding now asks what infrastructure changes need verification. |
| `/workspace` | A CORE | Authoritative result distribution, resulting-state issues, and recent verifications. |
| `/agents`, `/agents/new`, `/agents/[id]` | A CORE | Exact actor/version identity, connection, resulting-state history, and supporting Tests. Legacy capability/reliability UI is demoted. |
| `/verifications`, `/verifications/[id]` | A CORE | Core list and detail surfaces for independently verified infrastructure changes. |
| `/integrations` | A CORE | Truthful evidence-source availability and roadmap. |
| `/workspace/settings` | B SUPPORTING | Workspace access and operational settings. |
| `/scenarios`, `/scenarios/new`, `/scenarios/[id]` | B SUPPORTING | URLs preserved; customer labels and explanations now present these as development Tests. |
| `/workspace/tasks`, `/workspace/tasks/[id]` | B SUPPORTING | Authenticated aliases for development Tests. |
| `/runs`, `/runs/[id]` | B SUPPORTING | `/runs` aliases Verifications; detail is the implementation behind verification detail. |
| `/runs/[id]/trajectory` | C LEGACY / HIDDEN | Detailed internal evidence/trajectory surface; reachable from detail but not primary navigation. |
| `/qualification`, `/qualification/suites/[id]`, `/suite-executions/[id]` | C LEGACY / HIDDEN | Qualification machinery retained without primary product emphasis. |
| `/evaluations/[id]` | C LEGACY / HIDDEN | Compatibility redirect to the run/verification record. |
| `/comparisons/[id]` | C LEGACY / HIDDEN | Existing evaluator-era regression comparison; not primary navigation. |
| `/environments` | C LEGACY / HIDDEN | Runtime inventory retained; no longer a product pillar. |
| `/workspace/billing` | C LEGACY / HIDDEN | Operational/legacy plan surface, absent from primary navigation. |
| `/pricing` | C LEGACY / HIDDEN | Existing trial-credit pricing model is not linked from primary public navigation and needs a later commercial-model decision. |
| `/runs/new` | C LEGACY / HIDDEN | Compatibility redirect into qualification machinery. |

No route remains classified D in the primary experience after this pass. Some direct legacy routes still use their accurate internal terms; they are recorded gaps rather than deleted or falsely relabeled.

## 4. Navigation before and after

Before, authenticated primary navigation promoted `Scenarios` alongside Overview, Agents, and Verifications. Public navigation also promoted Scenarios.

After, authenticated primary navigation is:

1. Overview
2. Agents
3. Verifications
4. Integrations
5. Settings

Public navigation is Product, How it works, and Docs. Scenario URLs remain available as supporting Tests, but Scenarios is not a top-level company/category signal. Qualification, evaluation, activity, billing, environments, comparisons, and trajectory remain outside primary navigation.

## 5. Homepage alignment

Before, the homepage had verification-oriented styling and some useful concepts but still shared emphasis with test scenarios and version-comparison mechanics.

After:

- Eyebrow: `Autonomous infrastructure verification`
- Headline: `Verify autonomous infrastructure changes.`
- Primary CTA: `Start verifying`
- Secondary CTA: `See a verification`
- Supporting copy promises independent proof of outcome, stability, invariants, and risk—not agent self-reporting.
- The first product proof is explicitly a `DEMO VERIFICATION` in which the agent patches live checkout from v18 to v17 and reports success, while independent verification finds Git/live divergence, Argo OutOfSync, non-durability, and reconciliation risk. Final result: `FAILED`.
- The verification model is presented sequentially as Before State, Intent, Actual Change, Outcome, Invariants, Collateral Effects, Stability, and Residual Risk.
- Development and planned production modes are distinct. Production collection is not claimed as available.
- The existing visual system, editorial layout, restrained palette, typography, and component structure were preserved.

## 6. Scenario treatment

Scenario persistence, APIs, WorldSpec/composition support, hidden variants, runtime projections, and authoring remain intact. Customer-facing navigation and copy call them `Tests`, `Test Cases`, or `Development Tests` and explain that they create reproducible infrastructure situations for verification. Existing `/scenarios` URLs remain to avoid needless route migration.

## 7. Authenticated product changes

### Overview

Overview now loads authoritative `VerificationResult` records for terminal trials. It never converts a completed trial into `VERIFIED`. Missing projector data is labeled `LEGACY`. Status totals and resulting-state issues are derived only from real verification results; unavailable aggregates are omitted rather than fabricated.

### Agents

The directory emphasizes exact versions, verification history, supporting tests, and connection state. Agent detail asks what states the version leaves behind and links Overview, Versions, Verifications, Tests, and Connection. Capability claims, qualification planning, and evaluator-era reliability remain available as explicitly legacy supporting machinery; they are not represented as intelligence scores or verification results.

### Verification list

The core list prioritizes `VERIFIED`, `WARNING`, `FAILED`, and `INCONCLUSIVE`, with Agent, Version, Objective/Test, and Created. Unsupported source, environment, and commit metadata are not fabricated.

### Verification detail

The evidence hierarchy now follows the doctrine:

1. Verification result and reason
2. Intent
3. Pre-change state
4. Action attribution: requested, authorized, executed
5. Actual infrastructure delta
6. Outcome
7. Invariants
8. Collateral effects
9. Stability
10. Residual risk
11. Evidence/timeline

`Forward Risk` remains the backend/API contract while the UI says `Residual risk`. `Side Effects` remains the backend contract while the UI says `Collateral effects`. The Invariants section is intentionally honest: the current API has no first-class invariant result, so the UI explains that existing protected-resource enforcement and assertions contribute elsewhere without inventing a pass.

### Integrations

The page now describes evidence sources, with statuses grounded in current code:

- Kubernetes — **AVAILABLE** in the controlled development runtime
- Prometheus — **AVAILABLE** for scenario-scoped telemetry
- GitHub / CI — **IN DEVELOPMENT**; internal E2E workflow work is not called a customer integration
- Argo CD / Git — **PLANNED**
- Terraform — **PLANNED**
- Cloud audit/state — **PLANNED**

Production verification is explicitly planned; no production collector or fake telemetry was added.

## 8. Terminology changes

Primary product language now favors autonomous infrastructure change, resulting state, independent verification, actual infrastructure delta, invariant, collateral effect, stability, residual risk, and evidence. Scenario becomes Test in customer-facing development flows. Baseline becomes Pre-change state, Side Effects becomes Collateral effects, and Forward Risk becomes Residual risk in UI copy only.

Accurate internal/API names such as `ScenarioVersion`, evaluator, qualification, trajectory, `forward_risk`, and `side_effects` remain unchanged.

## 9. Preserved legacy machinery

No backend capability was deleted. This pass preserves WorldSpec, CompositionPlan, environment composition, scenario persistence, evaluator/evidence/trajectory systems, qualification, coverage, reliability, regression, billing, MCP, action execution, hidden variants, and existing route compatibility. The pre-existing E2E projector wiring was not rewritten.

## 10. Backend-to-doctrine capability map

Strict classification is based on executable source, migrations, and current API contracts—not planned architecture.

| Doctrine capability | State | Current evidence and limit |
|---|---|---|
| PRE-CHANGE STATE | IMPLEMENTED | Verification snapshots persist a scoped, typed Kubernetes/telemetry baseline. Scope is currently narrow but real. |
| INTENT | IMPLEMENTED | Server-derived intent persists objective, expected assertions, protected resources, and constraints from the pinned test/version. |
| ACTION ATTRIBUTION | IMPLEMENTED | Requested, authorized, and executed action records are projected; deterministic target matching produces attribution confidence. |
| ACTUAL STATE DELTA | PARTIAL | Independent snapshot differencing exists, but supported resource/field semantics are intentionally limited rather than general infrastructure state modeling. |
| OUTCOME | IMPLEMENTED | Deterministic evaluation uses Kubernetes and Prometheus evidence rather than the action response. It is implemented for supported scenarios. |
| INVARIANTS | PARTIAL | Protected-resource rules, forbidden behavior, safety findings, and assertions cover parts of invariants; there is no general first-class invariant result or GitOps desired-state invariant engine. |
| COLLATERAL EFFECTS | PARTIAL | Scoped unexpected/unattributed changes and side-effect findings exist; broad neighboring/dependency discovery and consequence modeling do not. |
| STABILITY | IMPLEMENTED | Bounded post-outcome observations and deterministic stability status exist for the supported runtime evidence. |
| RESIDUAL RISK | PARTIAL | Production risk analyzers run, but the present analyzer set is narrow (notably HPA/capacity consistency) and lacks controller/GitOps risk. |
| VERIFICATION RESULT | IMPLEMENTED | Server-derived, versioned, persisted result with validity, overall status, evidence, and API retrieval exists. |
| LONGITUDINAL STATE QUALITY | PARTIAL | Version, reliability, regression, and history machinery exists, but evaluator-era aggregates are not yet authoritative longitudinal VerificationResult analysis. |
| PRODUCTION VERIFICATION | MISSING | There is no production change-window collector or customer-runtime evidence ingestion path. The current real runtime path is controlled development E2E. |

## 11. Feature inventory under the doctrine

| Concept | Decision | Reason |
|---|---|---|
| Verifications | KEEP | Core record of an autonomous infrastructure change and resulting-state quality. |
| Agents and exact versions | KEEP | Necessary actor identity and longitudinal comparison axis. |
| Evidence, snapshots, evaluator, action execution | KEEP | Directly detect and prove infrastructure consequences. |
| Scenarios | RENAME in UI / DEMOTE | Supporting development Tests, not the company category. APIs and persistence stay. |
| Qualification | HIDE from primary UI | Useful policy/workflow machinery, but communicates generic agent evaluation when promoted. |
| Coverage | DEMOTE | Retained inside collapsed legacy agent detail; not a product-level outcome. |
| Reliability | DEMOTE | Retained as legacy trial evidence and explicitly not an intelligence score or VerificationResult. |
| Regression | DEMOTE | Potentially useful once rebuilt around resulting states; existing evaluator-era route remains hidden. |
| Activity/runs | RENAME or HIDE | Customer-facing list is Verifications; route compatibility remains. |
| Costs/Billing | HIDE | Operational support, not product identity; current credit model needs later commercial review. |
| Environment inventory | HIDE | Runtime support, not a product pillar. |
| Capability claims | DEMOTE | Connection/operation scope can help setup, but self-claims are not proof of outcomes. |
| Generic scores/intelligence | REMOVE FROM PRIMARY UI ONLY | They answer the wrong product question. No backend deletion was performed. |
| Production collector | DEFER | Important doctrine direction but not implemented or fabricated in this alignment pass. |
| GitOps/Argo verification | DEFER | Strong first-proof target; current evidence contracts do not implement it yet. |
| Terraform/cloud providers | DEFER | Explicitly outside the initial wedge until Kubernetes/SRE verification is proven. |

## 12. Validation

Validation results are recorded after the final local pass:

- `npm run typecheck` — PASS (`tsc --noEmit`)
- `npm run check:routes` — PASS (`18 critical routes present; customer mock imports absent; TESTED coverage source guarded`)
- `npm run build` — PASS (Next.js 15.5.25 production build; 22/22 static pages generated)
- `git diff --check` — PASS

The first typecheck attempt stalled locally and was interrupted; a bounded retry surfaced the removed `scenarios` navigation key still being used by the test-authoring page. That page was corrected, after which typecheck passed. The first bounded build compiled successfully in 9.1 minutes but its ten-minute wrapper expired during the subsequent type phase. A cached retry completed successfully end to end. These preliminary attempts are not represented as passes; the final commands above are.

No Kind E2E was run. No backend test was required because this pass made no shared backend type/API or product capability change.

## 13. Remaining product inconsistencies

- First-class invariant projection is not implemented; the detail page reports the gap rather than fabricating evidence.
- Git desired state, Argo CD sync state, reconciliation durability, and controller-risk evidence are not implemented. The homepage example is clearly labeled a demo.
- Production verification collection is not implemented and is labeled planned.
- Actual-delta and collateral-effect semantics remain scoped to currently supported resource observations.
- Longitudinal agent views still rely partly on evaluator-era reliability/qualification structures; authoritative VerificationResult-based trends need backend aggregation before they can be shown truthfully.
- Direct legacy pricing, qualification, suite, regression, environment, billing, and trajectory routes still contain accurate legacy-domain language. They are hidden from primary navigation, not deleted.

## 14. Final product check

1. **YES** — the homepage immediately says “Verify autonomous infrastructure changes.”
2. **YES** — the GitOps demo shows agent success and independent verification failure.
3. **YES** — primary surfaces emphasize resulting state, deltas, evidence, invariants, stability, and risk rather than reasoning.
4. **YES** — scenarios are presented as supporting Tests and removed from primary navigation.
5. **YES** — Verifications is the core authenticated result surface.
6. **YES, with an honest limitation** — the full model is understandable; invariants are visibly not yet first-class backend evidence.
7. **YES** — generic qualification/evaluation language is confined to hidden legacy machinery.
8. **YES** — production verification is labeled planned.
9. **YES** — the canonical doctrine includes product/frontend/backend gates and an expansion rule.

## 15. Files and final state

Files changed by this product-alignment pass:

- `docs/PRODUCT_DOCTRINE.md`
- `backend/README.md`
- `app/page.tsx`
- `app/site-navigation.tsx`
- `app/workspace/layout.tsx`
- `app/workspace/page.tsx`
- `app/workspace/tasks/page.tsx`
- `app/agents/agent-directory.tsx`
- `app/agents/[id]/agent-detail.tsx`
- `app/verifications/verification-list.tsx`
- `app/runs/[id]/trial-detail.tsx`
- `app/integrations/page.tsx`
- `app/onboarding/page.tsx`
- `app/onboarding/onboarding-form.tsx`
- `app/scenarios/page.tsx`
- `app/scenarios/scenario-directory.tsx`
- `app/scenarios/[id]/task-detail.tsx`
- `app/scenarios/new/page.tsx`
- `app/scenarios/new/scenario-form.tsx`
- `prompts/reports/product-doctrine-frontend-alignment.md`

Pre-existing E2E files preserved but not authored by this pass:

- `backend/cmd/e2equalification/main.go`
- `.github/workflows/verification-e2e.yml`
- `prompts/reports/github-hosted-e2e-verification.md`
- `prompts/reports/live-e2e-verification-proof.md`

Final tracked working-tree diff stat (includes the pre-existing E2E harness modification, excludes untracked files by Git design):

```text
19 files changed, 152 insertions(+), 75 deletions(-)
```

The two untracked files created by this pass contain 454 lines total: 235 in the doctrine and 219 in this report before this final appendix.

Final status:

```text
 M app/agents/[id]/agent-detail.tsx
 M app/agents/agent-directory.tsx
 M app/integrations/page.tsx
 M app/onboarding/onboarding-form.tsx
 M app/onboarding/page.tsx
 M app/page.tsx
 M app/runs/[id]/trial-detail.tsx
 M app/scenarios/[id]/task-detail.tsx
 M app/scenarios/new/page.tsx
 M app/scenarios/new/scenario-form.tsx
 M app/scenarios/page.tsx
 M app/scenarios/scenario-directory.tsx
 M app/site-navigation.tsx
 M app/verifications/verification-list.tsx
 M app/workspace/layout.tsx
 M app/workspace/page.tsx
 M app/workspace/tasks/page.tsx
 M backend/README.md
 M backend/cmd/e2equalification/main.go
?? .github/workflows/verification-e2e.yml
?? docs/PRODUCT_DOCTRINE.md
?? prompts/reports/github-hosted-e2e-verification.md
?? prompts/reports/live-e2e-verification-proof.md
?? prompts/reports/product-doctrine-frontend-alignment.md
```

No commit or push was performed.
