-- Counts the directives a boss message has produced (the per-message cap).
CREATE INDEX CONCURRENTLY IF NOT EXISTS idx_issue_directive_origin
    ON issue (workspace_id, origin_id)
    WHERE origin_type = 'directive';
