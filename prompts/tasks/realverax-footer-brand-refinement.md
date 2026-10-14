CODEX: MEDIUM

TASK: Refine the Realverax footer and brand casing only.

Repository:
~/projects/test-infra-agents

Product:
realverax — infrastructure agent verification.

IMPORTANT:
Do NOT redesign the entire website.
Do NOT change the hero, page structure, typography system, navigation, cards, colors across the application, or verifier/product content.

We are making a focused frontend branding/footer refinement.

## 1. BRAND CASING — MUST BE EXACT

The product/company name is:

realverax

It must always appear lowercase.

Specifically:

- Header wordmark: `realverax`
- Footer wordmark: `realverax`
- Any newly touched footer/header branding text must use lowercase `realverax`
- Do not use `RealVerax`, `Realverax`, `REALVERAX`, or any other casing.

Preserve the existing wordmark component if one already exists. Do not create a second competing wordmark implementation unless the existing architecture genuinely requires it.

## 2. FOOTER — REPLACE THE CURRENT SMALL FOOTER

The current footer is too small/minimal.

Transform it into a normal, substantial modern startup footer — roughly the level of polish and information density expected from a serious YC-backed infrastructure startup.

The footer should feel like a real product/company footer, not a decorative strip at the bottom.

Think:

- substantial vertical height
- generous top and bottom spacing
- clear content hierarchy
- strong but restrained typography
- multiple logical sections/columns
- clear company/product navigation
- product identity
- useful links
- legal links where appropriate
- copyright
- restrained social/contact links if they already exist or are appropriate
- responsive mobile layout

Do NOT copy a specific company's footer or reproduce another site's design.
Use the general information architecture and professionalism of modern YC/startup footers as inspiration.

## 3. INFORMATION ARCHITECTURE

Use a clean structure such as:

Brand / company area:
- `realverax`
- short one-line description explaining the product:
  "Independent verification for autonomous infrastructure changes."
  (Use the existing approved product wording if the repository already contains a better canonical version.)

Product:
- Runs
- Scenarios
- Agents
- Environments
- Documentation / relevant product link if it already exists

Company:
- About / appropriate existing company link
- Contact if an existing route exists

Resources:
- GitHub if already configured
- relevant documentation/resources if they already exist

Legal:
- Privacy
- Terms
Only include routes that actually exist or can safely be represented by existing project routes.
Do NOT invent broken navigation destinations.

Bottom row:
- `© [current year] realverax`
- appropriate legal links
- GitHub/social link only if an actual configured destination exists.

If the current application has a different established navigation structure, inspect it first and reuse the real routes instead of inventing routes.

## 4. COLOR PSYCHOLOGY

The product is infrastructure / autonomous-agent verification.

The footer should communicate:

- trust
- reliability
- technical seriousness
- security
- stability
- engineering quality

Do NOT use:
- purple AI gradients
- neon AI colors
- excessive blue gradients
- flashy startup decoration
- glassmorphism
- large gradients
- artificial "AI" visual language

Use the existing Realverax visual language as the foundation.

Prefer a restrained dark graphite / charcoal / deep slate treatment or another existing brand-compatible neutral that communicates infrastructure reliability.

The footer must feel visually connected to the rest of the site rather than looking like a separate theme.

Use color contrast intentionally:
- primary footer background
- strong readable text
- muted secondary text
- subtle borders/dividers
- restrained link hover states

Do not introduce a large number of new colors.

Do not specify arbitrary colors if the existing design tokens already provide suitable values. Reuse the existing design system wherever possible.

## 5. WORDMARK

The footer wordmark must be:

`realverax`

Lowercase.

It should remain quiet, professional, and text-based.

Do NOT add:
- an icon
- an R symbol
- a shield
- a checkmark
- a Kubernetes logo
- a robot
- a gradient logo
- a three-bar mark

The brand is the lowercase text wordmark.

The header must also display:

`realverax`

with lowercase `r`.

## 6. DESIGN QUALITY

The footer should have:

- substantial but not excessive height
- strong alignment with the page content width
- clear spacing between columns
- readable hierarchy
- good whitespace
- excellent desktop layout
- clean responsive mobile layout
- no cramped links
- no tiny unreadable text
- no excessive visual decoration

It should look like a mature infrastructure company rather than a personal project.

The footer should be visually confident but quiet.

## 7. RESPONSIVENESS

Desktop:
- multi-column footer
- clear brand section
- logical link groups

Tablet:
- gracefully reduce column density

Mobile:
- stack sections cleanly
- maintain generous spacing
- avoid horizontal overflow
- maintain readable text sizes
- keep `realverax` clearly visible

## 8. EXISTING CODE FIRST

Before changing anything:

1. Inspect the existing footer implementation.
2. Inspect `app/brand.ts`.
3. Inspect the existing `realverax-wordmark` component if present.
4. Inspect `app/layout.tsx`.
5. Inspect existing navigation/routes so no broken links are introduced.
6. Inspect the current footer CSS/module CSS.

Reuse existing components, design tokens, and typography where appropriate.

Do not create duplicate branding abstractions.

## 9. SCOPE CONTROL

Only change the files necessary for:

- footer redesign
- footer styling
- lowercase brand casing in header/wordmark where necessary

Do NOT touch backend/verifier/E2E files.

Do NOT touch the infrastructure verification implementation.

Do NOT modify unrelated pages.

Do NOT change the product's existing hero or main visual identity.

## 10. VALIDATION

After implementation:

- run the appropriate frontend typecheck
- run the appropriate lint/check if available
- verify no broken imports
- verify responsive layout
- verify all displayed `realverax` branding touched by this task is lowercase
- verify no unrelated files were modified

Report:

1. files changed
2. footer structure
3. color/design decisions
4. confirmation that `realverax` is lowercase in header and footer
5. validation results

Do NOT commit or push.
