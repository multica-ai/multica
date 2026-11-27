ALTER TABLE agent_workspace_grant
ADD COLUMN can_create_agents BOOLEAN NOT NULL DEFAULT FALSE;
