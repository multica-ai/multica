ALTER TABLE agent_runtime DROP COLUMN IF EXISTS failover_group_id;
ALTER TABLE agent_runtime DROP COLUMN IF EXISTS priority;
DROP TABLE IF EXISTS runtime_failover_group;
