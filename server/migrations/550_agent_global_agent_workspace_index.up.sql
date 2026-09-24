CREATE UNIQUE INDEX CONCURRENTLY IF NOT EXISTS agent_global_agent_workspace_uidx
    ON agent (global_agent_id, workspace_id)
    WHERE global_agent_id IS NOT NULL;
