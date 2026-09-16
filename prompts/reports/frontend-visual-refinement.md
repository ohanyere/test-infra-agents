# Frontend visual refinement report

## Homepage changes

The homepage was reduced to a restrained seven-part flow: shared public navigation,
a viewport-safe hero, CLAIMED → TESTED → QUALIFIED, a four-step workflow, real task
examples, an illustrative evidence trajectory, and a compact final CTA. Repeated
architecture explanations, environment marketing, large card grids, and the large
multi-column footer were removed. The evidence preview remains explicitly
illustrative and does not present fabricated product state.

## Typography scale

The desktop hero now caps at 64px and the mobile hero at 48px. Supporting copy is
19px desktop/17px mobile and constrained to 650px. Marketing section headings cap
at 44px. Product page headings were reduced to approximately 30–34px, with compact
19px section headings and 14px explanatory copy.

## Color system

The primary system now uses `#F6F5F0` background, `#FBFAF7` surface, `#11110F`
text, `#6F6B63` muted text, `#DDD9D1` borders, and `#F05A22` as the selective
action/active-state accent. There are no gradients, glass effects, or decorative
orbs.

## Navigation before/after

Public navigation previously duplicated four implementations and exposed Product,
Evaluation Tasks, Environments, and Pricing with “Get started.” It is now one
shared Product / Tasks / Pricing navigation with Sign in and Try now. Environments
remains a valid direct informational route but is no longer marketed as a primary
concept.

The Agents directory now uses the shared authenticated product navigation and its
Tasks link targets `/workspace/tasks`, preserving the Stage-36 authenticated alias.
The authenticated custom-task page also uses product navigation rather than the
public marketing header.

## Hero copy

The hero now reads “Test your agents before production does.” Supporting copy is
limited to one sentence about realistic tasks, repeated trials, and inspectable
behavior. Its actions are “Try now” and “Explore tasks.” Architecture terminology
was removed.

## CTA vocabulary

Generic “Get started” actions were replaced with Try now. Other revised actions
include View tasks, Define task, Register version, Inspect evidence, and View
pricing. Misleading Contact us links that routed to signup were replaced with Try
now.

## Page-header reductions

Tasks is now “Evaluation tasks” with one sentence. Agents is now “Agents” with one
sentence. Environments is now “Execution environments” with one sentence. Pricing,
login, signup, authenticated Tasks, and Usage & billing were similarly shortened.
Shared CSS also reduces headings and vertical spacing on the remaining product
screens without changing their data or behavior.

## Public/authenticated navigation behavior

Public discovery remains on `/`, `/scenarios`, `/pricing`, and the direct
`/environments` informational route. Public navigation never labels an authenticated
Agent directory as a marketing destination. Product navigation uses `/workspace`,
`/agents`, `/qualification`, `/workspace/tasks`, `/runs`, `/workspace/billing`, and
`/workspace/settings`. Auth-required routing remains enforced by the existing
AuthProvider.

## Links fixed

- Removed obsolete `/#how-qualification-works` and `/#environments` navigation.
- Changed the pricing environment-status link to the existing `/environments` route.
- Changed authenticated Agent task navigation from `/scenarios` to
  `/workspace/tasks`.
- Removed dead `#terms` and `#privacy` fragment links where no targets existed.
- Removed misleading Contact us → signup labels.

The route tree and every literal visible `href` were manually audited against App
Router pages. Dynamic agent, task, run, suite, comparison, and trajectory links were
also checked against their route templates.

## Card/density reductions

The homepage now relies on typographic hierarchy, separators, rows, and a single
trajectory panel. Product tables/lists retain their real backend data but use less
padding, smaller headings, softer borders, and fewer filled containers. Billing
summary blocks were flattened into bordered metrics rather than card tiles.

## Brand-name centralization status

The shared public and product navigation now source the wordmark from
`app/brand.ts`, creating one replacement point for the most visible brand usage.
Existing metadata, auth screens, and historical product components still contain
the current name; a global rename was intentionally not attempted.

## TypeScript result

`npx tsc --noEmit` passed with no errors.

## Build result

`npm run build` passed and generated all 20 static pages. The build continues to
emit existing Autoprefixer warnings about the mixed-support `end` alignment value
in scenario/workspace styles; the new compact homepage also has two equivalent
alignment warnings. These warnings do not fail compilation.

## Known remaining visual issues

Some deeper authenticated detail/form routes retain their stage-specific CSS and
compact local headers rather than the new shared product header. Their type scale
is reduced by shared product styling where applicable, but a future shell-only
cleanup could consolidate those headers without changing behavior. The existing
Autoprefixer alignment warnings should also be normalized in a separate mechanical
CSS compatibility pass.
