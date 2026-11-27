-- Channel Chat route migration.
CREATE UNIQUE INDEX CONCURRENTLY channel_chat_session_binding_installation_id_channel_chat_i_key ON channel_chat_session_binding (installation_id, channel_chat_id);
