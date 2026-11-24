ALTER TABLE remote_app_connection DROP COLUMN IF EXISTS workspace_id;
ALTER TABLE user_composio_connection DROP COLUMN IF EXISTS workspace_id;
DELETE FROM app_action_request WHERE task_id IS NULL OR agent_id IS NULL;
ALTER TABLE app_action_request
    ALTER COLUMN task_id SET NOT NULL,
    ALTER COLUMN agent_id SET NOT NULL;
