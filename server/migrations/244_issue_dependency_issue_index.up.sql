CREATE INDEX CONCURRENTLY IF NOT EXISTS idx_issue_dependency_issue_id
    ON issue_dependency(issue_id);
