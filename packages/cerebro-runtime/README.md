# @multica/cerebro-runtime

Cerebro fork agent-runtime extensions: extra runtime kinds, configuration
schemas, and hooks unique to the fork's agent landscape.

## Owns

- Cerebro-specific runtime type discriminators and configuration shapes.
- Hooks/queries that read or update cerebro-only runtime endpoints.
- UI components that render cerebro-only runtime configuration.

## May land here

- New runtime kinds added in the fork (e.g., bespoke local agents).
- Configuration UI for cerebro-only runtime parameters.

## May NOT land here

- The base runtime model and the generic runtime list — those stay in
  `@multica/core` and `@multica/views`.

## Imports

- May import from: `@multica/core`, `@multica/ui`, `@multica/views`,
  `@multica/cerebro-feature-flags`, `@multica/cerebro-types`,
  `@multica/cerebro-mcp`.
- May NOT import from: `apps/*`, `next/*`, `react-router-dom`.
