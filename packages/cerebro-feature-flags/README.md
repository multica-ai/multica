# @multica/cerebro-feature-flags

Cerebro fork-only feature flag system. Single source of truth for toggling
cerebro-specific behavior across both apps without forking upstream files.

## Owns

- The `cerebroFlags` registry (flag names, defaults, env var bindings).
- The `useCerebroFlag` / `getCerebroFlag` accessors.
- Build-time flag resolution (env -> static map) and runtime overrides for
  development/QA.

## May land here

- New flag definitions for cerebro-only features.
- Flag scopes (per-workspace, per-user, build-time-only).
- Test helpers that override flags inside `vitest`/`playwright`.

## May NOT land here

- Business logic gated by a flag — that lives in the feature's package.
- UI components — they consume flags but are not defined here.
- Upstream-only behavior — never gate upstream code by a cerebro flag;
  put the cerebro variant behind a flag check inside a cerebro package.

## Imports

- May import from: nothing in `packages/*` (this is a leaf).
- May NOT import from: `@multica/core`, `@multica/views`, `@multica/ui`,
  any other `cerebro-*` package, or `apps/*`. Keeping this package
  dependency-free makes it safe to import from anywhere.
