-- Support multiple sandbox configs per workspace.
-- Previously workspace_sandbox_config was keyed by workspace_id (1:1).
-- Now each config gets its own id and a user-chosen name.

-- 1. Add id + name columns
ALTER TABLE workspace_sandbox_config DROP CONSTRAINT workspace_sandbox_config_pkey;
ALTER TABLE workspace_sandbox_config
  ADD COLUMN id UUID NOT NULL DEFAULT gen_random_uuid(),
  ADD COLUMN name TEXT NOT NULL DEFAULT 'default';
ALTER TABLE workspace_sandbox_config ADD PRIMARY KEY (id);
ALTER TABLE workspace_sandbox_config
  ADD CONSTRAINT workspace_sandbox_config_ws_name_unique UNIQUE (workspace_id, name);

-- 2. Link agent_runtime → sandbox config
ALTER TABLE agent_runtime
  ADD COLUMN sandbox_config_id UUID REFERENCES workspace_sandbox_config(id) ON DELETE SET NULL;

-- 3. Backfill existing cloud runtimes
UPDATE agent_runtime ar
SET sandbox_config_id = wsc.id
FROM workspace_sandbox_config wsc
WHERE ar.workspace_id = wsc.workspace_id AND ar.runtime_mode = 'cloud';
