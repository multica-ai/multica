-- Runtime GC re-checks that no Agent references a candidate after locking the
-- runtime row, then agent.runtime_id's existing RESTRICT foreign key repeats
-- the same lookup while deleting it. Without a runtime-leading index, each
-- bounded GC batch can scan the full Agent table once per candidate.
--
-- Keep this as the migration's only statement: the runner executes files
-- outside an explicit transaction so PostgreSQL can build the index without
-- blocking Agent writes for the duration of the scan.
CREATE INDEX CONCURRENTLY IF NOT EXISTS idx_agent_runtime_id
    ON agent (runtime_id);
