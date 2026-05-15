-- Reverse of 9030_cerebro_agent_pass.up.sql
DROP INDEX IF EXISTS idx_cerebro_agent_pass_parent;
DROP INDEX IF EXISTS idx_cerebro_agent_pass_active_workspace;
DROP INDEX IF EXISTS idx_cerebro_agent_pass_active_agent_issue;
DROP TABLE IF EXISTS cerebro_agent_pass;
