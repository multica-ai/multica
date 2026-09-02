-- Channel Chat route migration.
CREATE UNIQUE INDEX CONCURRENTLY channel_task_delivery_pkey ON channel_task_delivery (task_id);
