-- 115_user_workflow_admin.down.sql
ALTER TABLE "user" DROP COLUMN IF EXISTS can_manage_workflows;
