CREATE UNIQUE INDEX CONCURRENTLY IF NOT EXISTS idx_agent_runtime_binding_agent_runtime ON agent_runtime_binding (agent_id, runtime_id);
