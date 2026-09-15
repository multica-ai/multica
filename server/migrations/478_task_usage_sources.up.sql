-- Run-level evidence also records attempts with no model/counter row. Empty
-- means unknown (old daemon, uninstrumented provider, or no received report).
ALTER TABLE agent_task_queue ADD COLUMN IF NOT EXISTS usage_sources text[] NOT NULL DEFAULT '{}';
