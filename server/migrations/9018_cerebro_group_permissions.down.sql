DROP TABLE IF EXISTS cerebro_workspace_default_group;

DROP INDEX IF EXISTS idx_cerebro_project_group_member_group;
DROP TABLE IF EXISTS cerebro_project_group_member;

DROP INDEX IF EXISTS idx_cerebro_group_agent_access_agent;
DROP TABLE IF EXISTS cerebro_group_agent_access;

DROP INDEX IF EXISTS idx_cerebro_group_runtime_access_runtime;
DROP TABLE IF EXISTS cerebro_group_runtime_access;

DROP INDEX IF EXISTS idx_cerebro_group_capability_capability;
DROP TABLE IF EXISTS cerebro_group_capability;
