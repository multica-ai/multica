-- Lookup / backfill index for issue.status_id. Built CONCURRENTLY because
-- issue is a hot table; kept in its own single-statement migration.
CREATE INDEX CONCURRENTLY IF NOT EXISTS issue_status_id_idx ON issue (status_id);
