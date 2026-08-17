CREATE UNIQUE INDEX CONCURRENTLY idx_dingtalk_workspace_grant_connector_workspace_unique
    ON dingtalk_workspace_grant(connector_id, workspace_id);
