-- Channel Chat route migration.
CREATE UNIQUE INDEX CONCURRENTLY idx_channel_chat_session_binding_active_route ON channel_chat_session_binding (installation_id, channel_chat_id) WHERE retired_at IS NULL;
