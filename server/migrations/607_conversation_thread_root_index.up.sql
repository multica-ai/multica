-- One thread per root message: get-or-create resolves concurrent opens to one row.
CREATE UNIQUE INDEX CONCURRENTLY IF NOT EXISTS conversation_thread_root_uidx
    ON conversation (root_message_id)
    WHERE root_message_id IS NOT NULL;
