CREATE INDEX CONCURRENTLY IF NOT EXISTS idx_issue_to_label_label ON issue_to_label(label_id, issue_id);
