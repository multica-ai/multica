CREATE UNIQUE INDEX CONCURRENTLY IF NOT EXISTS external_pull_request_issue_url_idx ON external_pull_request (workspace_id, issue_id, provider, html_url);
