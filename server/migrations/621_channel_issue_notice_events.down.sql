DROP TRIGGER IF EXISTS capture_channel_issue_failure_notice ON agent_task_queue;
DROP TRIGGER IF EXISTS capture_channel_issue_status_notice ON issue;
DROP FUNCTION IF EXISTS capture_channel_issue_failure_notice();
DROP FUNCTION IF EXISTS capture_channel_issue_status_notice();
DROP FUNCTION IF EXISTS queue_channel_issue_notice(UUID, TEXT, UUID);
