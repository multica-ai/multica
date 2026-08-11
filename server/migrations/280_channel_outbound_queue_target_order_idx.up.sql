-- Supports ClaimChannelOutbound's per-conversation predecessor probe. The
-- installation-wide due-order index remains separate because it serves the
-- outer candidate scan rather than this target-local order check.
CREATE INDEX CONCURRENTLY IF NOT EXISTS idx_channel_outbound_queue_target_order
    ON channel_outbound_queue
        (installation_id, target_chat_type, target_chat_id, created_at, seq)
    WHERE status = 'queued';
