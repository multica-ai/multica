# @multica/cerebro-types

Cerebro fork type system: shared discriminators, module-augmentation
declarations, and the bundler-alias map used to shadow upstream modules.

## Owns

- `index.ts` — shared type aliases used across cerebro packages.
- `augment.ts` (subpath `@multica/cerebro-types/augment`) — TypeScript
  module-augmentation declarations that extend upstream types with
  cerebro-only fields. Imported once per app for its side effects.
- `aliases.ts` (subpath `@multica/cerebro-types/aliases`) — the
  `cerebroAliases` map. Each app's bundler reads this to override
  upstream import paths with cerebro replacements at build time.

## May land here

- New type-only declarations shared across `cerebro-*` packages.
- New module-augmentation blocks for upstream types/interfaces.
- New entries in `cerebroAliases` when a cerebro package needs to
  shadow an upstream module.

## May NOT land here

- Runtime values, except the `cerebroAliases` constant. Keep this
  package type-heavy and dependency-free.
- Anything that imports from `@multica/core`, `@multica/views`, or
  `@multica/ui` — those are downstream.

## Imports

- May import from: nothing in `packages/*` (this is a leaf alongside
  `cerebro-feature-flags`).
- May NOT import from: any other `@multica/*` package, `apps/*`,
  `next/*`, `react-router-dom`.
