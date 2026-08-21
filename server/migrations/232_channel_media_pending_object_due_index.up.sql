-- The reconciler claims one row per statement, ordered by next_attempt_at
-- across all three states. Migration 230's index leads with state, so it
-- cannot serve that ordering: under backlog the planner falls back to a seq
-- scan plus an external merge sort, and FOR UPDATE blocks the bounded top-N
-- optimization, so every claim in a sweep pays the full sort. Ordering on
-- next_attempt_at alone lets the claim walk the index and stop at the first
-- due row. Same CONCURRENTLY-in-its-own-file convention as migration 230.
CREATE INDEX CONCURRENTLY IF NOT EXISTS idx_channel_media_pending_object_due
    ON channel_media_pending_object (next_attempt_at);
