# server/internal/cerebro/db/migrations

Cerebro-only schema input for sqlc (NOT for golang-migrate runtime migrations — those live in `server/migrations/cerebro/`).

**Owns:** sqlc schema files describing cerebro tables so sqlc can generate type-safe Go.

**May land here:** Schema-only `*.sql` files mirroring the cerebro migrations.

**May NOT land here:** Runtime migration files (those go in `server/migrations/cerebro/`).

**Naming:** matches migration filenames without `.up`/`.down` suffix.

**Upstream-import-niveau:** L4 (cerebro-only).

Currently empty — populated starting in chunk 3.
