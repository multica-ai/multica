CREATE UNIQUE INDEX CONCURRENTLY IF NOT EXISTS idx_comment_system_issue_source_task_unique
ON comment (issue_id, source_task_id)
WHERE author_type = 'system' AND source_task_id IS NOT NULL;
