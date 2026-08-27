-- Promote the concurrently-built unique index without rebuilding it or
-- holding a long index-build lock on channel_task_delivery.
ALTER TABLE channel_task_delivery
    ADD CONSTRAINT channel_task_delivery_pkey
    PRIMARY KEY USING INDEX channel_task_delivery_pkey;
