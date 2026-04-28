DROP INDEX IF EXISTS idx_artifact_folder;
ALTER TABLE artifact DROP COLUMN IF EXISTS file_size_bytes;
ALTER TABLE artifact DROP COLUMN IF EXISTS file_url;
ALTER TABLE artifact DROP COLUMN IF EXISTS format;
ALTER TABLE artifact DROP COLUMN IF EXISTS folder_id;

DROP INDEX IF EXISTS idx_artifact_folder_parent;
DROP INDEX IF EXISTS idx_artifact_folder_workspace;
DROP TABLE IF EXISTS artifact_folder;
