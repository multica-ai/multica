-- Channel Chat route migration.
DROP INDEX CONCURRENTLY IF EXISTS idx_channel_task_delivery_binding;
