-- The latest declared environments per machine for an issue.
CREATE INDEX CONCURRENTLY IF NOT EXISTS idx_issue_environment_config_issue
    ON issue_environment_config (issue_id, reported_at);
