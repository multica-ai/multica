CREATE UNIQUE INDEX CONCURRENTLY IF NOT EXISTS uq_autopilot_run_active_schedule
    ON autopilot_run (autopilot_id)
    WHERE source = 'schedule'
      AND status IN ('issue_created', 'running');
