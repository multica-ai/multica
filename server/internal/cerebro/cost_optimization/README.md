# server/internal/cerebro/cost_optimization

Cerebro-fork cost-optimization backend: storage + endpoints for
`cerebro_cost_optimization` (FIR-2325 phase 2).

**Owns:** GET `/api/workspaces/{id}/cost-optimization` (any member),
PUT/DELETE `/api/workspaces/{id}/cost-optimization/{key}` (admin/owner),
per-workspace saving-mode overrides (off/shadow/on).

**May land here:** Saving-mode handlers, services, registry helpers (server-side
mirror of `packages/cerebro-cost-optimization`).

**May NOT land here:** Per-user feature toggles — those live in
`server/internal/cerebro/feature_flags`. Cost-saving mode is a workspace-level
production decision, not a personal toggle.

**Naming:** snake_case files.

**Upstream-import-niveau:** L4 (cerebro-only).
