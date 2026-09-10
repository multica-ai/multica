CREATE UNIQUE INDEX CONCURRENTLY idx_issue_source_context_origin_task ON issue_source_context (origin_task_id) WHERE state = 'pending' AND origin_task_id IS NOT NULL;
