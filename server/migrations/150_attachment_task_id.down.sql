DROP INDEX IF EXISTS idx_attachment_task;
ALTER TABLE attachment
  DROP COLUMN task_id;
