# @multica/cerebro-access

Cerebro fork access control: combined restrict + pick flow, project/list
restriction model, red keylock indicator, and the queries/mutations that
back them.

## Owns

- Access policy types specific to cerebro (restrict + pick semantics).
- Hooks and components for the combined restrict+pick UI.
- Query/mutation wrappers around access-related endpoints when those
  endpoints are cerebro-only.
- `views/components/restricted-lock.{tsx,test.tsx}` — small lock badge
  shown next to a restricted project's name.
- `views/components/restricted-ref.{tsx,test.tsx}` — inline marker for a
  restricted-project reference (used inside issue detail).
- `views/projects/project-access-tab.{tsx,test.tsx}` — full
  manage-membership tab in project settings.

## Why the Go access handler stays in `server/internal/handler/`

`access.go` defines unexported helpers `isWorkspaceAdmin`,
`canAccessProject`, `canAccessIssue` as methods on the upstream
`*Handler` struct. The corresponding tests
(`access_test.go`, `access_handlers_test.go`, `access_privacy_test.go`,
`access_ws_test.go`) call those methods directly via the package-level
`testHandler` and `testPool` fixtures from upstream's `handler_test.go`.

Moving the production code to `server/internal/cerebro/access/` would
require renaming methods, re-implementing the test fixture
infrastructure per cerebro test-package, and updating every call site
that currently invokes `h.canAccessProject(...)` etc. — a real refactor
beyond Phase 6's mechanical-move scope. The existing
`CEREBRO-PATCH(access-handler)` markers keep the files compliant with
the upstream-zone validator. The same logic applies to
`project_access.go` / `project_access_test.go`.

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
