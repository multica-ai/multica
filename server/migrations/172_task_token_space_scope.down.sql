DROP INDEX IF EXISTS idx_task_token_workspace_space;
ALTER TABLE task_token DROP CONSTRAINT IF EXISTS task_token_workspace_space_fk;
ALTER TABLE task_token DROP COLUMN IF EXISTS space_id;
