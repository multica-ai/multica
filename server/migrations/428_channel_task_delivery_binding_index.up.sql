-- Channel Chat route migration.
CREATE INDEX CONCURRENTLY idx_channel_task_delivery_binding ON channel_task_delivery (binding_id);
