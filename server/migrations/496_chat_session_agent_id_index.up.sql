-- Runtime teardown finds and locks every chat session owned by a system agent
-- before deleting that agent. The same column backs the ON DELETE CASCADE
-- foreign key, so an unindexed lookup extends the teardown transaction and its
-- chat-session lock window with a full-table scan.
CREATE INDEX CONCURRENTLY IF NOT EXISTS idx_chat_session_agent_id
    ON chat_session (agent_id);
