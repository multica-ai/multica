-- The Triage queue reads exactly the rows this index holds, and every other
-- issue surface reads none of them. A partial index keeps it the size of the
-- queue rather than the size of the issue table.
CREATE INDEX CONCURRENTLY IF NOT EXISTS idx_issue_triage_state
ON issue (workspace_id, created_at ASC)
WHERE triage_state IS NOT NULL;
