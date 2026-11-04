-- The brief's People section reads the reviews published in its window.
CREATE INDEX CONCURRENTLY IF NOT EXISTS review_workspace_created_idx ON review (workspace_id, created_at DESC);
