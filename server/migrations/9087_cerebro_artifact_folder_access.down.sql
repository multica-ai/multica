-- Reverse FIR-1590 folder-level access control.
DROP FUNCTION IF EXISTS cerebro_artifact_folder_visible(uuid, uuid);
DROP TABLE IF EXISTS cerebro_artifact_folder_share;
DROP INDEX IF EXISTS cerebro_artifact_folder_owner_idx;
ALTER TABLE artifact_folder DROP COLUMN IF EXISTS visibility;
ALTER TABLE artifact_folder DROP COLUMN IF EXISTS owner_id;
