-- 117_user_workflow_admin.up.sql
-- Add global workflow admin permission to user table.
ALTER TABLE multica_user ADD COLUMN IF NOT EXISTS can_manage_workflows BOOLEAN NOT NULL DEFAULT FALSE;
