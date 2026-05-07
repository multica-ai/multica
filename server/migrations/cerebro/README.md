# server/migrations/cerebro

Reserved for cerebro-fork migrations. File names use `9NNN_*.sql` prefix (NNN starting at 001). Upstream multica uses NNN sequential — currently at 067. The 9000 gap protects against namespace collision.

As of 2026-05-05, 20 cerebro 9NNN files still live in `server/migrations/` and will be moved here as part of chunk 3.

**Owns:** golang-migrate runtime migrations for cerebro-only tables.

**May land here:** `9NNN_<feature>.up.sql` and `9NNN_<feature>.down.sql` pairs.

**May NOT land here:** Upstream migrations (they keep the bare `NNN_*.sql` form in `server/migrations/`).

**Naming:** `9NNN_<snake_case_feature>.{up,down}.sql`. NNN is monotonic per cerebro stream.

**Upstream-import-niveau:** L4 (cerebro-only). The migrate runner picks up both directories.
