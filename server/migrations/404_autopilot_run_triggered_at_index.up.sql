CREATE INDEX CONCURRENTLY IF NOT EXISTS idx_autopilot_run_triggered_at
    ON autopilot_run (triggered_at);
