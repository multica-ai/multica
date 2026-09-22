-- Uniqueness guard against a runtime appearing twice in one pool.
DROP INDEX CONCURRENTLY IF EXISTS idx_agent_runtime_binding_agent_runtime;
