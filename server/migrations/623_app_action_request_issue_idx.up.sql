-- Requests shown on an issue's timeline.
CREATE INDEX CONCURRENTLY IF NOT EXISTS idx_app_action_request_issue
    ON app_action_request (issue_id, created_at)
    WHERE issue_id IS NOT NULL;
