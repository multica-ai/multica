CREATE INDEX CONCURRENTLY IF NOT EXISTS idx_issue_dependency_depends_on_issue_id
    ON issue_dependency(depends_on_issue_id);
