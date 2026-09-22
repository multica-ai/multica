-- Workspace teardown deletes circuits by workspace_id.
CREATE INDEX CONCURRENTLY IF NOT EXISTS idx_runtime_provider_circuit_workspace ON runtime_provider_circuit (workspace_id);
