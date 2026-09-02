CREATE UNIQUE INDEX CONCURRENTLY IF NOT EXISTS channel_inbox_delivery_identity_uidx
    ON channel_inbox_delivery (inbox_item_id, channel_type);
