# server/migrations/cerebro

Reserved for cerebro-fork migrations. File names use `9NNN_*.sql` prefix (NNN starting at 001). Upstream multica uses NNN sequential — currently at 067. The 9000 gap protects against namespace collision.

As of 2026-05-05, 20 cerebro 9NNN files still live in `server/migrations/` and will be moved here as part of chunk 3.

**Owns:** golang-migrate runtime migrations for cerebro-only tables.

**May land here:** `9NNN_<feature>.up.sql` and `9NNN_<feature>.down.sql` pairs.

**May NOT land here:** Upstream migrations (they keep the bare `NNN_*.sql` form in `server/migrations/`).

**Naming:** `9NNN_<snake_case_feature>.{up,down}.sql`. NNN is monotonic per cerebro stream.

**Upstream-import-niveau:** L4 (cerebro-only).

> **The migrate runner does NOT read this directory yet.** `migrations.Files`
> globs `server/migrations/*.{up,down}.sql` with no recursion, so anything
> placed here is silently never applied. `9085_cerebro_agent_capability_scan_history`
> lived here from its introduction (d291bb772) and never ran in production: the
> nightly capability scan failed with `relation "cerebro_agent_capability_scan"
> does not exist` 42-47 times a night until it was moved back on 26 July 2026.
>
> Until the multi-dir scanner lands (`docs/upstream-sync/preflight/P4.md`), put
> cerebro migrations in `server/migrations/` alongside the upstream ones — the
> `9NNN_` prefix is what keeps the namespaces apart, not the directory.
> `TestNoMigrationsOutsideScannedDirectory` fails the build if a migration is
> added here again.
