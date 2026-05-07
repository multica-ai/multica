# @multica/cerebro-ui

Cerebro fork primitive UI components: visual atoms specific to the fork
(e.g., the red keylock indicator) that don't belong in upstream
`@multica/ui`.

## Owns

- Atomic UI components that are cerebro-only.
- Tailwind/style utilities scoped to cerebro components.

## May land here

- New atoms invented in the fork.
- Variants of upstream atoms when restyling them via theme tokens isn't
  enough.

## May NOT land here

- Business-logic components — those go in a domain `cerebro-*` package.
- Tokens and base styles — those stay in `@multica/ui/styles/`.

## Imports

- May import from: `@multica/ui`, `@multica/cerebro-feature-flags`,
  `@multica/cerebro-types`.
- May NOT import from: `@multica/core`, `@multica/views`, `apps/*`,
  `next/*`, `react-router-dom`. (Mirrors the upstream rule that
  `packages/ui` is business-logic-free.)
