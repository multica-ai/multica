-- Add timestamp columns to issue_label so labels track their own lifecycle.
-- The table was scaffolded in 001_init.up.sql but never wired up to any code
-- path; timestamps are added here as a precondition for the new CRUD handlers,
-- CLI, and UI (see #1191).

ALTER TABLE issue_label
    ADD COLUMN IF NOT EXISTS created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    ADD COLUMN IF NOT EXISTS updated_at TIMESTAMPTZ NOT NULL DEFAULT now();

-- Dedupe case-insensitive collisions before the unique index is created.
-- Self-hosted deployments may have `Bug` + `bug` pairs from manual poking at
-- the table, which would otherwise abort this migration when the unique index
-- is added below. Keep the oldest row intact (smallest id, by lexicographic
-- UUID order — effectively earliest-inserted since ids are gen_random_uuid()),
-- rename every later duplicate by appending a short UUID suffix. Visible only
-- on the rare installs that had duplicates; a no-op otherwise.
UPDATE issue_label AS il
SET name = il.name || ' (' || substring(il.id::text, 1, 8) || ')'
WHERE EXISTS (
    SELECT 1 FROM issue_label il2
    WHERE il2.workspace_id = il.workspace_id
      AND LOWER(il2.name) = LOWER(il.name)
      AND il2.id < il.id
);

-- Workspace-scoped uniqueness on label name. Case-insensitive to avoid
-- "Bug" / "bug" drift that would confuse users in the picker UI.
CREATE UNIQUE INDEX IF NOT EXISTS issue_label_workspace_name_lower_idx
    ON issue_label (workspace_id, LOWER(name));
