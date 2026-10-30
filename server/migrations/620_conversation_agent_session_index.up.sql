-- One context per agent per conversation; the upsert target.
CREATE UNIQUE INDEX CONCURRENTLY IF NOT EXISTS conversation_agent_session_uidx
    ON conversation_agent_session (conversation_id, agent_id);
