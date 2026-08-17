-- Adds an optional user-facing custom name for a runtime (MUL-4217).
-- custom_name overrides the daemon-proposed `name` for display only; NULL
-- falls back to `name`. Deliberately NOT written by the registration /
-- heartbeat upserts (which do name = EXCLUDED.name on every beat), so a
-- user's custom name is never clobbered by the daemon.
--
-- No index: the column is only ever read as part of a full-row runtime fetch
-- and never appears in a WHERE / ORDER BY, so it needs none. Adding a
-- nullable column with no default is a fast catalog-only change.
ALTER TABLE agent_runtime ADD COLUMN custom_name TEXT;
