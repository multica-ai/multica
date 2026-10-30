-- One thread per root message: get-or-create resolves concurrent opens to the
-- same row.
CREATE UNIQUE INDEX CONCURRENTLY IF NOT EXISTS chat_session_thread_root_uidx
    ON chat_session (root_chat_message_id)
    WHERE root_chat_message_id IS NOT NULL;
