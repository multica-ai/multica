-- Rollback phase 1 wakeup changes.
DROP INDEX IF EXISTS idx_cerebro_agent_wakeup_agent_issue;
ALTER TABLE cerebro_agent_wakeup DROP COLUMN IF EXISTS consecutive_postpones;
