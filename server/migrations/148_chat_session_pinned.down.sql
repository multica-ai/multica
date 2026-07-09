DROP INDEX IF EXISTS idx_chat_session_pinned;
ALTER TABLE chat_session DROP COLUMN IF EXISTS pinned_at;
