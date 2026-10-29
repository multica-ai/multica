-- Lists a workspace's channels and a member's direct conversations.
CREATE INDEX CONCURRENTLY IF NOT EXISTS conversation_workspace_kind_idx
    ON conversation (workspace_id, kind);
