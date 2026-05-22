-- agent_runtime.local_paths stores daemon-reported local directory paths.
-- When a project has a local_path resource whose daemon_id matches a runtime's
-- daemon_id, only that runtime can claim tasks for that project.
-- NULL/empty means no local paths declared (backward compatible with older daemons).
ALTER TABLE agent_runtime ADD COLUMN local_paths JSONB DEFAULT NULL;

CREATE INDEX idx_agent_runtime_local_paths ON agent_runtime USING GIN (local_paths);