# @multica/cerebro-inbox

Cerebro fork inbox extensions: extra folders, custom item renderers, and
filtering rules that exist only in the fork.

## Owns

- Cerebro-specific inbox folder definitions and ordering rules.
- Renderers for cerebro-only inbox item kinds.
- Hooks/queries for cerebro-only inbox endpoints.

## May land here

- New folder types and grouping rules invented in the fork.
- Cerebro-only filters and saved views.

## May NOT land here

- The base inbox query model — that stays in `@multica/core`.
- Generic inbox UI — that stays in `@multica/views`.

## Imports

- May import from: `@multica/core`, `@multica/ui`, `@multica/views`,
  `@multica/cerebro-feature-flags`, `@multica/cerebro-types`.
- May NOT import from: `apps/*`, `next/*`, `react-router-dom`.
