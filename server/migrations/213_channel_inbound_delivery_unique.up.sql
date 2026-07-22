CREATE UNIQUE INDEX CONCURRENTLY IF NOT EXISTS idx_channel_inbound_delivery_installation_message
    ON channel_inbound_delivery (installation_id, message_id);
