-- Serves the rate gate's sliding-window COUNT, which runs twice (minute and
-- hour) before every single send. The column order matches that predicate
-- exactly: equality on the target, then a range scan on attempted_at.
CREATE INDEX CONCURRENTLY IF NOT EXISTS idx_channel_outbound_send_attempt_window
    ON channel_outbound_send_attempt (
        installation_id,
        target_chat_type,
        target_chat_id,
        attempted_at
    );
