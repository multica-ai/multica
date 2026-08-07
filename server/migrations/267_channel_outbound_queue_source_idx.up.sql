-- Enforces enqueue idempotency on the business key. Both the realtime
-- producer and the reconciler try to enqueue the same reply; this index is
-- what turns the loser into an ON CONFLICT DO NOTHING no-op instead of a
-- duplicate message delivered to the user.
--
-- NOT partial by status: a 'sent' row must keep suppressing a re-enqueue of
-- the same business result, otherwise the reconciler would resend every
-- reply whose queue row had already been delivered.
CREATE UNIQUE INDEX CONCURRENTLY IF NOT EXISTS idx_channel_outbound_queue_source
    ON channel_outbound_queue (installation_id, source_kind, source_id);
