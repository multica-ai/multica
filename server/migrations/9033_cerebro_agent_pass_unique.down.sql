-- Reverse JEH-1731 uniqueness: restore the non-unique partial index from 9030.

DROP INDEX IF EXISTS idx_cerebro_agent_pass_active_agent_issue;

CREATE INDEX IF NOT EXISTS idx_cerebro_agent_pass_active_agent_issue
    ON cerebro_agent_pass (agent_id, issue_id)
    WHERE status = 'active';
