-- Channel Chat route migration.
CREATE INDEX CONCURRENTLY idx_channel_task_delivery_installation ON channel_task_delivery (installation_id);
