CREATE UNIQUE INDEX CONCURRENTLY uq_channel_issue_topic_root ON channel_issue_topic_binding (project_binding_id, topic_root_message_id) WHERE state = 'active';
