# server/internal/cerebro/queries

Cerebro-only sqlc query input. Each `.sql` file holds named queries grouped by feature
(e.g. `feature_flags.sql`, `artifacts.sql`).

**Owns:** sqlc-format SQL queries that target cerebro-only tables (and may also read upstream tables for cerebro projections).

**May land here:** `*.sql` files with `-- name: ...` sqlc directives.

**May NOT land here:** Migrations (those live in `server/migrations/cerebro/`). Hand-written Go.

**Naming:** one file per feature, snake_case.

**Upstream-import-niveau:** L4 (cerebro-only). sqlc generates code into `../db/generated/`.

Currently empty — populated starting in chunk 3.
