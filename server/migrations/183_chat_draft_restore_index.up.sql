-- Lookup index for chat_draft_restore.chat_session_id: the creator-authorized
-- read (ListChatDraftRestoresBySession) and the DeleteChatSession / workspace /
-- runtime cascade-path prunes all filter by chat_session_id.
--
-- Single-statement migration: CREATE INDEX CONCURRENTLY cannot run inside a
-- transaction or a multi-command string. Split from the table in migration 182
-- so every production index build runs CONCURRENTLY.
CREATE INDEX CONCURRENTLY IF NOT EXISTS idx_chat_draft_restore_session
    ON chat_draft_restore (chat_session_id);
