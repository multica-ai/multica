-- Reverse lookup: is-this-task-a-fallback (loop prevention) and supersede-by-
-- fallback. Partial: only active-mode rows ever set fallback_task_id.
CREATE INDEX CONCURRENTLY IF NOT EXISTS provider_failover_fallback_task_idx ON provider_failover_handoff (fallback_task_id) WHERE fallback_task_id IS NOT NULL;
