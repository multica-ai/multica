CREATE UNIQUE INDEX CONCURRENTLY IF NOT EXISTS uq_comment_delivery_obligation_target
    ON comment_delivery_obligation (comment_id, agent_id);
