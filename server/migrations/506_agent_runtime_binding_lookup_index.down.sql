-- Resolution-order read path; rebuilt by re-applying the migration.
DROP INDEX CONCURRENTLY IF EXISTS idx_agent_runtime_binding_lookup;
