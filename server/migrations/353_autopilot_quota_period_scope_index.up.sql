-- Natural-key backing index for the primary key attached in migration 359.
CREATE UNIQUE INDEX CONCURRENTLY IF NOT EXISTS uq_autopilot_quota_period_scope
    ON autopilot_quota_period(workspace_id, period_start, period_end);
