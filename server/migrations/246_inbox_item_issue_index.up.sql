CREATE INDEX CONCURRENTLY IF NOT EXISTS idx_inbox_item_issue_id
    ON inbox_item(issue_id);
