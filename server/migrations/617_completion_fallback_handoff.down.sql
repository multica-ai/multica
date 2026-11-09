SET LOCAL lock_timeout = '2s';
SET LOCAL statement_timeout = '10s';

ALTER TABLE agent_task_queue DROP COLUMN IF EXISTS completion_fallback_state;
ALTER TABLE agent_task_queue DROP COLUMN IF EXISTS completion_fallback_comment_id;
