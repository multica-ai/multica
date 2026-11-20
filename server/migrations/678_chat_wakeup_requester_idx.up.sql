-- Serves the per-person active limit and workspace teardown.
CREATE INDEX CONCURRENTLY IF NOT EXISTS idx_chat_wakeup_requester
    ON chat_wakeup (workspace_id, requester_id, created_at DESC);
