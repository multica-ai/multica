-- Phase 1: consecutive postpone tracking + wakeup state cleanup.
-- consecutive_postpones counts how many times a wakeup has dispatched
-- without being resolved (cancelled/superseded). After reaching the
-- configured max (default 3), the sweeper sends an inbox notification
-- to the issue owner and resets the counter.

ALTER TABLE cerebro_agent_wakeup
    ADD COLUMN IF NOT EXISTS consecutive_postpones int NOT NULL DEFAULT 0;

-- Index for listing active wakeups per agent+issue — used by the
-- hanging-task cleanup and the postpone notification path.
CREATE INDEX IF NOT EXISTS idx_cerebro_agent_wakeup_agent_issue
    ON cerebro_agent_wakeup (agent_id, issue_id)
    WHERE state = 'pending';
