# @multica/cerebro-attachments

Cerebro fork attachment handling: storage adapters, viewer extensions,
and any attachment flows that exist only in the fork.

## Owns

- Cerebro-specific upload/download flows (e.g., alternate storage backends).
- Viewer extensions for fork-only file types.
- Hooks/queries for cerebro-only attachment endpoints.

## May land here

- New attachment kinds, viewers, and upload paths added in the fork.
- Test fixtures for cerebro-specific attachment scenarios.

## May NOT land here

- The base attachment model and upstream viewers — those stay in
  `@multica/core` and `@multica/views`.

## Imports

- May import from: `@multica/core`, `@multica/ui`, `@multica/views`,
  `@multica/cerebro-feature-flags`, `@multica/cerebro-types`.
- May NOT import from: `apps/*`, `next/*`, `react-router-dom`.
