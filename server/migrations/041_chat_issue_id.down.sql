DROP INDEX IF EXISTS idx_chat_session_issue;
ALTER TABLE chat_session DROP COLUMN IF EXISTS issue_id;
