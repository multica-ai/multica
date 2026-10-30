-- Deleting a runtime clears chat-session resume pointers through the runtime_id
-- foreign key. Equality to the deleted non-NULL runtime implies this predicate,
-- so the partial index serves the FK action without indexing unbound sessions.
CREATE INDEX CONCURRENTLY IF NOT EXISTS idx_chat_session_runtime_id
    ON chat_session (runtime_id)
    WHERE runtime_id IS NOT NULL;
