# Positioning differentiation copy audit

## 1. Generic positioning found

The homepage led with “Test your agents before production does” and made repeated trials the primary supporting idea. Task introductions described broad “problems used to test infrastructure agents,” onboarding included an unsupported “browse infrastructure agents” marketplace implication, and several entry points emphasized testing without explaining the capability-to-qualification contract.

## 2. Copy changed

Customer-facing copy now centers the question the product answers: what an exact infrastructure `AgentVersion` can be trusted to operate. The homepage establishes controlled operational conditions, bounded actions, server-owned coverage, evidence, reliability, version regression, and policy qualification without exposing implementation architecture. Tasks, Pricing, Signup, Onboarding, the Agents empty state, authenticated Tasks, and root metadata use the same positioning.

No backend behavior, API contract, qualification rule, status derivation, navigation, or page layout changed.

## 3. Final hero

- Eyebrow: `INFRASTRUCTURE AGENT QUALIFICATION`
- Headline: `Prove what your infrastructure agents can operate.`
- Supporting copy: `Qualify their capabilities against controlled infrastructure failures, inspect what they actually do, and build evidence before production access.`
- Actions: `Try now` and `View tasks`

## 4. Final differentiation section

The first section after the hero is `From capability claim to operational proof.` It uses three restrained concepts:

- Operational conditions: controlled infrastructure problems rather than prompt or response grading.
- Bounded actions: limits on inspection/change plus requested-versus-executed recording.
- Qualification evidence: outcome, safety, coverage, reliability, and version regression informing the decision.

## 5. CLAIMED / TESTED / QUALIFIED copy

- `CLAIMED`: This AgentVersion declares the capability.
- `TESTED`: Required independent capability coverage has been exercised under the server coverage contract.
- `QUALIFIED`: Evidence satisfies the qualification policy.

The copy describes the authoritative states; it does not infer a customer’s status or derive trial counts in the frontend.

## 6. Evidence positioning

The evidence section is now `Evidence, not a score.` It distinguishes what the agent observed and requested, what was authorized and executed, and the resulting system outcome. Its illustrative sequence labels persisted evidence concepts rather than implying chain-of-thought or exposing evaluator-only data.

## 7. Infrastructure-specific examples used

The homepage uses safe catalog concepts: capacity saturation, deployment failure, dependency outage, source-of-truth drift, canary degradation, and protected-resource boundaries. Descriptions emphasize bounded recovery, restraint, owned control paths, and task scope. Hidden variants, reference remediation, ground truth, and evaluator-only evidence are not exposed.

## 8. Planned/live capability language review

Catalog-backed infrastructure qualification concepts are described as the product model, not as a claim that every runtime is production-ready. Pricing continues to state that only the Kubernetes Sandbox is available today and that GitOps, dedicated Kubernetes, and AWS environments are planned or future. No new claim of live or end-to-end-proven higher-fidelity environments was introduced.

## 9. Pages audited

- Homepage and root metadata
- Product/differentiation sections and public navigation context
- Public Tasks catalog and authenticated Tasks entry
- Pricing introduction and environment-availability disclosure
- Signup and onboarding
- Authenticated Workspace Overview
- Agents landing page, directory, and empty state
- Qualification landing page and suite entry language

Concise, already-specific copy on Workspace Overview, Agents, and Qualification was retained where changing it would only create variation rather than improve positioning.

## 10. TypeScript result

`npx tsc --noEmit` — passed.

## 11. Build result

`npm run build` — passed.
