-- Drives the rollup worker's "what changed since last tick" scan in
-- 073's window function. CONCURRENTLY avoids blocking writes during
-- build (matches the pattern used in 035/067 for live indexes).
CREATE INDEX CONCURRENTLY IF NOT EXISTS idx_task_usage_updated_at
    ON task_usage (updated_at);
