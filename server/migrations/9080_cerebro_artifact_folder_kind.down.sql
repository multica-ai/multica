-- Revert TECH-3637 folder-kind separation.
ALTER TABLE artifact_folder
    DROP CONSTRAINT IF EXISTS artifact_folder_ws_kind_parent_name_key;
DROP INDEX IF EXISTS idx_artifact_folder_workspace_kind;
ALTER TABLE artifact_folder
    ADD CONSTRAINT artifact_folder_workspace_id_parent_id_name_key
    UNIQUE (workspace_id, parent_id, name);
ALTER TABLE artifact_folder
    DROP COLUMN IF EXISTS kind;
