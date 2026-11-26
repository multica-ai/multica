-- An issue page reads its comments' findings, and deleting an issue drops them.
CREATE INDEX CONCURRENTLY IF NOT EXISTS idx_secret_finding_issue
    ON secret_finding (issue_id);
