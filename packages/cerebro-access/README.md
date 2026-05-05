# @multica/cerebro-access

Cerebro fork access control: combined restrict + pick flow, project/list
restriction model, red keylock indicator, and the queries/mutations that
back them.

## Owns

- Access policy types specific to cerebro (restrict + pick semantics).
- Hooks and components for the combined restrict+pick UI.
- Query/mutation wrappers around access-related endpoints when those
  endpoints are cerebro-only.

## May land here

- New cerebro access flows that don't exist upstream.
- Cerebro-specific access UI (badges, indicators, dialogs).
- Test fixtures for access scenarios unique to the fork.

## May NOT land here

- Generic membership/role logic that should live in upstream `@multica/core`.
  If upstream gains a similar concept, contribute upstream and remove the
  cerebro version.
- App-specific routing — keep that in `apps/web/` or `apps/desktop/`.

## Imports

- May import from: `@multica/core`, `@multica/ui`, `@multica/views`,
  `@multica/cerebro-feature-flags`, `@multica/cerebro-types`.
- May NOT import from: `apps/*`, `next/*`, `react-router-dom`.
