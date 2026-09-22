-- Uniqueness guard that keeps pool ordering total and deterministic.
DROP INDEX CONCURRENTLY IF EXISTS idx_agent_runtime_binding_agent_priority;
