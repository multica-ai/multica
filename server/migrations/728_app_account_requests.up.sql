-- MUL-8990: one request for every missing account. A member asking the
-- admins for a team account is an app_action_request with no run behind it,
-- so task_id and agent_id become optional; an agent's request still carries
-- both.
ALTER TABLE app_action_request
    ALTER COLUMN task_id DROP NOT NULL,
    ALTER COLUMN agent_id DROP NOT NULL;

-- Personal accounts belong to one workspace. Rows written before this
-- migration have no workspace and keep working in every workspace of their
-- owner; new connections are recorded with the workspace they were made in.
ALTER TABLE user_composio_connection ADD COLUMN IF NOT EXISTS workspace_id UUID;

-- Remote app accounts carry their workspace too, team rows included, so the
-- owner key in 724 can tell one member's accounts in two workspaces apart.
ALTER TABLE remote_app_connection ADD COLUMN IF NOT EXISTS workspace_id UUID;
UPDATE remote_app_connection SET workspace_id = owner_id
WHERE scope = 'team' AND workspace_id IS NULL;
