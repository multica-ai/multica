CREATE INDEX CONCURRENTLY idx_dingtalk_connector_lease
    ON dingtalk_connector(ws_lease_expires_at)
    WHERE status = 'active';
