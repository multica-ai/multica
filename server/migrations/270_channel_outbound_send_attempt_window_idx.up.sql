CREATE INDEX CONCURRENTLY IF NOT EXISTS idx_channel_outbound_send_attempt_window
    ON channel_outbound_send_attempt (
        installation_id,
        target_chat_type,
        target_chat_id,
        attempted_at
    );
