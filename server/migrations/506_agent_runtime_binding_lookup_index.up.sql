CREATE INDEX CONCURRENTLY IF NOT EXISTS idx_agent_runtime_binding_lookup ON agent_runtime_binding (workspace_id, agent_id, priority, id);
