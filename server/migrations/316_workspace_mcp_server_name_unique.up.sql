-- Names identify a server to an agent's runtime, so they must not collide
-- inside a workspace. Concurrent build per the repo migration rule; kept in
-- its own single-statement file because Postgres rejects CONCURRENTLY inside a
-- transaction or a multi-command string.
CREATE UNIQUE INDEX CONCURRENTLY IF NOT EXISTS idx_workspace_mcp_server_workspace_name
    ON workspace_mcp_server (workspace_id, name);
