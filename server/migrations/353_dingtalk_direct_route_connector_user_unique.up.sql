CREATE UNIQUE INDEX CONCURRENTLY idx_dingtalk_direct_route_connector_user_unique
    ON dingtalk_direct_route(connector_id, channel_user_id);
