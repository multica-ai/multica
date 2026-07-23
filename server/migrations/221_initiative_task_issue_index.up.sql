CREATE INDEX CONCURRENTLY IF NOT EXISTS idx_initiative_task_issue ON initiative_task (issue_id) WHERE issue_id IS NOT NULL;
