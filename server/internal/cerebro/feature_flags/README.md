# server/internal/cerebro/feature_flags

Cerebro-fork feature-flag backend: storage + endpoints for `cerebro_feature_flags`.

**Owns:** GET/PUT `/api/cerebro/feature-flags`, default-on resolution, per-workspace + per-user overrides.

**May land here:** Flag handlers, services, registry helpers (server-side mirror of `packages/cerebro-feature-flags`).

**May NOT land here:** Upstream multica feature toggles (none today; if upstream adds them they stay separate).

**Naming:** snake_case files.

**Upstream-import-niveau:** L4 (cerebro-only).
