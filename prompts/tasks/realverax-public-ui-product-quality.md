CODEX: DEFAULT / MEDIUM REASONING

Repository:
~/projects/test-infra-agents

Branch:
infrastructure-agent-verification

TASK:
UPGRADE THE PUBLIC-FACING REALVERAX INTERFACE TO FUNDED-STARTUP PRODUCT QUALITY.

This is a visual/product-quality upgrade of the existing application.

The goal is NOT to redesign the product architecture.

The goal is to make the existing interface feel like a serious, highly funded infrastructure/AI company preparing for YC/customer/investor demos rather than an engineering project.

==================================================
PRODUCT CONTEXT
==================================================

Brand:
realverax

Core product:
Realverax independently verifies what state an autonomous system leaves the world/environment in after it acts.

Current wedge:
Autonomous infrastructure change verification.

Core idea:

Agent says:
"I succeeded."

Realverax independently determines:
"What actually happened?"
"Is the resulting state correct?"
"Is it safe?"
"Did it remain stable?"
"What evidence supports the verdict?"

Primary verdicts:
VERIFIED
WARNING
FAILED
INCONCLUSIVE

This is an infrastructure/agent-verification product.

The visual language should communicate:
- trust
- technical authority
- precision
- reliability
- serious infrastructure software
- enterprise credibility
- calm confidence

It must NOT look like a generic AI startup template.

==================================================
NON-NEGOTIABLE CONSTRAINTS
==================================================

DO NOT:

- redesign the application's information architecture
- invent new product functionality
- invent new pages
- remove existing routes
- change backend APIs
- change database models
- change authentication behavior
- change verifier behavior
- change application logic merely for visual purposes
- introduce a generic dashboard redesign
- introduce bento-card layouts everywhere
- introduce purple/blue "AI startup" gradients
- add decorative AI artwork
- add meaningless illustrations
- add fake metrics
- add fake customers
- add fake testimonials
- add fake funding claims
- add fake logos
- add fake enterprise customers
- use "raised millions" as literal marketing copy

"Feels like a company that has raised money" means PRODUCT QUALITY, not fabricated claims.

Do not change the underlying product structure.

Do not touch backend/verifier/E2E code.

Do not stage unrelated backend changes.

==================================================
FIRST: AUDIT THE EXISTING PUBLIC UI
==================================================

Before editing anything, inspect the current implementation.

Pay particular attention to:

- app/page.tsx
- app/layout.tsx
- app/brand.ts
- app/site-navigation.tsx
- app/site-navigation.module.css
- app/footer.module.css
- app/site-footer.tsx
- app/realverax-wordmark.tsx
- app/realverax-wordmark.module.css
- app/login/page.tsx
- app/signup/page.tsx
- app/auth/auth-shell.tsx
- app/onboarding/page.tsx
- app/onboarding/onboarding-auth-header.tsx
- app/pricing/page.tsx

Also inspect any shared styling/token infrastructure.

Understand what already exists before modifying it.

Do NOT rebuild components that are already structurally sound.

==================================================
DESIGN DIRECTION
==================================================

The design should feel like:

"serious infrastructure company with exceptional product design"

rather than:

"developer side project"
or
"generic AI SaaS landing page."

Think:
- Linear-level restraint
- Vercel-level typography discipline
- Stripe-level information hierarchy
- modern infrastructure tooling
- enterprise software confidence

Do NOT copy any company's visual identity.

==================================================
COLOR SYSTEM
==================================================

Establish a coherent restrained design system.

Use color intentionally.

The dominant interface should remain calm and neutral.

Prefer:
- sophisticated off-white / very light neutral surfaces
- near-black / charcoal typography
- restrained borders
- subtle secondary text
- one controlled Realverax accent
- semantic status colors only where meaning requires them

Status colors must remain semantically consistent:

VERIFIED → success
WARNING → warning
FAILED → destructive
INCONCLUSIVE → uncertainty/neutral

Do not turn the whole site into a colorful dashboard.

Avoid:
- excessive gradients
- glowing effects
- neon
- purple AI aesthetics
- saturated backgrounds
- excessive shadows

If a gradient does not communicate anything meaningful, remove it.

==================================================
TYPOGRAPHY
==================================================

Typography must feel deliberate and premium.

Establish a clear hierarchy:

- display/hero
- page title
- section title
- body
- metadata
- labels
- navigation
- button text

Improve:
- font sizing
- font weight
- line height
- letter spacing
- text measure
- heading rhythm
- whitespace

Do not simply make everything larger.

The goal is hierarchy and confidence.

Use typography to make the product feel engineered.

==================================================
HOMEPAGE
==================================================

Preserve the existing homepage structure and content intent.

Upgrade the execution.

The first screen should immediately communicate:

WHAT:
Realverax verifies autonomous infrastructure changes.

WHY:
An agent reporting success is not proof that the resulting infrastructure state is correct.

HOW:
Realverax independently observes and verifies the resulting state.

The hero should feel extremely confident and concise.

Avoid marketing fluff such as:
"Revolutionizing the future of AI..."

Do not use generic:
"AI-powered..."
"Next-generation..."
"Transform your..."
language unless it is genuinely necessary.

The homepage should make a technically sophisticated visitor understand the product within seconds.

==================================================
HERO
==================================================

Improve:

- headline typography
- headline width
- supporting copy
- CTA hierarchy
- navigation spacing
- vertical rhythm
- background treatment
- subtle technical visual cues

The hero must not become a fake product dashboard.

If a visual representation is already present, improve it rather than inventing an unrelated illustration.

The product itself should be the visual story.

==================================================
NAVIGATION
==================================================

Make navigation feel like a funded infrastructure startup.

Improve:

- spacing
- alignment
- typography
- CTA hierarchy
- hover states
- active states
- border treatment
- responsive behavior

The realverax wordmark should remain text-based and understated.

Do not create a logo/icon mark.

==================================================
SECTIONS
==================================================

Review every existing homepage section.

For each section ask:

1. Does this communicate a real product capability?
2. Is the hierarchy obvious?
3. Is the copy precise?
4. Does the visual treatment support the message?
5. Does it look like production software?
6. Is anything decorative without purpose?

Improve existing sections rather than inventing additional product claims.

Use technical language where appropriate.

Realverax should sound like infrastructure/evaluation software, not consumer AI.

==================================================
FOOTER
==================================================

The footer is important.

It should feel complete and intentional.

Improve:

- hierarchy
- spacing
- navigation grouping
- legal/product links that already exist
- typography
- brand treatment
- responsive layout
- visual separation from the page

Do not invent links or company information.

Do not make the footer enormous.

==================================================
LOGIN / SIGNUP / AUTH SHELL
==================================================

These pages are currently part of the public-facing quality pass.

Bring them to the same design standard.

They should feel like they belong to the exact same company as the homepage.

Improve:

- layout
- typography
- spacing
- form hierarchy
- labels
- inputs
- buttons
- errors
- focus states
- loading states
- background
- brand treatment
- responsive behavior

Do not change authentication functionality.

Do not change form submission behavior.

Do not add unnecessary decorative elements.

==================================================
PRICING / ONBOARDING
==================================================

Bring existing pricing and onboarding surfaces into the same design system.

Do not invent pricing or features.

Improve presentation only.

==================================================
DESIGN SYSTEM
==================================================

Before scattering individual CSS values throughout the application, establish reusable visual primitives/tokens where the current architecture allows it.

Examples:

- page background
- elevated surface
- border
- primary text
- secondary text
- muted text
- accent
- success
- warning
- destructive
- radius
- spacing
- typography scale
- content width

Avoid introducing a massive new design framework unless the repository already uses one.

Prefer the existing stack and conventions.

==================================================
RESPONSIVENESS
==================================================

The public experience must work properly on:

- desktop
- tablet
- mobile

Do not optimize only for a large monitor.

Check:
- navigation
- hero
- CTA layout
- footer
- forms
- pricing
- onboarding

==================================================
QUALITY BAR
==================================================

Before considering the task complete, perform a visual consistency audit.

Every public-facing page should look like it belongs to the same product.

Check:

- typography
- colors
- borders
- radii
- spacing
- buttons
- inputs
- navigation
- footer
- page widths
- responsive behavior
- empty states
- loading states
- error states

No page should feel like an older part of the application.

==================================================
IMPORTANT PRODUCT PRINCIPLE
==================================================

We are upgrading the standard, not decorating the application.

Every visual decision should answer:

"Does this make Realverax look more trustworthy, technically serious, precise, and production-ready?"

If not, do not add it.

==================================================
VALIDATION
==================================================

After implementation:

- run the existing frontend typecheck
- run the existing lint/build checks if available
- verify all existing public routes still work
- verify authentication behavior is unchanged
- verify no backend files were modified
- verify no verifier/E2E files were modified
- verify responsive layouts
- verify no broken links
- verify no console-breaking errors introduced

Use the existing project tooling.

Do not run Kind.

Do not modify EKS.

Do not commit.

Do not push.

==================================================
FINAL REPORT
==================================================

Report:

1. Public pages audited
2. Files changed
3. Design system changes
4. Typography changes
5. Color/background changes
6. Homepage changes
7. Navigation/footer changes
8. Login/signup/onboarding changes
9. Responsive improvements
10. Validation results
11. Any remaining visual inconsistencies

Do not claim the interface is "perfect."

Identify anything that should be addressed in the second authenticated-product pass.
