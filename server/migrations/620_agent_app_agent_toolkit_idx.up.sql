-- One entry per app per agent; the upsert key for the agent's app list.
CREATE UNIQUE INDEX CONCURRENTLY IF NOT EXISTS idx_agent_app_agent_toolkit
    ON agent_app (agent_id, toolkit_slug);
