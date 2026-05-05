-- CEREBRO-PATCH(migration-idempotent-049-work-session): cerebro modification of upstream file
DROP INDEX IF EXISTS idx_task_message_work_session;
ALTER TABLE task_message DROP CONSTRAINT IF EXISTS task_message_one_parent;
ALTER TABLE task_message DROP COLUMN IF EXISTS work_session_id;
ALTER TABLE task_message ALTER COLUMN task_id SET NOT NULL;
DROP TABLE IF EXISTS work_session;
