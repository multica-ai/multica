DROP INDEX IF EXISTS idx_agent_runtime_local_paths;
ALTER TABLE agent_runtime DROP COLUMN IF EXISTS local_paths;