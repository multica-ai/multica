# @multica/cerebro-notifications

Cerebro fork notification routing and rendering: extra notification
kinds, channel adapters, and routing rules unique to the fork.

## Owns

- Cerebro-specific notification kinds and their renderers.
- Routing rules that direct cerebro notifications to inbox folders or
  external channels.
- Hooks/queries for cerebro-only notification endpoints.

## May land here

- New notification kinds invented in the fork.
- Cerebro-only notification preferences.

## May NOT land here

- The base notification model — that stays in `@multica/core`.
- Generic inbox rendering — keep that in `@multica/views`.

## Imports

- May import from: `@multica/core`, `@multica/ui`, `@multica/views`,
  `@multica/cerebro-feature-flags`, `@multica/cerebro-types`,
  `@multica/cerebro-inbox`.
- May NOT import from: `apps/*`, `next/*`, `react-router-dom`.
