# FIR-3447 targeted custom issue fields port

This change ports only the upstream custom issue property catalog, typed issue
values, API, CLI, migrations, and server-side list filtering and sorting. It is
not a full upstream sync.

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
- Updated the built-in issue-working skill because the public API and CLI gain
  property operations.

The change deliberately carries no `CEREBRO-PATCH` marker: it is an exact,
documented sync-down of upstream behavior with conflict resolution at the fork
seams. A later full upstream sync should use this provenance to avoid stacking
or reimplementing the feature.
