# Frontend Product Reset — Infrastructure Agent Verification

## Product position

The customer-facing product is now presented as independent outcome
verification for infrastructure agents: inspect what an agent changed, verify
the intended outcome, identify side effects, establish stability, and surface
only evidence-backed forward risk. Simulation, qualification, coverage, and
environment machinery remain available underneath this framing.

## Routes changed

- `/` — fully reset public product narrative and example verification.
- `/workspace` — outcome-oriented overview using live agents, batches, and runs.
- `/agents` and `/agents/[id]` — verification-first agent language; exact
  versions, connection, verification history, and CI links are emphasized.
- `/scenarios`, `/scenarios/[id]`, `/scenarios/new`, `/workspace/tasks` —
  customer-facing “Tasks” renamed to “Scenarios” where compatibility permits.
- `/runs` — preserved compatibility route, now renders the verification list.
- `/runs/[id]` — preserved compatibility detail, reset around the verification
  evidence model.
- `/verifications` — new primary authenticated verification list using existing
  batch/run APIs.
- `/verifications/[id]` — new customer-facing route to the existing run-backed
  verification detail.
- `/integrations` — new honest CI-oriented setup preview.
- `/pricing` — retained, but removed from primary public navigation.

## Components and styling changed

- Rebuilt `app/page.tsx` and `app/globals.css` around editorial evidence,
  separators, concrete diffs, CI check rows, and restrained neutral surfaces.
- Updated public/product navigation, workspace sidebar, auth route detection,
  product layout metadata, product shared styles, scenario directory/detail,
  agent directory/detail, and frontend route integrity checks.
- Existing auth forms and functioning form primitives were preserved.

## Navigation before and after

Public before: Product, Tasks, Pricing, Sign in, Try now.

Public after: Product, Scenarios, Docs, Sign in, Get started.

Authenticated before: Overview, Agents, Qualification, Tasks, Activity,
Billing, with Settings secondary.

Authenticated after: Overview, Agents, Verifications, Scenarios, Integrations,
Settings. Qualification, Activity, Billing, Environment, Capability Claims,
and Coverage are absent from primary navigation; their compatible routes and
backend domains were not deleted.

## Homepage

The large editorial hero was retained. Its copy is now:

- Eyebrow: “Infrastructure agent verification”
- Headline: “Your agent changed infrastructure. Verify what happened next.”
- Primary CTA: “Start verifying”
- Secondary CTA: “See a verification”

Sections now include:

1. A clearly labeled demo verification with a concrete Kubernetes diff,
   recovered outcome, Argo CD side effect, failed stability, and forward risk.
2. “A successful API call doesn’t mean the fix worked.”
3. The vertical Baseline, Intent, Action Inspection, Outcome, Side Effects, and
   Stability verification model.
4. A separately emphasized Forward Risk definition.
5. “Run it before every merge” with an example GitHub-style check and version
   comparison.
6. “Your agent stays yours.”
7. Simple scenario rows.
8. “Your agent says it’s fixed. Verify it.”

## Palette and auth

The main background is neutral `#F7F7F5`, surfaces are white, and text/borders
remain restrained. Orange `#F05A22` is retained only as a brand/accent color.
Primary app/auth controls are near-black, with distinct green, red, amber, and
gray outcome semantics. No gradients, glass effects, decorative orbs, bento
grids, or orange page walls were added.

Auth was already structurally sound and neutral. Its centered form, near-white
background, and black primary buttons were preserved.

## Overview and Agents

Overview now answers “what is happening with my infrastructure agents?” using
live recent run records. It provides a restrained Needs Attention list, recent
verification rows, and agent rows—without qualification scores, trust scores,
or percentage KPI cards.

Agents remain first-class. Directory copy now emphasizes exact versions,
connections, verification evidence, and CI readiness. Agent detail exposes
Versions, Verifications, Connection, and CI entry points. Existing capability,
coverage, reliability, and qualification capabilities remain available;
qualification planning is collapsed and explicitly described as a legacy
downstream policy workflow.

The current API does not expose commit/source, unexpected mutation count,
side-effect count, stability-failure count, or forward-risk count per version,
so the authenticated agent view does not fabricate those values.

## Verifications and Action Inspection

The new list uses current evaluation batch and run APIs and displays Result,
Agent, Version, Source, Scenario, and Created columns. The UI maps completed,
failed, stopped, and active run states into customer-facing presentation while
retaining the source run record.

Detail is structured as:

- Intent
- Baseline
- Action Inspection
- Outcome
- Side Effects
- Stability
- Forward Risk
- Evidence / timeline

Action Inspection promotes actual trajectory events and requested/executed/
denied action totals. Evidence is rendered as time, concrete event summary,
phase/status rows. Forward Risk explicitly states that the current backend does
not produce it; the UI never infers a forecast from incomplete evidence.

## Scenarios and Integrations

Scenario catalog/detail now focuses on Objective, Environment, Protected
Resources, and Verification Expectations. Internal WorldSpec and
CompositionPlan terminology is not exposed as primary UX. Existing API names
and backend qualification-task routes remain unchanged for compatibility.

Integrations puts GitHub first and communicates repository, workflow, trigger,
required check, and connection state. It is clearly labeled “intended setup”
and “not available”; it does not claim a live GitHub App, Action, repository
connection, workflow, or required check.

## Demo and fixture data

Homepage GitOps verification, CI check, and version comparison are explicitly
labeled “DEMO DATA” or “DEMO CHECK.” The GitHub integration is labeled an
intended setup preview. Authenticated verification and overview rows use live
current APIs. No demo record is silently mixed into authenticated backend data.

## Backend concepts deliberately preserved

No backend schema, migration, API behavior, runtime, qualification, coverage,
reliability, environment, evidence, trajectory, billing, auth, or workspace
authorization code was removed. Qualification and `/runs` routes remain for
compatibility. Stage 39B work was not started.

## Validation

Passed:

```text
npm run typecheck
npm run check:routes
npm run build
```

The production build compiled, type-checked, generated all static pages, and
included `/verifications`, `/verifications/[id]`, and `/integrations`.

Final `git diff --check` and status are recorded in the completion response.

## Limitations

- Forward Risk has no current backend contract and is unavailable in live
  authenticated verification details.
- GitHub/CI integration is presentation-only and clearly labeled unavailable.
- Existing backend status vocabulary does not natively contain the full
  VERIFIED/FAILED/WARNING/INCONCLUSIVE product vocabulary; current run states
  are presented conservatively.
- Source/commit metadata and structured resource diffs are not available on the
  current run list API. The UI uses trajectory summaries/events where present.
