DROP INDEX IF EXISTS idx_artifact_requester;
ALTER TABLE artifact DROP COLUMN IF EXISTS requester_user_id;
