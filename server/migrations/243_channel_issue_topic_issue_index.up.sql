CREATE UNIQUE INDEX CONCURRENTLY uq_channel_issue_topic_issue ON channel_issue_topic_binding (issue_id) WHERE state = 'active';
