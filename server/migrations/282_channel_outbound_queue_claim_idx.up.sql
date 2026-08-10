-- Drives ClaimChannelOutbound's candidate scan: one installation's due rows
-- in (next_attempt_at, created_at, seq) order. Partial on status='queued'
-- because 'sent' and 'failed' rows accumulate until the retention sweep runs
-- and must not bloat the index the hot claim path walks.
--
-- seq is in the index so the claim's total order comes from the scan rather
-- than a sort on top of it.
CREATE INDEX CONCURRENTLY IF NOT EXISTS idx_channel_outbound_queue_claim
    ON channel_outbound_queue (installation_id, next_attempt_at, created_at, seq)
    WHERE status = 'queued';
