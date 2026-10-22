-- Serves retention pruning, which deletes the oldest rows in bounded batches.
CREATE INDEX CONCURRENTLY IF NOT EXISTS idx_search_index_change_changed_at
    ON search_index_change (changed_at);
