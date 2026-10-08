DROP TRIGGER IF EXISTS stop_issue_wakeups ON issue;
DROP TRIGGER IF EXISTS capture_comment_wakeup ON comment;
DROP TRIGGER IF EXISTS capture_task_wakeup ON agent_task_queue;
DROP FUNCTION IF EXISTS stop_issue_wakeups();
DROP FUNCTION IF EXISTS capture_comment_wakeup();
DROP FUNCTION IF EXISTS capture_task_wakeup();
DROP FUNCTION IF EXISTS capture_issue_wakeup(uuid,text,text,uuid,uuid,jsonb);
