-- Drop the comment.workspace_id supporting btree index (MUL-4059).
-- The search handler's WHERE clause will still work without it — the
-- planner will just fall back to a Seq Scan on `comment` filtered by
-- workspace_id, which is the pre-migration-135 behavior. Only run this
-- down migration if operating below the query-rewrite change; otherwise
-- expect search latency to regress.

DROP INDEX CONCURRENTLY IF EXISTS idx_comment_workspace;
