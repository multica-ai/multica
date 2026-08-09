CREATE UNIQUE INDEX CONCURRENTLY idx_agent_comment_followup_obligation_agent_comment
    ON agent_comment_followup_obligation (agent_id, comment_id);
