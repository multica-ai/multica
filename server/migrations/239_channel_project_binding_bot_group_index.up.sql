CREATE UNIQUE INDEX CONCURRENTLY uq_channel_project_binding_bot_group ON channel_project_binding (installation_id, channel_chat_id) WHERE state = 'active';
