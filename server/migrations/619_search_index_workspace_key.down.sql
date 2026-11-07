-- 620 down removed the workspace key and collapsed departure projections.
-- Restore the original conflict target without a blocking index build.
CREATE UNIQUE INDEX CONCURRENTLY IF NOT EXISTS search_index_change_pkey ON search_index_change(entity_type,entity_id);
