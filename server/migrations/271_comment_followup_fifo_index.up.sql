CREATE INDEX CONCURRENTLY idx_agent_comment_followup_obligation_fifo
    ON agent_comment_followup_obligation (updated_at ASC, id ASC);
