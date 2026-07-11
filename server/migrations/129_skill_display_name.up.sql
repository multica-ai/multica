-- Add display_name: a UTF-8, human-readable label (e.g. Chinese) shown in the
-- UI. The existing `name` stays the ASCII-slug identity / filesystem dir name /
-- runtime match key; display_name is optional, non-unique, and falls back to
-- name when empty. See SKILL display/display split design.
-- IF NOT EXISTS: this migration was renumbered from 127 -> 129 to resolve a
-- collision with upstream's 127_task_squad_id. DBs that already applied it
-- under the old number still have the column, so make it idempotent.
ALTER TABLE skill ADD COLUMN IF NOT EXISTS display_name TEXT NOT NULL DEFAULT '';
