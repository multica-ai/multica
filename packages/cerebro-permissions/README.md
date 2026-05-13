# @multica/cerebro-permissions

Workspace admin UI for Persona grant administration (JEH-1180). Provides
the `/:workspace/permissions` top-level route — list, create, edit, delete
grants and inspect the audit log.

## Owns

- `core/types.ts` — proposed grant shape mirroring JEH-1179's API surface
  (subject × resource × capability × classification × time window ×
  approval). Lenient zod schemas guard against API drift.
- `core/queries.ts` / `core/mutations.ts` — TanStack Query options +
  optimistic mutations against the `/api/workspaces/{id}/grants` endpoints.
- `core/store.ts` — Zustand store for the filter bar (subject, resource,
  status, classification, search).
- `views/permissions-page.tsx` — top-level page with the grants table,
  filter bar, audit tab, and create/edit/delete flows.

## Why a new package and not `cerebro-access`?

`cerebro-access` owns the legacy project/issue restrict-pick model. Persona
grants are a different concept layered on top: subject × resource pattern
× capability. Keeping them separate so a future "delete cerebro-access in
favour of grants" sync doesn't touch this page.

## Feature flag

`cerebro_persona_permissions` (defaults OFF). The flag stays off until
JEH-1179 lands a working backend.

## Imports

- May import from: `@multica/core`, `@multica/ui`, `@multica/views`,
  `@multica/cerebro-feature-flags`.
- May NOT import from: `apps/*`, `next/*`, `react-router-dom`.
