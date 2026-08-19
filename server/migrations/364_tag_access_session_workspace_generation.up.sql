ALTER TABLE tag_access_session
ADD COLUMN session_workspace_generation BIGINT
CHECK (session_workspace_generation > 0);
