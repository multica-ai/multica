CREATE UNIQUE INDEX CONCURRENTLY uq_channel_issue_topic_route ON channel_issue_topic_binding (installation_id, channel_chat_id, topic_root_message_id) WHERE state = 'active';
