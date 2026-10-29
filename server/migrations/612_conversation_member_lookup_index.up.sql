-- Finds the conversations a member or agent is in.
CREATE INDEX CONCURRENTLY IF NOT EXISTS conversation_member_lookup_idx
    ON conversation_member (workspace_id, member_type, member_id);
