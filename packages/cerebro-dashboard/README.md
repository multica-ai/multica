# @multica/cerebro-dashboard

Cerebro fork dashboard — workspace-level operations overview (agents, runs,
spend, issues). Lives at `/[workspaceSlug]/dashboard`.

## Owns

- Dashboard page layout, actor tabs, time-range picker.
- KPI cards (active agents, tasks running, spend, issues open).
- Recent tasks list (workspace-wide).
- Sidebar nav-item that links to `/dashboard`.

## May land here

- Charts (Fase 3 — JEH-704).
- Activity feed wiring (Fase 2 — JEH-703).
- Member strip + admin-only spend gating.

## May NOT land here

- Generic agent list / queries — those stay in `@multica/core/agents`.
- Atomic UI primitives — those stay in `@multica/ui`.
- Backend dashboard handler — that lives in `server/internal/cerebro/dashboard/`.

## Imports

- May import from: `@multica/core`, `@multica/ui`, `@multica/views`,
  `@multica/cerebro-feature-flags`.
- May NOT import from: `apps/*`, `next/*`, `react-router-dom`.

## Feature flag

Gated on `cerebro_dashboard` (default: enabled). When the flag is OFF, the
sidebar entry is hidden and the route renders nothing — falling back to
upstream behaviour (no dashboard page).
