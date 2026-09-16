# Stage 36 completion report — Complete customer frontend

Completed 2026-09-16 as a frontend-integration pass over Stages 23–35. No backend subsystem or API was changed: the existing contracts were sufficient for the implemented customer journey.

## 1. Frontend architecture

The customer product now uses `lib/api.ts` as the typed contract boundary and `lib/auth.tsx` for authenticated workspace context. Shared quiet operational styling lives in `app/product.module.css`. Backend-backed client surfaces explicitly model loading, empty, success, and error states; server redirects preserve obsolete deep links without retaining their mock implementations.

## 2. Navigation

The authenticated workspace navigation is compact: Overview, Agents, Qualification, Tasks, Activity, Billing, plus Settings. Active state follows the actual pathname. The Agents index now uses this product vocabulary instead of marketing navigation. Dead demo links were removed. Marketing navigation remains on the public home, pricing, environment, and task-catalog entry surfaces.

## 3. Route inventory

See section 33 for the complete route-by-route audit. Missing `/runs` and `/qualification` indexes were added. `/workspace/settings` and `/qualification/suites/[id]` were added. Legacy `/runs/new` and `/evaluations/[id]` are redirects.

## 4. Terminology mapping

Customer-facing surfaces consistently use Evaluation Task, Task Family, Trial, Qualification Suite, Trajectory, Outcome, Capability Claims, Qualification, and Regression. Backend identifiers such as scenario, run, and SuiteExecution remain unchanged in transport contracts.

## 5. Onboarding flow

Signup/onboarding still creates or selects the workspace. The truthful empty Overview then directs the customer to Connect Agent. Agent registration requires a successful connection test, creates an exact AgentVersion, captures low-level tool access separately from Capability Claims, stores credentials server-side, clears the token field, and continues to Agent detail/readiness. The flow is resumable through ordinary routes rather than a large wizard.

## 6. Agent directory/detail

The directory is workspace API-backed and has a real first-agent empty state. It shows the latest exact version and declared claim count without synthesizing qualification. Agent detail remains the primary qualification workspace and loads versions, safe connection state, Capability Claims, coverage, server-derived qualification plans, and reliability per version.

## 7. AgentVersion UX

Version label/ID, invocation protocol, direct/MCP tool access, safe endpoint, connection test state, claim state, coverage, qualification readiness, and reliability remain version-scoped. Credential rotation never returns the stored secret. A built suite now links directly to its review/start page.

## 8. Capability Claims UX

Agent detail preserves the exact distinction:

- CLAIMED comes from immutable AgentVersion declarations.
- TESTED comes exclusively from the authoritative Stage-27 capability-coverage verdict for the exact AgentVersion.
- QUALIFIED remains unavailable unless a policy result exists; the UI displays an honest dash rather than inferring it.

Claims, coverage families, valid Trial counts, missing requirements, and catalog pins come from backend responses.

## 9. Qualification readiness

The Stage-32 plan endpoint drives READY/PARTIAL/BLOCKED state, selected tasks, required family breadth, Trial counts, and structured blockers. React does not reproduce planner logic. Technical/environment blockers remain machine-truthful. Commercial denial remains an execution-time Stage-35 server decision because the current planner response has no separate commercial-blocker field.

## 10. Suite planning/build

The AgentVersion flow is now explicit: inspect claims and coverage → build a server-derived plan → inspect readiness → create the immutable Qualification Suite. Copy clearly states that plan and suite construction do not execute Trials or consume credits.

## 11. Suite detail

`/qualification/suites/[id]` loads the real suite, latest immutable version, task/ScenarioVersion pins, required Trial counts, description, creation time, and candidate exact AgentVersions. Hidden variant configuration is not rendered.

## 12. Execution start

The suite detail calls the Stage-35 `POST /suite-executions` contract. Structured denials are translated without duplicating billing logic: insufficient credits, plan limit, past due, environment tier, and general entitlement denial. Credit denial links to Billing & Usage. Successful creation opens the real SuiteExecution.

## 13. Activity/executions

The new `/runs` Activity index loads real evaluation batches and their Trials. It explains the difference between a grouped Qualification Suite execution and one Trial, then shows task, exact AgentVersion, ordinal, persisted status, start time, and Trial link. No fake recent activity remains.

## 14. Trial detail

`/runs/[id]` now loads the actual Trial. For terminal Trials it also attempts to load the deterministic evaluation and trajectory summary. It shows exact task, AgentVersion, attempt, status, environment presence, validity, Outcome, actions executed, safety violations, and deterministic findings, with a link to the existing trajectory explorer.

## 15. Trajectory/evidence integration

The Stage-34 explorer remains the sole detailed trajectory implementation. Trial detail links into it rather than duplicating it. The existing explorer distinguishes requested/executed/denied actions, actor/view, platform/evaluator evidence, recovery, redacted detail, and Outcome.

## 16. Outcome

Outcome comes only from trajectory/evaluation contracts: pass, fail, inconclusive, pending, or platform-invalid. No LLM narrative or client-generated reasoning was added. Platform-invalid is called out as neither an AgentVersion outcome nor committed customer evaluation usage.

## 17. Reliability

Agent detail continues to use the Stage-33 endpoint for valid attempts, pass rate, pass@k, pass^k, sample sufficiency, platform-invalid counts, family breadth, contrast evidence, variant breadth, and execution-level readings. Unavailable k values remain “Unavailable,” never zero.

## 18. Qualification

The `/qualification` index centers the journey on exact AgentVersions and their claim counts, then lists real Qualification Suites. Agent detail exposes claimed/tested semantics and planner readiness. Policy-derived QUALIFIED state is not synthesized where the backend has not returned a policy assessment.

## 19. Regression

`/comparisons/[id]` now reads real Stage-21/22 regression results. It shows baseline/candidate versions, classification, evidence quality, comparable pairs, regressions, improvements, pass-rate delta, insufficient-evidence state, and new/resolved safety violations. It does not select a subjective winner.

## 20. Task catalog/detail

The former static scenario catalog was replaced by public `/v1/qualification-tasks` data. Catalog and detail show task name, Task Family, capabilities, case type, difficulty/duration, components, minimum isolation, eligible environment profile, runnable/unavailable state, scheduler reason, certification note, and runtime contract. The detail intentionally cannot show hidden variants, reference remediation, evaluator ground truth, or grading secrets because the backend safe projection excludes them.

## 21. Empty states

Purposeful empty states cover no agents, no suites, no activity, no findings, pending Outcome, unavailable deterministic evaluation, no connection test, no Capability Claims, no coverage, and insufficient reliability samples. Backend/network failures render errors rather than empty results.

## 22. Connection/MCP/integrations

Agent creation and detail use Stage-23 connection APIs. “Saved” and “healthy test” are distinct; initial registration requires the latter. Stored credentials never reappear. MCP is described as evaluation-scoped tool access, not external proof. Settings points PagerDuty/Datadog configuration to the existing incident-source authoring surface and states that a provider is only connected after a successful test.

## 23. Billing integration

Billing remains naturally reachable from navigation, Settings, Overview usage state, and execution denial recovery. It displays server plan/status/usage/entitlements and keeps provider actions disabled when no provider-backed flow exists. Pricing economics were not changed.

## 24. Settings

`/workspace/settings` is intentionally focused: real workspace identity/current role, real member list, links to per-AgentVersion integrations, incident integrations, and Billing & Usage. No ornamental settings controls were added.

## 25. API client changes

`lib/api.ts` gained shared DTOs and accessors for Agents, evaluation batches, Trials, Qualification Suites/versions/executions/results, deterministic evaluations, qualification tasks, workspace memberships, and Stage-35 billing. `APIError` codes remain intact for entitlement/runtime/auth recovery. Duplicate mock DTOs were not introduced.

## 26. Mock/static removal

Removed production-route mock modules and their consumers:

- `app/agents/agents-data.ts`
- `app/scenarios/scenarios-data.ts`
- `app/scenarios/[id]/agent-selection.tsx`
- `app/runs/new/run-setup.tsx`
- `app/runs/[id]/run-data.ts`
- `app/runs/[id]/live-run.tsx`
- `app/evaluations/[id]/evaluation-data.ts`
- `app/comparisons/[id]/comparison-data.ts`

The public homepage keeps one clearly labeled visual trajectory illustration, but its result was changed from “QUALIFIED” to “PASS (ILLUSTRATIVE TRIAL)” so a single example cannot imply qualification.

## 27. Security/leakage review

No customer surface renders credential ciphertext/token values after submission, provider secrets, raw payment data, provider payloads, hidden variant configuration, reference remediation, evaluator-only ground truth, or secret grading inputs. Qualification, Outcome, billing state, entitlements, usage, connection health, and reliability all come from server contracts. Environment provider references are deliberately not rendered on Trial detail.

## 28. Accessibility

New surfaces use semantic headings, navigation, sections, lists, labels, tables, status/error roles, real buttons, and text labels in addition to status color. Tables have deliberate horizontal overflow at narrow widths. Native controls remain keyboard accessible and focus behavior is browser-standard.

## 29. Responsive behavior

Shared operational layout collapses headers and multi-column lists at mobile width; metrics wrap; tables use a bounded horizontal scroll container. Existing trajectory and Agent surfaces retain their own responsive styles.

## 30. TypeScript result

`npx tsc --noEmit` passed.

## 31. Production build

`npm run build` passed under Next.js 15.5.25. The original Stage-36 build compiled 25 customer routes (excluding the framework `_not-found` entry), including new Activity, Qualification, suite detail, Settings, real Trial, Regression, and task-detail surfaces.

## 32. Backend tests if changed

No backend code or API contract changed during Stage 36, so focused backend tests were not required or run, per the task. Docker and Kubernetes were not started.

## 33. All customer routes and status

| Route | Status | Source/behavior |
|---|---|---|
| `/` | Working static marketing | Explicitly labeled illustration only; no customer state |
| `/pricing` | Working static marketing | Approved Stage-35 public pricing/copy |
| `/environments` | Working static marketing/catalog | Truthfully marks current/planned environment availability |
| `/signup` | Working backend-backed | Auth signup/workspace creation |
| `/login` | Working backend-backed | Auth login |
| `/onboarding` | Working backend-backed | Existing workspace onboarding/resume |
| `/workspace` | Working backend-backed | Agents, suites, batches, usage summary |
| `/workspace/settings` | Working backend-backed | Workspace/member APIs and real integration links |
| `/workspace/billing` | Working backend-backed | Stage-35 billing/entitlements/usage |
| `/agents` | Working backend-backed | Workspace Agent directory |
| `/agents/new` | Working backend-backed | Connection test, Agent/AgentVersion, credentials, claims |
| `/agents/[id]` | Working backend-backed | Versions, connection, claims, coverage, planner, reliability |
| `/qualification` | Working backend-backed | AgentVersion entry points and Qualification Suites |
| `/qualification/suites/[id]` | Working backend-backed | Immutable suite review and server-enforced execution start |
| `/suite-executions/[id]` | Working backend-backed | Execution state and actual Trial outcomes |
| `/runs` | Working backend-backed | Activity/Trials index |
| `/runs/[id]` | Working backend-backed | Trial, Outcome, evaluation, environment state |
| `/runs/[id]/trajectory` | Working backend-backed | Stage-34 trajectory/evidence explorer |
| `/runs/new` | Redirected | Redirects to `/qualification`; direct ad-hoc mock Trial setup removed |
| `/evaluations/[id]` | Redirected | Compatibility redirect to `/runs/[id]` |
| `/comparisons/[id]` | Working backend-backed | Real Regression results for a known comparison ID |
| `/scenarios` | Working backend-backed/public | Server qualification-task catalog |
| `/scenarios/[id]` | Working backend-backed/public | Safe qualification-task detail |
| `/scenarios/new` | Working backend-backed | Custom Evaluation Task and incident-source authoring |

Removed obsolete/mock behavior has no route of its own. There is intentionally no fake invoices, fake checkout, fake Regression index, or fake historical execution.

## 34. Known frontend gaps

- The backend has no workspace-wide SuiteExecution list endpoint, so Activity derives real Trials from the existing batch list and SuiteExecution detail is reachable after start/deep link. No execution history was fabricated.
- The backend has no Regression list endpoint, so only known real comparison IDs have detail pages; no ornamental `/comparisons` index was added.
- Qualification policy assessments are available by ID but there is no AgentVersion-centric list contract. Agent detail therefore does not infer QUALIFIED from coverage/reliability.
- The safe built-in task projection does not expose customer-visible criteria/allowed-tools fields, so task detail shows only fields actually returned and explicitly excludes evaluator-only internals.
- No live notification/polling channel exists; pages refresh on navigation/load rather than presenting fabricated live progress.

## 35. Screenshots/manual verification limitations

No browser, Docker database, Kubernetes runtime, or seeded authenticated customer workspace was started. Verification was compile/build based plus a manual source-level route/link/data-source audit. Consequently, no screenshot or live click-through claim is made.

## 36. Deferred work

- Workspace-wide SuiteExecution and Regression list APIs, if product requirements warrant those indexes.
- An AgentVersion-centric qualification-assessment listing contract for direct QUALIFIED badges.
- Optional server-safe expansion of qualification-task detail with customer-visible criteria/tools, without exposing evaluator truth.
- Browser E2E against a scratch migrated database and deterministic reference worker.

## Final frontend integrity audit

Completed 2026-09-16 under `prompts/tasks/frontend-integrity-final.md`.

1. **Actual authenticated Tasks navigation behavior:** the audit confirmed that the authenticated workspace navigation previously linked directly to public `/scenarios`, whose intentional marketing shell had no coherent product return path. Authenticated Tasks now links to `/workspace/tasks`, a thin product-shell surface that reuses the same `/v1/qualification-tasks` data and shared catalog component. Its detail alias is `/workspace/tasks/[id]`. Anonymous public discovery remains unchanged at `/scenarios` and `/scenarios/[id]`.
2. **Tasks production-code change:** yes, narrowly. Added the two authenticated aliases, parameterized the shared catalog’s detail base path, and changed only the authenticated navigation target. No task catalog or backend logic was duplicated.
3. **Final TESTED source/semantics:** TESTED comes exclusively from `CapabilityCoverageResponse.items[].test_status`, returned by the Stage-27 capability-coverage endpoint for the exact AgentVersion. The backend coverage contract owns Task Family breadth, required cases/trials, valid exercised failures, platform-invalid exclusion, and independent-family versus hidden-variant depth semantics.
4. **TESTED production-code change:** yes, one narrow fallback removal. Agent detail no longer falls back from a missing coverage item to `CapabilityClaim.test_status`; without authoritative Stage-27 coverage it displays UNTESTED. Claim presence can only display CLAIMED.
5. **Route/build count correction:** the original Stage-36 build output contained 25 customer routes when the framework `_not-found` entry is excluded, so the original number was correct but its counting convention was undocumented. This integrity pass adds `/workspace/tasks` and `/workspace/tasks/[id]`; the final build contains **27 customer routes**, or **28 entries including `_not-found`**. No route was added merely to satisfy a count.
6. **TypeScript:** `npx tsc --noEmit` passed.
7. **Production build:** `npm run build` passed under Next.js 15.5.25; all 27 customer routes compiled.
8. **Semantic confirmation:** CLAIMED != TESTED != QUALIFIED. CLAIMED is the exact AgentVersion declaration; TESTED is the Stage-27 coverage verdict; QUALIFIED requires a qualification-policy result and is never inferred from either claim or coverage presence.
9. **Coverage-algorithm confirmation:** no frontend coverage algorithm was introduced. React displays server-returned `test_status`, family coverage, valid-Trial counts, and missing requirements; it does not calculate or promote TESTED from those display fields.

No backend code changed. Per the audit task, no backend tests, Docker, or Kubernetes were run.
