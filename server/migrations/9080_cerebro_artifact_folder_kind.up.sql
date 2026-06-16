-- TECH-3637: separate note folders from document folders in the interface.
-- Notes and Documents share the artifact engine, but their folder trees must
-- not be mixed. `kind` scopes a folder to one surface: existing folders were
-- all created by the Documents file-manager, so they backfill to 'document'.
-- Notes create + list folders with kind='note'.

ALTER TABLE artifact_folder
    ADD COLUMN IF NOT EXISTS kind TEXT NOT NULL DEFAULT 'document';

-- The old uniqueness was (workspace_id, parent_id, name). Two surfaces may now
-- legitimately have a folder of the same name at the same level, so kind is
-- part of the key.
ALTER TABLE artifact_folder
    DROP CONSTRAINT IF EXISTS artifact_folder_workspace_id_parent_id_name_key;
-- Postgres has no ADD CONSTRAINT IF NOT EXISTS, so drop the new constraint
-- first to keep this migration idempotent (re-runnable without error).
ALTER TABLE artifact_folder
    DROP CONSTRAINT IF EXISTS artifact_folder_ws_kind_parent_name_key;
ALTER TABLE artifact_folder
    ADD CONSTRAINT artifact_folder_ws_kind_parent_name_key
    UNIQUE (workspace_id, kind, parent_id, name);

CREATE INDEX IF NOT EXISTS idx_artifact_folder_workspace_kind
    ON artifact_folder(workspace_id, kind);
