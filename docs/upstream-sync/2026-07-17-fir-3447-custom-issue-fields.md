# FIR-3447 targeted custom issue fields port

This change ports only the upstream custom issue property catalog, typed issue
values, API, CLI, migrations, and server-side list filtering and sorting. It is
not a full upstream sync.

## Port status

- Schema/API slice: migrations 191–196, handlers, CLI, routes, generated DB
  code, and the public issue-property contract are present.
- Core client/cache/realtime slice: defensive API schemas, workspace-scoped
  query keys, optimistic single-property writes, single-key rollback,
  property-filter/sort persistence, and websocket reconciliation are present.
- Issue detail and manual Create issue expose the active property catalog with
  typed editors.
- Settings > Properties now exposes the upstream catalog UI, including
  admin-only definition changes, archive/restore, icons, and option colors.
- Board cards and List rows now render the custom fields selected in Display.
- Remaining UI slices are intentionally separate: property grouping, the
  configurable Table, Create issue field visibility/Quick create, and final
  filter/sort polish.

## Upstream provenance

- `b85bb71a582ee0a106290fcda8346599f2a383b9`: custom issue properties.
- `f46d5d7ba55fac0c42d77a6c9601a31c5de1524a`: safe workspace deletion.
- `07e2d378bd6d5e6cb3aacff50a322090c01ad952`: property colors.
- `101f21d55dd5be7e73f65645c212eb20aa7d26a3`: select labels.
- `ea8511340e6949436c04edea00dabeeebba34233`: property icons.
- `002ea0d87949d112d96586bd8b42c779142cf77d`: configurable Table, to be
  ported in a later isolated slice.

## Cerebro conflict decisions

- Kept Cerebro issue fields, privacy, project access, reference filters, sprint
  filters, metadata filters, and custom status behavior.
- Added the property value bag plus server-side property filters and sorting
  without restoring unrelated upstream issue fields.
- Kept Cerebro work-session routes while adding only the property catalog and
  issue-value routes.
- Kept Cerebro workspace deletion behavior and added only property cleanup.
- Adapted the upstream CLI to Cerebro's existing request context convention.
- Kept Cerebro's workspace-scoped query keys, saved/date/sprint/reference
  filters, sub-issue display state, and external realtime handler registration
  while adding property filters, sorting, cache reconciliation, and reconnect
  invalidation.
- Added Properties to Cerebro's existing desktop/mobile Settings navigation
  without replacing its Agent Profile, Groups, Accounts, Status models,
  Auth & Permissions, Model registry, or Documentation tabs.
- Did not apply upstream's generic issue-mutation reconciliation hunk because
  this fork's update mutation does not write server responses back into the
  cache; property writes keep their own authoritative single-key pipeline.
- Updated the built-in issue-working skill because the public API and CLI gain
  property operations.

The change deliberately carries no `CEREBRO-PATCH` marker: it is an exact,
documented sync-down of upstream behavior with conflict resolution at the fork
seams. A later full upstream sync should use this provenance to avoid stacking
or reimplementing the feature.

## Current-revision QA list derived from changed UI files

- Settings navigation: desktop sidebar and responsive selector with Properties.
- Settings > Properties: empty state, admin create/edit/archive flow, member
  read-only state, option colors, and property icons.
- Board: Display-selected number/select/date values on cards.
- List: Display-selected values on rows at wide and responsive widths.

The current conflict self-review preserves Cerebro's complete Settings tab set
and reuses the existing workspace-scoped catalog/query pipeline. Fresh browser
evidence is still required for the revision that completes Table and grouping;
prior Issue detail/Create issue screenshots are not reused for that revision.
