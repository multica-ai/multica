# @multica/cerebro-artifacts

Cerebro fork extensions to artifacts: extra artifact kinds, custom
renderers, and fork-only artifact flows.

## Owns

- Cerebro-specific artifact type discriminators and their renderers.
- Hooks/queries for cerebro-only artifact endpoints.
- UI components that render artifacts in cerebro-only contexts.

## May land here

- New artifact kinds invented in the fork.
- Cerebro-specific artifact metadata and renderers.

## May NOT land here

- The base artifact model and the upstream renderers — those stay in
  `@multica/core` and `@multica/views`.

## Imports

- May import from: `@multica/core`, `@multica/ui`, `@multica/views`,
  `@multica/cerebro-feature-flags`, `@multica/cerebro-types`,
  `@multica/cerebro-attachments`.
- May NOT import from: `apps/*`, `next/*`, `react-router-dom`.
