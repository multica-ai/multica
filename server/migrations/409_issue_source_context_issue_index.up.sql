CREATE UNIQUE INDEX CONCURRENTLY idx_issue_source_context_issue ON issue_source_context (issue_id) WHERE state = 'attached' AND issue_id IS NOT NULL;
