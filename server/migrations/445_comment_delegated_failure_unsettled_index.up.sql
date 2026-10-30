CREATE INDEX CONCURRENTLY IF NOT EXISTS idx_comment_delegated_failure_unsettled
ON comment (created_at, id)
WHERE author_type = 'system'
  AND type = 'progress_update'
  AND source_task_id IS NOT NULL
  AND recovery_settled_at IS NULL;
