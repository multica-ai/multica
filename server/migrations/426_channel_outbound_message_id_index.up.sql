-- Channel Chat route migration.
CREATE UNIQUE INDEX CONCURRENTLY idx_channel_outbound_message_id ON channel_outbound_message (installation_id, channel_message_id);
