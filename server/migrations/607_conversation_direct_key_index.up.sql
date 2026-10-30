-- One direct conversation per pair of members.
CREATE UNIQUE INDEX CONCURRENTLY IF NOT EXISTS conversation_direct_key_uidx
    ON conversation (workspace_id, direct_key)
    WHERE direct_key IS NOT NULL;
