-- Revert multi sandbox config support.

ALTER TABLE agent_runtime DROP COLUMN IF EXISTS sandbox_config_id;

ALTER TABLE workspace_sandbox_config DROP CONSTRAINT IF EXISTS workspace_sandbox_config_ws_name_unique;
ALTER TABLE workspace_sandbox_config DROP CONSTRAINT workspace_sandbox_config_pkey;
ALTER TABLE workspace_sandbox_config DROP COLUMN IF EXISTS name;
ALTER TABLE workspace_sandbox_config DROP COLUMN IF EXISTS id;
ALTER TABLE workspace_sandbox_config ADD PRIMARY KEY (workspace_id);
