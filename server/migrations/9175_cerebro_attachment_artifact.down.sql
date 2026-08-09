DROP INDEX IF EXISTS idx_attachment_artifact_id;
ALTER TABLE attachment DROP COLUMN IF EXISTS artifact_id;
