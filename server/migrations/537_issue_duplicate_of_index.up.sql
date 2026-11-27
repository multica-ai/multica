-- Lists an issue's duplicates and finds them when it is deleted. Only
-- duplicates carry the pointer, so a partial index stays the size of that set.
CREATE INDEX CONCURRENTLY IF NOT EXISTS idx_issue_duplicate_of
ON issue (duplicate_of_issue_id)
WHERE duplicate_of_issue_id IS NOT NULL;
