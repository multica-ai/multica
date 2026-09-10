-- Channel Chat route migration.
DROP INDEX CONCURRENTLY IF EXISTS idx_channel_outbound_message_id;
