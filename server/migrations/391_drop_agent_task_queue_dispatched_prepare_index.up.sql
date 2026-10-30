-- Migration 390 replaces this index with the same partial predicate and a key
-- order that makes dispatched_at usable as a stale-reclaim range condition.
DROP INDEX CONCURRENTLY IF EXISTS idx_agent_task_queue_dispatched_prepare;
