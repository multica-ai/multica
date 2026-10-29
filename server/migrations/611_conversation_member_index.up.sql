-- One membership row per member of a conversation; the upsert target.
CREATE UNIQUE INDEX CONCURRENTLY IF NOT EXISTS conversation_member_uidx
    ON conversation_member (conversation_id, member_type, member_id);
