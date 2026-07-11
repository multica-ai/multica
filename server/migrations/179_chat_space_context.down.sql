ALTER TABLE task_token DROP COLUMN IF EXISTS space_ids;

DROP INDEX IF EXISTS idx_chat_session_workspace_space;
ALTER TABLE chat_session DROP CONSTRAINT IF EXISTS chat_session_workspace_space_fk;
ALTER TABLE chat_session DROP COLUMN IF EXISTS space_id;
