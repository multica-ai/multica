# @multica/cerebro-test

Shared test helpers for cerebro packages: fixture builders, mock
factories, and Vitest/Playwright utilities used by more than one
`cerebro-*` package or app.

## Owns

- Fixture builders for cerebro-only entities (e.g., access policies,
  fork-only artifact kinds).
- Mock factories that wrap `@multica/core` mocks with cerebro fields.
- Test setup helpers (e.g., flag overrides) used across packages.

## May land here

- Helpers that two or more cerebro packages would otherwise duplicate.
- Snapshot fixtures used by E2E tests.

## May NOT land here

- Tests themselves — they live next to the code they cover.
- App-specific test setup — that stays in `apps/web/` or `apps/desktop/`.

## Imports

- May import from: `@multica/core`, `@multica/ui`, `@multica/views`,
  any `@multica/cerebro-*` package, and test runners.
- May NOT import from: `apps/*`, `next/*`, `react-router-dom`.
