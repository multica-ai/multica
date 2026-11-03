-- The expiry sweep and a member's pending list only ever read pending rows.
CREATE INDEX CONCURRENTLY IF NOT EXISTS idx_app_action_request_pending
    ON app_action_request (expires_at)
    WHERE status = 'pending';
