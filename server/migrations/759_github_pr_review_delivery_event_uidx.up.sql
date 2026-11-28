CREATE UNIQUE INDEX CONCURRENTLY IF NOT EXISTS uq_github_pr_review_delivery_event ON github_pr_review_delivery (issue_id, event_type, github_event_id);
