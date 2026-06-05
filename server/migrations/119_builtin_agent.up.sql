-- 119_builtin_agent.up.sql
-- Built-in agents: global (cross-workspace) agents manageable only by users
-- with the can_manage_workflows permission.

ALTER TABLE multica_agent ADD COLUMN IF NOT EXISTS is_builtin BOOLEAN NOT NULL DEFAULT FALSE;
ALTER TABLE multica_agent ALTER COLUMN workspace_id DROP NOT NULL;
ALTER TABLE multica_agent ALTER COLUMN runtime_id DROP NOT NULL;

CREATE INDEX IF NOT EXISTS idx_agent_is_builtin ON multica_agent(is_builtin)
  WHERE is_builtin = TRUE AND archived_at IS NULL;
