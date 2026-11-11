-- "Which agents use this app" on app pages and the disconnect impact check.
CREATE INDEX CONCURRENTLY IF NOT EXISTS idx_agent_app_workspace_toolkit
    ON agent_app (workspace_id, toolkit_slug);
