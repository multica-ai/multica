DROP TRIGGER IF EXISTS trg_channel_task_notification ON agent_task_queue;
DROP FUNCTION IF EXISTS enqueue_channel_task_notification();
DROP TRIGGER IF EXISTS trg_channel_issue_updated_notification ON issue;
DROP TRIGGER IF EXISTS trg_channel_issue_created_notification ON issue;
DROP FUNCTION IF EXISTS enqueue_channel_issue_notification();
DROP TABLE IF EXISTS channel_notification_outbox;
DROP TABLE IF EXISTS channel_issue_topic_binding;
DROP TABLE IF EXISTS channel_project_binding;
