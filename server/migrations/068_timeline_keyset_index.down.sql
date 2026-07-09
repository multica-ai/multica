CREATE INDEX IF NOT EXISTS idx_comment_issue
    ON comment (issue_id);

CREATE INDEX IF NOT EXISTS idx_activity_log_issue
    ON activity_log (issue_id);

DROP INDEX IF EXISTS idx_comment_issue_keyset;
DROP INDEX IF EXISTS idx_activity_log_issue_keyset;
