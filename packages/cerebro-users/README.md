# @multica/cerebro-users

Cerebro fork user-model extensions: extra profile fields, user lifecycle
hooks, and any user-related queries/mutations that exist only in the fork.

## Owns

- Cerebro-specific user profile fields and the typed accessors for them.
- Hooks/queries that read or update those fields.
- UI fragments that render fork-only user attributes.

## May land here

- New profile sections cerebro adds on top of upstream `User`.
- Cerebro-only invite/onboarding logic that diverges from upstream.

## May NOT land here

- The base `User` type or generic profile fields — those stay in
  `@multica/core`. Use module augmentation via `@multica/cerebro-types/augment`
  when extra fields need to attach to upstream types.

## Imports

- May import from: `@multica/core`, `@multica/ui`, `@multica/views`,
  `@multica/cerebro-feature-flags`, `@multica/cerebro-types`.
- May NOT import from: `apps/*`, `next/*`, `react-router-dom`.
