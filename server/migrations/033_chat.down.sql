-- Reverse chat feature migration.

ALTER TABLE agent_task_queue DROP COLUMN IF EXISTS chat_session_id;

-- Restore issue_id NOT NULL (remove any rows with NULL issue_id first).
DELETE FROM agent_task_queue WHERE issue_id IS NULL;
ALTER TABLE agent_task_queue ALTER COLUMN issue_id SET NOT NULL;

DROP TABLE IF EXISTS chat_message;
DROP TABLE IF EXISTS chat_session;
