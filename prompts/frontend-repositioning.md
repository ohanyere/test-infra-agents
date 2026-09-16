Frontend repositioning implemented exactly as specified. No backend, DB, or evaluation semantics touched.

**1. Files changed**
- `app/page.tsx`, `app/globals.css`
- `app/scenarios/scenarios-data.ts`, `app/scenarios/page.tsx`, `app/scenarios/scenario-directory.tsx`, `app/scenarios/scenarios.module.css`, `app/scenarios/[id]/page.tsx`, `app/scenarios/[id]/agent-selection.tsx`, `app/scenarios/[id]/scenario-detail.module.css`, `app/scenarios/new/page.tsx`, `app/scenarios/new/scenario-form.tsx`
- `app/agents/page.tsx`, `app/agents/agent-directory.tsx`, `app/agents/agents.module.css`, `app/agents/[id]/agent-detail.tsx`, `app/agents/new/agent-form.tsx`
- `app/runs/new/page.tsx`, `app/runs/new/run-setup.tsx`, `app/runs/new/run-setup.module.css`, `app/runs/[id]/page.tsx`, `app/runs/[id]/live-run.tsx`
- `app/evaluations/[id]/page.tsx`, `app/comparisons/[id]/page.tsx`
- `app/workspace/page.tsx`, `app/workspace/workspace.module.css`, `app/workspace/layout.tsx`
- New: `app/pricing/page.tsx`, `app/pricing/pricing.module.css`

**2. Homepage changes**
- Hero headline now exactly: “Qualify infrastructure agents before they reach production.” with the specified supporting copy.
- CTAs: “Connect an agent” → `/agents/new`; “See how qualification works” → `/#how-qualification-works`.
- Removed old “Run an evaluation / Explore scenarios” hero and ADOPT/COMPARE/REGRESS + fake Atlas SRE report blocks as primary story.

**3. New positioning/copy**
- Eyebrow: “An evaluation harness and qualification platform for infrastructure agents”.
- New section “Turn capability claims into qualification evidence” with 8-step editorial flow: Agent → Capability claims → Qualification suite → Infrastructure tasks → Isolated environment → Repeated trials → Trajectory + outcome evidence → Qualified / Not qualified / Inconclusive.
- New “Claimed / Tested / Qualified” section using the spec’s GitOps-drift example; explicitly states a claim is not a qualification.
- Banned phrases removed: “Test against ours. Or bring your own.” headline deleted; no “real world / bring your own” generic SaaS copy.

**4. Terminology changes (UI only, routes untouched)**
- Scenario → Evaluation Task, Run → Trial, Evaluation Suite → Qualification Suite, Events/actions → Trajectory, Final environment → Outcome, Capabilities → Capability Claims (agent competency), Policy result → Qualification, Version comparison → Regression.
- Applied across homepage, task directory/detail, trial setup, live trial, trial evidence, regression, workspace nav (“Evaluation Tasks / Trials / Trial Evidence / Regressions”), and task authoring form. `/scenarios`, `/runs/*` paths unchanged.

**5. Evaluation Tasks changes**
- `/scenarios` header/metadata now “Evaluation Tasks”; grouped by domain/task family (Kubernetes: Capacity & scaling, Workload health, Networking/dependencies; Delivery: Progressive delivery; plus Observability/Safety families listed).
- Rigorous names: “Black Friday traffic spike” → “Traffic Spike / Capacity Saturation”; node/DNS/deployment tasks renamed to “Node Failure / Workload Rescheduling”, “DNS Resolution Failure / Service Discovery”, etc. Canary kept under Delivery → Progressive Delivery.
- Filters changed to task families; “Evaluations count” column removed, replaced with Environment + availability badge.

**6. Unsupported/mock features removed or relabelled**
- Node failure, DNS failure, canary, failed deployment marked “Requires GitOps / Dedicated Kubernetes (planned) — live trials unavailable”; detail pages show planned banner and disable “Start trial”.
- Fake pass rates / evaluated-scenario counts removed from public selection flows. `AgentSelection` and `RunSetup` now lead with “Connect your own agent”; reference agents kept only as clearly labelled illustrative examples.
- All demo surfaces labelled: homepage trajectory “Illustrative example — not a live qualification result”; trial evidence/regression/live trial headers carry “Illustrative reference — no live qualification/execution”; workspace shows “illustrative reference data” banner.

**7. Agent-page copy changes**
- `/agents` headline “Connect an infrastructure agent” + “qualify the capabilities it claims to support”.
- Directory empty state + rows explain qualification is earned through trials, never assumed; “No capability claims declared”.
- Detail page adds Claimed / Tested (“Not yet tested”) / Qualification status (“Not yet qualified”) per version without faking data; connection fields (HTTP, Direct/MCP, endpoint, status) preserved.
- Form: “Declared capabilities” → “Capability claims”, with tested/qualification earned-later explainer.

**8. Environment positioning**
- Homepage `#environments`: “Test against the infrastructure the capability actually requires.” Only Kubernetes Sandbox = Available; GitOps = Planned/building; Dedicated Kubernetes = Planned; AWS Sandbox = Future. Planned items explicitly “not yet available”.

**9. Pricing page (`/pricing`)**
- Developer $0, Starter $49, Pro $299, Organization $999, Enterprise Custom/Contact us — with the spec’s limits/features. PagerDuty/Datadog, CI qualification, webhooks, audit/governance marked “(planned)”. No “$2,500+” hardcode.
- Credits section states exactly: “Evaluation credits account for the infrastructure required to execute trials.” Base unit = standard Kubernetes namespace trial; higher fidelity consumes more; no 5x/20x multipliers published; billing automation noted as not yet enabled.

**10. Navigation changes**
- Public header now: Product (`/#how-qualification-works`), Evaluation Tasks (`/scenarios`), Environments (`/#environments`), Pricing (`/pricing`). Fake `#docs/#blog` header links removed; footer rebuilt around real routes (how it works, tasks, environments, pricing, connect agent, define task, start trial, sign in). Agents not turned into a marketplace.

**11. Visual changes**
- Kept neutral near-white, near-black sections, Geist Sans/Mono, compact editorial rows, restrained borders, semantic status colors only. No gradients/blobs/glass/bento/terminal/dashboard art added. New badges use flat available/planned/future pills. Navbar remains sticky.

**12. Planned-vs-available labeling**
- Badges + banners everywhere: `Runnable · Kubernetes Sandbox` vs `Requires GitOps/Dedicated Kubernetes (planned)`; pricing + environments pages reiterate only K8s Sandbox is billed/available.

**13. TypeScript result**
- `npx tsc --noEmit`: clean, no output.

**14. Production build result**
- `npm run build`: compiled successfully, 22 static pages generated, including `/pricing` and all 8 `/scenarios/[id]` paths. Only warnings are pre-existing autoprefixer flex-end notes.

**15. Intentionally deferred**
- No capability/claim, scenario-family, coverage, environment-provider, AWS, Argo CD, billing/Stripe, PagerDuty/Datadog, CI gates, webhook, evaluator, or scoring backend work — per §15.
- Mock data files (`agents-data`, `evaluation-data`, `comparison-data`, `run-data`) retained as clearly-labelled illustrative references to avoid breaking demo routes; no new runnable scenarios invented.
