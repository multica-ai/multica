-- Channel Chat route migration.
DROP INDEX CONCURRENTLY IF EXISTS idx_channel_chat_session_binding_active_route;
