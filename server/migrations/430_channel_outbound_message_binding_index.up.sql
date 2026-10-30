-- Channel Chat route migration.
CREATE INDEX CONCURRENTLY idx_channel_outbound_message_binding_route ON channel_outbound_message (binding_id, route_revision);
