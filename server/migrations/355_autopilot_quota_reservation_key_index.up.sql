CREATE UNIQUE INDEX CONCURRENTLY IF NOT EXISTS uq_autopilot_quota_reservation_key
    ON autopilot_quota_reservation(workspace_id, period_start, period_end, idempotency_key)
    WHERE state <> 'released';
