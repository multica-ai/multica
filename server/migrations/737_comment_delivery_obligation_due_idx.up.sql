CREATE INDEX CONCURRENTLY IF NOT EXISTS idx_comment_delivery_obligation_due
    ON comment_delivery_obligation (next_attempt_at, comment_id, agent_id);
